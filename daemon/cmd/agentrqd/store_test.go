// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/agentrq/agentrq/daemon/internal/supervisor"
	"github.com/agentrq/agentrq/daemon/internal/update"
)

func request(t *testing.T, raw string) *http.Request {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Request{URL: u}
}

func TestARedirectMayNotLeaveHTTPS(t *testing.T) {
	feed := request(t, "https://github.com/agentrq/agentrq/releases/latest/download/agentrqd.json")

	if err := noDowngrade(request(t, "https://objects.githubusercontent.com/x"), []*http.Request{feed}); err != nil {
		t.Errorf("https to https refused: %v", err)
	}
	if err := noDowngrade(request(t, "http://objects.githubusercontent.com/x"), []*http.Request{feed}); err == nil {
		t.Error("https to http followed")
	}
	// Enrolment over plain http, which --insecure allows, may still redirect.
	plain := request(t, "http://localhost:3000/api/v1/machines/enroll")
	if err := noDowngrade(request(t, "http://localhost:3000/elsewhere"), []*http.Request{plain}); err != nil {
		t.Errorf("http to http refused: %v", err)
	}
	many := make([]*http.Request, 10)
	for i := range many {
		many[i] = feed
	}
	if err := noDowngrade(feed, many); err == nil {
		t.Error("followed an eleventh redirect")
	}
}

// The feed the release workflow actually publishes: the manifest attached to
// the newest release.
func TestTheDefaultFeedIsTheReleaseAsset(t *testing.T) {
	if !strings.HasPrefix(DefaultManifestURL, "https://github.com/agentrq/agentrq/releases/latest/download/") ||
		!strings.HasSuffix(DefaultManifestURL, "/agentrqd.json") {
		t.Errorf("DefaultManifestURL = %q", DefaultManifestURL)
	}
}

func TestTheUpdaterNeedsARestarterAndAKey(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if u := newUpdater(nil, log, DefaultManifestURL); u != nil {
		t.Error("an updater with nothing to restart")
	}
	r := newRestarter(t.TempDir(), log, supervisor.New(nil, 0, 0))
	if r == nil || r.BinaryPath == "" {
		t.Fatalf("restarter = %+v", r)
	}

	previous := update.ReleaseKey
	t.Cleanup(func() { update.ReleaseKey = previous })
	update.ReleaseKey = ""
	if u := newUpdater(r, log, DefaultManifestURL); u != nil {
		t.Error("a build with no release key would update itself")
	}
	update.ReleaseKey = "00"
	if u := newUpdater(r, log, DefaultManifestURL); u == nil || u.Restarter != r {
		t.Errorf("updater = %+v", u)
	}
	if r, u := remoteControl(t.TempDir(), log, supervisor.New(nil, 0, 0), DefaultManifestURL); r == nil || u == nil || u.Restarter != r {
		t.Errorf("remoteControl = %+v, %+v", r, u)
	}
}

// A daemon that cannot find its own binary says so, and offers neither.
func TestADaemonThatCannotFindItselfCannotBeRestarted(t *testing.T) {
	executable = func() (string, error) { return "", errors.New("no /proc here") }
	t.Cleanup(func() { executable = os.Executable })

	r, u := remoteControl(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)), supervisor.New(nil, 0, 0), DefaultManifestURL)
	if r != nil || u != nil {
		t.Errorf("remoteControl = %+v, %+v", r, u)
	}
}

func TestTheClientFollowsARedirectThatStaysOnHTTPS(t *testing.T) {
	final := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("manifest"))
	}))
	defer final.Close()
	downgrade := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer downgrade.Close()
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		to := final.URL
		if r.URL.Path == "/down" {
			to = downgrade.URL
		}
		http.Redirect(w, r, to, http.StatusFound)
	}))
	defer redirect.Close()

	// The test servers' certificates are trusted for this test only.
	previous := http.DefaultTransport
	http.DefaultTransport = redirect.Client().Transport
	defer func() { http.DefaultTransport = previous }()
	pool := redirect.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	pool.AddCert(final.Certificate())

	c := &httpClient{timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, redirect.URL+"/feed", nil)
	res, err := c.Do(req)
	if err != nil {
		t.Fatalf("https to https: %v", err)
	}
	b, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if string(b) != "manifest" {
		t.Errorf("body = %q", b)
	}

	req, _ = http.NewRequest(http.MethodGet, redirect.URL+"/down", nil)
	if res, err := c.Do(req); err == nil {
		_ = res.Body.Close()
		t.Error("followed a redirect from https to http")
	}
}
