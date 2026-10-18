// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"
)

func has(sigs []os.Signal, want os.Signal) bool {
	for _, s := range sigs {
		if s == want {
			return true
		}
	}
	return false
}

func TestShutdownSignalsAlwaysStopsOnInterruptAndTerm(t *testing.T) {
	for name, ignored := range map[string]func(os.Signal) bool{
		"nothing ignored": func(os.Signal) bool { return false },
		"hangup ignored":  func(os.Signal) bool { return true },
	} {
		t.Run(name, func(t *testing.T) {
			sigs := shutdownSignals(ignored)
			if !has(sigs, os.Interrupt) {
				t.Errorf("os.Interrupt missing from %v", sigs)
			}
			if !has(sigs, syscall.SIGTERM) {
				t.Errorf("SIGTERM missing from %v", sigs)
			}
		})
	}
}

// A terminal closing must run the shutdown path rather than the default
// disposition, which would terminate the process and orphan every agent.
func TestShutdownSignalsIncludesHangup(t *testing.T) {
	sigs := shutdownSignals(func(os.Signal) bool { return false })
	if !has(sigs, syscall.SIGHUP) {
		t.Fatalf("SIGHUP missing from %v; a terminal closing would orphan every agent", sigs)
	}
}

// `nohup agentrqd serve &` ignores SIGHUP so the daemon survives the terminal
// closing. Registering a handler would replace that and stop it instead.
func TestShutdownSignalsLeavesAnIgnoredHangupAlone(t *testing.T) {
	var asked os.Signal
	sigs := shutdownSignals(func(s os.Signal) bool {
		asked = s
		return true
	})
	if asked != syscall.SIGHUP {
		t.Errorf("asked whether %v was ignored, want SIGHUP", asked)
	}
	if has(sigs, syscall.SIGHUP) {
		t.Fatalf("SIGHUP in %v although it is ignored; nohup would stop working", sigs)
	}
}

func TestShutdownCauseNamesTheSignal(t *testing.T) {
	if got := shutdownCause(syscall.SIGTERM).Error(); got != "signal: terminated" {
		t.Errorf("shutdownCause(SIGTERM) = %q, want %q", got, "signal: terminated")
	}
}

func TestWatchSignalsCancelsWithTheSignalThatArrived(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(context.Canceled)

	ch := make(chan os.Signal, 1)
	ch <- syscall.SIGHUP
	go watchSignals(cancel, ch, ctx.Done())

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the context was never cancelled")
	}
	if got := stopReason(ctx); got != "signal: hangup" {
		t.Errorf("stopReason = %q, want %q", got, "signal: hangup")
	}
}

// The other way out: something else cancelled first, so there is no signal to
// wait for and the watcher must not outlive the context.
func TestWatchSignalsStopsWhenTheContextIsAlreadyDone(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(context.Canceled)

	done := make(chan struct{})
	go func() {
		watchSignals(cancel, make(chan os.Signal), ctx.Done())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchSignals did not return once the context was done")
	}
}

func TestNotifyShutdownStopsCleanly(t *testing.T) {
	// SIGHUP reported as ignored, so the test never touches this process's own
	// hangup disposition.
	ctx, stop := notifyShutdown(shutdownSignals(func(os.Signal) bool { return true }))
	if ctx.Err() != nil {
		t.Fatalf("cancelled before anything happened: %v", ctx.Err())
	}

	stop()

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("stop() did not cancel the context")
	}
	// A deliberate stop is not a signal, and must not be logged as one.
	if got := stopReason(ctx); got != "asked to stop" {
		t.Errorf("stopReason = %q, want %q", got, "asked to stop")
	}
}

func TestStopReasonOfALiveContext(t *testing.T) {
	if got := stopReason(context.Background()); got != "asked to stop" {
		t.Errorf("stopReason = %q, want %q", got, "asked to stop")
	}
}
