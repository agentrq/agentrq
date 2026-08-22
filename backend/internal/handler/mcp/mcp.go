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
	"strconv"
	"strings"
	"time"

	zlog "github.com/rs/zerolog/log"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"

	mcpctrl "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/golang-jwt/jwt/v5"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/mustafaturan/monoflake"
)

type Params struct {
	MCPManager *mcpctrl.Manager
	Crud       crud.Controller
	TokenSvc   auth.TokenService
	// CIMD resolves Client ID Metadata Document URLs. Optional: a default
	// network-backed resolver is used when nil.
	CIMD    auth.CIMDResolver
	BaseURL string
	Mux     *http.ServeMux
}

type Handler interface{}

type handler struct {
	mcpManager *mcpctrl.Manager
	crud       crud.Controller
	tokenSvc   auth.TokenService
	cimd       auth.CIMDResolver
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
	cimd := p.CIMD
	if cimd == nil {
		cimd = auth.NewCIMDResolver()
	}

	h := &handler{
		mcpManager: p.MCPManager,
		crud:       p.Crud,
		tokenSvc:   p.TokenSvc,
		cimd:       cimd,
		baseURL:    p.BaseURL,
	}

	// Mount the unified Streamable HTTP endpoint natively.
	// We handle both exact and trailing slash versions to be robust.
	p.Mux.Handle("/mcp/{workspaceID}", corsWrapper(h.streamableHandler()))

	// discovery endpoints (path-based)
	p.Mux.Handle("/mcp/{workspaceID}/.well-known/oauth-authorization-server", corsWrapper(h.oauthMetadataHandler()))
	// RFC 8414 §3.1: the well-known suffix MUST be inserted between the host
	// and any path component of the issuer, not appended after it. This
	// server's issuer for a given workspace is {baseURL}/mcp/{workspaceID},
	// so the discovery URL a strictly-compliant client actually computes is
	// this one — the path-suffixed route above exists only for backward
	// compatibility with clients that resolve it via oauth-protected-resource.
	p.Mux.Handle("/.well-known/oauth-authorization-server/mcp/{workspaceID}", corsWrapper(h.oauthMetadataHandler()))
	p.Mux.Handle("/mcp/{workspaceID}/.well-known/oauth-protected-resource", corsWrapper(h.oauthProtectedResourceHandler()))
	p.Mux.Handle("/.well-known/oauth-protected-resource/mcp/{workspaceID}", corsWrapper(h.oauthProtectedResourceHandler()))

	// OAuth2 endpoints (path-based)
	p.Mux.Handle("/mcp/{workspaceID}/oauth2/authorize", h.oauthAuthorizeHandler())
	p.Mux.Handle("/mcp/{workspaceID}/oauth2/token", corsWrapper(h.oauthTokenHandler()))
	p.Mux.Handle("/mcp/{workspaceID}/oauth2/register", corsWrapper(h.oauthRegisterHandler()))

	return h, nil
}

// workspaceIDFromParam parses the base62 workspace ID from the route or base36 from host.
func workspaceIDFromParam(r *http.Request) int64 {
	idStr := r.PathValue("workspaceID")
	if idStr != "" {
		return monoflake.IDFromBase62(idStr).Int64()
	}

	// Try extracting from subdomain: {workspaceID}.mcp.{domain}
	parts := strings.Split(r.Host, ".")
	if len(parts) >= 3 && parts[1] == "mcp" {
		// Subdomain is in base36 for case-insensitive DNS compatibility
		if id, err := strconv.ParseInt(parts[0], 36, 64); err == nil {
			return id
		}
	}

	return 0
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

// oauthIdentity holds the URLs that identify this workspace's OAuth resource
// and authorization server. Every handler derives them here so the issuer that
// oauthMetadataHandler publishes can never drift from the one
// oauthProtectedResourceHandler points clients at.
type oauthIdentity struct {
	baseURL string
	// issuer is the authorization server's issuer identifier (RFC 8414 §2).
	issuer string
	// resource is the protected resource identifier (RFC 9728 §2).
	resource string
	// prmURL is the RFC 9728 §3.1 metadata URL for resource.
	prmURL string
}

func oauthIdentityFor(r *http.Request, workspaceID int64) oauthIdentity {
	proto := "https://"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" && !strings.Contains(r.Host, "mcp.") {
		proto = "http://"
	}
	baseURL := proto + r.Host

	// On a workspace subdomain the workspace *is* the origin, so both the
	// issuer and the resource sit at the root and the well-known suffixes
	// need no path component.
	if strings.Contains(r.Host, ".mcp.") {
		return oauthIdentity{
			baseURL:  baseURL,
			issuer:   baseURL,
			resource: baseURL,
			prmURL:   baseURL + "/.well-known/oauth-protected-resource",
		}
	}

	// Path-based routing: every workspace shares one host, so the workspace
	// path segment has to be part of both identifiers or all workspaces would
	// claim the same identity. RFC 8414 §3.1 and RFC 9728 §3.1 both insert the
	// well-known suffix between the host and that path component.
	wsPath := "/mcp/" + monoflake.ID(workspaceID).String()
	return oauthIdentity{
		baseURL:  baseURL,
		issuer:   baseURL + wsPath,
		resource: baseURL + wsPath,
		prmURL:   baseURL + "/.well-known/oauth-protected-resource" + wsPath,
	}
}

// challengeUnauthorized writes the RFC 9728 §5.1 challenge that tells an
// unauthenticated client exactly where to find our metadata, instead of making
// it guess well-known paths.
func challengeUnauthorized(w http.ResponseWriter, r *http.Request, workspaceID int64, message string) {
	id := oauthIdentityFor(r, workspaceID)
	w.Header().Set("WWW-Authenticate", fmt.Sprintf(
		`Bearer realm=%q, resource_metadata=%q`, id.resource, id.prmURL))
	sendJSONRPCError(w, message, jsonrpc.CodeInvalidRequest, http.StatusUnauthorized)
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

// registrationError writes an RFC 7591 §3.2.2 client registration error
// response.
func registrationError(w http.ResponseWriter, errorCode, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":             errorCode,
		"error_description": description,
	})
}

// parseRegisteredRedirectURIs extracts and validates the RFC 7591 §2
// "redirect_uris" client metadata field. A client that doesn't declare any
// redirect_uris is allowed through (ok=true, empty slice) so registration
// stays permissive; a malformed field is rejected outright.
func parseRegisteredRedirectURIs(payload map[string]interface{}) (uris []string, ok bool) {
	raw, present := payload["redirect_uris"]
	if !present {
		return nil, true
	}
	items, isArray := raw.([]interface{})
	if !isArray {
		return nil, false
	}
	for _, item := range items {
		s, isString := item.(string)
		if !isString || s == "" {
			return nil, false
		}
		if _, err := url.Parse(s); err != nil {
			return nil, false
		}
		uris = append(uris, s)
	}
	return uris, true
}

func (h *handler) oauthRegisterHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			registrationError(w, "invalid_client_metadata", "request body must be a JSON object")
			return
		}

		if payload == nil {
			payload = make(map[string]interface{})
		}

		redirectURIs, ok := parseRegisteredRedirectURIs(payload)
		if !ok {
			registrationError(w, "invalid_redirect_uri", "redirect_uris must be an array of non-empty URI strings")
			return
		}

		// The client_id IS a signed credential carrying the client's
		// registered redirect_uris, so /oauth2/authorize can later bind the
		// authorization request's redirect_uri to what this client actually
		// registered (RFC 6749 §3.1.2.3) instead of accepting any value.
		clientID, err := h.tokenSvc.CreateClientRegistrationToken(redirectURIs)
		if err != nil {
			registrationError(w, "invalid_client_metadata", "failed to register client")
			return
		}
		payload["client_id"] = clientID
		payload["client_id_issued_at"] = time.Now().Unix()
		// These are always public clients (no client_secret is ever issued),
		// per RFC 7591 §2's token_endpoint_auth_method metadata field.
		if _, hasAuthMethod := payload["token_endpoint_auth_method"]; !hasAuthMethod {
			payload["token_endpoint_auth_method"] = "none"
		}
		payload["client_secret_expires_at"] = 0

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
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
			if strings.ToLower(k) == "authorization" {
				ev = ev.Str("h_"+strings.ToLower(k), "[REDACTED]")
				continue
			}
			ev = ev.Str("h_"+strings.ToLower(k), strings.Join(v, ", "))
		}
		ev.Msg("MCP call")

		// 1. Mandatory token check if workspace has it in DB
		queryToken := getTokenVal(r)
		userID := ""
		if queryToken == "" {
			challengeUnauthorized(w, r, workspaceID, "situational security: mission token required")
			return
		}

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
		}

		// Final check: userID must be set
		if userID == "" {
			challengeUnauthorized(w, r, workspaceID, "situational security: unauthorized")
			return
		}

		// Authorization: verify that the user has access to this workspace
		if ok, err := h.crud.CheckWorkspaceAccess(r.Context(), workspaceID, userID); err != nil || !ok {
			sendJSONRPCError(w, "situational security: forbidden", jsonrpc.CodeInvalidRequest, http.StatusForbidden)
			return
		}

		srv := h.mcpManager.Get(workspaceID, userID)
		zlog.Debug().Int64("workspace_id", workspaceID).Str("user_id", userID).Str("method", r.Method).Msg("MCP streamable handler")

		// Create a new context with claims if we have them
		ctx := r.Context()
		var requestClaims *auth.Claims
		if claims, err := h.tokenSvc.ValidateToken(queryToken); err == nil {
			requestClaims = claims
		}
		if requestClaims == nil && userID != "" {
			requestClaims = &auth.Claims{
				RegisteredClaims: jwt.RegisteredClaims{Subject: userID},
			}
		}
		if requestClaims != nil {
			ctx = context.WithValue(ctx, auth.CtxKeyMCPClaims, requestClaims)
		}

		if r.Method == "POST" {
			// Custom handling for notifications/claude/channel/permission_request
			// because mcp-go (SDK) rejects them as unsupported methods.
			if strings.Contains(string(body), "notifications/claude/channel/permission_request") {
				zlog.Debug().Str("session_id", sessionID).Msg("Handling custom permission notification")
				srv.HandleCustomNotification(ctx, sessionID, body)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				return
			}
		}
		srv.Handler().ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *handler) identifyUser(ctx context.Context, workspaceID int64, tokenStr string) string {
	if tokenStr == "" {
		return ""
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

func (h *handler) oauthProtectedResourceHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		workspaceID := workspaceIDFromParam(r)
		if workspaceID == 0 {
			http.Error(w, "workspace not found", http.StatusNotFound)
			return
		}

		id := oauthIdentityFor(r, workspaceID)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"resource": id.resource,
			// RFC 9728 §2: "authorization_servers" is an ARRAY of issuer
			// identifiers — not metadata document URLs. The client derives the
			// RFC 8414 well-known URL from the issuer itself; handing it the
			// metadata URL made strict clients resolve
			// .../.well-known/oauth-authorization-server/.well-known/oauth-authorization-server.
			"authorization_servers":    []string{id.issuer},
			"bearer_methods_supported": []string{"header"},
		})
	})
}

func (h *handler) oauthMetadataHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		workspaceID := workspaceIDFromParam(r)
		if workspaceID == 0 {
			http.Error(w, "workspace not found", http.StatusNotFound)
			return
		}

		// issuer MUST identify this specific workspace's authorization
		// server (RFC 8414 §3.3: "the issuer value returned MUST be
		// identical to the issuer value... used to construct the URL").
		// Every workspace shares the same baseURL/host in the path-based
		// (non-subdomain) case, so the workspace path segment has to be part
		// of the issuer or every workspace would claim the same identity.
		id := oauthIdentityFor(r, workspaceID)

		// The OAuth endpoints hang off the issuer, which already carries the
		// workspace path segment (or is the subdomain root).
		metadata := map[string]interface{}{
			"issuer":                   id.issuer,
			"authorization_endpoint":   id.issuer + "/oauth2/authorize",
			"token_endpoint":           id.issuer + "/oauth2/token",
			"registration_endpoint":    id.issuer + "/oauth2/register",
			"response_types_supported": []string{"code"},
			// Deliberately no client_credentials: every token here is bound to
			// a specific user's workspace access, and that grant has no user to
			// bind one to. Clients are also all public (see below), so there
			// would be no secret to authenticate with either — any self-
			// registered client could mint workspace tokens. Headless callers
			// reuse a refresh token obtained once via authorization_code.
			"grant_types_supported": []string{"authorization_code", "refresh_token"},
			// Registered clients are always public (DCR never issues a
			// client_secret); advertise "none" rather than letting clients
			// assume the RFC 8414 default of client_secret_basic.
			"token_endpoint_auth_methods_supported": []string{"none"},
			// See draft-ietf-oauth-client-id-metadata-document §6: a client_id
			// that is itself an https URL is resolved as a client metadata
			// document (oauthAuthorizeHandler's CIMDResolver backs this).
			"client_id_metadata_document_supported": true,
		}

		importJson := json.NewEncoder(w)
		importJson.Encode(metadata)
	})
}

func (h *handler) oauthAuthorizeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		workspaceID := workspaceIDFromParam(r)

		workspace, err := h.crud.SystemGetWorkspace(r.Context(), workspaceID)
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

		clientID := r.URL.Query().Get("client_id")
		redirectURI := r.URL.Query().Get("redirect_uri")
		state := r.URL.Query().Get("state")

		// Bind the requested redirect_uri to whatever this client actually
		// registered (RFC 6749 §3.1.2.3), instead of accepting any value —
		// this is what makes client registration meaningful instead of
		// decorative, and it closes custom-scheme redirect_uris (e.g.
		// "evilapp://callback") that the heuristic below never checked.
		// client_id can be registered two ways: a Client ID Metadata
		// Document URL (draft-ietf-oauth-client-id-metadata-document), or a
		// client_id minted by our own /oauth2/register (RFC 7591 DCR).
		var registeredRedirectURIs []string
		switch {
		case clientID != "" && h.cimd.IsClientIDURL(clientID):
			metadata, err := h.cimd.Resolve(r.Context(), clientID)
			if err != nil {
				// The draft requires aborting the authorization request when
				// the client's metadata document can't be fetched/validated.
				zlog.Warn().Err(err).Str("client_id", clientID).Msg("CIMD resolution failed")
				http.Error(w, "invalid_client: could not resolve client_id metadata document", http.StatusBadRequest)
				return
			}
			registeredRedirectURIs = metadata.RedirectURIs
		case clientID != "":
			if claims, err := h.tokenSvc.ValidateClientRegistrationToken(clientID); err == nil {
				registeredRedirectURIs = claims.RedirectURIs
			}
		}

		if redirectURI != "" && len(registeredRedirectURIs) > 0 {
			// Exact match, except that loopback URIs ignore the port — native
			// clients get an ephemeral port from the OS at request time and so
			// cannot register it (RFC 8252 §7.3).
			if !auth.AnyRedirectURIMatches(registeredRedirectURIs, redirectURI) {
				http.Error(w, "invalid redirect_uri: not registered for this client_id", http.StatusBadRequest)
				return
			}
		} else if redirectURI != "" {
			// No registered client to bind to: fall back to the same-origin
			// heuristic below, preserving behavior for legacy / first-party
			// callers that don't use client_id-scoped redirects.
			if strings.HasPrefix(redirectURI, "/") && !strings.HasPrefix(redirectURI, "//") && !strings.HasPrefix(redirectURI, "/\\") {
				// OK: local path
			} else {
				// Parse absolute URL and validate against baseURL
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

					isCustomScheme := pRedirect.Scheme != "" && pRedirect.Scheme != "http" && pRedirect.Scheme != "https"

					if isCustomScheme {
						// A private-use scheme has no origin to validate
						// against, so an unregistered one is only accepted if
						// it belongs to a known native client. Accepting any
						// scheme here would make the whole check moot: a
						// caller could present an unrecognized client_id and
						// have the code delivered to a scheme of their
						// choosing.
						if !auth.IsAllowedNativeRedirectScheme(pRedirect.Scheme) {
							http.Error(w, "invalid redirect_uri: unrecognized custom scheme; register the redirect_uri to use it", http.StatusBadRequest)
							return
						}
					} else {
						if pRedirect.Scheme != "https" && !isLocal {
							http.Error(w, "invalid redirect_uri: https required for non-localhost", http.StatusBadRequest)
							return
						}

						// Allow host mismatch ONLY for localhost/127.0.0.1
						if pRedirect.Host != pBase.Host && !isLocal {
							http.Error(w, "invalid redirect_uri: host mismatch", http.StatusBadRequest)
							return
						}
					}
				} else {
					// It's not absolute and doesn't start with /
					http.Error(w, "invalid redirect_uri: relative path must start with /", http.StatusBadRequest)
					return
				}
			}
		}

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
		// RFC 6749 §5.1: token responses MUST NOT be cached.
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")

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
