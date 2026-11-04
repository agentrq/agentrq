// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package oauthconsenttest

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// page stands in for an authorize handler: GET shows a consent form with the
// access chosen as checked, and POST echoes what it was sent.
func page(checked string, status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(status)
			radio := ""
			if checked != "" {
				radio = `<input type="radio" name="access" value="` + checked + `" checked>`
			}
			_, _ = w.Write([]byte(`<input type="hidden" name="consent" value="tok">` + radio))
			return
		}
		_ = r.ParseForm()
		cookie, _ := r.Cookie("at")
		got := url.Values{
			"decision": {r.PostForm.Get("decision")},
			"consent":  {r.PostForm.Get("consent")},
			"access":   {r.PostForm.Get("access")},
			"host":     {r.Host},
			"proto":    {r.Header.Get("X-Forwarded-Proto")},
			"ws":       {r.PathValue("workspaceID")},
		}
		if cookie != nil {
			got.Set("cookie", cookie.Value)
		}
		_, _ = w.Write([]byte(got.Encode()))
	})
}

func get() *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/mcp/oauth2/authorize?state=s", nil)
	r.Host = "mcp.example"
	r.Header.Set("X-Forwarded-Proto", "https")
	r.AddCookie(&http.Cookie{Name: "at", Value: "session"})
	r.SetPathValue("workspaceID", "ws1")
	return r
}

func posted(t *testing.T, w *httptest.ResponseRecorder) url.Values {
	t.Helper()
	v, err := url.ParseQuery(w.Body.String())
	if err != nil {
		t.Fatalf("the post answered %q", w.Body.String())
	}
	return v
}

// A browser submits the page's token, its button, and the access the page
// has chosen, from the same host with the same cookies.
func TestDecidePostsWhatTheBrowserWould(t *testing.T) {
	for name, tc := range map[string]struct {
		w                *httptest.ResponseRecorder
		decision, access string
	}{
		"allow":                 {Allow(page("rw", 200), get()), "allow", "rw"},
		"deny":                  {Deny(page("rw", 200), get()), "deny", "rw"},
		"allow as":              {AllowAs(page("rw", 200), get(), "ro"), "allow", "ro"},
		"a page with no choice": {Allow(page("", 200), get()), "allow", ""},
	} {
		v := posted(t, tc.w)
		want := url.Values{"decision": {tc.decision}, "consent": {"tok"}, "access": {tc.access}, "host": {"mcp.example"}, "proto": {"https"}, "ws": {"ws1"}, "cookie": {"session"}}
		if v.Encode() != want.Encode() {
			t.Errorf("%s posted %v, want %v", name, v, want)
		}
	}
}

// A GET that does not show the page is returned as it is.
func TestDecideReturnsAnythingButThePage(t *testing.T) {
	if w := Allow(page("rw", http.StatusFound), get()); w.Code != http.StatusFound {
		t.Errorf("a redirect came back as %d", w.Code)
	}
}

// A request with no workspace or proxy header posts none.
func TestPostWithoutPathValueOrProxy(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/mcp/oauth2/authorize", nil)
	v := posted(t, Post(page("", 200), r, url.Values{"decision": {"deny"}}))
	if v.Get("ws") != "" || v.Get("proto") != "" || v.Get("cookie") != "" {
		t.Errorf("posted %v", v)
	}
}
