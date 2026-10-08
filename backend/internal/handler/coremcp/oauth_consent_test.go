// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/handler/oauthconsent/oauthconsenttest"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
)

const claudeCodeClientID = "https://claude.ai/oauth/claude-code-client-metadata"

func consentHandler() (*handler, *consentRecorder) {
	crud := &consentRecorder{}
	return &handler{
		crud:     crud,
		tokenSvc: authorizeTokenSvc{},
		baseURL:  "https://mcp.agentrq.com",
		cimd: &stubCIMD{metadata: map[string]*auth.ClientMetadata{
			claudeCodeClientID: {
				ClientName:   "Claude Code",
				RedirectURIs: []string{"http://localhost/callback"},
			},
		}},
	}, crud
}

func supervisorAuthorizeRequest() *http.Request {
	q := url.Values{
		"response_type": {"code"},
		"client_id":     {claudeCodeClientID},
		"redirect_uri":  {"http://localhost:3118/callback"},
		"state":         {"state-1"},
	}
	req := httptest.NewRequest("GET", "/mcp/oauth2/authorize?"+q.Encode(), nil)
	req.Host = "mcp.agentrq.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.AddCookie(&http.Cookie{Name: "at", Value: "valid-auth-cookie"})
	return req
}

// A signed-out person goes to the login page, which offers every sign-in the
// server has, and comes back here: sending them to Google's route left GitHub
// and root-token accounts unable to sign in.
func TestSupervisorAuthorize_SignedOutPersonGoesToTheLoginPage(t *testing.T) {
	h, _ := consentHandler()
	req := supervisorAuthorizeRequest()
	req.Header.Del("Cookie")

	w := httptest.NewRecorder()
	h.oauthAuthorizeHandler().ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("a signed-out authorize answered %d, want 302", w.Code)
	}
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("Location is not a URL: %v", err)
	}
	if got := loc.Scheme + "://" + loc.Host + loc.Path; got != "https://mcp.agentrq.com/login" {
		t.Errorf("a signed-out authorize went to %q, want the login page", got)
	}
	if got, want := loc.Query().Get("redirect_url"), "https://mcp.agentrq.com"+req.URL.RequestURI(); got != want {
		t.Errorf("redirect_url = %q, want %q", got, want)
	}
}

// The reported bug, on the supervisor: being signed in handed out a code for
// the whole account without a word.
func TestSupervisorAuthorize_SignedInPersonIsAskedFirst(t *testing.T) {
	h, crud := consentHandler()

	w := httptest.NewRecorder()
	h.oauthAuthorizeHandler().ServeHTTP(w, supervisorAuthorizeRequest())

	if w.Code != http.StatusOK {
		t.Fatalf("a signed-in authorize answered %d, want the consent page (200)", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "" {
		t.Fatalf("a signed-in authorize redirected to %q before the person answered", loc)
	}
	body := w.Body.String()
	for _, want := range []string{
		`data-test="consent-client">Claude Code<`,
		"published by <strong>claude.ai</strong>",
		"every workspace on this account",
		"an app on this computer (localhost:3118)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the consent page does not show %q", want)
		}
	}
	if len(crud.consents) != 0 {
		t.Errorf("showing the page recorded %+v, want nothing until the person answers", crud.consents)
	}
}

// A client registered here named itself, and the page says nobody checked.
func TestSupervisorAuthorize_RegisteredClientNameIsMarkedUnverified(t *testing.T) {
	h, _ := consentHandler()
	req := supervisorAuthorizeRequest()
	q := req.URL.Query()
	q.Set("client_id", "registered-client")
	req.URL.RawQuery = q.Encode()

	w := httptest.NewRecorder()
	h.oauthAuthorizeHandler().ServeHTTP(w, req)

	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, `data-test="consent-client">Cursor<`) || !strings.Contains(body, "Unverified name") {
		t.Errorf("a registered client's page answered %d and did not show Cursor as an unverified name", w.Code)
	}
}

func TestSupervisorAuthorize_AllowAndDenyAreCountedForTheAccount(t *testing.T) {
	for _, tc := range []struct {
		decision string
		allowed  bool
		param    string
	}{
		{"allow", true, "code"},
		{"deny", false, "error"},
	} {
		t.Run(tc.decision, func(t *testing.T) {
			h, crud := consentHandler()

			w := oauthconsenttest.Decide(h.oauthAuthorizeHandler(), supervisorAuthorizeRequest(), tc.decision)

			if w.Code != http.StatusFound {
				t.Fatalf("%s answered %d, want 302: %s", tc.decision, w.Code, w.Body.String())
			}
			loc, _ := url.Parse(w.Header().Get("Location"))
			if loc.Query().Get(tc.param) == "" {
				t.Errorf("%s sent back %q, want a %s", tc.decision, loc.RawQuery, tc.param)
			}
			if len(crud.consents) != 1 || crud.consents[0].Allowed != tc.allowed || crud.consents[0].WorkspaceID != 0 {
				t.Errorf("%s recorded %+v, want one answer, allowed=%v, for workspace 0 (the account)", tc.decision, crud.consents, tc.allowed)
			}
		})
	}
}

func TestSupervisorRegister_ClientName(t *testing.T) {
	register := func(body string) (*httptest.ResponseRecorder, map[string]any) {
		h, _ := consentHandler()
		w := httptest.NewRecorder()
		h.oauthRegisterHandler().ServeHTTP(w, httptest.NewRequest("POST", "/mcp/oauth2/register", strings.NewReader(body)))
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w, out
	}

	w, out := register(`{"client_name":" Claude  Code ","redirect_uris":["http://localhost/callback"]}`)
	if w.Code != http.StatusCreated || out["client_name"] != "Claude Code" {
		t.Errorf("registering a plain name answered %d with %v, want 201 and the name Claude Code", w.Code, out["client_name"])
	}

	w, out = register(`{"client_name":"moc.elgoog‮","redirect_uris":["http://localhost/callback"]}`)
	if w.Code != http.StatusBadRequest || out["error"] != "invalid_client_metadata" {
		t.Errorf("registering a disguised name answered %d with %v, want 400 invalid_client_metadata", w.Code, out)
	}
}
