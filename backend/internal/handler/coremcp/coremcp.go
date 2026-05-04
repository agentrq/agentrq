package coremcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"
)

type Params struct {
	Crud     crud.Controller
	TokenSvc auth.TokenService
	BaseURL  string
	Domain   string
	Mux      *http.ServeMux
}

type Handler interface{}

type handler struct {
	coremcpServer *WorkspaceServer
	tokenSvc      auth.TokenService
	baseURL       string
	domain        string
}

func corsWrapper(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (strings.HasPrefix(origin, "http://localhost") || strings.HasPrefix(origin, "http://127.0.0.1") || strings.HasPrefix(origin, "https://localhost") || strings.HasPrefix(origin, "https://127.0.0.1")) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Mcp-Session-Id, Mcp-Protocol-Version, Authorization")
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
		coremcpServer: NewServer(p.Crud, p.BaseURL),
		tokenSvc:      p.TokenSvc,
		baseURL:       p.BaseURL,
		domain:        p.Domain,
	}

	isLocal := p.Domain == "localhost" || p.Domain == "127.0.0.1" || p.Domain == ""

	var hostPattern string
	if !isLocal {
		hostPattern = "mcp." + p.Domain
	}

	p.Mux.Handle("/mcp", corsWrapper(h.streamableHandler()))
	if hostPattern != "" {
		p.Mux.Handle(hostPattern+"/", corsWrapper(h.streamableHandler()))
	}

	// Localhost distinct paths
	p.Mux.Handle("/.well-known/oauth-authorization-server", corsWrapper(h.oauthMetadataHandler()))
	p.Mux.Handle("/mcp/.well-known/oauth-authorization-server", corsWrapper(h.oauthMetadataHandler()))
	p.Mux.Handle("/.well-known/oauth-protected-resource", corsWrapper(h.oauthProtectedResourceHandler()))
	p.Mux.Handle("/.well-known/oauth-protected-resource/mcp", corsWrapper(h.oauthProtectedResourceHandler()))
	p.Mux.Handle("/mcp/oauth2/authorize", h.oauthAuthorizeHandler())
	p.Mux.Handle("/mcp/oauth2/token", corsWrapper(h.oauthTokenHandler()))
	p.Mux.Handle("/mcp/oauth2/register", corsWrapper(h.oauthRegisterHandler()))

	// Host-based distinct paths
	if hostPattern != "" {
		p.Mux.Handle(hostPattern+"/.well-known/oauth-authorization-server", corsWrapper(h.oauthMetadataHandler()))
		p.Mux.Handle(hostPattern+"/.well-known/oauth-protected-resource", corsWrapper(h.oauthProtectedResourceHandler()))
		p.Mux.Handle(hostPattern+"/.well-known/oauth-protected-resource/mcp", corsWrapper(h.oauthProtectedResourceHandler()))
		p.Mux.Handle(hostPattern+"/oauth2/authorize", h.oauthAuthorizeHandler())
		p.Mux.Handle(hostPattern+"/oauth2/token", corsWrapper(h.oauthTokenHandler()))
		p.Mux.Handle(hostPattern+"/oauth2/register", corsWrapper(h.oauthRegisterHandler()))
	}

	return h, nil
}

func getTokenVal(r *http.Request) string {
	if token := r.URL.Query().Get("token"); token != "" {
		return token
	}
	if cookie, err := r.Cookie("at"); err == nil && cookie.Value != "" {
		return cookie.Value
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

		clientID := "coremcp-" + monoflake.ID(time.Now().UnixNano()).String()
		payload["client_id"] = clientID
		payload["client_id_issued_at"] = time.Now().Unix()
		payload["client_secret_expires_at"] = 0

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(payload)
	})
}

func (h *handler) streamableHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ev := zlog.Debug().Str("method", r.Method).Str("path", r.URL.Path).Str("remote", r.RemoteAddr)
		for k, v := range r.Header {
			if strings.ToLower(k) == "authorization" {
				ev = ev.Str("h_"+strings.ToLower(k), "[REDACTED]")
				continue
			}
			ev = ev.Str("h_"+strings.ToLower(k), strings.Join(v, ", "))
		}
		ev.Msg("CoreMCP call")

		queryToken := getTokenVal(r)
		if queryToken == "" {
			sendJSONRPCError(w, "unauthorized", -32000, http.StatusUnauthorized)
			return
		}

		claims, err := h.tokenSvc.ValidateToken(queryToken)
		if err != nil || claims == nil {
			sendJSONRPCError(w, "unauthorized", -32000, http.StatusUnauthorized)
			return
		}

		// Ensure it's a valid coremcp access token
		hasCoreMCP := false
		hasRestricted := false
		for _, aud := range claims.Audience {
			if aud == "coremcp" {
				hasCoreMCP = true
			}
			if aud == "refresh" || aud == "authorization_code" {
				hasRestricted = true
			}
		}

		if !hasCoreMCP || hasRestricted || claims.Subject == "" {
			sendJSONRPCError(w, "unauthorized", -32000, http.StatusUnauthorized)
			return
		}

		userID := claims.Subject
		ctx := context.WithValue(r.Context(), "user_id", userID)

		zlog.Debug().Str("user_id", userID).Str("method", r.Method).Msg("CoreMCP streamable handler")

		h.coremcpServer.Handler().ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *handler) oauthMetadataHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		proto := "https://"
		if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" && !strings.Contains(r.Host, "mcp.") {
			proto = "http://"
		}

		baseURL := proto + r.Host

		pathPrefix := ""
		if !strings.Contains(r.Host, "mcp.") {
			pathPrefix = "/mcp"
		} else if strings.Contains(r.Host, ".mcp.") {
			// If it's a workspace subdomain, endpoints are at the root
			pathPrefix = ""
		}

		authEndpoint := baseURL + pathPrefix + "/oauth2/authorize"
		tokenEndpoint := baseURL + pathPrefix + "/oauth2/token"
		regEndpoint := baseURL + pathPrefix + "/oauth2/register"

		metadata := map[string]interface{}{
			"issuer":                                baseURL,
			"authorization_endpoint":                authEndpoint,
			"token_endpoint":                        tokenEndpoint,
			"registration_endpoint":                 regEndpoint,
			"client_id_metadata_document_supported": true,
			"response_types_supported":              []string{"code"},
			"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
			"logo_uri":                              h.baseURL + "/agentrq.png",
		}

		json.NewEncoder(w).Encode(metadata)
	})
}

func (h *handler) oauthProtectedResourceHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		proto := "https://"
		if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" && !strings.Contains(r.Host, "mcp.") {
			proto = "http://"
		}

		baseURL := proto + r.Host

		resource := baseURL + "/mcp"
		if strings.Contains(r.Host, ".mcp.") {
			// If it's a workspace subdomain, the resource is the root
			resource = baseURL
		}

		authServer := baseURL + "/.well-known/oauth-authorization-server"

		json.NewEncoder(w).Encode(map[string]interface{}{
			"resource":             resource,
			"authorization_server": authServer,
		})
	})
}

func (h *handler) oauthAuthorizeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var userID string
		if cookie, err := r.Cookie("at"); err == nil && cookie.Value != "" {
			if claims, err := h.tokenSvc.ValidateToken(cookie.Value); err == nil && claims != nil {
				userID = claims.Subject
			}
		}

		redirectURI := r.URL.Query().Get("redirect_uri")
		state := r.URL.Query().Get("state")

		// Validate redirectURI to prevent open redirect
		if redirectURI != "" {
			if strings.HasPrefix(redirectURI, "/") && !strings.HasPrefix(redirectURI, "//") && !strings.HasPrefix(redirectURI, "/\\") {
				// OK: local path
			} else {
				// Parse absolute URL
				pRedirect, err := url.Parse(redirectURI)
				if err != nil {
					http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
					return
				}
				if pRedirect.IsAbs() {
					pBase, err := url.Parse(h.baseURL)
					if err != nil {
						http.Error(w, "internal server error", http.StatusInternalServerError)
						return
					}

					// Require https for absolute URLs unless it's localhost
					isLocal := pRedirect.Host == "localhost" || strings.HasPrefix(pRedirect.Host, "localhost:") ||
						pRedirect.Host == "127.0.0.1" || strings.HasPrefix(pRedirect.Host, "127.0.0.1:")

					if pRedirect.Scheme != "https" && !isLocal {
						http.Error(w, "invalid redirect_uri: https required for non-localhost", http.StatusBadRequest)
						return
					}

					// Allow host mismatch ONLY for localhost/127.0.0.1
					if pRedirect.Host != pBase.Host && !isLocal {
						http.Error(w, "invalid redirect_uri: host mismatch", http.StatusBadRequest)
						return
					}
				} else {
					// It's not absolute and doesn't start with /
					http.Error(w, "invalid redirect_uri: relative path must start with /", http.StatusBadRequest)
					return
				}
			}
		}

		if userID == "" {
			proto := "https://"
			if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" && !strings.Contains(r.Host, "mcp.") {
				proto = "http://"
			}

			returnURL := proto + r.Host + r.URL.Path
			if r.URL.RawQuery != "" {
				returnURL += "?" + r.URL.RawQuery
			}
			loginURL := fmt.Sprintf("%s/api/v1/auth/google/login?redirect_url=%s", h.baseURL, url.QueryEscape(returnURL))
			http.Redirect(w, r, loginURL, http.StatusFound)
			return
		}

		code, err := h.tokenSvc.CreateOAuthCodeToken(userID, "coremcp")
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

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

		// Ensure it was issued for CoreMCP
		hasCoreMCP := false
		for _, aud := range claims.Audience {
			if aud == "coremcp" {
				hasCoreMCP = true
				break
			}
		}

		if !hasCoreMCP {
			http.Error(w, `{"error": "invalid_grant"}`, http.StatusUnauthorized)
			return
		}

		if grantType == "authorization_code" {
			hasAuthCode := false
			for _, aud := range claims.Audience {
				if aud == "authorization_code" {
					hasAuthCode = true
					break
				}
			}
			if !hasAuthCode {
				http.Error(w, `{"error": "invalid_grant"}`, http.StatusUnauthorized)
				return
			}
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

		accessToken, err := h.tokenSvc.CreateMCPToken(userID, "coremcp", "access")
		if err != nil {
			http.Error(w, `{"error": "server_error"}`, http.StatusInternalServerError)
			return
		}

		refreshToken, err := h.tokenSvc.CreateMCPToken(userID, "coremcp", "refresh")

		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  accessToken,
			"refresh_token": refreshToken,
			"token_type":    "bearer",
			"expires_in":    2592000, // 30 days
		})
	})
}
