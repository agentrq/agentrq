// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package machine

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/agentrq/agentrq/daemon/wire"
)

// releaseWatch is a liveAuth that says when a connection has been released,
// which is the last thing a daemon connection does before its handler ends.
type releaseWatch struct {
	*liveAuth
	err      error
	released chan struct{}
}

func (a *releaseWatch) Release(context.Context, int64, string) error {
	a.released <- struct{}{}
	return a.err
}

func watchedAuth(err error) *releaseWatch {
	return &releaseWatch{
		liveAuth: &liveAuth{token: "good", identity: Identity{MachineID: 7, UserID: 42}},
		err:      err,
		released: make(chan struct{}, 1),
	}
}

func (a *releaseWatch) wait(t *testing.T) {
	t.Helper()
	select {
	case <-a.released:
	case <-time.After(5 * time.Second):
		t.Fatal("the connection was never released")
	}
}

var goodBearer = http.Header{"Authorization": []string{"Bearer good"}}

func frameBytes(t *testing.T, f wire.Frame) []byte {
	t.Helper()
	b, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A frame that cannot be encoded is refused before the socket is touched, and
// a closed socket refuses everything and closes once.
func TestSocketAdaptersRefuseWhatTheyCannotSend(t *testing.T) {
	bad := wire.Frame{Type: wire.Type(0xEE)}
	good := outFrame(t, 1, []byte("x"))

	d := &wsConn{}
	if err := d.Send(bad); err == nil {
		t.Error("the daemon socket sent a frame of an unknown type")
	}
	d.closed = true
	if err := d.Send(good); err == nil {
		t.Error("a closed daemon socket accepted a frame")
	}
	if err := d.Close(); err != nil {
		t.Errorf("closing a closed daemon socket = %v, want nil", err)
	}

	v := &viewerConn{name: "Ada"}
	if v.Name() != "Ada" {
		t.Errorf("viewer name = %q, want Ada", v.Name())
	}
	if err := v.Send(bad); err == nil {
		t.Error("the viewer socket sent a frame of an unknown type")
	}
	v.closed = true
	if err := v.Send(good); !errors.Is(err, websocket.ErrCloseSent) {
		t.Errorf("a closed viewer socket = %v, want ErrCloseSent", err)
	}
	if err := v.Close(); err != nil {
		t.Errorf("closing a closed viewer socket = %v, want nil", err)
	}
}

// A daemon that authenticates but does not ask for a WebSocket is answered
// by the upgrade and never registered.
func TestADaemonRequestThatIsNotAnUpgradeIsNotRegistered(t *testing.T) {
	r := NewRegistry("pod-a")
	srv := newServer(t, testHandler(watchedAuth(nil), r, nil))
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header = goodBearer.Clone()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status %d, want 400", resp.StatusCode)
	}
	if r.Count() != 0 {
		t.Error("a request that was never upgraded was registered")
	}
}

// A connection whose instance cannot be recorded is closed rather than served,
// since nothing could route to it.
func TestAConnectionThatCannotBeRecordedIsClosed(t *testing.T) {
	r := NewRegistry("pod-a")
	auth := watchedAuth(nil)
	auth.touchErr = errors.New("database down")
	srv := newServer(t, testHandler(auth, r, nil))

	ws, _, err := dial(t, srv, goodBearer)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = ws.Close() }()
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, _, err := ws.ReadMessage(); err == nil {
		t.Error("an unrecorded connection was kept open")
	}
	if r.Count() != 0 {
		t.Error("an unrecorded connection stayed registered")
	}
}

// Every pong is a heartbeat, and a heartbeat that cannot be recorded does not
// end the connection. A release that fails still ends it.
func TestPongsAreHeartbeats(t *testing.T) {
	r := NewRegistry("pod-a")
	auth := watchedAuth(errors.New("release failed"))
	got := make(chan wire.Frame, 1)
	srv := newServer(t, testHandler(auth, r, func(_ context.Context, _ *Session, f wire.Frame) error {
		got <- f
		return nil
	}))

	ws, _, err := dial(t, srv, goodBearer)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	waitFor(t, func() bool { touched, _ := auth.counts(); return touched == 1 }, "the connection was never recorded")

	pong := func() {
		t.Helper()
		if err := ws.WriteControl(websocket.PongMessage, nil, time.Now().Add(5*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	pong()
	waitFor(t, func() bool { touched, _ := auth.counts(); return touched == 2 }, "a pong was not counted as a heartbeat")

	auth.mu.Lock()
	auth.touchErr = errors.New("database down")
	auth.mu.Unlock()
	pong()
	if err := ws.WriteMessage(websocket.BinaryMessage, frameBytes(t, outFrame(t, 1, []byte("still here")))); err != nil {
		t.Fatal(err)
	}
	select {
	case f := <-got:
		if string(f.Payload) != "still here" {
			t.Errorf("frame after a failed heartbeat = %q", f.Payload)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a failed heartbeat ended the connection")
	}

	_ = ws.Close()
	auth.wait(t)
}

// With nobody handling frames they are read and dropped; a frame that does not
// decode ends the connection.
func TestAnUndecodableFrameEndsTheConnection(t *testing.T) {
	r := NewRegistry("pod-a")
	auth := watchedAuth(nil)
	srv := newServer(t, testHandler(auth, r, nil))

	ws, _, err := dial(t, srv, goodBearer)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = ws.Close() }()
	if err := ws.WriteMessage(websocket.BinaryMessage, frameBytes(t, outFrame(t, 1, []byte("dropped")))); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteMessage(websocket.BinaryMessage, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	auth.wait(t)
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, _, err := ws.ReadMessage(); err == nil {
		t.Error("the connection survived a frame that does not decode")
	}
}

// viewerWithDaemon serves session 7 of machine 11, whose daemon socket is
// daemon, and says when a viewer has been detached.
func viewerWithDaemon(t *testing.T, daemon Conn) (*httptest.Server, *Relay, chan struct{}, chan struct{}) {
	t.Helper()
	reg := NewRegistry("pod-a")
	reg.Add(11, daemon)
	relay := NewRelay(reg)
	attached, detached := make(chan struct{}, 1), make(chan struct{}, 1)
	h := &ViewerHandler{
		Relay:     relay,
		Lookup:    fakeLookup{at: Attachment{MachineID: 11, InstanceID: "pod-a", UserID: 3, ViewerName: "Ada"}},
		SessionID: func(*http.Request) uint64 { return 7 },
		OnAttach:  func(*http.Request, uint64, Attachment) { attached <- struct{}{} },
		OnDetach:  func(*http.Request, uint64, Attachment) { detached <- struct{}{} },
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, relay, attached, detached
}

func waitDetached(t *testing.T, detached chan struct{}) {
	t.Helper()
	select {
	case <-detached:
	case <-time.After(5 * time.Second):
		t.Fatal("the viewer was never detached")
	}
}

func TestAViewerRequestThatIsNotAnUpgradeIsNotAttached(t *testing.T) {
	srv, relay, _, _ := viewerWithDaemon(t, &fakeConn{})
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status %d, want 400", resp.StatusCode)
	}
	if relay.Viewers(7) != 0 {
		t.Error("a request that was never upgraded was attached")
	}
}

// When the daemon cannot be asked to stream, the browser's socket is closed
// and nobody is told it attached.
func TestAViewerIsClosedWhenTheDaemonCannotBeAsked(t *testing.T) {
	srv, _, attached, _ := viewerWithDaemon(t, &fakeConn{sendErr: errors.New("daemon gone")})
	ws := dialViewer(t, srv)
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		if _, _, err := ws.ReadMessage(); err != nil {
			break
		}
	}
	select {
	case <-attached:
		t.Error("an attach the daemon never heard of was reported")
	default:
	}
}

// A pong keeps the viewer attached; a text frame ends it.
func TestAViewerPongKeepsItAttachedAndTextEndsIt(t *testing.T) {
	daemon := &fakeConn{}
	srv, relay, attached, detached := viewerWithDaemon(t, daemon)
	ws := dialViewer(t, srv)
	select {
	case <-attached:
	case <-time.After(5 * time.Second):
		t.Fatal("the viewer never attached")
	}

	if err := ws.WriteControl(websocket.PongMessage, nil, time.Now().Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, wire.HeaderSize+1)
	raw[0] = byte(wire.TypeInput)
	raw[wire.HeaderSize] = 'k'
	if err := ws.WriteMessage(websocket.BinaryMessage, raw); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { _, ok := daemon.firstOfType(wire.TypeInput); return ok }, "input after a pong never reached the daemon")

	if err := ws.WriteMessage(websocket.TextMessage, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	waitDetached(t, detached)
	if relay.Viewers(7) != 0 {
		t.Error("a viewer that sent text is still attached")
	}
}

// A frame that does not decode, and a frame a viewer may not send, each end
// the viewer's connection.
func TestAViewerSendingNonsenseIsDetached(t *testing.T) {
	output := make([]byte, wire.HeaderSize+1)
	output[0] = byte(wire.TypeOutput)
	for name, msg := range map[string][]byte{
		"short frame":          {byte(wire.TypeInput), 0},
		"output from a viewer": output,
	} {
		t.Run(name, func(t *testing.T) {
			daemon := &fakeConn{}
			srv, relay, _, detached := viewerWithDaemon(t, daemon)
			ws := dialViewer(t, srv)
			waitFor(t, func() bool { return relay.Viewers(7) == 1 }, "the viewer never attached")
			if err := ws.WriteMessage(websocket.BinaryMessage, msg); err != nil {
				t.Fatal(err)
			}
			waitDetached(t, detached)
			if n := daemon.countOfType(wire.TypeOutput); n != 0 {
				t.Errorf("the daemon was sent %d output frames by a viewer", n)
			}
		})
	}
}
