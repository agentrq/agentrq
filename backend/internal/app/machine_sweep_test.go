// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeMachineSweeper answers every sweep with n and err, and counts the sweeps.
type fakeMachineSweeper struct {
	mu    sync.Mutex
	n     int64
	err   error
	calls int
	swept chan struct{}
}

func (f *fakeMachineSweeper) DeleteStaleSessions(context.Context) (int64, error) {
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

func TestSweepMachines(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    *fakeMachineSweeper
		want string
	}{
		{"a sweep that ended sessions says how many", &fakeMachineSweeper{n: 2}, `"deleted":2,"message":"machines: ended the sessions of stale machines"`},
		{"a sweep that failed says so", &fakeMachineSweeper{err: errors.New("database unavailable")}, `"error":"database unavailable"`},
		{"a sweep that found nothing says nothing", &fakeMachineSweeper{}, ""},
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
	startMachineSweep(ctx, f, time.Millisecond)
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
