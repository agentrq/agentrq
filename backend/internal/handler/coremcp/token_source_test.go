package coremcp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Which credential a request is judged on, when it carries more than one.
//
// A session-aware client sends its cookie with everything, so a Bearer token
// and an `at` cookie arrive together routinely. Reading the cookie first meant
// the token was ignored — and a session cookie carries the "actor:human"
// audience, never "coremcp", so the call was refused with credentials that were
// in the request all along. That is the desktop app's supervisor connection:
// "AgentRQ could not open a session: the server did not accept this app's
// credentials", immediately after a successful authorisation.
func TestGetTokenVal_PrefersTheCredentialTheClientNamed(t *testing.T) {
	tests := []struct {
		name   string
		build  func(*http.Request)
		expect string
	}{
		{
			name: "the header wins over a cookie riding along with it",
			build: func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer coremcp-token")
				r.AddCookie(&http.Cookie{Name: "at", Value: "session-cookie"})
			},
			expect: "coremcp-token",
		},
		{
			name:   "the cookie still answers for a browser that sends no header",
			build:  func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "at", Value: "session-cookie"}) },
			expect: "session-cookie",
		},
		{
			name:   "the header alone",
			build:  func(r *http.Request) { r.Header.Set("Authorization", "Bearer coremcp-token") },
			expect: "coremcp-token",
		},
		{
			// The query parameter is for transports that cannot set a header;
			// a client using it has named its credential just as plainly.
			name: "the query parameter comes before both",
			build: func(r *http.Request) {
				r.URL.RawQuery = "token=query-token"
				r.Header.Set("Authorization", "Bearer coremcp-token")
				r.AddCookie(&http.Cookie{Name: "at", Value: "session-cookie"})
			},
			expect: "query-token",
		},
		{
			// Not a Bearer credential, so not one this reads — the cookie is
			// then the only thing the request actually presents.
			name: "a non-Bearer header is not mistaken for one",
			build: func(r *http.Request) {
				r.Header.Set("Authorization", "Basic abc")
				r.AddCookie(&http.Cookie{Name: "at", Value: "session-cookie"})
			},
			expect: "session-cookie",
		},
		{
			name:   "nothing presented at all",
			build:  func(r *http.Request) {},
			expect: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/mcp", nil)
			tt.build(req)

			if got := getTokenVal(req); got != tt.expect {
				t.Fatalf("getTokenVal() = %q, want %q", got, tt.expect)
			}
		})
	}
}
