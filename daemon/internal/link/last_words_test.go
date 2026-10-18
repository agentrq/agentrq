// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package link

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/agentrq/agentrq/daemon/wire"
)

// launchRunning starts session 7 and waits for it to report running.
func launchRunning(t *testing.T, b *fakeBackend, h *harness) {
	t.Helper()
	b.send(t, controlFrame(t, wire.OpStartSession, wire.StartSession{
		SessionID: 7, Kind: "acp-gateway", Dir: t.TempDir(), Agent: "a", MCPURL: "https://agentrq.example/mcp/ws?token=test", ServerName: "agentrq-workspace", Cols: 80, Rows: 24,
	}))
	waitFor(t, func() bool { return endState(t, b, "running") != nil }, "the session never reported running")
}

// endState is session 7's report of state, or nil before there is one.
func endState(t *testing.T, b *fakeBackend, state string) *wire.SessionState {
	t.Helper()
	for _, c := range b.controls(t, wire.OpSessionState) {
		var st wire.SessionState
		_ = json.Unmarshal(c.Body, &st)
		if st.SessionID == 7 && st.State == state {
			return &st
		}
	}
	return nil
}

// exitWith ends the agent with code after it printed out.
func exitWith(t *testing.T, h *harness, out string, code int) {
	t.Helper()
	if out != "" {
		if _, err := h.tty.outW.Write([]byte(out)); err != nil {
			t.Fatal(err)
		}
	}
	h.tty.code = code
	_ = h.tty.Close()
}

// A session that has ended has no terminal to open, so a failing exit carries
// the last line the agent printed: that is where the gateway says why.
func TestAFailingExitCarriesTheAgentsLastLine(t *testing.T) {
	b := newBackend(t)
	h := start(t, b)
	launchRunning(t, b, h)

	exitWith(t, h, "Starting\r\n[acp-gateway] No build for this machine.\r\n", 1)

	waitFor(t, func() bool { return endState(t, b, "exited") != nil }, "the exit was never reported")
	if got, want := endState(t, b, "exited").Error, "[acp-gateway] No build for this machine."; got != want {
		t.Errorf("the exit's error = %q, want %q", got, want)
	}
}

// The launch's own notices are not the agent's words.
func TestAFailingExitWithNoOutputCarriesNoReason(t *testing.T) {
	b := newBackend(t)
	h := start(t, b)
	launchRunning(t, b, h)

	exitWith(t, h, "", 1)

	waitFor(t, func() bool { return endState(t, b, "exited") != nil }, "the exit was never reported")
	if got := endState(t, b, "exited").Error; got != "" {
		t.Errorf("the exit's error = %q, want none", got)
	}
}

// A clean exit explains nothing.
func TestACleanExitCarriesNoReason(t *testing.T) {
	b := newBackend(t)
	h := start(t, b)
	launchRunning(t, b, h)

	exitWith(t, h, "bye\r\n", 0)

	waitFor(t, func() bool { return endState(t, b, "exited") != nil }, "the exit was never reported")
	if got := endState(t, b, "exited").Error; got != "" {
		t.Errorf("the exit's error = %q, want none", got)
	}
}

// A terminal something still holds open does not hold the exit report back.
func TestAnExitIsReportedWhileTheTerminalIsStillOpen(t *testing.T) {
	defer func(d time.Duration) { lastWordsWait = d }(lastWordsWait)
	lastWordsWait = 10 * time.Millisecond

	b := newBackend(t)
	h := start(t, b)
	launchRunning(t, b, h)

	if _, err := h.tty.outW.Write([]byte("still here\r\n")); err != nil {
		t.Fatal(err)
	}
	// The process ends; its terminal does not.
	h.tty.mu.Lock()
	h.tty.code, h.tty.closed = 2, true
	h.tty.mu.Unlock()
	close(h.tty.done)
	t.Cleanup(func() { _ = h.tty.outW.Close() })

	waitFor(t, func() bool { return endState(t, b, "exited") != nil }, "the exit was never reported")
	if got := endState(t, b, "exited").Error; got != "still here" {
		t.Errorf("the exit's error = %q, want %q", got, "still here")
	}
}

// A session that is already gone has no terminal to read.
func TestNoLastWordsForASessionThatIsGone(t *testing.T) {
	h := start(t, newBackend(t))
	if h.link.stream(nil, wire.StartSession{SessionID: 99}) != nil {
		t.Error("a session that is not running was given a terminal to read")
	}
}
