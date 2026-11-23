// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type sweepCounter struct {
	runs atomic.Int32
	err  error
}

func (s *sweepCounter) CountExpiredInvitations(context.Context) (int, error) {
	s.runs.Add(1)
	return 0, s.err
}

// The sweep runs on every tick, a failed run does not stop it, and it stops
// with its context.
func TestTheInvitationExpirySweepRunsUntilStopped(t *testing.T) {
	for _, err := range []error{nil, errors.New("database unavailable")} {
		ctx, cancel := context.WithCancel(context.Background())
		c := &sweepCounter{err: err}
		startInvitationExpirySweep(ctx, c, time.Millisecond)
		deadline := time.Now().Add(5 * time.Second)
		for c.runs.Load() < 3 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if c.runs.Load() < 3 {
			t.Fatalf("the sweep ran %d times, want at least 3", c.runs.Load())
		}
		cancel()
		time.Sleep(20 * time.Millisecond)
		stopped := c.runs.Load()
		time.Sleep(20 * time.Millisecond)
		if c.runs.Load() != stopped {
			t.Errorf("the sweep kept running after its context ended")
		}
	}
}
