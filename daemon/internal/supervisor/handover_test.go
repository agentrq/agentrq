// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package supervisor

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/agentrq/agentrq/daemon/wire"
)

func gatewayRequest(t *testing.T, id uint64) Request {
	t.Helper()
	return Request{
		ID: id, Kind: KindACPGateway, Dir: t.TempDir(), MCPURL: testURL,
		Params: Params{Model: "m", Agent: "a", ServerName: "agentrq-workspace"},
	}
}

// A handed-over session is stopped, and still named until it is started again:
// the backend deletes the row of anything a heartbeat leaves out.
func TestHandOverStopsTheSessionsAndKeepsNamingThem(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	for _, id := range []uint64{4, 2} {
		if _, err := s.Start(t.Context(), "work", gatewayRequest(t, id)); err != nil {
			t.Fatal(err)
		}
	}

	handed := s.HandOver(t.Context(), 5*time.Second)
	if len(handed) != 2 || !handed[0].HandedOver() || !handed[1].HandedOver() {
		t.Fatalf("handed over %v", handed)
	}
	if live := s.Live(); len(live) != 0 {
		t.Errorf("%d sessions are still running", len(live))
	}
	if got := s.Running(); !slices.Equal(got, []uint64{2, 4}) {
		t.Errorf("Running() = %v, want both sessions still named", got)
	}

	// Started again under the same id, which the stopped session must not
	// block, and named once.
	if _, err := s.Start(t.Context(), "work", gatewayRequest(t, 4)); err != nil {
		t.Fatalf("starting a handed-over session again: %v", err)
	}
	if got := s.Running(); !slices.Equal(got, []uint64{2, 4}) {
		t.Errorf("Running() = %v after one came back", got)
	}

	s.Abandon()
	if got := s.Running(); !slices.Equal(got, []uint64{4}) {
		t.Errorf("Running() = %v after abandoning the rest, want only the one running", got)
	}
}

// Its end is not reported: that report deletes the row the restored agent
// needs to be listed and stopped.
func TestAHandedOverSessionsEndIsNotReported(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	rep := &recordingReporter{}
	c := control(t, wire.OpStartSession, wire.StartSession{
		SessionID: 7, Kind: string(KindACPGateway), Dir: t.TempDir(),
		Model: "m", Agent: "a", MCPURL: testURL, ServerName: "agentrq-workspace",
	})
	if err := s.Handle(t.Context(), "work", c, rep); err != nil {
		t.Fatal(err)
	}
	rep.waitFor(t, string(StateRunning))

	s.HandOver(t.Context(), 5*time.Second)
	time.Sleep(50 * time.Millisecond) // time for a report that must not come
	for _, st := range rep.all() {
		if st.State != string(StateRunning) {
			t.Errorf("reported %q for a session that is coming back", st.State)
		}
	}
}

// A process that ignores the hang-up must not hold the restart open, and
// stays listed as the live process it still is.
func TestHandOverGivesUpOnAProcessThatWillNotGo(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	if _, err := s.Start(t.Context(), "work", gatewayRequest(t, 9)); err != nil {
		t.Fatal(err)
	}
	_, p := st.last()
	p.blockWait(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	start := time.Now()
	s.HandOver(ctx, time.Minute)
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("HandOver waited %v", elapsed)
	}
	if got := s.Running(); !slices.Equal(got, []uint64{9}) {
		t.Errorf("Running() = %v, want the session once", got)
	}
	if _, err := s.Get(9); err != nil {
		t.Errorf("a session that has not ended was dropped: %v", err)
	}
}

func TestHandOverWaitsNoLongerThanItsGrace(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	if _, err := s.Start(t.Context(), "work", gatewayRequest(t, 9)); err != nil {
		t.Fatal(err)
	}
	_, p := st.last()
	p.blockWait(t)

	start := time.Now()
	s.HandOver(t.Context(), 50*time.Millisecond)
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("HandOver waited %v", elapsed)
	}
}

// A session expected back that is running already is named once.
func TestAnExpectedSessionAlreadyRunningIsNamedOnce(t *testing.T) {
	s := New((&recordingStarter{}).start, 0, 0)
	if _, err := s.Start(t.Context(), "work", gatewayRequest(t, 5)); err != nil {
		t.Fatal(err)
	}
	s.Expect(5, 6)
	if got := s.Running(); !slices.Equal(got, []uint64{5, 6}) {
		t.Errorf("Running() = %v", got)
	}
}
