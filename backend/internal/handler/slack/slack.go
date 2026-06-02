// Package slack provides HTTP handlers for Slack's Events API and
// Block Kit Interactive Components endpoints.
package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	slackctrl "github.com/agentrq/agentrq/backend/internal/controller/slack"
	slacksvc "github.com/agentrq/agentrq/backend/internal/service/slack"
	zlog "github.com/rs/zerolog/log"
)

// Params holds the dependencies for the Slack HTTP handler.
type Params struct {
	SlackCtrl slackctrl.Controller
	SlackSvc  slacksvc.Service
	BaseURL   string
	Mux       *http.ServeMux
}

// New registers the Slack webhook routes on the provided mux.
//
//	POST /slack/events       — Slack Events API (app_mention, url_verification)
//	POST /slack/interactions — Slack Block Kit Interactive Components (button clicks)
//	GET  /slack/oauth/callback — Slack OAuth v2 callback redirect
//	POST /slack/commands     — Slack Slash Commands (e.g. /t)
func New(p Params) {
	p.Mux.Handle("/slack/events", eventsHandler(p.SlackSvc, p.SlackCtrl))
	p.Mux.Handle("/slack/interactions", interactionsHandler(p.SlackSvc, p.SlackCtrl))
	p.Mux.Handle("/slack/oauth/callback", oauthCallbackHandler(p.SlackSvc, p.SlackCtrl, p.BaseURL))
	p.Mux.Handle("/slack/commands", commandHandler(p.SlackSvc, p.SlackCtrl))
}

// eventsHandler handles Slack Events API payloads.
func eventsHandler(svc slacksvc.Service, ctrl slackctrl.Controller) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}

		if err := svc.VerifyRequest(r, body); err != nil {
			zlog.Warn().Err(err).Msg("[slack/events] signature verification failed")
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		var payload slackctrl.SlackEventPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}

		// Respond to Slack's url_verification challenge immediately
		if payload.Type == "url_verification" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"challenge": payload.Challenge})
			return
		}

		// ACK immediately — Slack requires a response within 3 seconds
		w.WriteHeader(http.StatusOK)

		// Process in the background
		go func() {
			if err := ctrl.HandleSlackEvent(context.Background(), payload); err != nil {
				zlog.Error().Err(err).Msg("[slack/events] HandleSlackEvent error")
			}
		}()
	})
}

// interactionsHandler handles Slack Block Kit interactive component payloads.
// Slack sends a form-encoded `payload` field containing the JSON interaction data.
func interactionsHandler(svc slacksvc.Service, ctrl slackctrl.Controller) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}

		if err := svc.VerifyRequest(r, body); err != nil {
			zlog.Warn().Err(err).Msg("[slack/interactions] signature verification failed")
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		// Reconstruct request body for form parsing
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		if err := r.ParseForm(); err != nil {
			http.Error(w, "failed to parse form", http.StatusBadRequest)
			return
		}
		payloadStr := r.FormValue("payload")
		if payloadStr == "" {
			// Try body directly (some Slack apps send JSON)
			payloadStr = string(body)
		}

		var interaction struct {
			Type string `json:"type"`
			User struct {
				ID       string `json:"id"`
				Username string `json:"username"`
			} `json:"user"`
			Channel struct {
				ID string `json:"id"`
			} `json:"channel"`
			Message struct {
				Ts string `json:"ts"`
			} `json:"message"`
			Actions []struct {
				ActionID string `json:"action_id"`
				Value    string `json:"value"`
			} `json:"actions"`
		}

		if err := json.Unmarshal([]byte(payloadStr), &interaction); err != nil {
			http.Error(w, "invalid interaction payload", http.StatusBadRequest)
			return
		}

		if interaction.Type != "block_actions" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// ACK immediately
		w.WriteHeader(http.StatusOK)

		for _, act := range interaction.Actions {
			action := slackctrl.SlackBlockAction{
				ActionID:  act.ActionID,
				ChannelID: interaction.Channel.ID,
				MessageTS: interaction.Message.Ts,
				UserID:    interaction.User.ID,
				UserName:  interaction.User.Username,
			}

			go func(a slackctrl.SlackBlockAction) {
				var handlerErr error
				switch {
				case strings.HasPrefix(a.ActionID, "task_respond:"):
					handlerErr = ctrl.HandleTaskApproval(context.Background(), a)
				case strings.HasPrefix(a.ActionID, "task_permission:"):
					handlerErr = ctrl.HandleMCPPermission(context.Background(), a)
				default:
					zlog.Warn().Str("actionID", a.ActionID).Msg("[slack/interactions] unknown action_id prefix")
				}
				if handlerErr != nil {
					zlog.Error().Err(handlerErr).Str("actionID", a.ActionID).Msg("[slack/interactions] handler error")
				}
			}(action)
		}
	})
}

// oauthCallbackHandler handles Slack's OAuth redirect callback.
func oauthCallbackHandler(svc slacksvc.Service, ctrl slackctrl.Controller, baseURL string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state") // base62 workspace ID

		if code == "" || state == "" {
			zlog.Warn().Msg("[slack/oauth] missing code or state parameter")
			http.Error(w, "missing code or state parameter", http.StatusBadRequest)
			return
		}

		redirectURI := fmt.Sprintf("%s/slack/oauth/callback", baseURL)
		err := ctrl.HandleOAuthCallback(r.Context(), state, code, redirectURI)
		if err != nil {
			zlog.Error().Err(err).Msg("[slack/oauth] HandleOAuthCallback error")
			// Redirect back with a generic error query param to prevent information leakage
			errorMsg := url.QueryEscape("failed to complete slack authorization")
			http.Redirect(w, r, fmt.Sprintf("%s/workspaces/%s/settings?tab=slack&slack_error=%s", baseURL, state, errorMsg), http.StatusTemporaryRedirect)
			return
		}

		// Redirect back to settings page on success
		http.Redirect(w, r, fmt.Sprintf("%s/workspaces/%s/settings?tab=slack", baseURL, state), http.StatusTemporaryRedirect)
	})
}

// commandHandler handles Slack Slash Commands (e.g. /t).
func commandHandler(svc slacksvc.Service, ctrl slackctrl.Controller) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}

		if err := svc.VerifyRequest(r, body); err != nil {
			zlog.Warn().Err(err).Msg("[slack/commands] signature verification failed")
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		// Reconstruct request body for form parsing
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		if err := r.ParseForm(); err != nil {
			http.Error(w, "failed to parse form", http.StatusBadRequest)
			return
		}

		command := r.FormValue("command")
		text := r.FormValue("text")
		channelID := r.FormValue("channel_id")

		if command != "/t" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"response_type": "ephemeral",
				"text":          "⚠️ Unknown command. Use `/t` to interact with AgentRQ.",
			})
			return
		}

		msg, ephemeral, err := ctrl.HandleSlashCommand(r.Context(), channelID, text)
		if err != nil {
			zlog.Error().Err(err).Str("channelID", channelID).Msg("[slack/commands] HandleSlashCommand error")
		}

		respType := "in_channel"
		if ephemeral {
			respType = "ephemeral"
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response_type": respType,
			"text":          msg,
		})
	})
}
