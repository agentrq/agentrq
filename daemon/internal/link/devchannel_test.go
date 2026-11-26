// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package link

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentrq/agentrq/daemon/internal/pty"
	"github.com/agentrq/agentrq/daemon/internal/restore"
	"github.com/agentrq/agentrq/daemon/internal/stream"
	"github.com/agentrq/agentrq/daemon/internal/supervisor"
	"github.com/agentrq/agentrq/daemon/wire"
)

// devChannelPrompt is Claude Code's development-channels warning for
// `server:devtest`, as recorded from a real 100×30 terminal.
func devChannelPrompt(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../stream/testdata/devchannel-prompt.bin")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// restoreClaudeCode brings back a Claude Code session whose MCP server is
// serverName, the way a daemon does after replacing itself.
func restoreClaudeCode(t *testing.T, h *harness, serverName string) *supervisor.Session {
	t.Helper()
	dir := t.TempDir()
	if _, _, err := supervisor.WriteMCPConfig(dir, supervisor.MCPEntry{Name: serverName, URL: "https://agentrq.example/mcp/ws?token=test"}); err != nil {
		t.Fatal(err)
	}
	conn := &Conn{out: make(chan []byte, 8), done: make(chan struct{})}
	h.link.Restore(context.Background(), conn, restore.Session{
		ID: 9, Profile: "work", Kind: string(supervisor.KindClaudeCode),
		Dir: dir, Workspace: "demo", ServerName: serverName, Cols: 100, Rows: 30,
	})
	sess, err := h.sup.Get(9)
	if err != nil {
		t.Fatalf("the session did not come back: %v", err)
	}
	return sess
}

// The issue itself: a Claude Code session started from the panel reaches its
// first task without anybody pressing Enter in its terminal.
func TestALaunchedClaudeCodeConfirmsItsOwnDevChannel(t *testing.T) {
	b := newBackend(t)
	h := start(t, b)
	b.send(t, controlFrame(t, wire.OpStartSession, wire.StartSession{
		SessionID: 7, Kind: string(supervisor.KindClaudeCode), Dir: t.TempDir(), Workspace: "demo",
		MCPURL: "https://agentrq.example/mcp/ws?token=test", ServerName: "devtest", Cols: 100, Rows: 30,
	}))
	waitFor(t, func() bool { _, ok := h.link.streams.get(7); return ok }, "the session never streamed")

	if _, err := h.tty.outW.Write(devChannelPrompt(t)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return string(h.tty.written()) == "\r" }, "the warning was never confirmed")
}

// And one that comes back after the daemon updated itself, which nobody is
// watching by design.
func TestARestoredClaudeCodeConfirmsItsOwnDevChannel(t *testing.T) {
	h := start(t, newBackend(t))
	restoreClaudeCode(t, h, "devtest")

	if _, err := h.tty.outW.Write(devChannelPrompt(t)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return string(h.tty.written()) == "\r" }, "the warning was never confirmed")
}

// A warning for any channel but the session's own is a person's to answer.
func TestADevChannelTheDaemonDidNotPassIsLeftAlone(t *testing.T) {
	for name, out := range map[string][]byte{
		"another server": nil,
		"one more":       []byte("server:devtest plugin:x@y"),
	} {
		t.Run(name, func(t *testing.T) {
			var logged syncBuffer
			h := start(t, newBackend(t), logTo(&logged))
			serverName := "devtest"
			prompt := devChannelPrompt(t)
			if out == nil {
				serverName = "mine"
			} else {
				prompt = bytes.Replace(prompt, []byte("server:devtest"), out, 1)
			}
			sess := restoreClaudeCode(t, h, serverName)

			screen := stream.NewScreen(100, 30)
			if _, err := screen.Write(prompt); err != nil {
				t.Fatal(err)
			}
			h.link.confirmDevChannel(sess, screen, time.Minute)
			if got := h.tty.written(); len(got) != 0 {
				t.Errorf("typed %q at a warning for channels the daemon did not pass", got)
			}
			if !strings.Contains(logged.String(), "for a person") {
				t.Errorf("the log does not say why: %s", logged.String())
			}
		})
	}
}

// Watching stops when the warning never comes, and when the agent ends.
func TestWatchingForTheDevChannelWarningStops(t *testing.T) {
	h := start(t, newBackend(t))
	sess := restoreClaudeCode(t, h, "devtest")
	blank := stream.NewScreen(100, 30)

	h.link.confirmDevChannel(sess, blank, 5*time.Millisecond) // returns: no warning in time

	_ = h.tty.Close()
	<-sess.Ended()
	h.link.confirmDevChannel(sess, blank, time.Minute) // returns: the agent ended
	if got := h.tty.written(); len(got) != 0 {
		t.Errorf("typed %q with no warning on screen", got)
	}
}

// Only Claude Code is started with a development channel.
func TestAGatewayIsNotWatchedForTheDevChannelWarning(t *testing.T) {
	h := start(t, newBackend(t))
	h.link.confirmDevChannel(&supervisor.Session{Kind: supervisor.KindACPGateway}, nil, time.Minute)
}

// A terminal that cannot be typed into is said so, and not retried.
func TestAFailedDevChannelConfirmationIsLogged(t *testing.T) {
	tty := &unwritableTTY{newTTY()}
	var logged syncBuffer
	h := start(t, newBackend(t), logTo(&logged), func(l *Link) {
		l.Supervisor = supervisor.New(func(context.Context, pty.Spec) (pty.Session, error) { return tty, nil }, 0, 0)
	})
	h.sup = h.link.Supervisor
	sess := restoreClaudeCode(t, h, "devtest")

	screen := stream.NewScreen(100, 30)
	if _, err := screen.Write(devChannelPrompt(t)); err != nil {
		t.Fatal(err)
	}
	h.link.confirmDevChannel(sess, screen, time.Minute)
	if !strings.Contains(logged.String(), "could not confirm") {
		t.Errorf("the failure was not logged: %s", logged.String())
	}
}

// logTo sends a link's log to b, set before the link runs.
func logTo(b *syncBuffer) func(*Link) {
	return func(l *Link) { l.Log = slog.New(slog.NewTextHandler(b, nil)) }
}

// syncBuffer is a log that the link's goroutines and the test share.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type unwritableTTY struct{ *fakeTTY }

func (unwritableTTY) Write([]byte) (int, error) { return 0, errors.New("closed") }

var _ io.Writer = unwritableTTY{}
