// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/handler/oauthconsent/oauthconsenttest"
	"github.com/mustafaturan/monoflake"
)

// consentRouter is setupBindingRouter with the controller kept, so a test can
// see what the consent page recorded.
func consentRouter(tokenSvc *bindingTokenSvc) (*http.ServeMux, *mockCrud) {
	crud := &mockCrud{}
	mux := http.NewServeMux()
	New(Params{
		TokenSvc: tokenSvc,
		Crud:     crud,
		CIMD:     &fakeCIMD{},
		BaseURL:  "https://agentrq.com",
		Mux:      mux,
	})
	return mux, crud
}

func registeredClaudeCode() *bindingTokenSvc {
	return &bindingTokenSvc{
		registeredClientID: "registered-client",
		registeredURIs:     []string{"http://localhost/callback"},
		registeredName:     "Claude Code",
	}
}

// The reported bug: a signed-in person was given a code without being asked.
func TestAuthorize_SignedInPersonIsAskedFirst(t *testing.T) {
	mux, crud := consentRouter(registeredClaudeCode())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, authorizeRequest("registered-client", "http://localhost:3118/callback"))

	if w.Code != http.StatusOK {
		t.Fatalf("a signed-in authorize answered %d, want the consent page (200)", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "" {
		t.Fatalf("a signed-in authorize redirected to %q before the person answered", loc)
	}
	body := w.Body.String()
	for _, want := range []string{
		`data-test="consent-client">Claude Code<`,
		"Unverified name",
		"Work as the agent of the workspace “Release notes”",
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

func TestAuthorize_AllowIsCountedForTheWorkspace(t *testing.T) {
	mux, crud := consentRouter(registeredClaudeCode())
	req := authorizeRequest("registered-client", "http://localhost:3118/callback")

	w := oauthconsenttest.Allow(mux, req)

	if w.Code != http.StatusFound {
		t.Fatalf("Allow answered %d, want 302: %s", w.Code, w.Body.String())
	}
	loc, _ := url.Parse(w.Header().Get("Location"))
	if loc.Query().Get("code") == "" {
		t.Errorf("Allow sent back %q, want a code", loc)
	}
	if len(crud.consents) != 1 {
		t.Fatalf("Allow recorded %+v, want one consent", crud.consents)
	}
	got := crud.consents[0]
	wantUser := monoflake.IDFromBase62("user123").Int64()
	if !got.Allowed || got.UserID != wantUser || got.WorkspaceID != workspaceIDFromParam(req) {
		t.Errorf("Allow recorded %+v, want allowed by user %d for workspace %d", got, wantUser, workspaceIDFromParam(req))
	}
}

func TestAuthorize_DenyIssuesNoCode(t *testing.T) {
	mux, crud := consentRouter(registeredClaudeCode())

	w := oauthconsenttest.Deny(mux, authorizeRequest("registered-client", "http://localhost:3118/callback"))

	if w.Code != http.StatusFound {
		t.Fatalf("Deny answered %d, want 302", w.Code)
	}
	loc, _ := url.Parse(w.Header().Get("Location"))
	if loc.Query().Get("error") != "access_denied" || loc.Query().Has("code") {
		t.Errorf("Deny sent back %q, want error=access_denied and no code", loc.RawQuery)
	}
	if len(crud.consents) != 1 || crud.consents[0].Allowed {
		t.Errorf("Deny recorded %+v, want one deny", crud.consents)
	}
}

// The approval names the redirect it was shown for; posting it back with
// another redirect_uri in the URL must not deliver a code there.
func TestAuthorize_ApprovalCannotBeRedirectedElsewhere(t *testing.T) {
	mux, crud := consentRouter(&bindingTokenSvc{
		registeredClientID: "registered-client",
		registeredURIs:     []string{"http://localhost/callback", "http://localhost/other"},
	})

	page := httptest.NewRecorder()
	mux.ServeHTTP(page, authorizeRequest("registered-client", "http://localhost:3118/callback"))
	token := oauthconsenttest.Token(page.Body.String())

	w := oauthconsenttest.Post(mux, authorizeRequest("registered-client", "http://localhost:3118/other"),
		url.Values{"consent": {token}, "decision": {"allow"}})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("an approval for one redirect, posted for another, answered %d (%s), want 400", w.Code, w.Header().Get("Location"))
	}
	if len(crud.consents) != 0 {
		t.Errorf("the refused approval recorded %+v", crud.consents)
	}
}

func TestRegister_ClientName(t *testing.T) {
	register := func(t *testing.T, tokenSvc *bindingTokenSvc, body string) (*httptest.ResponseRecorder, map[string]any) {
		t.Helper()
		mux, _ := consentRouter(tokenSvc)
		req := httptest.NewRequest("POST", "/mcp/12345/oauth2/register", strings.NewReader(body))
		req.SetPathValue("workspaceID", "12345")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("the registration answer is not JSON: %v (%s)", err, w.Body.String())
		}
		return w, out
	}

	t.Run("a plain name is registered with its spacing tidied", func(t *testing.T) {
		tokenSvc := &bindingTokenSvc{}
		w, out := register(t, tokenSvc, `{"client_name":"  Claude   Code ","redirect_uris":["http://localhost/callback"]}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("registering answered %d, want 201: %s", w.Code, w.Body.String())
		}
		if out["client_name"] != "Claude Code" || tokenSvc.lastRegisteredName != "Claude Code" {
			t.Errorf("registered the name %q and answered %q, want Claude Code for both", tokenSvc.lastRegisteredName, out["client_name"])
		}
	})

	t.Run("a client with no name registers without one", func(t *testing.T) {
		tokenSvc := &bindingTokenSvc{}
		w, out := register(t, tokenSvc, `{"redirect_uris":["http://localhost/callback"]}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("registering answered %d, want 201", w.Code)
		}
		if _, has := out["client_name"]; has || tokenSvc.lastRegisteredName != "" {
			t.Errorf("a nameless client was given the name %q / %v", tokenSvc.lastRegisteredName, out["client_name"])
		}
	})

	for name, body := range map[string]string{
		"a right-to-left override": `{"client_name":"moc.elgoog‮","redirect_uris":["http://localhost/callback"]}`,
		"a newline":                `{"client_name":"Claude Code\nApproved by AgentRQ","redirect_uris":["http://localhost/callback"]}`,
		"a non-string name":        `{"client_name":{"en":"Claude Code"},"redirect_uris":["http://localhost/callback"]}`,
		"a name too long to show":  `{"client_name":"` + strings.Repeat("a", 81) + `","redirect_uris":["http://localhost/callback"]}`,
	} {
		t.Run("refused: "+name, func(t *testing.T) {
			w, out := register(t, &bindingTokenSvc{}, body)
			if w.Code != http.StatusBadRequest || out["error"] != "invalid_client_metadata" {
				t.Errorf("registering %s answered %d %v, want 400 invalid_client_metadata", name, w.Code, out)
			}
			if desc, _ := out["error_description"].(string); !strings.Contains(desc, "client_name") {
				t.Errorf("the refusal %q does not say the client_name is the problem", desc)
			}
		})
	}
}

func TestConsentWorkspaceName(t *testing.T) {
	for name, want := range map[string]string{
		"Release notes":           "the workspace “Release notes”",
		"  Release   notes ":      "the workspace “Release notes”",
		"":                        "workspace 4kc",
		"Release notes‮":          "workspace 4kc",
		"Approved by\nyour admin": "workspace 4kc",
	} {
		if got := consentWorkspaceName(name, "4kc"); got != want {
			t.Errorf("consentWorkspaceName(%q) returned %q, want %q", name, got, want)
		}
	}
}

// A client that registered one address may leave redirect_uri out (RFC 6749
// §3.1.2.3); the page then names that address, and the code goes there.
func TestAuthorize_OnlyRegisteredAddressIsTheDefault(t *testing.T) {
	mux, _ := consentRouter(&bindingTokenSvc{
		registeredClientID: "registered-client",
		registeredURIs:     []string{"https://client.example.com/callback"},
	})

	w := oauthconsenttest.Allow(mux, authorizeRequest("registered-client", ""))

	if w.Code != http.StatusFound || !strings.HasPrefix(w.Header().Get("Location"), "https://client.example.com/callback?") {
		t.Errorf("Allow with no redirect_uri answered %d to %q, want the one registered address", w.Code, w.Header().Get("Location"))
	}
}

func TestAuthorize_NoAddressAndNoRegistrationIsRefused(t *testing.T) {
	mux, _ := consentRouter(&bindingTokenSvc{})

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, authorizeRequest("unknown-client", ""))

	if w.Code != http.StatusBadRequest {
		t.Errorf("an authorize with nowhere to send the code answered %d, want 400", w.Code)
	}
}
