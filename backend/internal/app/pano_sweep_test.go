// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
)

// fakeSweeper answers each sweep with the next of its results, and counts the
// sweeps.
type fakeSweeper struct {
	mu      sync.Mutex
	results []sweepResult
	calls   int
	swept   chan struct{}
}

type sweepResult struct {
	n   int
	err error
}

func (f *fakeSweeper) DeleteExpiredPanoDocs(context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := sweepResult{}
	if f.calls < len(f.results) {
		r = f.results[f.calls]
	}
	f.calls++
	if f.swept != nil {
		select {
		case f.swept <- struct{}{}:
		default:
		}
	}
	return r.n, r.err
}

// captureLog sends the global logger to a buffer for the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := zlog.Logger
	zlog.Logger = zerolog.New(&buf)
	t.Cleanup(func() { zlog.Logger = old })
	return &buf
}

func TestSweepPano(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result sweepResult
		want   string
	}{
		{"a sweep that deleted docs says how many", sweepResult{n: 3}, `"deleted":3,"message":"pano: deleted expired docs"`},
		{"a sweep that failed says so, with how many it deleted first", sweepResult{n: 1, err: errors.New("database unavailable")}, `"error":"database unavailable","deleted":1`},
		{"a sweep that found nothing says nothing", sweepResult{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buf := captureLog(t)
			sweepPano(context.Background(), &fakeSweeper{results: []sweepResult{tc.result}})
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
func TestStartPanoSweep(t *testing.T) {
	captureLog(t)
	f := &fakeSweeper{swept: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	startPanoSweep(ctx, f, time.Millisecond)
	for range 2 {
		select {
		case <-f.swept:
		case <-time.After(5 * time.Second):
			t.Fatal("no sweep ran within 5s, want one every millisecond")
		}
	}
	cancel()
	// Once stopped, it sweeps no more.
	time.Sleep(20 * time.Millisecond)
	f.mu.Lock()
	stopped := f.calls
	f.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls != stopped {
		t.Errorf("the sweep ran %d more times after its context ended, want none", f.calls-stopped)
	}
}
