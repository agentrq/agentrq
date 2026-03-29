// Package mcp provides a Fiber handler that bridges Fiber routing with the
// standard http.Handler returned by mcp-go's SSEServer.
package mcp

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	zlog "github.com/rs/zerolog/log"

	mcpctrl "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/agentrq/agentrq/backend/internal/service/security"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mustafaturan/monoflake"
)

type Params struct {
	MCPManager *mcpctrl.Manager
	Repository base.Repository
	TokenSvc   auth.TokenService
	TokenKey   string
	Mux        *http.ServeMux
}

type Handler interface{}

type handler struct {
	mcpManager *mcpctrl.Manager
	repo       base.Repository
	tokenSvc   auth.TokenService
	tokenKey   string
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
	}

	// Mount the unified Streamable HTTP endpoint natively.
	// We handle both exact and trailing slash versions to be robust.
	p.Mux.Handle("/mcp/{workspaceID}", corsWrapper(h.streamableHandler()))

	return h, nil
}

// workspaceIDFromParam parses the base62 workspace ID from the route.
func workspaceIDFromParam(r *http.Request) int64 {
	return monoflake.IDFromBase62(r.PathValue("workspaceID")).Int64()
}

// streamableHandler serves both GET (Stream) and POST (Messages) via mcp-go StreamableHTTPServer.
func (h *handler) streamableHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		workspaceID := workspaceIDFromParam(r)
		if workspaceID == 0 {
			http.Error(w, "invalid workspace id", http.StatusBadRequest)
			return
		}

		// Log all incoming MCP calls with headers
		ev := zlog.Debug().Str("method", r.Method).Str("path", r.URL.Path).Str("remote", r.RemoteAddr)
		for k, v := range r.Header {
			ev = ev.Str("h_"+strings.ToLower(k), strings.Join(v, ", "))
		}
		ev.Msg("MCP call")

		// 1. Mandatory token check if workspace has it in DB
		queryToken := r.URL.Query().Get("token")
		workspace, err := h.repo.SystemGetWorkspace(r.Context(), workspaceID)

		userID := ""
		if err == nil && workspace.TokenEncrypted != "" {
			if queryToken == "" {
				http.Error(w, "situational security: mission token required", http.StatusUnauthorized)
				return
			}
			dec, decErr := security.Decrypt(workspace.TokenEncrypted, h.tokenKey, workspace.TokenNonce)
			if decErr != nil || dec != queryToken {
				http.Error(w, "situational security: invalid mission token", http.StatusUnauthorized)
				return
			}
			userID = monoflake.ID(workspace.UserID).String()
		}

		// 2. Mandatory Mcp-Session-Id for non-initialize requests
		sessionID := r.Header.Get("Mcp-Session-Id")

		var body []byte
		if r.Method == "POST" {
			body, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewBuffer(body))
		}

		// isInitializeCall := false
		// if r.Method == "POST" {
		// 	var rpc struct {
		// 		Method string `json:"method"`
		// 	}
		// 	if err := json.Unmarshal(body, &rpc); err == nil {
		// 		if rpc.Method == "initialize" {
		// 			isInitializeCall = true
		// 			// On initialize, create session ID and return as header
		// 			newSessID, err := h.tokenSvc.CreateMCPToken(userID, monoflake.ID(workspaceID).String())
		// 			if err == nil {
		// 				sessionID = newSessID
		// 				w.Header().Set("Mcp-Session-Id", sessionID)
		// 			}
		// 		}
		// 	}
		// }

		// // Mcp-Session-Id is mandatory for all requests except initialize call
		// if !isInitializeCall && sessionID == "" {
		// 	http.Error(w, "situational security: mcp-session-id required by MCP spec", http.StatusBadRequest)
		// 	return
		// }

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

		// Final check: if workspace has token, userID must be set
		if err == nil && workspace.TokenEncrypted != "" && userID == "" {
			http.Error(w, "situational security: unauthorized", http.StatusUnauthorized)
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
			return claims.Subject
		}
	}

	return ""
}
