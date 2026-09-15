// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/controller/machine"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/daemon/wire"
)

type recordedState struct {
	reqs []entity.UpdateSessionStateRequest
	err  error
}

func (r *recordedState) UpdateSessionState(_ context.Context, req entity.UpdateSessionStateRequest) error {
	r.reqs = append(r.reqs, req)
	return r.err
}

type countingViewer struct {
	got []wire.Frame
}

func (v *countingViewer) Send(f wire.Frame) error { v.got = append(v.got, f); return nil }
func (v *countingViewer) Close() error            { return nil }
func (v *countingViewer) Name() string            { return "Ada" }

// quietConn stands in for a daemon socket that accepts whatever it is sent.
type quietConn struct{}

func (quietConn) Send(wire.Frame) error { return nil }
func (quietConn) Close() error          { return nil }

// sessionFrames are copied to the viewers untouched. The backend does not read
// terminal traffic, so there is nothing else it could do with one.
func TestSessionFramesReachTheViewers(t *testing.T) {
	reg := machine.NewRegistry("pod-a")
	reg.Add(11, quietConn{})
	relay := machine.NewRelay(reg)
	v := &countingViewer{}
	if err := relay.Attach(9, 11, v); err != nil {
		t.Fatalf("attach: %v", err)
	}
	v.got = nil // the presence announcement; the terminal traffic is below

	handle := daemonFrames(relay, &recordedState{})
	f, err := wire.SessionFrame(wire.TypeOutput, 9, []byte{0x1b, 0x5b, 0x41})
	if err != nil {
		t.Fatal(err)
	}
	if err := handle(context.Background(), &machine.Session{}, f); err != nil {
		t.Fatalf("routing a session frame failed: %v", err)
	}
	if len(v.got) != 1 || string(v.got[0].Payload) != "\x1b[A" {
		t.Fatalf("the viewer got %+v", v.got)
	}
}

func controlFrame(t *testing.T, st wire.SessionState) wire.Frame {
	t.Helper()
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	f, err := wire.ControlFrame(wire.Control{Op: wire.OpSessionState, Body: b})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestSessionStateIsRecorded(t *testing.T) {
	rec := &recordedState{}
	handle := daemonFrames(machine.NewRelay(machine.NewRegistry("pod-a")), rec)
	code := 130

	if err := handle(context.Background(), &machine.Session{}, controlFrame(t, wire.SessionState{
		SessionID: 9, State: machine.SessionExited, ExitCode: &code,
	})); err != nil {
		t.Fatal(err)
	}
	if len(rec.reqs) != 1 {
		t.Fatalf("recorded %d states", len(rec.reqs))
	}
	got := rec.reqs[0]
	if got.SessionID != monoflake.ID(9).String() {
		t.Errorf("session id = %q", got.SessionID)
	}
	if got.Status != machine.SessionExited || got.ExitCode == nil || *got.ExitCode != 130 {
		t.Errorf("state = %+v", got)
	}
	if got.EndedAt == nil {
		t.Error("an exited session was recorded with no end time")
	}
}

// A session that has only just started has not ended. Stamping an end time on
// every report would make each one look finished the moment it began.
func TestARunningSessionHasNotEnded(t *testing.T) {
	rec := &recordedState{}
	handle := daemonFrames(machine.NewRelay(machine.NewRegistry("pod-a")), rec)

	if err := handle(context.Background(), &machine.Session{}, controlFrame(t, wire.SessionState{
		SessionID: 9, State: machine.SessionRunning,
	})); err != nil {
		t.Fatal(err)
	}
	if rec.reqs[0].EndedAt != nil {
		t.Error("a running session was given an end time")
	}
}

// Nothing a daemon can send should cost it its connection — the machine's
// other sessions are on it.
func TestNoFrameEndsTheConnection(t *testing.T) {
	rec := &recordedState{err: errors.New("database is down")}
	handle := daemonFrames(machine.NewRelay(machine.NewRegistry("pod-a")), rec)

	unreadable := wire.Frame{Type: wire.TypeControl, Payload: []byte("{not json")}
	unknownOp, err := wire.ControlFrame(wire.Control{Op: "somethingNewer"})
	if err != nil {
		t.Fatal(err)
	}
	badBody, err := wire.ControlFrame(wire.Control{Op: wire.OpSessionState, Body: []byte(`"a string"`)})
	if err != nil {
		t.Fatal(err)
	}
	failing := controlFrame(t, wire.SessionState{SessionID: 9, State: machine.SessionRunning})

	for name, f := range map[string]wire.Frame{
		"unreadable control":              unreadable,
		"an op from a newer daemon":       unknownOp,
		"a state body of the wrong shape": badBody,
		"a state the database refused":    failing,
	} {
		if err := handle(context.Background(), &machine.Session{}, f); err != nil {
			t.Errorf("%s ended the connection: %v", name, err)
		}
	}
}
