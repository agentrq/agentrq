// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/memq"
)

// steps records the order shutdown does things in.
type steps []string

type orderedServer struct {
	steps *steps
	err   error
}

func (*orderedServer) Run() error { return nil }
func (s *orderedServer) Shutdown(context.Context) error {
	*s.steps = append(*s.steps, "drain http")
	return s.err
}

type orderedQueue struct {
	memq.Service
	steps *steps
	err   error
}

func (q *orderedQueue) Drain(context.Context) error {
	*q.steps = append(*q.steps, "send queued emails")
	return q.err
}

// In-flight requests finish before the app context is cancelled, so the
// consumers wired to it still see what they publish; queued emails go last.
func TestShutdownDrainsRequestsBeforeCancellingTheApp(t *testing.T) {
	for _, tt := range []struct {
		name      string
		serverErr error
		queueErr  error
	}{
		{name: "Clean"},
		{name: "DeadlinePassed", serverErr: context.DeadlineExceeded, queueErr: context.DeadlineExceeded},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var got steps
			a := &App{
				server: &orderedServer{steps: &got, err: tt.serverErr},
				memq:   &orderedQueue{steps: &got, err: tt.queueErr},
				cancel: func() { got = append(got, "cancel app context") },
			}
			err := a.Shutdown(context.Background())
			if !errors.Is(err, tt.serverErr) {
				t.Errorf("Shutdown = %v, want the server's %v", err, tt.serverErr)
			}
			want := steps{"drain http", "cancel app context", "send queued emails"}
			if !slices.Equal(got, want) {
				t.Errorf("shutdown did %v, want %v", got, want)
			}
		})
	}
}

// Shutdown closes the database New opened, last: a background sweep still
// querying it would otherwise outlive the app.
func TestShutdownClosesTheDatabaseItOpened(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	if err := a.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown returned %v", err)
	}
	if err := a.db.Conn(ctx).Exec("SELECT 1").Error; err == nil || !strings.Contains(err.Error(), "database is closed") {
		t.Fatalf("a query after shutdown returned %v, want the database closed", err)
	}
}
