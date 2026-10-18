// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/golang/mock/gomock"
)

// serverWithClear is a WorkspaceServer with nothing but the clear seam wired.
//
// Built by hand rather than through NewWorkspaceServer: everything else that
// constructor takes is irrelevant to this decision, and passing twenty-odd
// nils would obscure the one field under test.
func serverWithClear(fn ClearAgentContextFunc) *WorkspaceServer {
	return &WorkspaceServer{
		workspaceID:       1,
		done:              make(chan struct{}),
		clearAgentContext: fn,
	}
}

func TestClearIsAskedForOnlyWhenTheTaskWantsIt(t *testing.T) {
	calls := 0
	ps := serverWithClear(func(context.Context) error {
		calls++
		return nil
	})

	ps.clearContextFor(context.Background(), model.Task{ID: 7, ClearContext: false})

	if calls != 0 {
		t.Errorf("clear called %d times for a task that did not ask, want 0", calls)
	}
}

func TestClearIsAskedForWhenTheTaskWantsIt(t *testing.T) {
	calls := 0
	ps := serverWithClear(func(context.Context) error {
		calls++
		return nil
	})

	// Closed up front so the settle wait returns immediately: the delay is
	// covered by its own test rather than paid for in every other one.
	close(ps.done)
	ps.clearContextFor(context.Background(), model.Task{ID: 7, ClearContext: true})

	if calls != 1 {
		t.Errorf("clear called %d times, want 1", calls)
	}
}

// The whole point of the error being returned is that the caller may log it.
// A task must still be pushed: a workspace with no machine, an offline daemon,
// an exited session and a socket held by another instance are all ordinary
// states, and none is a reason to withhold work from an agent waiting for it.
func TestAFailedClearDoesNotStopTheTask(t *testing.T) {
	ps := serverWithClear(func(context.Context) error {
		return errors.New("no running session for this workspace")
	})

	done := make(chan struct{})
	go func() {
		ps.clearContextFor(context.Background(), model.Task{ID: 7, ClearContext: true})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		// It must not wait out the settle delay either: there is nothing to
		// settle when nothing was sent.
		t.Fatal("a failed clear blocked the push")
	}
}

// A server built without the seam — which is every test of every other part of
// this file, and any future caller that has nothing to send to.
func TestNoClearSeamIsNotACrash(t *testing.T) {
	ps := serverWithClear(nil)

	ps.clearContextFor(context.Background(), model.Task{ID: 7, ClearContext: true})
}

// /clear travels down the terminal while the task travels over the MCP
// session. Two transports with no ordering between them, so without this pause
// the task can land while the agent is still acting on the clear and be wiped
// by it — the exact opposite of what was asked for.
func TestTheClearIsGivenTimeToLandBeforeTheTaskIsPushed(t *testing.T) {
	ps := serverWithClear(func(context.Context) error { return nil })

	start := time.Now()
	go func() {
		// Stands in for the server shutting down mid-wait, which is the only
		// thing that may cut the settle short.
		time.Sleep(20 * time.Millisecond)
		close(ps.done)
	}()
	ps.clearContextFor(context.Background(), model.Task{ID: 7, ClearContext: true})

	if elapsed := time.Since(start); elapsed >= ClearSettleDelay {
		t.Errorf("waited %v; a shutdown should cut the settle short", elapsed)
	}
	if ClearSettleDelay <= 0 {
		t.Error("there is no settle at all, so the push is a race")
	}
}

// ClearContextForTask is clearContextFor as seen by the REST handlers, which
// hold an entity's fields rather than a model.Task.
func TestClearContextForTaskDelegatesToClearContextFor(t *testing.T) {
	calls := 0
	ps := serverWithClear(func(context.Context) error {
		calls++
		return nil
	})
	close(ps.done)

	ps.ClearContextForTask(context.Background(), 7, true)
	if calls != 1 {
		t.Errorf("clear called %d times, want 1", calls)
	}

	ps.ClearContextForTask(context.Background(), 7, false)
	if calls != 1 {
		t.Errorf("clear called %d times for a task that did not ask, want still 1", calls)
	}
}

// The push repeats until the agent takes the task; the clear behind it must
// not, or each push would land in a context the next clear wipes. This is the
// state that tells the two apart.
func TestContextClearedIsRememberedUntilReconciled(t *testing.T) {
	ps := serverWithClear(nil)

	if ps.wasContextCleared(7) {
		t.Fatal("a task nothing has marked reads as already cleared")
	}

	ps.markContextCleared(7)
	if !ps.wasContextCleared(7) {
		t.Fatal("markContextCleared did not stick")
	}

	// Still notstarted on the next tick: still remembered, so the tick that
	// pushes it again does not clear again.
	ps.reconcileClearedTaskIDs(map[int64]struct{}{7: {}})
	if !ps.wasContextCleared(7) {
		t.Fatal("reconcile dropped a task that is still pending")
	}

	// No longer notstarted (taken, or resolved some other way): forgotten, so
	// the set does not grow forever and a later handover clears afresh.
	ps.reconcileClearedTaskIDs(map[int64]struct{}{})
	if ps.wasContextCleared(7) {
		t.Fatal("reconcile kept a task that is no longer pending")
	}
}

// serverWithClear leaves clearedTaskIDs nil, same as the zero value every
// caller outside this package's constructor sees; markContextCleared has to
// initialise it lazily rather than assume NewWorkspaceServer already did.
func TestMarkContextClearedOnANilMapDoesNotPanic(t *testing.T) {
	ps := &WorkspaceServer{}

	ps.markContextCleared(7)

	if !ps.wasContextCleared(7) {
		t.Fatal("markContextCleared did not stick on a lazily-initialised map")
	}
}

// The rule itself, at the level it is enforced: asking twice for the same task
// clears once. Every push after the first goes to an agent that has already
// been given its clean slate.
func TestClearIsAskedForOnlyOncePerTask(t *testing.T) {
	calls := 0
	ps := serverWithClear(func(context.Context) error {
		calls++
		return nil
	})
	close(ps.done)

	ps.clearContextFor(context.Background(), model.Task{ID: 7, ClearContext: true})
	ps.clearContextFor(context.Background(), model.Task{ID: 7, ClearContext: true})

	if calls != 1 {
		t.Errorf("clear called %d times for one task, want 1", calls)
	}
}

// A clear that failed sent nothing, so it is not remembered as done — the next
// push tries again rather than handing the agent the task on the very context
// it asked to be rid of.
func TestAFailedClearIsTriedAgainOnTheNextPush(t *testing.T) {
	calls := 0
	ps := serverWithClear(func(context.Context) error {
		calls++
		return errors.New("no running session for this workspace")
	})
	close(ps.done)

	ps.clearContextFor(context.Background(), model.Task{ID: 7, ClearContext: true})
	ps.clearContextFor(context.Background(), model.Task{ID: 7, ClearContext: true})

	if calls != 2 {
		t.Errorf("clear attempted %d times, want 2", calls)
	}
}

// A successful /clear is counted, so how often this happens is visible
// without reading a pty transcript.
func TestASuccessfulClearIsCounted(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPS := mock_pubsub.NewMockService(ctrl)
	ps := serverWithClear(func(context.Context) error { return nil })
	ps.pubsub = mockPS
	close(ps.done)

	var published MCPEvent
	mockPS.EXPECT().Publish(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, req pubsub.PublishRequest) (*pubsub.PublishResponse, error) {
			published = req.Event.(MCPEvent)
			return &pubsub.PublishResponse{}, nil
		})

	ps.clearContextFor(context.Background(), model.Task{ID: 7, ClearContext: true})

	if published.Action != ActionMCPClearContext {
		t.Errorf("published action = %v, want ActionMCPClearContext", published.Action)
	}
}

// A failed clear sent nothing down the pty, so it must not inflate the count
// of clears that actually happened.
func TestAFailedClearIsNotCounted(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPS := mock_pubsub.NewMockService(ctrl)
	ps := serverWithClear(func(context.Context) error { return errors.New("no running session") })
	ps.pubsub = mockPS

	mockPS.EXPECT().Publish(gomock.Any(), gomock.Any()).Times(0)

	ps.clearContextFor(context.Background(), model.Task{ID: 7, ClearContext: true})
}

// emitTelemetry is reachable from callers, like serverWithClear, that never
// wired a pubsub in — every other test in this file among them.
func TestEmitTelemetryWithNoPubsubDoesNotPanic(t *testing.T) {
	ps := serverWithClear(nil)

	ps.emitTelemetry(context.Background(), ActionMCPClearContext, "clear", clientIdentity{})
}

func TestActionMCPClearContextStringsAsClearContext(t *testing.T) {
	if got := ActionMCPClearContext.String(); got != "clear_context" {
		t.Errorf("ActionMCPClearContext.String() = %q, want %q", got, "clear_context")
	}
}

func TestActionMCPPlanUpdateStringsAsPlanUpdate(t *testing.T) {
	if got := ActionMCPPlanUpdate.String(); got != "plan_update" {
		t.Errorf("ActionMCPPlanUpdate.String() = %q, want %q", got, "plan_update")
	}
}
