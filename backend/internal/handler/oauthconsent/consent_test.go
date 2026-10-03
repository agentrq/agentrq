// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package oauthconsent

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/handler/oauthconsent/oauthconsenttest"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
)

var errSigningKeyUnavailable = errors.New("signing key unavailable")

// consentTokens signs consent tokens for real; failMint makes minting one
// fail the way a broken signer would.
type consentTokens struct {
	auth.TokenService
	failMint bool
}

func (c consentTokens) CreateOAuthConsentToken(userID string, consent auth.OAuthConsent) (string, error) {
	if c.failMint {
		return "", errSigningKeyUnavailable
	}
	return c.TokenService.CreateOAuthConsentToken(userID, consent)
}

func newTokens() consentTokens {
	return consentTokens{TokenService: auth.NewTokenService(auth.TokenConfig{JWTSecret: "consent-test-secret"})}
}

// outcome is what the authorize handler was told to do.
type outcome struct {
	issued   int
	decision []bool
}

func newRequest(o *outcome) Request {
	return Request{
		UserID:      "user-1",
		Email:       "ada@example.com",
		Client:      Client{ID: "client-1", Name: "Claude Code", Kind: ClientRegistered},
		RedirectURI: "http://localhost:3118/callback",
		State:       "state-1",
		Resource:    "ws1",
		Access:      "Work as the agent of the workspace “Release notes”.",
		Issue: func() (string, error) {
			o.issued++
			return "code-1", nil
		},
		Decided: func(allowed bool) { o.decision = append(o.decision, allowed) },
	}
}

// handlerFor serves req as an authorize handler would once it has validated
// the request.
func handlerFor(tokens auth.TokenService, req Request) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { Serve(w, r, tokens, req) })
}

func authorizeGET() *http.Request {
	return httptest.NewRequest(http.MethodGet, "/mcp/oauth2/authorize?client_id=client-1&state=state-1", nil)
}

func showPage(t *testing.T, tokens auth.TokenService, req Request) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	handlerFor(tokens, req).ServeHTTP(w, authorizeGET())
	if w.Code != http.StatusOK {
		t.Fatalf("the consent page answered %d, want 200: %s", w.Code, w.Body.String())
	}
	return w
}

func TestConsentPageShowsWhoIsAskingAndForWhat(t *testing.T) {
	var o outcome
	req := newRequest(&o)
	w := showPage(t, newTokens(), req)
	body := w.Body.String()

	for _, want := range []string{
		`data-test="consent-client">Claude Code<`,
		"Unverified name",
		"Work as the agent of the workspace “Release notes”.",
		"an app on this computer (localhost:3118)",
		"ada@example.com",
		`value="allow"`,
		`value="deny"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the consent page does not show %q", want)
		}
	}
	if o.issued != 0 || len(o.decision) != 0 {
		t.Errorf("showing the page issued %d codes and recorded %v, want neither", o.issued, o.decision)
	}

	token := oauthconsenttest.Token(body)
	if err := newTokens().ValidateOAuthConsentToken(token, "user-1", auth.OAuthConsent{
		Resource: "ws1", ClientID: "client-1", RedirectURI: req.RedirectURI, State: "state-1",
	}); err != nil {
		t.Errorf("the page carries a consent token that does not approve this request: %v", err)
	}
}

func TestConsentPageCannotBeFramedCachedOrScripted(t *testing.T) {
	var o outcome
	w := showPage(t, newTokens(), newRequest(&o))

	for header, want := range map[string]string{
		"Content-Type":           "text/html; charset=utf-8",
		"Cache-Control":          "no-store",
		"Pragma":                 "no-cache",
		"X-Frame-Options":        "DENY",
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
	} {
		if got := w.Header().Get(header); got != want {
			t.Errorf("%s is %q, want %q", header, got, want)
		}
	}

	csp := w.Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "frame-ancestors 'none'", "base-uri 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("the Content-Security-Policy %q lacks %q", csp, want)
		}
	}
	// form-action would also govern the redirect Allow answers with, and the
	// client's redirect_uri is on another origin, so it must stay unset.
	if strings.Contains(csp, "form-action") {
		t.Errorf("the Content-Security-Policy %q restricts form-action, which would block the redirect back to the app", csp)
	}

	nonce := regexp.MustCompile(`script-src 'nonce-([^']+)'`).FindStringSubmatch(csp)
	if nonce == nil {
		t.Fatalf("the Content-Security-Policy %q allows no script by nonce", csp)
	}
	if !strings.Contains(w.Body.String(), `<script nonce="`+nonce[1]+`">`) {
		t.Errorf("the theme script does not carry the nonce %q the policy allows", nonce[1])
	}

	again := showPage(t, newTokens(), newRequest(&o))
	if again.Header().Get("Content-Security-Policy") == csp {
		t.Error("two renders of the page used the same script nonce")
	}
}

func TestConsentPageEscapesTheClientName(t *testing.T) {
	var o outcome
	req := newRequest(&o)
	req.Client.Name = `<img src=x onerror=alert(1)>`
	body := showPage(t, newTokens(), req).Body.String()

	if strings.Contains(body, "<img src=x") {
		t.Fatal("the client's name reached the page as markup")
	}
	if !strings.Contains(body, "&lt;img src=x onerror=alert(1)&gt;") {
		t.Error("the client's name is not shown as text")
	}
}

func TestConsentPageSaysHowMuchIsKnownAboutTheClient(t *testing.T) {
	cases := []struct {
		name   string
		client Client
		title  string
		badge  string
	}{
		{
			name:   "a metadata document names its host as checked",
			client: Client{ID: "https://claude.ai/oauth/claude-code-client-metadata", Name: "Claude Code", Kind: ClientMetadataDocument},
			title:  "Claude Code",
			badge:  "published by <strong>claude.ai</strong>",
		},
		{
			name:   "a metadata document without a name is called by its host",
			client: Client{ID: "https://tools.example.com/client.json", Kind: ClientMetadataDocument},
			title:  "tools.example.com",
			badge:  "published by <strong>tools.example.com</strong>",
		},
		{
			name:   "a lookalike host is shown in its ASCII form",
			client: Client{ID: "https://g\u043e\u043egle.com/client.json", Name: "Google", Kind: ClientMetadataDocument},
			title:  "Google",
			badge:  "published by <strong>xn--ggle-55da.com</strong>",
		},
		{
			name:   "a host the page cannot show makes the client unknown",
			client: Client{ID: "https://exa_mple.com/client.json", Name: "Example", Kind: ClientMetadataDocument},
			title:  "Example",
			badge:  "Unknown app",
		},
		{
			name:   "a registered client without a name",
			client: Client{ID: "client-1", Kind: ClientRegistered},
			title:  "An unnamed app",
			badge:  "Unverified name",
		},
		{
			name:   "a client this server never heard of",
			client: Client{ID: "who-knows"},
			title:  "An unnamed app",
			badge:  "Unknown app",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var o outcome
			req := newRequest(&o)
			req.Client = tc.client
			body := showPage(t, newTokens(), req).Body.String()
			if !strings.Contains(body, `data-test="consent-client">`+tc.title+`<`) {
				t.Errorf("the page does not title the client %q", tc.title)
			}
			if !strings.Contains(body, tc.badge) {
				t.Errorf("the page does not say %q", tc.badge)
			}
		})
	}
}

func TestConsentPageLeavesOutAnUnknownAccount(t *testing.T) {
	var o outcome
	req := newRequest(&o)
	req.Email = ""
	if body := showPage(t, newTokens(), req).Body.String(); strings.Contains(body, "Signed in as") {
		t.Error("the page shows a 'Signed in as' row with no account in it")
	}
}

func TestDestination(t *testing.T) {
	for redirectURI, want := range map[string]string{
		"http://localhost:3118/callback":  "an app on this computer (localhost:3118)",
		"http://127.0.0.1/callback":       "an app on this computer (127.0.0.1)",
		"http://[::1]:9000/callback":      "an app on this computer ([::1]:9000)",
		"https://client.example.com/cb":   "client.example.com",
		"https://Client.Example.COM/cb":   "client.example.com",
		"https://localhost.evil.com/cb":   "localhost.evil.com",
		"https://g\u043e\u043egle.com/cb": "xn--ggle-55da.com",
		"cursor://anysphere.cursor/oauth": "the app that opens cursor: links",
		"/settings/callback":              "this AgentRQ server",
	} {
		if got := destination(redirectURI); got != want {
			t.Errorf("destination(%q) returned %q, want %q", redirectURI, got, want)
		}
	}
}

// Serve is the last stop before the code leaves, so it checks the address
// itself instead of trusting that its caller did.
func TestServeRefusesAnAddressItCannotValidate(t *testing.T) {
	for name, redirectURI := range map[string]string{
		"another site":             "https://evil.example.com/callback",
		"an unknown custom scheme": "evilapp://callback",
		"no address at all":        "",
	} {
		t.Run(name, func(t *testing.T) {
			var o outcome
			req := newRequest(&o)
			req.RedirectURI = redirectURI
			req.BaseURL = "https://agentrq.com"

			page := httptest.NewRecorder()
			handlerFor(newTokens(), req).ServeHTTP(page, authorizeGET())
			if page.Code != http.StatusBadRequest {
				t.Errorf("the page for %q answered %d, want 400", redirectURI, page.Code)
			}

			post := oauthconsenttest.Post(handlerFor(newTokens(), req), authorizeGET(), url.Values{"decision": {DecisionAllow}})
			if post.Code != http.StatusBadRequest || post.Header().Get("Location") != "" {
				t.Errorf("Allow for %q answered %d to %q, want 400 and no redirect", redirectURI, post.Code, post.Header().Get("Location"))
			}
			if o.issued != 0 || len(o.decision) != 0 {
				t.Errorf("an unvalidated address issued %d codes and recorded %v", o.issued, o.decision)
			}
		})
	}
}

// The page re-checks against the same registration the caller used.
func TestServeAcceptsARegisteredAddress(t *testing.T) {
	var o outcome
	req := newRequest(&o)
	req.RedirectURI = "https://client.example.com/callback"
	req.RegisteredRedirectURIs = []string{"https://client.example.com/callback"}
	if w := oauthconsenttest.Allow(handlerFor(newTokens(), req), authorizeGET()); w.Code != http.StatusFound {
		t.Fatalf("Allow for a registered address answered %d, want 302: %s", w.Code, w.Body.String())
	}
}

func TestAllowIssuesACodeAndSendsItBack(t *testing.T) {
	var o outcome
	w := oauthconsenttest.Allow(handlerFor(newTokens(), newRequest(&o)), authorizeGET())

	if w.Code != http.StatusFound {
		t.Fatalf("Allow answered %d, want 302: %s", w.Code, w.Body.String())
	}
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if loc.Host != "localhost:3118" || loc.Path != "/callback" {
		t.Errorf("Allow sent the person to %q, want the app's callback", loc)
	}
	if loc.Query().Get("code") != "code-1" || loc.Query().Get("state") != "state-1" {
		t.Errorf("Allow sent back %q, want code code-1 and state state-1", loc.RawQuery)
	}
	if o.issued != 1 || len(o.decision) != 1 || !o.decision[0] {
		t.Errorf("Allow issued %d codes and recorded %v, want one code and one allow", o.issued, o.decision)
	}
}

func TestAllowWithoutADecidedCallback(t *testing.T) {
	var o outcome
	req := newRequest(&o)
	req.Decided = nil
	if w := oauthconsenttest.Allow(handlerFor(newTokens(), req), authorizeGET()); w.Code != http.StatusFound {
		t.Fatalf("Allow answered %d with no Decided callback, want 302", w.Code)
	}
}

func TestDenyTellsTheAppNo(t *testing.T) {
	var o outcome
	w := oauthconsenttest.Deny(handlerFor(newTokens(), newRequest(&o)), authorizeGET())

	if w.Code != http.StatusFound {
		t.Fatalf("Deny answered %d, want 302: %s", w.Code, w.Body.String())
	}
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if loc.Query().Get("error") != "access_denied" || loc.Query().Get("state") != "state-1" || loc.Query().Has("code") {
		t.Errorf("Deny sent back %q, want error=access_denied with the state and no code", loc.RawQuery)
	}
	if o.issued != 0 || len(o.decision) != 1 || o.decision[0] {
		t.Errorf("Deny issued %d codes and recorded %v, want no code and one deny", o.issued, o.decision)
	}
}

func TestAPostWithoutAValidConsentTokenIssuesNothing(t *testing.T) {
	tokens := newTokens()
	var o outcome
	req := newRequest(&o)

	otherPerson := req
	otherPerson.UserID = "user-2"
	otherPersonsToken := oauthconsenttest.Token(showPage(t, tokens, otherPerson).Body.String())

	otherState := req
	otherState.State = "state-2"
	otherStatesToken := oauthconsenttest.Token(showPage(t, tokens, otherState).Body.String())

	for name, token := range map[string]string{
		"no token":                          "",
		"a made-up token":                   "not-a-token",
		"the token shown to someone else":   otherPersonsToken,
		"the token shown for another state": otherStatesToken,
	} {
		t.Run(name, func(t *testing.T) {
			w := oauthconsenttest.Post(handlerFor(tokens, req), authorizeGET(), url.Values{"consent": {token}, "decision": {DecisionAllow}})
			if w.Code != http.StatusBadRequest {
				t.Errorf("Allow with %s answered %d, want 400", name, w.Code)
			}
			if !strings.Contains(w.Body.String(), "connect again") {
				t.Errorf("the refusal %q does not tell the person what to do", w.Body.String())
			}
		})
	}
	if o.issued != 0 || len(o.decision) != 0 {
		t.Errorf("refused posts issued %d codes and recorded %v, want neither", o.issued, o.decision)
	}
}

func TestAnUnknownDecisionIsRefused(t *testing.T) {
	var o outcome
	w := oauthconsenttest.Decide(handlerFor(newTokens(), newRequest(&o)), authorizeGET(), "maybe")
	if w.Code != http.StatusBadRequest {
		t.Errorf("the decision 'maybe' answered %d, want 400", w.Code)
	}
	if o.issued != 0 || len(o.decision) != 0 {
		t.Errorf("an unknown decision issued %d codes and recorded %v, want neither", o.issued, o.decision)
	}
}

func TestAMalformedFormIsRefused(t *testing.T) {
	var o outcome
	post := httptest.NewRequest(http.MethodPost, "/mcp/oauth2/authorize", strings.NewReader("consent=%zz"))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handlerFor(newTokens(), newRequest(&o)).ServeHTTP(w, post)
	if w.Code != http.StatusBadRequest {
		t.Errorf("a malformed form answered %d, want 400", w.Code)
	}
}

func TestOtherMethodsAreNotAllowed(t *testing.T) {
	var o outcome
	w := httptest.NewRecorder()
	handlerFor(newTokens(), newRequest(&o)).ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/mcp/oauth2/authorize", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT answered %d, want 405", w.Code)
	}
	if got := w.Header().Get("Allow"); got != "GET, HEAD, POST" {
		t.Errorf("Allow header is %q, want GET, HEAD, POST", got)
	}
}

func TestHeadShowsThePageToo(t *testing.T) {
	var o outcome
	w := httptest.NewRecorder()
	handlerFor(newTokens(), newRequest(&o)).ServeHTTP(w, httptest.NewRequest(http.MethodHead, "/mcp/oauth2/authorize", nil))
	if w.Code != http.StatusOK {
		t.Errorf("HEAD answered %d, want 200", w.Code)
	}
}

func TestAFailedConsentTokenShowsNoPage(t *testing.T) {
	var o outcome
	tokens := newTokens()
	tokens.failMint = true
	w := httptest.NewRecorder()
	handlerFor(tokens, newRequest(&o)).ServeHTTP(w, authorizeGET())
	if w.Code != http.StatusInternalServerError {
		t.Errorf("a failed consent token answered %d, want 500", w.Code)
	}
	if strings.Contains(w.Body.String(), "consent-allow") {
		t.Error("the page was shown without a consent token")
	}
}

func TestAFailedCodeIsNotCountedAsAllowed(t *testing.T) {
	var o outcome
	req := newRequest(&o)
	req.Issue = func() (string, error) { return "", errSigningKeyUnavailable }
	w := oauthconsenttest.Allow(handlerFor(newTokens(), req), authorizeGET())
	if w.Code != http.StatusInternalServerError {
		t.Errorf("a failed code answered %d, want 500", w.Code)
	}
	if len(o.decision) != 0 {
		t.Errorf("a failed code recorded %v, want nothing", o.decision)
	}
}

// brokenWriter fails every write, as a connection the person closed does.
type brokenWriter struct{ *httptest.ResponseRecorder }

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("socket closed") }

func TestAPageThatCannotBeWrittenDoesNotPanic(t *testing.T) {
	var o outcome
	w := brokenWriter{httptest.NewRecorder()}
	Serve(w, authorizeGET(), newTokens(), newRequest(&o))
	if w.Code != http.StatusOK {
		t.Errorf("the status before the failed write was %d, want 200", w.Code)
	}
}

func TestRedirectURL(t *testing.T) {
	for redirectURI, want := range map[string]string{
		"http://localhost:3118/callback":      "http://localhost:3118/callback?code=c&state=s",
		"https://app.example.com/cb?tenant=7": "https://app.example.com/cb?tenant=7&code=c&state=s",
		"cursor://anysphere.cursor/oauth?x=1": "cursor://anysphere.cursor/oauth?x=1&code=c&state=s",
	} {
		if got := RedirectURL(redirectURI, url.Values{"code": {"c"}, "state": {"s"}}); got != want {
			t.Errorf("RedirectURL(%q) returned %q, want %q", redirectURI, got, want)
		}
	}
}
