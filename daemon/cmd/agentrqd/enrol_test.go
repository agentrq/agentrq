// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agentrq/agentrq/daemon/internal/localstatus"
)

// stubServe replaces the serve that follows an enrolment and counts the calls.
func stubServe(t *testing.T) *int {
	t.Helper()
	calls := 0
	previous := serveEnrolled
	t.Cleanup(func() { serveEnrolled = previous })
	serveEnrolled = func(_ context.Context, args []string) error {
		calls++
		if len(args) != 0 {
			t.Errorf("serve args = %q; want none, as a plain `agentrqd serve`", args)
		}
		return nil
	}
	return &calls
}

// A first enrolment is one command: it connects without being asked to.
func TestEnrolThenServes(t *testing.T) {
	calls := stubServe(t)
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("APPDATA", dir)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"machineId":"m1","userId":"u1","machineToken":"t1"}`))
	}))
	defer srv.Close()

	if err := cmdEnrol(context.Background(), []string{"--server", srv.URL, "--code", "ABCD"}); err != nil {
		t.Fatalf("enrol: %v", err)
	}
	if *calls != 1 {
		t.Errorf("serve ran %d times after enrolling; want 1", *calls)
	}
}

func TestServeAfterEnrol(t *testing.T) {
	t.Run("serves when nothing is running", func(t *testing.T) {
		calls := stubServe(t)
		if err := serveAfterEnrol(context.Background(), t.TempDir(), time.Now()); err != nil {
			t.Fatal(err)
		}
		if *calls != 1 {
			t.Errorf("serve ran %d times; want 1", *calls)
		}
	})

	// A second daemon would open a second connection with the same token.
	t.Run("leaves a running daemon alone", func(t *testing.T) {
		calls := stubServe(t)
		dir := t.TempDir()
		if err := localstatus.Write(dir, localstatus.File{PID: 4242}); err != nil {
			t.Fatal(err)
		}
		if err := serveAfterEnrol(context.Background(), dir, time.Now()); err != nil {
			t.Fatal(err)
		}
		if *calls != 0 {
			t.Errorf("serve ran %d times beside a running daemon; want 0", *calls)
		}
	})

	// A killed daemon leaves its report behind; that is not one running.
	t.Run("serves past a stale report", func(t *testing.T) {
		calls := stubServe(t)
		dir := t.TempDir()
		if err := localstatus.Write(dir, localstatus.File{PID: 4242}); err != nil {
			t.Fatal(err)
		}
		if err := serveAfterEnrol(context.Background(), dir, time.Now().Add(localstatus.Stale+time.Minute)); err != nil {
			t.Fatal(err)
		}
		if *calls != 1 {
			t.Errorf("serve ran %d times; want 1", *calls)
		}
	})
}
