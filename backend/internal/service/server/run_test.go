// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package server

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// retryUntil calls try until it succeeds or 10s pass, for a server Run is
// still bringing up on its own goroutine.
func retryUntil(t *testing.T, what string, try func() error) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		err := try()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %v", what, err)
		}
		select {
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func shutdown(t *testing.T, s *service, done chan error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("Run returned %v, want http.ErrServerClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after Shutdown")
	}
}

// Without SSL, Run serves plain HTTP on the configured port until Shutdown.
func TestRunServesPlainHTTP(t *testing.T) {
	port := freePort(t)
	svc, err := New(Params{
		Config: Config{Port: port},
		Router: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }),
	})
	if err != nil {
		t.Fatal(err)
	}
	s := svc.(*service)
	done := make(chan error, 1)
	go func() { done <- s.Run() }()

	retryUntil(t, "GET over plain HTTP", func() error {
		res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
		if err != nil {
			return err
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != http.StatusOK || string(body) != "ok" {
			t.Fatalf("got %d %q, want 200 \"ok\"", res.StatusCode, body)
		}
		return nil
	})

	shutdown(t, s, done)
}

func TestRunSSLSetupFailures(t *testing.T) {
	t.Run("no domain", func(t *testing.T) {
		s := &service{cfg: Config{SSLEnabled: true}}
		err := s.Run()
		if err == nil || !strings.Contains(err.Error(), "domain is not set") {
			t.Fatalf("Run = %v, want the missing domain refused", err)
		}
	})

	t.Run("cache dir cannot be created", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "a-file")
		if err := os.WriteFile(file, nil, 0600); err != nil {
			t.Fatal(err)
		}
		s := &service{cfg: Config{SSLEnabled: true, Domain: "example.com", SSLCacheDir: filepath.Join(file, "certs")}}
		err := s.Run()
		if err == nil || !strings.Contains(err.Error(), "failed to create ssl cache dir") {
			t.Fatalf("Run = %v, want the cache dir failure", err)
		}
	})
}

// waitRedirectServerDone waits until the redirect server's goroutine has
// returned, which it does at once when its port is taken.
func waitRedirectServerDone(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if !strings.Contains(string(buf[:n]), "server.(*service).Run.func2") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("redirect server goroutine still running after 5s:\n%s", buf[:n])
		}
		runtime.Gosched()
	}
}

// With no certificate on disk, Run still starts (the certificate manager
// obtains one), but a port it cannot listen on is an error, and so is the
// redirect server's.
func TestRunSSLPortTaken(t *testing.T) {
	busy, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port

	obtained := make(chan struct{}, 1)
	s := &service{
		cfg:      Config{SSLEnabled: true, Port: port, SSLPort: port, Domain: "example.com", SSLCacheDir: t.TempDir()},
		obtainer: &fakeObtainer{err: errors.New("CA unavailable"), called: obtained},
	}

	err = s.Run()
	if err == nil || !strings.Contains(err.Error(), "TLS listener setup failed") {
		t.Fatalf("Run = %v, want the TLS listener failure", err)
	}

	// The missing local certificate sends the manager to the CA.
	select {
	case <-obtained:
	case <-time.After(5 * time.Second):
		t.Fatal("the certificate manager never asked the CA")
	}
	waitRedirectServerDone(t)
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

// An unset SSL port means 443.
func TestRunSSLDefaultsToPort443(t *testing.T) {
	// Where the test may bind :443 it holds it, so Run cannot; elsewhere Run
	// is refused it just as the test was.
	if hold, err := net.Listen("tcp", ":443"); err == nil {
		defer hold.Close()
	}

	obtained := make(chan struct{}, 1)
	s := &service{
		cfg:      Config{SSLEnabled: true, Port: freePort(t), Domain: "example.com", SSLCacheDir: t.TempDir()},
		obtainer: &fakeObtainer{err: errors.New("CA unavailable"), called: obtained},
	}
	err := s.Run()
	if err == nil || !strings.Contains(err.Error(), ":443") {
		t.Fatalf("Run = %v, want the failure to listen on :443", err)
	}
	select {
	case <-obtained:
	case <-time.After(5 * time.Second):
		t.Fatal("the certificate manager never asked the CA")
	}
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

// The TLS server hands its certificate only to the app's own names, and to
// nobody while it has none.
func TestRunTLSRefusesUnknownHostsAndAMissingCertificate(t *testing.T) {
	dir := t.TempDir()
	certPEM, keyPEM := newTestPair(t, testDomains, 90*24*time.Hour)
	if err := os.WriteFile(filepath.Join(dir, certFileName), certPEM, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, keyFileName), keyPEM, 0600); err != nil {
		t.Fatal(err)
	}

	port := freePort(t)
	s := &service{
		cfg:      Config{SSLEnabled: true, Port: freePort(t), SSLPort: port, Domain: "example.com", SSLCacheDir: dir},
		router:   http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }),
		obtainer: &fakeObtainer{err: errors.New("the CA must not be asked")},
	}
	done := make(chan error, 1)
	go func() { done <- s.Run() }()

	handshake := func(serverName string) error {
		conn, err := tls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port), &tls.Config{ServerName: serverName, InsecureSkipVerify: true})
		if err != nil {
			return err
		}
		return conn.Close()
	}
	retryUntil(t, "TLS handshake for app.example.com", func() error { return handshake("app.example.com") })

	if err := handshake("attacker.com"); err == nil {
		t.Error("a handshake for a host outside the domain succeeded")
	}

	s.certMu.Lock()
	s.currCert = nil
	s.certMu.Unlock()
	if err := handshake("app.example.com"); err == nil {
		t.Error("a handshake succeeded with no certificate loaded")
	}

	shutdown(t, s, done)
}

type hijackableRecorder struct {
	*httptest.ResponseRecorder
	hijacked bool
}

func (h *hijackableRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijacked = true
	return nil, nil, nil
}

// Hijack reaches the wrapped writer when it can be hijacked, and says so when
// it cannot, rather than panicking.
func TestStatusResponseWriterHijack(t *testing.T) {
	inner := &hijackableRecorder{ResponseRecorder: httptest.NewRecorder()}
	if _, _, err := (&statusResponseWriter{ResponseWriter: inner}).Hijack(); err != nil {
		t.Fatalf("Hijack: %v", err)
	}
	if !inner.hijacked {
		t.Error("Hijack did not reach the wrapped writer")
	}

	_, _, err := (&statusResponseWriter{ResponseWriter: httptest.NewRecorder()}).Hijack()
	if err == nil || !strings.Contains(err.Error(), "cannot be hijacked") {
		t.Fatalf("Hijack on a plain writer = %v, want an error", err)
	}
}
