// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/handler/authmd"
	"github.com/agentrq/agentrq/backend/internal/handler/oauthconsent"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"
)

type Params struct {
	Crud crud.Controller
	// ForkMerger merges a fork back, stopping its agent first.
	ForkMerger ForkMerger
	TokenSvc   auth.TokenService
	// CIMD resolves Client ID Metadata Document URLs. Optional: a default
	// network-backed resolver is used when nil.
	CIMD    auth.CIMDResolver
	BaseURL string
	Domain  string
	Mux     *http.ServeMux
	PubSub  pubsub.Service
	// PublicDir is the web build the static handler serves, for its
	// robots.txt on the mcp. host. Defaults to ./public, as the app's is.
	PublicDir string
}

type Handler interface{}

type handler struct {
	coremcpServer *WorkspaceServer
	crud          crud.Controller
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

	srv := NewServer(p.Crud, p.BaseURL, p.PubSub)
	srv.forks = p.ForkMerger
	h := &handler{
		coremcpServer: srv,
		crud:          p.Crud,
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
	// Covers the workspace servers too; it lives here because the mcp. host
	// below answers every other path with this server's endpoint.
	authMD := authmd.Handler(p.BaseURL)
	p.Mux.Handle("/auth.md", authMD)

	// Host-based distinct paths
	if hostPattern != "" {
		p.Mux.Handle(hostPattern+"/.well-known/oauth-authorization-server", corsWrapper(h.oauthMetadataHandler()))
		p.Mux.Handle(hostPattern+"/.well-known/oauth-protected-resource", corsWrapper(h.oauthProtectedResourceHandler()))
		p.Mux.Handle(hostPattern+"/.well-known/oauth-protected-resource/mcp", corsWrapper(h.oauthProtectedResourceHandler()))
		p.Mux.Handle(hostPattern+"/oauth2/authorize", h.oauthAuthorizeHandler())
		p.Mux.Handle(hostPattern+"/oauth2/token", corsWrapper(h.oauthTokenHandler()))
		p.Mux.Handle(hostPattern+"/oauth2/register", corsWrapper(h.oauthRegisterHandler()))
		p.Mux.Handle(hostPattern+"/auth.md", authMD)
		// Every other path on this host is the MCP endpoint, which would
		// answer robots.txt with a 401.
		publicDir := p.PublicDir
		if publicDir == "" {
			publicDir = "./public"
		}
		p.Mux.Handle(hostPattern+"/robots.txt", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, filepath.Join(publicDir, "robots.txt"))
		}))
	}

	return h, nil
}

// getTokenVal picks the credential a request is presenting.
//
// The Authorization header comes before the `at` cookie, and the order is the
// whole point: a client that sets the header is naming the credential it means
// to use, while the cookie merely rides along with anything a signed-in browser
// session sends. Reading the cookie first meant a request carrying a perfectly
// good coremcp Bearer token was judged on its session cookie instead — and
// since a session cookie is minted with the "actor:human" audience and never
// "coremcp", it fails the audience check below and the call is refused. That is
// what the desktop app hit: its fetch is session-aware, so the cookie shadowed
// the token it had just been granted.
//
// The cookie is still read last, for a signed-in browser that sends no header.
func getTokenVal(r *http.Request) string {
	if token := r.URL.Query().Get("token"); token != "" {
		return token
	}
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer ")
	}
	if cookie, err := r.Cookie("at"); err == nil && cookie.Value != "" {
		return cookie.Value
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

// oauthScope is the only scope: everything the supervisor MCP server offers. Whatever scope a
// client asks for, it is granted this one and told so in the token response
// (RFC 6749 §3.3); refusing others would break clients that send a default
// scope such as openid or offline_access without reading ours.
const oauthScope = "supervisor-mcp"

// challengeUnauthorized writes the RFC 9728 §5.1 challenge that tells an
// unauthenticated client exactly where to find our metadata, instead of making
// it guess well-known paths.
func challengeUnauthorized(w http.ResponseWriter, r *http.Request, message string) {
	id := oauthIdentityFor(r)
	w.Header().Set("WWW-Authenticate", fmt.Sprintf(
		`Bearer realm=%q, resource_metadata=%q, scope=%q`, id.resource, id.prmURL, oauthScope))
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

		// The consent page shows this name to the person, so a name that
		// could disguise itself is refused here, not rendered later.
		clientName, err := auth.ValidateDisplayName(payload["client_name"])
		if err != nil {
			registrationError(w, "invalid_client_metadata", "client_name: "+err.Error())
			return
		}
		if clientName != "" {
			payload["client_name"] = clientName
		}

		// The client_id IS a signed credential carrying the client's
		// registered redirect_uris, so /oauth2/authorize can later bind the
		// authorization request's redirect_uri to what this client actually
		// registered (RFC 6749 §3.1.2.3) instead of accepting any value.
		clientID, err := h.tokenSvc.CreateClientRegistrationToken(redirectURIs, clientName)
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
			"scopes_supported":         []string{oauthScope},
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
			"scopes_supported":         []string{oauthScope},
		})
	})
}

func (h *handler) oauthAuthorizeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var userID, email string
		if cookie, err := r.Cookie("at"); err == nil && cookie.Value != "" {
			if claims, err := h.tokenSvc.ValidateToken(cookie.Value); err == nil && claims != nil {
				userID = claims.Subject
				email = claims.Email
			}
		}

		clientID := r.URL.Query().Get("client_id")
		redirectURI := r.URL.Query().Get("redirect_uri")
		state := r.URL.Query().Get("state")

		// Who is asking. A client_id is registered one of two ways: a Client
		// ID Metadata Document URL (draft-ietf-oauth-client-id-metadata-
		// document), or a client_id minted by our own /oauth2/register (RFC
		// 7591 DCR). Either way it carries the redirect_uris it may use.
		var registeredRedirectURIs []string
		client := oauthconsent.Client{ID: clientID}
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
			client.Name, client.Kind = metadata.ClientName, oauthconsent.ClientMetadataDocument
		case clientID != "":
			if claims, err := h.tokenSvc.ValidateClientRegistrationToken(clientID); err == nil {
				registeredRedirectURIs = claims.RedirectURIs
				client.Name, client.Kind = claims.ClientName, oauthconsent.ClientRegistered
			}
		}

		// Bind the redirect_uri to what the client registered (RFC 6749
		// §3.1.2.3) before anything else, so a bad one is refused before the
		// person is sent through a login for nothing.
		redirectURI, err := oauthconsent.CheckRedirectURI(redirectURI, registeredRedirectURIs, h.baseURL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
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
			http.Redirect(w, r, oauthconsent.LoginURL(h.baseURL, returnURL), http.StatusFound)
			return
		}

		// Being signed in is not consent: the person sees who is asking and
		// for what, and the code is issued only when they press Allow.
		oauthconsent.Serve(w, r, h.tokenSvc, oauthconsent.Request{
			UserID:                 userID,
			Email:                  email,
			Client:                 client,
			RedirectURI:            redirectURI,
			RegisteredRedirectURIs: registeredRedirectURIs,
			BaseURL:                h.baseURL,
			State:                  state,
			Resource:               "coremcp",
			Access:                 "Act as your supervisor across every workspace on this account: create, change and fork workspaces, and read, create, change and delete their tasks.",
			Issue:                  func() (string, error) { return h.tokenSvc.CreateOAuthCodeToken(userID, "coremcp") },
			Decided: func(allowed bool) {
				h.crud.RecordOAuthConsent(r.Context(), entity.RecordOAuthConsentRequest{
					UserID:  monoflake.IDFromBase62(userID).Int64(),
					Allowed: allowed,
				})
			},
		})
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
			"scope":         oauthScope,
			"expires_in":    2592000, // 30 days
		})
	})
}
