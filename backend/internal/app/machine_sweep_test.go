// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeMachineSweeper answers every stale sweep with n and err, every expiry
// with expired and expiredErr, and every finished sweep with finished and
// finishedErr, and counts the sweeps.
type fakeMachineSweeper struct {
	mu          sync.Mutex
	n           int64
	err         error
	expired     int64
	expiredErr  error
	finished    int64
	finishedErr error
	calls       int
	swept       chan struct{}
}

func (f *fakeMachineSweeper) MarkStaleSessionsAway(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.swept != nil {
		select {
		case f.swept <- struct{}{}:
		default:
		}
	}
	return f.n, f.err
}

func (f *fakeMachineSweeper) DeleteExpiredAwaySessions(context.Context) (int64, error) {
	return f.expired, f.expiredErr
}

func (f *fakeMachineSweeper) DeleteFinishedSessions(context.Context) (int64, error) {
	return f.finished, f.finishedErr
}

func TestSweepMachines(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    *fakeMachineSweeper
		want string
	}{
		{"a sweep that marked sessions away says how many", &fakeMachineSweeper{n: 2}, `"marked":2,"message":"machines: marked the sessions of stale machines away"`},
		{"a sweep that removed expired away sessions says how many", &fakeMachineSweeper{expired: 4}, `"deleted":4,"message":"machines: removed expired away sessions"`},
		{"a failed expiry says so", &fakeMachineSweeper{expiredErr: errors.New("database unavailable")}, `"message":"machines: removing expired away sessions failed; retried at the next sweep"`},
		{"a sweep that failed says so", &fakeMachineSweeper{err: errors.New("database unavailable")}, `"error":"database unavailable"`},
		{"a sweep that found nothing says nothing", &fakeMachineSweeper{}, ""},
		{"a sweep that removed finished sessions says how many", &fakeMachineSweeper{finished: 3}, `"deleted":3,"message":"machines: removed finished sessions"`},
		{"a failed finished sweep says so", &fakeMachineSweeper{finishedErr: errors.New("database unavailable")}, `"message":"machines: removing finished sessions failed; retried at the next sweep"`},
		{"finished sessions are removed even when the stale sweep failed", &fakeMachineSweeper{err: errors.New("database unavailable"), finished: 1}, `"deleted":1,"message":"machines: removed finished sessions"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buf := captureLog(t)
			sweepMachines(context.Background(), tc.f)
			if tc.want == "" && buf.Len() != 0 {
				t.Errorf("the sweep logged %s, want nothing", buf)
			}
			if !strings.Contains(buf.String(), tc.want) {
				t.Errorf("the sweep logged %s, want it to contain %s", buf, tc.want)
			}
		})
	}
}

// The sweep runs every interval, and stops when its context ends.
func TestStartMachineSweep(t *testing.T) {
	captureLog(t)
	f := &fakeMachineSweeper{swept: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	startMachineSweep(ctx, f, time.Millisecond, 0)
	for range 2 {
		select {
		case <-f.swept:
		case <-time.After(5 * time.Second):
			t.Fatal("no sweep ran within 5s, want one every millisecond")
		}
	}
	cancel()
	time.Sleep(20 * time.Millisecond)
	f.mu.Lock()
	calls := f.calls
	f.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls != calls {
		t.Errorf("swept %d more times after the context ended", f.calls-calls)
	}
}

// Each wait is the interval plus 0 to 2 minutes, in whole seconds, and not
// the same every time.
func TestSweepWaitAddsJitterInSeconds(t *testing.T) {
	seen := map[time.Duration]bool{}
	for range 1000 {
		d := sweepWait(machineSweepInterval, sweepJitter)
		if d < 5*time.Minute || d > 7*time.Minute || d%time.Second != 0 {
			t.Fatalf("waited %s, want 5m to 7m in whole seconds", d)
		}
		seen[d] = true
	}
	if len(seen) < 2 {
		t.Error("every wait was the same: no jitter")
	}
	if d := sweepWait(time.Minute, 0); d != time.Minute {
		t.Errorf("with no jitter waited %s, want 1m", d)
	}
}
