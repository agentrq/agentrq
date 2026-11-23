// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package machine

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// A viewer arriving and leaving is reported to whoever wants to count it.
//
// The hooks are how this package stays free of a database and a bus while the
// attach still gets counted: it reports the fact, and the caller that owns
// those decides what to do with it.
func TestAttachAndDetachAreReported(t *testing.T) {
	const sessionID = 7

	reg := NewRegistry("pod-a")
	reg.Add(11, &fakeConn{})
	relay := NewRelay(reg)

	var mu sync.Mutex
	var calls []string
	var sawWorkspace int64
	var sawSession uint64

	h := &ViewerHandler{
		Relay: relay,
		Lookup: fakeLookup{at: Attachment{
			MachineID:   11,
			InstanceID:  "pod-a",
			UserID:      3,
			WorkspaceID: 70,
			ViewerName:  "Ada",
		}},
		SessionID: func(*http.Request) uint64 { return sessionID },
		OnAttach: func(_ *http.Request, id uint64, at Attachment) {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, "attach")
			sawSession = id
			sawWorkspace = at.WorkspaceID
		},
		OnDetach: func(_ *http.Request, _ uint64, _ Attachment) {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, "detach")
		},
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	ws := dialViewer(t, srv)
	waitFor(t, func() bool { return relay.Viewers(sessionID) == 1 }, "the viewer never attached")
	_ = ws.Close()

	// Waited on by what is being asserted rather than by the relay's count:
	// the detach hook runs in the same defer as the audit line, after the
	// count has already dropped.
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(calls) == 2
	}, "the detach was never reported")

	mu.Lock()
	defer mu.Unlock()
	// In order, and exactly once each. A detach without an attach would count
	// somebody leaving a terminal they never opened.
	if calls[0] != "attach" || calls[1] != "detach" {
		t.Errorf("reported %v, want attach then detach", calls)
	}
	if sawSession != sessionID {
		t.Errorf("session = %d, want %d", sawSession, sessionID)
	}
	// Carried so the attach can be counted against the workspace the agent is
	// working in — the session knows it, unlike the machine.
	if sawWorkspace != 70 {
		t.Errorf("workspace = %d, want 70", sawWorkspace)
	}
}

// Nobody counting is the ordinary case for this package's own tests, and must
// not be a crash.
func TestAttachWithNobodyCountingStillWorks(t *testing.T) {
	const sessionID = 7
	srv, relay, _ := viewerServer(t, sessionID)

	ws := dialViewer(t, srv)
	waitFor(t, func() bool { return relay.Viewers(sessionID) == 1 }, "the viewer never attached")
	_ = ws.Close()
	waitFor(t, func() bool { return relay.Viewers(sessionID) == 0 }, "the viewer was never detached")
}

// A refused attach is not a viewing.
//
// The handler gives up before upgrading when the machine is held by another
// instance, and counting that would record terminals nobody ever saw.
func TestARefusedAttachIsNotReported(t *testing.T) {
	reg := NewRegistry("pod-a")
	relay := NewRelay(reg)

	var mu sync.Mutex
	counted := 0
	h := &ViewerHandler{
		Relay: relay,
		// Machine 11 is not in the registry, so the attach is refused.
		Lookup:    fakeLookup{at: Attachment{MachineID: 11, InstanceID: "pod-b", UserID: 3}},
		SessionID: func(*http.Request) uint64 { return 7 },
		OnAttach: func(*http.Request, uint64, Attachment) {
			mu.Lock()
			counted++
			mu.Unlock()
		},
		OnDetach: func(*http.Request, uint64, Attachment) {
			mu.Lock()
			counted++
			mu.Unlock()
		},
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", res.StatusCode)
	}

	mu.Lock()
	defer mu.Unlock()
	if counted != 0 {
		t.Errorf("counted %d times, want 0 — nothing was ever watched", counted)
	}
}
