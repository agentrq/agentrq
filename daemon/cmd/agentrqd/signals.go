// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// shutdownSignals is what this daemon stops for.
//
// SIGHUP is in the list because `agentrqd serve` is documented as something you
// run in a terminal. Without it the default disposition terminates the process
// the instant that terminal closes, before any shutdown code runs — so every
// agent on the machine is orphaned, which is the single outcome the shutdown
// path exists to prevent.
//
// Unless it is already ignored, which is what `nohup` does precisely so the
// daemon *does* survive the terminal closing. signal.Notify would quietly
// replace that with a handler and turn nohup into the opposite of what it
// means, so an inherited SIG_IGN is left alone.
func shutdownSignals(ignored func(os.Signal) bool) []os.Signal {
	sigs := []os.Signal{os.Interrupt, syscall.SIGTERM}
	if !ignored(syscall.SIGHUP) {
		sigs = append(sigs, syscall.SIGHUP)
	}
	return sigs
}

// shutdownCause records which signal is ending this process.
//
// Spelled the way os/exec spells a death — "signal: terminated" — so a daemon's
// own log and the report of its exit use one vocabulary rather than two.
func shutdownCause(s os.Signal) error { return fmt.Errorf("signal: %s", s) }

// notifyShutdown returns a context cancelled by the first of sigs to arrive,
// carrying which one as its cause.
//
// The cause is the reason this is not signal.NotifyContext: that cancels
// without saying what did it, and "who stopped this" is the first question
// asked of a daemon that is no longer running.
func notifyShutdown(sigs []os.Signal) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, sigs...)
	go watchSignals(cancel, ch, ctx.Done())
	return ctx, func() {
		signal.Stop(ch)
		cancel(context.Canceled)
	}
}

// watchSignals cancels on the first signal, or gives up when the context has
// already been cancelled by something else.
func watchSignals(cancel context.CancelCauseFunc, ch <-chan os.Signal, done <-chan struct{}) {
	select {
	case s := <-ch:
		cancel(shutdownCause(s))
	case <-done:
	}
}

// stopReason says why the daemon is shutting down, for the log.
func stopReason(ctx context.Context) string {
	cause := context.Cause(ctx)
	if cause == nil || errors.Is(cause, context.Canceled) {
		return "asked to stop"
	}
	return cause.Error()
}
