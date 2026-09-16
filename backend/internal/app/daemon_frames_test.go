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
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/agentrq/agentrq/daemon/wire"
)

type recordedState struct {
	reqs       []entity.UpdateSessionStateRequest
	metrics    []entity.RecordMachineMetricsRequest
	reconciled []entity.ReconcileSessionsRequest
	offers     []entity.RecordAvailableVersionRequest
	versions   []entity.RecordMachineVersionRequest
	err        error
}

func (r *recordedState) UpdateSessionState(_ context.Context, req entity.UpdateSessionStateRequest) error {
	r.reqs = append(r.reqs, req)
	return r.err
}

func (r *recordedState) RecordMachineMetrics(_ context.Context, req entity.RecordMachineMetricsRequest) error {
	r.metrics = append(r.metrics, req)
	return r.err
}

func (r *recordedState) RecordAvailableVersion(_ context.Context, req entity.RecordAvailableVersionRequest) error {
	r.offers = append(r.offers, req)
	return r.err
}

func (r *recordedState) RecordMachineVersion(_ context.Context, req entity.RecordMachineVersionRequest) error {
	r.versions = append(r.versions, req)
	return r.err
}

func (r *recordedState) ReconcileSessions(_ context.Context, req entity.ReconcileSessionsRequest) error {
	r.reconciled = append(r.reconciled, req)
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

	handle := daemonFrames(relay, &recordedState{}, nil)
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
	handle := daemonFrames(machine.NewRelay(machine.NewRegistry("pod-a")), rec, nil)
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
	handle := daemonFrames(machine.NewRelay(machine.NewRegistry("pod-a")), rec, nil)

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
	handle := daemonFrames(machine.NewRelay(machine.NewRegistry("pod-a")), rec, nil)

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

func heartbeatFrame(t *testing.T, hb wire.Heartbeat) wire.Frame {
	t.Helper()
	b, err := json.Marshal(hb)
	if err != nil {
		t.Fatal(err)
	}
	f, err := wire.ControlFrame(wire.Control{Op: wire.OpHeartbeat, Body: b})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestAHeartbeatIsRecorded(t *testing.T) {
	rec := &recordedState{}
	handle := daemonFrames(machine.NewRelay(machine.NewRegistry("pod-a")), rec, nil)
	session := &machine.Session{Identity: machine.Identity{MachineID: 11}}

	err := handle(context.Background(), session, heartbeatFrame(t, wire.Heartbeat{
		MemTotal: 16 << 30, MemAvailable: 4 << 30,
		CPUPercent: 37.4, LoadAvg: []float64{1.2, 0.9, 0.7}, UptimeSec: 918273,
		Disks: []wire.Disk{{Mount: "/", Total: 100, Free: 40}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.metrics) != 1 {
		t.Fatalf("recorded %d snapshots", len(rec.metrics))
	}
	got := rec.metrics[0]
	if got.MemAvailable != 4<<30 || got.CPUPercent != 37.4 || got.UptimeSec != 918273 {
		t.Errorf("metrics = %+v", got)
	}
	if len(got.LoadAvg) != 3 {
		t.Errorf("loadAvg = %v", got.LoadAvg)
	}
	if len(got.Disks) != 1 || got.Disks[0].Mount != "/" || got.Disks[0].Free != 40 {
		t.Errorf("disks = %+v", got.Disks)
	}
	if got.ReportedAt.IsZero() {
		t.Error("a snapshot with no time on it cannot be shown as stale")
	}
}

// The machine id comes from the authenticated socket, never from the payload:
// a heartbeat claiming somebody else's machine is the one thing this must not
// honour.
func TestAHeartbeatCanOnlyDescribeItsOwnMachine(t *testing.T) {
	rec := &recordedState{}
	handle := daemonFrames(machine.NewRelay(machine.NewRegistry("pod-a")), rec, nil)
	session := &machine.Session{Identity: machine.Identity{MachineID: 11}}

	if err := handle(context.Background(), session, heartbeatFrame(t, wire.Heartbeat{MemTotal: 1})); err != nil {
		t.Fatal(err)
	}
	if rec.metrics[0].MachineID != 11 {
		t.Errorf("metrics were recorded against machine %d", rec.metrics[0].MachineID)
	}
}

// Three zeroes render as a perfectly idle machine. A platform with no load
// average must arrive with none.
func TestAMachineWithNoLoadAverageRecordsNone(t *testing.T) {
	rec := &recordedState{}
	handle := daemonFrames(machine.NewRelay(machine.NewRegistry("pod-a")), rec, nil)

	if err := handle(context.Background(), &machine.Session{}, heartbeatFrame(t, wire.Heartbeat{
		MemTotal: 1, CPUPercent: 3,
	})); err != nil {
		t.Fatal(err)
	}
	if rec.metrics[0].LoadAvg != nil {
		t.Errorf("loadAvg = %v, want nothing at all", rec.metrics[0].LoadAvg)
	}
}

// A daemon that restarted comes back supervising nothing, and the rows it left
// behind would otherwise sit as running forever.
func TestWhatTheDaemonIsRunningReconcilesTheRows(t *testing.T) {
	rec := &recordedState{}
	handle := daemonFrames(machine.NewRelay(machine.NewRegistry("pod-a")), rec, nil)
	session := &machine.Session{Identity: machine.Identity{MachineID: 11}}

	hello, err := wire.ControlFrame(wire.Control{Op: wire.OpHello, Body: mustBytes(t, wire.Hello{
		Version: "1.0", Sessions: []uint64{9, 3},
	})})
	if err != nil {
		t.Fatal(err)
	}
	if err := handle(context.Background(), session, hello); err != nil {
		t.Fatal(err)
	}

	if len(rec.reconciled) != 1 {
		t.Fatalf("reconciled %d times", len(rec.reconciled))
	}
	got := rec.reconciled[0]
	if got.MachineID != 11 {
		t.Errorf("reconciled machine %d", got.MachineID)
	}
	if len(got.Running) != 2 || got.Running[0] != 9 || got.Running[1] != 3 {
		t.Errorf("running = %v", got.Running)
	}

	// And a daemon that came back with nothing says so, which is the case that
	// matters: an empty list must end every row, not be read as "no news".
	if err := handle(context.Background(), session, heartbeatFrame(t, wire.Heartbeat{})); err != nil {
		t.Fatal(err)
	}
	if len(rec.reconciled) != 2 || len(rec.reconciled[1].Running) != 0 {
		t.Errorf("a daemon supervising nothing reconciled %+v", rec.reconciled)
	}
}

func mustBytes(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Nothing a daemon sends costs it its connection, metrics included.
func TestABadHeartbeatDoesNotEndTheConnection(t *testing.T) {
	rec := &recordedState{err: errors.New("database is down")}
	handle := daemonFrames(machine.NewRelay(machine.NewRegistry("pod-a")), rec, nil)

	bad, err := wire.ControlFrame(wire.Control{Op: wire.OpHeartbeat, Body: []byte(`"a string"`)})
	if err != nil {
		t.Fatal(err)
	}
	badHello, err := wire.ControlFrame(wire.Control{Op: wire.OpHello, Body: []byte(`7`)})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []wire.Frame{bad, badHello, heartbeatFrame(t, wire.Heartbeat{MemTotal: 1})} {
		if err := handle(context.Background(), &machine.Session{}, f); err != nil {
			t.Errorf("a heartbeat ended the connection: %v", err)
		}
	}
}

type announced struct {
	userID string
	evt    eventbus.Event
}

func collectAnnouncements(got *[]announced) notifier {
	return func(userID string, evt eventbus.Event) {
		*got = append(*got, announced{userID: userID, evt: evt})
	}
}

// A session that failed to start is exactly the moment somebody needs to be
// told why, and the reason is not stored on the row — the event stream is how
// it reaches them.
func TestASessionChangeReachesTheBrowser(t *testing.T) {
	var sent []announced
	handle := daemonFrames(machine.NewRelay(machine.NewRegistry("pod-a")), &recordedState{},
		collectAnnouncements(&sent))
	session := &machine.Session{Identity: machine.Identity{MachineID: 11, UserID: 3}}

	err := handle(context.Background(), session, controlFrame(t, wire.SessionState{
		SessionID: 9, State: machine.SessionFailed, Error: "working directory does not exist: /srv/app",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 {
		t.Fatalf("announced %d times", len(sent))
	}
	if sent[0].userID != monoflake.ID(3).String() {
		t.Errorf("announced to %q", sent[0].userID)
	}
	if sent[0].evt.Type != "session.updated" {
		t.Errorf("event type = %q", sent[0].evt.Type)
	}
	payload := sent[0].evt.Payload.(map[string]any)
	if payload["status"] != machine.SessionFailed {
		t.Errorf("status = %v", payload["status"])
	}
	if payload["error"] != "working directory does not exist: /srv/app" {
		t.Errorf("the reason did not travel: %v", payload["error"])
	}
}

// The numbers that just arrived are the numbers; reading them back would be a
// query per heartbeat per machine to learn what the handler already held.
func TestAHeartbeatReachesTheBrowser(t *testing.T) {
	var sent []announced
	handle := daemonFrames(machine.NewRelay(machine.NewRegistry("pod-a")), &recordedState{},
		collectAnnouncements(&sent))
	session := &machine.Session{Identity: machine.Identity{MachineID: 11, UserID: 3}}

	if err := handle(context.Background(), session, heartbeatFrame(t, wire.Heartbeat{
		MemTotal: 16 << 30, MemAvailable: 4 << 30, CPUPercent: 37.4,
		Disks: []wire.Disk{{Mount: "/", Total: 100, Free: 40}},
	})); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0].evt.Type != "machine.updated" {
		t.Fatalf("announced %+v", sent)
	}
	payload := sent[0].evt.Payload.(map[string]any)
	if payload["id"] != monoflake.ID(11).String() {
		t.Errorf("machine id = %v", payload["id"])
	}
	m := payload["metrics"].(entity.MachineMetricsView)
	if m.CPUPercent != 37.4 || len(m.Disks) != 1 {
		t.Errorf("metrics = %+v", m)
	}
}

// A backend with nowhere to send events still records everything.
func TestNothingRequiresSomebodyToBeWatching(t *testing.T) {
	rec := &recordedState{}
	handle := daemonFrames(machine.NewRelay(machine.NewRegistry("pod-a")), rec, nil)

	if err := handle(context.Background(), &machine.Session{}, heartbeatFrame(t, wire.Heartbeat{MemTotal: 1})); err != nil {
		t.Fatal(err)
	}
	if len(rec.metrics) != 1 {
		t.Error("metrics were lost when nobody was listening")
	}
}

// An offer and nothing more: the panel shows it and somebody decides.
func TestAnUpdateOfferIsRecordedAndAnnounced(t *testing.T) {
	rec := &recordedState{}
	var sent []announced
	handle := daemonFrames(machine.NewRelay(machine.NewRegistry("pod-a")), rec, collectAnnouncements(&sent))
	session := &machine.Session{Identity: machine.Identity{MachineID: 11, UserID: 3}}

	offer, err := wire.ControlFrame(wire.Control{Op: wire.OpUpdateAvailable, Body: mustBytes(t, wire.UpdateAvailable{
		Version: "0.7.1",
	})})
	if err != nil {
		t.Fatal(err)
	}
	if err := handle(context.Background(), session, offer); err != nil {
		t.Fatal(err)
	}

	if len(rec.offers) != 1 || rec.offers[0].Version != "0.7.1" {
		t.Fatalf("recorded %+v", rec.offers)
	}
	// The machine id comes from the socket, never the payload.
	if rec.offers[0].MachineID != 11 {
		t.Errorf("recorded against machine %d", rec.offers[0].MachineID)
	}
	if len(sent) != 1 || sent[0].evt.Type != "machine.updated" {
		t.Fatalf("announced %+v", sent)
	}
	if sent[0].evt.Payload.(map[string]any)["availableVersion"] != "0.7.1" {
		t.Errorf("announced %+v", sent[0].evt.Payload)
	}
}

// A restored session is a new process with an empty terminal. It is recorded
// as restored so the panel can say so rather than leaving somebody wondering.
func TestARestoredSessionIsRecordedAsOne(t *testing.T) {
	rec := &recordedState{}
	var sent []announced
	handle := daemonFrames(machine.NewRelay(machine.NewRegistry("pod-a")), rec, collectAnnouncements(&sent))

	err := handle(context.Background(), &machine.Session{}, controlFrame(t, wire.SessionState{
		SessionID: 9, State: machine.SessionRunning, Restored: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !rec.reqs[0].Restored {
		t.Error("a restored session was recorded as an ordinary one")
	}
	if sent[0].evt.Payload.(map[string]any)["restored"] != true {
		t.Error("the browser was not told the session was restored")
	}
}

func TestABadUpdateOfferDoesNotEndTheConnection(t *testing.T) {
	rec := &recordedState{err: errors.New("database is down")}
	handle := daemonFrames(machine.NewRelay(machine.NewRegistry("pod-a")), rec, nil)

	bad, err := wire.ControlFrame(wire.Control{Op: wire.OpUpdateAvailable, Body: []byte(`"a string"`)})
	if err != nil {
		t.Fatal(err)
	}
	good, err := wire.ControlFrame(wire.Control{Op: wire.OpUpdateAvailable, Body: mustBytes(t, wire.UpdateAvailable{Version: "0.7.1"})})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []wire.Frame{bad, good} {
		if err := handle(context.Background(), &machine.Session{}, f); err != nil {
			t.Errorf("an update offer ended the connection: %v", err)
		}
	}
}

// The hello is the only message that says what is actually running, which is
// how a machine that has just replaced itself stops showing the version it
// replaced and an offer it has already taken.
func TestTheHelloRecordsWhatIsRunning(t *testing.T) {
	rec := &recordedState{}
	var sent []announced
	handle := daemonFrames(machine.NewRelay(machine.NewRegistry("pod-a")), rec, collectAnnouncements(&sent))
	session := &machine.Session{Identity: machine.Identity{MachineID: 11, UserID: 3}}

	hello, err := wire.ControlFrame(wire.Control{Op: wire.OpHello, Body: mustBytes(t, wire.Hello{
		Version: "0.7.1",
	})})
	if err != nil {
		t.Fatal(err)
	}
	if err := handle(context.Background(), session, hello); err != nil {
		t.Fatal(err)
	}

	if len(rec.versions) != 1 || rec.versions[0].Version != "0.7.1" || rec.versions[0].MachineID != 11 {
		t.Fatalf("recorded %+v", rec.versions)
	}
	if len(sent) != 1 || sent[0].evt.Payload.(map[string]any)["version"] != "0.7.1" {
		t.Errorf("announced %+v", sent)
	}
}

// A daemon that reports no version at all is not worth announcing as one.
func TestAHelloWithNoVersionAnnouncesNothing(t *testing.T) {
	var sent []announced
	handle := daemonFrames(machine.NewRelay(machine.NewRegistry("pod-a")), &recordedState{},
		collectAnnouncements(&sent))

	hello, err := wire.ControlFrame(wire.Control{Op: wire.OpHello, Body: mustBytes(t, wire.Hello{})})
	if err != nil {
		t.Fatal(err)
	}
	if err := handle(context.Background(), &machine.Session{}, hello); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 0 {
		t.Errorf("announced %+v for a daemon that named no version", sent)
	}
}
