// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package enrol

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testMachine() Machine {
	return Machine{Name: "build-box", Hostname: "bb01", OS: "linux", Arch: "arm64", Version: "0.7.0"}
}

func TestEnrolExchangesACodeForAToken(t *testing.T) {
	var got Request
	var headers http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != Path {
			t.Errorf("path = %s, want %s", r.URL.Path, Path)
		}
		headers = r.Header.Clone()
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("request body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(Response{MachineID: "m-1", MachineToken: "tok-1"})
	}))
	defer srv.Close()

	out, err := Enrol(t.Context(), srv.Client(), srv.URL, "ABCD-1234", testMachine())
	if err != nil {
		t.Fatalf("Enrol: %v", err)
	}
	if out.MachineID != "m-1" || out.MachineToken != "tok-1" {
		t.Errorf("response = %+v", out)
	}
	if got.Code != "ABCD-1234" || got.Hostname != "bb01" || got.Arch != "arm64" {
		t.Errorf("request = %+v", got)
	}
	// Sent from the first request onwards, so a server can answer an old
	// daemon differently and we can see what is deployed.
	if headers.Get("X-AgentRQ-Version") != "0.7.0" {
		t.Errorf("version header = %q", headers.Get("X-AgentRQ-Version"))
	}
	if headers.Get("X-AgentRQ-Protocol") == "" {
		t.Error("protocol version header missing")
	}
}

// The code is read off a screen and typed by a person. "abcd-1234" failing
// where ABCD-1234 works is an error message nobody deserves.
func TestCodeIsNormalisedBeforeSending(t *testing.T) {
	var got Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(Response{MachineID: "m", MachineToken: "t"})
	}))
	defer srv.Close()

	for _, typed := range []string{"abcd-1234", "  ABCD-1234  ", "AbCd-1234\n"} {
		if _, err := Enrol(t.Context(), srv.Client(), srv.URL, typed, testMachine()); err != nil {
			t.Fatalf("Enrol(%q): %v", typed, err)
		}
		if got.Code != "ABCD-1234" {
			t.Errorf("typed %q -> sent %q, want ABCD-1234", typed, got.Code)
		}
	}
}

func TestEnrolDistinguishesARejectedCodeFromAServerFault(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"bad code", http.StatusBadRequest, `{"error":"code expired"}`, ErrCodeRejected},
		{"unauthorised", http.StatusUnauthorized, `{"error":"unknown code"}`, ErrCodeRejected},
		{"not found", http.StatusNotFound, ``, ErrCodeRejected},
		{"server error", http.StatusInternalServerError, `{"error":"database down"}`, ErrServer},
		{"bad gateway", http.StatusBadGateway, `<html>proxy</html>`, ErrServer},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			_, err := Enrol(t.Context(), srv.Client(), srv.URL, "ABCD-1234", testMachine())
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

// A person debugging an enrolment needs whatever the server actually said.
func TestServerReasonReachesTheUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"this code was already used"}`)
	}))
	defer srv.Close()

	_, err := Enrol(t.Context(), srv.Client(), srv.URL, "ABCD-1234", testMachine())
	if err == nil || !strings.Contains(err.Error(), "already used") {
		t.Errorf("error does not carry the server's reason: %v", err)
	}
}

func TestServerMessageFallsBackToRawText(t *testing.T) {
	tests := []struct{ in, want string }{
		{`{"error":"a"}`, "a"},
		{`{"message":"b"}`, "b"},
		{`plain text`, "plain text"},
		{``, "no reason given"},
		{`   `, "no reason given"},
		{strings.Repeat("x", 500), strings.Repeat("x", 200) + "…"},
	}
	for _, tc := range tests {
		if got := serverMessage([]byte(tc.in)); got != tc.want {
			t.Errorf("serverMessage(%.20q) = %.30q, want %.30q", tc.in, got, tc.want)
		}
	}
}

func TestEnrolRejectsAnEmptyCodeWithoutCallingTheServer(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer srv.Close()

	if _, err := Enrol(t.Context(), srv.Client(), srv.URL, "   ", testMachine()); !errors.Is(err, ErrCodeRejected) {
		t.Errorf("error = %v, want ErrCodeRejected", err)
	}
	if called {
		t.Error("an empty code should not reach the server")
	}
}

func TestEnrolRejectsAnIncompleteAnswer(t *testing.T) {
	tests := []struct{ name, body string }{
		{"not json", `{oh dear`},
		{"no token", `{"machineId":"m-1"}`},
		{"no machine id", `{"machineToken":"tok"}`},
		{"empty object", `{}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			if _, err := Enrol(t.Context(), srv.Client(), srv.URL, "ABCD-1234", testMachine()); !errors.Is(err, ErrMalformed) {
				t.Errorf("error = %v, want ErrMalformed", err)
			}
		})
	}
}

// An unbounded read from a server we do not yet trust is an easy way to be
// handed a gigabyte.
func TestResponseReadIsBounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 4096; i++ {
			_, _ = io.WriteString(w, strings.Repeat("x", 1024))
		}
	}))
	defer srv.Close()

	// 4 MiB of junk: the read stops at the cap and the parse fails, rather
	// than the daemon buffering whatever it is sent.
	if _, err := Enrol(t.Context(), srv.Client(), srv.URL, "ABCD-1234", testMachine()); !errors.Is(err, ErrMalformed) {
		t.Errorf("error = %v, want ErrMalformed", err)
	}
}

func TestEnrolReportsAnUnreachableServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening now

	_, err := Enrol(t.Context(), http.DefaultClient, url, "ABCD-1234", testMachine())
	if err == nil {
		t.Fatal("expected a failure")
	}
	// The address belongs in the message: "connection refused" on its own does
	// not tell someone which server they got wrong.
	if !strings.Contains(err.Error(), url) {
		t.Errorf("error does not name the server: %v", err)
	}
}

func TestEnrolHonoursContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	if _, err := Enrol(ctx, srv.Client(), srv.URL, "ABCD-1234", testMachine()); err == nil {
		t.Error("a cancelled context should stop the request")
	}
}

func TestEnrolToleratesATrailingSlashOnTheServerURL(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewEncoder(w).Encode(Response{MachineID: "m", MachineToken: "t"})
	}))
	defer srv.Close()

	if _, err := Enrol(t.Context(), srv.Client(), srv.URL+"/", "ABCD-1234", testMachine()); err != nil {
		t.Fatalf("Enrol: %v", err)
	}
	if path != Path {
		t.Errorf("path = %q, want %q — a doubled slash means a 404 nobody can explain", path, Path)
	}
}
