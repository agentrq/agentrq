// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package machine

import (
	"errors"
	"strings"
	"testing"

	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/daemon/wire"
)

func TestKillSession(t *testing.T) {
	session := monoflake.ID(500).String()
	if err := KillSession(nil, 1, session); !errors.Is(err, ErrNoConnections) {
		t.Errorf("no registry: err = %v", err)
	}
	r := NewRegistry("pod-a")
	if err := KillSession(r, 1, session); !errors.Is(err, ErrNotConnected) {
		t.Errorf("not connected: err = %v", err)
	}

	c := &fakeConn{}
	r.Add(1, c)
	if err := KillSession(r, 1, session); err != nil {
		t.Fatalf("err = %v", err)
	}
	ctl, err := wire.ParseControl(c.sent[0])
	if err != nil || ctl.Op != wire.OpKillSession || !strings.Contains(string(ctl.Body), "500") {
		t.Errorf("sent %+v, %v; want a kill of session 500", ctl, err)
	}

	c.sendErr = errors.New("socket closed")
	if err := KillSession(r, 1, session); !errors.Is(err, ErrUnreachable) {
		t.Errorf("send fails: err = %v", err)
	}
}

func TestRemoveForkDir(t *testing.T) {
	if err := RemoveForkDir(nil, 1, 20); !errors.Is(err, ErrNoConnections) {
		t.Errorf("no registry: err = %v", err)
	}
	r := NewRegistry("pod-a")
	if err := RemoveForkDir(r, 1, 20); !errors.Is(err, ErrNotConnected) {
		t.Errorf("not connected: err = %v", err)
	}
	if CanRemoveForkDir(nil, 1) || CanRemoveForkDir(r, 1) {
		t.Error("a machine that is not connected can remove a folder")
	}

	c := &fakeConn{}
	r.Add(1, c)
	if CanRemoveForkDir(r, 1) {
		t.Error("a daemon that did not say so can remove a folder")
	}
	r.SetCapabilities(1, c, []string{wire.CapabilityForkCleanup})
	if !CanRemoveForkDir(r, 1) {
		t.Error("a daemon that said so cannot")
	}
	if err := RemoveForkDir(r, 1, 20); err != nil {
		t.Fatalf("err = %v", err)
	}
	ctl, err := wire.ParseControl(c.sent[0])
	if err != nil || ctl.Op != wire.OpRemoveForkDir || !strings.Contains(string(ctl.Body), monoflake.ID(20).String()) {
		t.Errorf("sent %+v, %v; want a removeForkDir naming the fork", ctl, err)
	}

	c.sendErr = errors.New("socket closed")
	if err := RemoveForkDir(r, 1, 20); !errors.Is(err, ErrUnreachable) {
		t.Errorf("send fails: err = %v", err)
	}
}
