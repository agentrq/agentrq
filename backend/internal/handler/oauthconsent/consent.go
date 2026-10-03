// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package oauthconsent is the page both MCP servers' /oauth2/authorize show a
// signed-in person before an app gets a code: which app is asking, what it
// will reach, and where it sends them back.
//
// The page is rendered here, not by the Vue app, so that nothing an agent can
// drive — WebMCP tools or the REST API — can press Allow on someone's behalf.
package oauthconsent

import (
	"crypto/rand"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/agentrq/agentrq/backend/internal/service/auth"
	zlog "github.com/rs/zerolog/log"
)

// ClientKind is how much the server knows about who is asking.
type ClientKind int

const (
	// ClientUnregistered is a client_id this server never issued and that is
	// not a metadata document URL: nothing about it is known.
	ClientUnregistered ClientKind = iota
	// ClientRegistered registered at this server's /oauth2/register. Its name
	// is whatever it chose to call itself.
	ClientRegistered
	// ClientMetadataDocument is a client_id that is an https URL serving the
	// client's metadata, so its host is the one thing about it that is proven.
	ClientMetadataDocument
)

// Client is the app asking for access.
type Client struct {
	ID   string
	Name string // validated by auth.ValidateDisplayName; "" when it gave none
	Kind ClientKind
}

// Request is one authorization request. The caller has checked that the
// person may grant Resource, and the redirect_uri with CheckRedirectURI;
// Serve checks the redirect_uri again before it sends anything there.
type Request struct {
	UserID string
	Email  string
	Client Client
	// RedirectURI is where the answer goes, as CheckRedirectURI returned it.
	RedirectURI string
	// RegisteredRedirectURIs and BaseURL are what CheckRedirectURI was given.
	RegisteredRedirectURIs []string
	BaseURL                string
	State                  string
	// Resource is the base62 workspace ID, or "coremcp" for the supervisor.
	Resource string
	// Access says, in a sentence, what the app will be able to do.
	Access string
	// Issue mints the authorization code once the person allows the app.
	Issue func() (string, error)
	// Decided is told what the person chose, after the redirect is written.
	Decided func(allowed bool)
}

const (
	DecisionAllow = "allow"
	DecisionDeny  = "deny"
)

// Serve shows the consent page on GET and acts on the person's choice on
// POST. The form posts back to the authorize URL itself, so the request's
// query string arrives again and the caller validates it again.
func Serve(w http.ResponseWriter, r *http.Request, tokens auth.TokenService, req Request) {
	redirectURI := req.RedirectURI
	if !isValidRedirect(redirectURI, req) {
		http.Error(w, ErrRedirectMalformed.Error(), http.StatusBadRequest)
		return
	}

	consent := auth.OAuthConsent{
		Resource:    req.Resource,
		ClientID:    req.Client.ID,
		RedirectURI: redirectURI,
		State:       req.State,
	}

	switch r.Method {
	case http.MethodGet, http.MethodHead:
		token, err := tokens.CreateOAuthConsentToken(req.UserID, consent)
		if err != nil {
			zlog.Error().Err(err).Msg("could not mint an OAuth consent token, so the consent page cannot be shown")
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		render(w, newPage(req, token))
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		if err := tokens.ValidateOAuthConsentToken(r.PostForm.Get("consent"), req.UserID, consent); err != nil {
			zlog.Warn().Err(err).Msg("OAuth consent refused: the posted token is expired, forged or for another request")
			http.Error(w, "This approval has expired or does not match this request. Go back to the app and connect again.", http.StatusBadRequest)
			return
		}
		switch r.PostForm.Get("decision") {
		case DecisionAllow:
			code, err := req.Issue()
			if err != nil {
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			http.Redirect(w, r, RedirectURL(redirectURI, url.Values{"code": {code}, "state": {req.State}}), http.StatusFound)
			decided(req, true)
		case DecisionDeny:
			// RFC 6749 §4.1.2.1: the client learns the person said no.
			http.Redirect(w, r, RedirectURL(redirectURI, url.Values{"error": {"access_denied"}, "state": {req.State}}), http.StatusFound)
			decided(req, false)
		default:
			http.Error(w, "decision must be allow or deny", http.StatusBadRequest)
		}
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func decided(req Request, allowed bool) {
	if req.Decided != nil {
		req.Decided(allowed)
	}
}

// RedirectURL adds params to the client's redirect_uri, keeping any query it
// registered with (RFC 6749 §3.1.2).
func RedirectURL(redirectURI string, params url.Values) string {
	separator := "?"
	if strings.Contains(redirectURI, "?") {
		separator = "&"
	}
	return redirectURI + separator + params.Encode()
}

type page struct {
	ClientName  string
	Named       bool
	Kind        ClientKind
	ClientHost  string
	Destination string
	Access      string
	Email       string
	Token       string
	Nonce       string
}

func newPage(req Request, token string) page {
	p := page{
		ClientName:  req.Client.Name,
		Named:       req.Client.Name != "",
		Kind:        req.Client.Kind,
		Destination: destination(req.RedirectURI),
		Access:      req.Access,
		Email:       req.Email,
		Token:       token,
	}
	if req.Client.Kind == ClientMetadataDocument {
		// The host is the one proven thing about such a client. One the page
		// cannot show truthfully proves nothing, so the client is unknown.
		host, ok := "", false
		if u, err := url.Parse(req.Client.ID); err == nil {
			host, ok = displayHost(u)
		}
		if !ok {
			p.Kind = ClientUnregistered
			return p
		}
		p.ClientHost = host
		if !p.Named {
			p.ClientName, p.Named = host, true
		}
	}
	return p
}

// destination says where the code will go, in the words the page uses. The
// address has passed CheckRedirectURI.
func destination(redirectURI string) string {
	u, _ := url.Parse(redirectURI)
	switch {
	case !u.IsAbs():
		return "this AgentRQ server"
	case !isWeb(u):
		return "the app that opens " + u.Scheme + ": links"
	}
	host, _ := displayHost(u)
	if isLoopback(u) {
		return "an app on this computer (" + host + ")"
	}
	return host
}

func render(w http.ResponseWriter, p page) {
	nonce := rand.Text()
	p.Nonce = nonce

	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	// The page holds a live approval: never cached, never framed (a framed
	// Allow button can be clickjacked), and no scripts but the theme one.
	h.Set("Cache-Control", "no-store")
	h.Set("Pragma", "no-cache")
	h.Set("X-Frame-Options", "DENY")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'nonce-"+nonce+"'; base-uri 'none'; frame-ancestors 'none'")

	if err := pageTemplate.Execute(w, p); err != nil {
		zlog.Error().Err(err).Msg("could not render the OAuth consent page")
	}
}

var pageTemplate = template.Must(template.New("consent").Funcs(template.FuncMap{
	"metadataDocument": func(k ClientKind) bool { return k == ClientMetadataDocument },
	"registered":       func(k ClientKind) bool { return k == ClientRegistered },
}).Parse(pageHTML))
