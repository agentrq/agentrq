// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package link

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/daemon/internal/stream"
	"github.com/agentrq/agentrq/daemon/internal/supervisor"
	"github.com/agentrq/agentrq/daemon/wire"
)

func forkStart(t *testing.T) wire.Frame {
	t.Helper()
	return controlFrame(t, wire.OpStartSession, wire.StartSession{
		SessionID: 7, Kind: "acp-gateway", Model: "m", Agent: "a",
		MCPURL: "https://agentrq.example/mcp/ws?token=test", ServerName: "agentrq-workspace",
		Fork: &wire.ForkSpec{ID: "0jUM5wEc1Hl", From: t.TempDir()},
	})
}

// A fork's folder can take minutes to check out. A viewer who opens its
// terminal meanwhile watches it happen, and the agent's output follows on.
func TestAForkIsWatchableWhileItsFolderIsMade(t *testing.T) {
	b := newBackend(t)
	h := start(t, b)

	made := t.TempDir()
	preparing, release := make(chan struct{}), make(chan struct{})
	h.sup.Home = t.TempDir()
	h.sup.PrepareDir = func(_ context.Context, _, _, _ string, out supervisor.Progress) (string, error) {
		out.Notice("$ cd /repo && git checkout --progress")
		_, _ = out.Write([]byte("Preparing worktree\nUpdating files:  50% (1/2)\r"))
		close(preparing)
		<-release
		return made, nil
	}
	b.send(t, forkStart(t))
	<-preparing

	b.send(t, controlFrame(t, wire.OpAttach, wire.KillSession{SessionID: 7}))
	waitFor(t, func() bool {
		return outputContains(b, "agentrqd: $ cd /repo && git checkout --progress") &&
			outputContains(b, "Preparing worktree\r\nUpdating files:  50% (1/2)\r")
	}, "the checkout was not shown while it ran")

	close(release)
	waitFor(t, func() bool { return outputContains(b, "&& npx -y @agentrq/acp-gateway@latest") },
		"the agent's command was not shown once the folder was made")
	if _, err := h.tty.outW.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return outputContains(b, "hello") }, "the agent's output never followed the checkout")
	if len(h.tty.written()) != 0 {
		t.Errorf("the progress reached the terminal's input: %q", h.tty.written())
	}
}

// A fork whose folder could not be made has no terminal, so its stream goes
// with it rather than waiting for one forever.
func TestARefusedForkDropsItsStream(t *testing.T) {
	b := newBackend(t)
	h := start(t, b)

	h.sup.Home = t.TempDir()
	h.sup.PrepareDir = func(context.Context, string, string, string, supervisor.Progress) (string, error) {
		return "", errors.New("git checkout: took longer than 30m0s")
	}
	b.send(t, forkStart(t))
	waitFor(t, func() bool {
		for _, c := range b.controls(t, wire.OpSessionState) {
			var st wire.SessionState
			_ = json.Unmarshal(c.Body, &st)
			if st.SessionID == 7 && st.State == "failed" && strings.Contains(st.Error, "took longer than") {
				return true
			}
		}
		return false
	}, "the refusal was never reported")
	waitFor(t, func() bool { _, ok := h.link.streams.get(7); return !ok }, "the refused fork kept its stream")
}

// A session that turns out to have no terminal gives back the stream opened
// for its launch.
func TestAnOpenedStreamWithNoTerminalIsDropped(t *testing.T) {
	b := newBackend(t)
	h := start(t, b)

	preparing, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	h.sup.Home = t.TempDir()
	h.sup.PrepareDir = func(context.Context, string, string, string, supervisor.Progress) (string, error) {
		close(preparing)
		<-release
		return "", errors.New("stopped")
	}
	b.send(t, forkStart(t))
	<-preparing
	if _, ok := h.link.streams.get(7); !ok {
		t.Fatal("no stream was opened for the fork's launch")
	}
	// Still starting, so it has no terminal yet.
	h.link.stream(nil, wire.StartSession{SessionID: 7})
	if _, ok := h.link.streams.get(7); ok {
		t.Error("the stream outlived finding no terminal")
	}
}

// A terminal that cannot be drawn does not fail the checkout being drawn.
func TestProgressThatCannotBeShownIsNotAnError(t *testing.T) {
	p := stream.NewPump(1, stream.NewScreen(80, 24), failingSender{err: errors.New("gone")})
	_ = p.Attach()
	tp := &terminalProgress{pump: p, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	big := []byte(strings.Repeat("x", stream.MaxBatch+1))
	if n, err := tp.Write(big); n != len(big) || err != nil {
		t.Errorf("Write = %d, %v; want all of it taken and no error", n, err)
	}
}
