// Package mcp provides a Fiber handler that bridges Fiber routing with the
// standard http.Handler returned by mcp-go's SSEServer.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	zlog "github.com/rs/zerolog/log"

	mcpctrl "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/agentrq/agentrq/backend/internal/service/security"
	"github.com/golang-jwt/jwt/v5"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/mustafaturan/monoflake"
)

type Params struct {
	MCPManager *mcpctrl.Manager
	Repository base.Repository
	TokenSvc   auth.TokenService
	TokenKey   string
	BaseURL    string
	Mux        *http.ServeMux
}

type Handler interface{}

type handler struct {
	mcpManager *mcpctrl.Manager
	repo       base.Repository
	tokenSvc   auth.TokenService
	tokenKey   string
	baseURL    string
}

func corsWrapper(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Mcp-Session-Id, Mcp-Protocol-Version, Last-Event-ID")
		w.Header().Set("Access-Control-Expose-Headers", "Mcp-Session-Id, Mcp-Protocol-Version")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func New(p Params) (Handler, error) {
	h := &handler{
		mcpManager: p.MCPManager,
		repo:       p.Repository,
		tokenSvc:   p.TokenSvc,
		tokenKey:   p.TokenKey,
		baseURL:    p.BaseURL,
	}

	// Mount the unified Streamable HTTP endpoint natively.
	// We handle both exact and trailing slash versions to be robust.
	p.Mux.Handle("/mcp/{workspaceID}", corsWrapper(h.streamableHandler()))

	// OAuth2 and CIMD endpoints
	p.Mux.Handle("/mcp/{workspaceID}/.well-known/oauth-authorization-server", corsWrapper(h.oauthMetadataHandler()))
	p.Mux.Handle("/.well-known/oauth-protected-resource/mcp/{workspaceID}", corsWrapper(h.oauthMetadataHandler()))
	p.Mux.Handle("/mcp/{workspaceID}/oauth2/authorize", h.oauthAuthorizeHandler())
	p.Mux.Handle("/mcp/{workspaceID}/oauth2/token", corsWrapper(h.oauthTokenHandler()))
	p.Mux.Handle("/mcp/{workspaceID}/oauth2/register", corsWrapper(h.oauthRegisterHandler()))

	return h, nil
}

// workspaceIDFromParam parses the base62 workspace ID from the route.
func workspaceIDFromParam(r *http.Request) int64 {
	return monoflake.IDFromBase62(r.PathValue("workspaceID")).Int64()
}

func getTokenVal(r *http.Request) string {
	if token := r.URL.Query().Get("token"); token != "" {
		return token
	}
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer ")
	}
	return ""
}

func sendJSONRPCError(w http.ResponseWriter, message string, code int, httpStatus int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      nil,
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	})
}

func (h *handler) oauthRegisterHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var payload map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&payload)

		if payload == nil {
			payload = make(map[string]interface{})
		}

		// Strictly stateless/stateless-like: issue a dynamic auto-generated PKCE clientId string
		clientID := "dynamic-" + monoflake.ID(time.Now().UnixNano()).String()
		payload["client_id"] = clientID
		payload["client_id_issued_at"] = time.Now().Unix()
		// Implicitly public clients, so no strictly enforced client_secret, but we set expires_at=0 to satisfy strict SDK parsers
		payload["client_secret_expires_at"] = 0

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(payload)
	})
}

// streamableHandler serves both GET (Stream) and POST (Messages) via mcp-go StreamableHTTPServer.
func (h *handler) streamableHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		workspaceID := workspaceIDFromParam(r)
		if workspaceID == 0 {
			sendJSONRPCError(w, "invalid workspace id", jsonrpc.CodeInvalidParams, http.StatusBadRequest)
			return
		}

		// Log all incoming MCP calls with headers
		ev := zlog.Debug().Str("method", r.Method).Str("path", r.URL.Path).Str("remote", r.RemoteAddr)
		for k, v := range r.Header {
			ev = ev.Str("h_"+strings.ToLower(k), strings.Join(v, ", "))
		}
		ev.Msg("MCP call")

		// 1. Mandatory token check if workspace has it in DB
		queryToken := getTokenVal(r)
		workspace, err := h.repo.SystemGetWorkspace(r.Context(), workspaceID)
		if err != nil {
			sendJSONRPCError(w, "situational security: workspace not found", jsonrpc.CodeInvalidParams, http.StatusNotFound)
			return
		}

		userID := ""
		if queryToken == "" {
			sendJSONRPCError(w, "situational security: mission token required", jsonrpc.CodeInvalidRequest, http.StatusUnauthorized)
			return
		}

		// If it's a 16-character mission token, decrypt and check
		if len(queryToken) == 16 {
			dec, decErr := security.Decrypt(workspace.TokenEncrypted, h.tokenKey, workspace.TokenNonce)
			if decErr != nil || dec != queryToken {
				sendJSONRPCError(w, "situational security: invalid mission token", jsonrpc.CodeInvalidRequest, http.StatusUnauthorized)
				return
			}
			userID = monoflake.ID(workspace.UserID).String()
		}
		// If length is not 16, it might be a valid JWT OAuth token. It will be checked via identifyUser below.

		// 2. Mandatory Mcp-Session-Id for non-initialize requests
		sessionID := r.Header.Get("Mcp-Session-Id")

		var body []byte
		if r.Method == "POST" {
			body, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewBuffer(body))
		}

		// Try to identify user if not already set by secret
		if userID == "" {
			if queryToken != "" {
				userID = h.identifyUser(r.Context(), workspaceID, queryToken)
			}
			if userID == "" && sessionID != "" {
				// The session ID is a JWT — validate it and extract user
				userID = h.identifyUser(r.Context(), workspaceID, sessionID)
			}
		}

		// Final check: userID must be set
		if userID == "" {
			sendJSONRPCError(w, "situational security: unauthorized", jsonrpc.CodeInvalidRequest, http.StatusUnauthorized)
			return
		}

		srv := h.mcpManager.Get(workspaceID, userID)
		zlog.Debug().Int64("workspace_id", workspaceID).Str("user_id", userID).Str("method", r.Method).Msg("MCP streamable handler")

		// Create a new context with claims if we have them
		ctx := r.Context()
		if userID != "" {
			ctx = context.WithValue(ctx, auth.ClaimsContextKey, &auth.Claims{
				RegisteredClaims: jwt.RegisteredClaims{Subject: userID},
			})
		}

		if r.Method == "POST" {
			// Custom handling for notifications/claude/channel/permission_request
			// because mcp-go (SDK) rejects them as unsupported methods.
			if strings.Contains(string(body), "notifications/claude/channel/permission_request") {
				srv.HandleCustomNotification(ctx, sessionID, body)
			}
		}
		srv.Handler().ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *handler) identifyUser(ctx context.Context, workspaceID int64, tokenStr string) string {
	if tokenStr == "" {
		return ""
	}

	// 1. Try situational secret (16-chars)
	if len(tokenStr) == 16 {
		workspace, err := h.repo.SystemGetWorkspace(ctx, workspaceID)
		if err == nil && workspace.TokenEncrypted != "" {
			dec, decErr := security.Decrypt(workspace.TokenEncrypted, h.tokenKey, workspace.TokenNonce)
			if decErr == nil && dec == tokenStr {
				return monoflake.ID(workspace.UserID).String()
			}
		}
	}

	// 2. Try JWT situational authentication
	claims, err := h.tokenSvc.ValidateToken(tokenStr)
	if err == nil {
		workspaceIDBase62 := monoflake.ID(workspaceID).String()
		isWorkspaceValid := false
		if len(claims.Audience) == 0 {
			isWorkspaceValid = true // Global token
		} else {
			for _, aud := range claims.Audience {
				if aud == workspaceIDBase62 {
					isWorkspaceValid = true
					break
				}
			}
		}

		if isWorkspaceValid {
			hasInvalidAudience := false
			for _, aud := range claims.Audience {
				if aud == "refresh" || aud == "authorization_code" {
					hasInvalidAudience = true
					break
				}
			}
			if !hasInvalidAudience {
				return claims.Subject
			}
		}
	}

	return ""
}

func (h *handler) oauthMetadataHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		proto := "https://"
		if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" && !strings.Contains(r.Host, "mcp.") {
			proto = "http://"
		}

		baseURL := proto + r.Host

		authEndpoint := baseURL + "/oauth2/authorize"
		tokenEndpoint := baseURL + "/oauth2/token"
		regEndpoint := baseURL + "/oauth2/register"

		// If accessed without a subdomain (e.g. localhost or a custom domain root), append the workspace route
		if !strings.Contains(r.Host, ".mcp.") {
			workspaceID := workspaceIDFromParam(r)
			if workspaceID != 0 {
				workspaceIDBase62 := monoflake.ID(workspaceID).String()
				authEndpoint = baseURL + "/mcp/" + workspaceIDBase62 + "/oauth2/authorize"
				tokenEndpoint = baseURL + "/mcp/" + workspaceIDBase62 + "/oauth2/token"
				regEndpoint = baseURL + "/mcp/" + workspaceIDBase62 + "/oauth2/register"
			}
		}

		metadata := map[string]interface{}{
			"issuer":                                baseURL,
			"authorization_endpoint":                authEndpoint,
			"token_endpoint":                        tokenEndpoint,
			"registration_endpoint":                 regEndpoint,
			"client_id_metadata_document_supported": true,
			"response_types_supported":              []string{"code"},
			"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		}

		importJson := json.NewEncoder(w)
		importJson.Encode(metadata)
	})
}

func (h *handler) oauthAuthorizeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		workspaceID := workspaceIDFromParam(r)

		workspace, err := h.repo.SystemGetWorkspace(r.Context(), workspaceID)
		if err != nil {
			http.Error(w, "workspace not found", http.StatusNotFound)
			return
		}

		// 1. Is user logged in?
		var userID string
		if cookie, err := r.Cookie("at"); err == nil && cookie.Value != "" {
			if claims, err := h.tokenSvc.ValidateToken(cookie.Value); err == nil && claims != nil {
				userID = claims.Subject
			}
		}

		// Optional clientID validation can be added here
		_ = r.URL.Query().Get("client_id")
		redirectURI := r.URL.Query().Get("redirect_uri")
		state := r.URL.Query().Get("state")

		if userID == "" {
			// Not authenticated, redirect to main login with 'redirect_url'
			// To return back, building the current full URL:
			proto := "https://"
			if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" && !strings.Contains(r.Host, "mcp.") {
				proto = "http://"
			}

			returnPath := r.URL.Path
			if strings.Contains(r.Host, ".mcp.") {
				prefix := "/mcp/" + monoflake.ID(workspaceID).String()
				if strings.HasPrefix(returnPath, prefix) {
					returnPath = strings.TrimPrefix(returnPath, prefix)
					if returnPath == "" {
						returnPath = "/"
					}
				}
			}

			returnQuery := ""
			if r.URL.RawQuery != "" {
				returnQuery = "?" + r.URL.RawQuery
			}

			returnURL := proto + r.Host + returnPath + returnQuery
			loginURL := fmt.Sprintf("%s/api/v1/auth/google/login?redirect_url=%s", h.baseURL, url.QueryEscape(returnURL))
			http.Redirect(w, r, loginURL, http.StatusFound)
			return
		}

		if monoflake.ID(workspace.UserID).String() != userID {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		workspaceIDBase62 := monoflake.ID(workspaceID).String()
		code, err := h.tokenSvc.CreateOAuthCodeToken(userID, workspaceIDBase62)
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Redirect back to client
		finalRedirect := fmt.Sprintf("%s?code=%s&state=%s", redirectURI, url.QueryEscape(code), url.QueryEscape(state))
		http.Redirect(w, r, finalRedirect, http.StatusFound)
	})
}

func (h *handler) oauthTokenHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		err := r.ParseForm()
		if err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}

		grantType := r.Form.Get("grant_type")

		var tokenStr string
		switch grantType {
		case "authorization_code":
			tokenStr = r.Form.Get("code")
		case "refresh_token":
			tokenStr = r.Form.Get("refresh_token")
		default:
			http.Error(w, `{"error": "unsupported_grant_type"}`, http.StatusBadRequest)
			return
		}

		claims, err := h.tokenSvc.ValidateToken(tokenStr)
		if err != nil || claims == nil {
			http.Error(w, `{"error": "invalid_grant"}`, http.StatusUnauthorized)
			return
		}

		if grantType == "refresh_token" {
			hasRefresh := false
			for _, aud := range claims.Audience {
				if aud == "refresh" {
					hasRefresh = true
					break
				}
			}
			if !hasRefresh {
				http.Error(w, `{"error": "invalid_grant"}`, http.StatusUnauthorized)
				return
			}
		}

		userID := claims.Subject
		var workspaceIDBase62 string
		if len(claims.Audience) > 0 {
			workspaceIDBase62 = claims.Audience[0]
		}

		accessToken, err := h.tokenSvc.CreateMCPToken(userID, workspaceIDBase62, "access")
		if err != nil {
			http.Error(w, `{"error": "server_error"}`, http.StatusInternalServerError)
			return
		}

		// The refresh token can just be the same token format for our stateless needs
		refreshToken, err := h.tokenSvc.CreateMCPToken(userID, workspaceIDBase62, "refresh")

		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  accessToken,
			"refresh_token": refreshToken,
			"token_type":    "bearer",
			"expires_in":    2592000, // 30 days
		})
	})
}
