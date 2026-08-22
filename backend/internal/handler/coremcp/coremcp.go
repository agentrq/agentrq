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
	zlog "github.com/rs/zerolog/log"
)

type Params struct {
	Crud     crud.Controller
	TokenSvc auth.TokenService
	// CIMD resolves Client ID Metadata Document URLs. Optional: a default
	// network-backed resolver is used when nil.
	CIMD    auth.CIMDResolver
	BaseURL string
	Domain  string
	Mux     *http.ServeMux
}

type Handler interface{}

type handler struct {
	coremcpServer *WorkspaceServer
	tokenSvc      auth.TokenService
	cimd          auth.CIMDResolver
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
	cimd := p.CIMD
	if cimd == nil {
		cimd = auth.NewCIMDResolver()
	}

	h := &handler{
		coremcpServer: NewServer(p.Crud, p.BaseURL),
		tokenSvc:      p.TokenSvc,
		cimd:          cimd,
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

// oauthIdentity holds the URLs identifying the core MCP resource and its
// authorization server. Both metadata handlers derive them here so the issuer
// one publishes can never drift from the one the other points clients at.
type oauthIdentity struct {
	baseURL string
	// issuer is the authorization server's issuer identifier (RFC 8414 §2).
	issuer string
	// resource is the protected resource identifier (RFC 9728 §2).
	resource string
	// prmURL is the RFC 9728 §3.1 metadata URL for resource.
	prmURL string
}

func oauthIdentityFor(r *http.Request) oauthIdentity {
	proto := "https://"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" && !strings.Contains(r.Host, "mcp.") {
		proto = "http://"
	}
	baseURL := proto + r.Host

	// On a workspace subdomain the resource is the origin root; otherwise the
	// core MCP endpoint lives under /mcp. The authorization server is the
	// origin either way, so its RFC 8414 well-known URL needs no path segment.
	id := oauthIdentity{
		baseURL:  baseURL,
		issuer:   baseURL,
		resource: baseURL + "/mcp",
		prmURL:   baseURL + "/.well-known/oauth-protected-resource/mcp",
	}
	if strings.Contains(r.Host, ".mcp.") {
		id.resource = baseURL
		id.prmURL = baseURL + "/.well-known/oauth-protected-resource"
	}
	return id
}

// challengeUnauthorized writes the RFC 9728 §5.1 challenge that tells an
// unauthenticated client exactly where to find our metadata, instead of making
// it guess well-known paths.
func challengeUnauthorized(w http.ResponseWriter, r *http.Request, message string) {
	id := oauthIdentityFor(r)
	w.Header().Set("WWW-Authenticate", fmt.Sprintf(
		`Bearer realm=%q, resource_metadata=%q`, id.resource, id.prmURL))
	sendJSONRPCError(w, message, -32000, http.StatusUnauthorized)
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
			challengeUnauthorized(w, r, "unauthorized")
			return
		}

		claims, err := h.tokenSvc.ValidateToken(queryToken)
		if err != nil || claims == nil {
			challengeUnauthorized(w, r, "unauthorized")
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
			challengeUnauthorized(w, r, "unauthorized")
			return
		}

		userID := claims.Subject
		ctx := context.WithValue(r.Context(), "user_id", userID)
		ctx = context.WithValue(ctx, auth.CtxKeyMCPClaims, claims)

		zlog.Debug().Str("user_id", userID).Str("method", r.Method).Msg("CoreMCP streamable handler")

		h.coremcpServer.Handler().ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *handler) oauthMetadataHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id := oauthIdentityFor(r)
		baseURL := id.baseURL

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
			"issuer":                   id.issuer,
			"authorization_endpoint":   authEndpoint,
			"token_endpoint":           tokenEndpoint,
			"registration_endpoint":    regEndpoint,
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
			"logo_uri":                              h.baseURL + "/agentrq.png",
		}

		json.NewEncoder(w).Encode(metadata)
	})
}

func (h *handler) oauthProtectedResourceHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id := oauthIdentityFor(r)

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

func (h *handler) oauthAuthorizeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			matched := false
			for _, registered := range registeredRedirectURIs {
				if registered == redirectURI {
					matched = true
					break
				}
			}
			if !matched {
				http.Error(w, "invalid redirect_uri: not registered for this client_id", http.StatusBadRequest)
				return
			}
		} else if redirectURI != "" {
			// No DCR-registered client to bind to: fall back to the
			// same-origin heuristic below, preserving behavior for legacy /
			// first-party callers that don't use client_id-scoped redirects.
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
