// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package machine

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/agentrq/agentrq/daemon/wire"
)

// liveAuth is an Authenticator that accepts one token.
type liveAuth struct {
	fakeAuth
	token    string
	identity Identity
	disabled bool
}

func (a *liveAuth) AuthenticateMachine(_ context.Context, token string) (Identity, error) {
	if token != a.token {
		return Identity{}, ErrUnknownToken
	}
	if a.disabled {
		return Identity{}, ErrMachineDisabled
	}
	return a.identity, nil
}

// dial connects to a test server as a daemon would.
func dial(t *testing.T, srv *httptest.Server, headers http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	return websocket.DefaultDialer.Dial(url, headers)
}

func newServer(t *testing.T, h *Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func testHandler(auth Authenticator, r *Registry, onFrame func(context.Context, *Session, wire.Frame) error) *Handler {
	return &Handler{
		Registry: r,
		Auth:     auth,
		DecodeID: func(string) int64 { return 0 },
		OnFrame:  onFrame,
	}
}

// The assertion the whole terminal feature rests on, now through a real
// socket: arbitrary bytes survive the trip.
func TestFramesSurviveARealWebSocket(t *testing.T) {
	r := NewRegistry("pod-a")
	auth := &liveAuth{token: "good", identity: Identity{MachineID: 7, UserID: 42}}

	got := make(chan wire.Frame, 1)
	h := testHandler(auth, r, func(_ context.Context, _ *Session, f wire.Frame) error {
		got <- f
		return nil
	})
	srv := newServer(t, h)

	hdr := http.Header{"Authorization": []string{"Bearer good"}}
	ws, _, err := dial(t, srv, hdr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = ws.Close() }()

	payload := []byte{0x1b, '[', '2', 'K', 0xff, 0xfe, 0x00}
	f, err := wire.SessionFrame(wire.TypeOutput, 9, payload)
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteMessage(websocket.BinaryMessage, b); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case f := <-got:
		if string(f.Payload) != string(payload) {
			t.Errorf("payload changed: got %#v want %#v", f.Payload, payload)
		}
		if f.SessionID != 9 || f.Type != wire.TypeOutput {
			t.Errorf("header changed: %s", f)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no frame arrived")
	}
}

func TestSocketRefusesWithoutAGoodToken(t *testing.T) {
	r := NewRegistry("pod-a")
	auth := &liveAuth{token: "good", identity: Identity{MachineID: 7}}
	srv := newServer(t, testHandler(auth, r, nil))

	tests := []struct {
		name   string
		header http.Header
		want   int
	}{
		{"no header", http.Header{}, http.StatusUnauthorized},
		{"wrong scheme", http.Header{"Authorization": []string{"Basic good"}}, http.StatusUnauthorized},
		{"wrong token", http.Header{"Authorization": []string{"Bearer bad"}}, http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, resp, err := dial(t, srv, tc.header)
			if err == nil {
				t.Fatal("the connection was accepted")
			}
			if resp == nil || resp.StatusCode != tc.want {
				t.Errorf("status = %v, want %d", resp, tc.want)
			}
		})
	}
	if r.Count() != 0 {
		t.Errorf("a refused connection was registered: %d", r.Count())
	}
}

// Disabled is the kill switch, and whoever just used it deserves to see why.
// An unknown token gets no such explanation.
func TestDisabledMachineIsToldSoAndUnknownTokenIsNot(t *testing.T) {
	r := NewRegistry("pod-a")
	auth := &liveAuth{token: "good", identity: Identity{MachineID: 7}, disabled: true}
	srv := newServer(t, testHandler(auth, r, nil))

	_, resp, err := dial(t, srv, http.Header{"Authorization": []string{"Bearer good"}})
	if err == nil {
		t.Fatal("a disabled machine was allowed to connect")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %v, want 403 for a disabled machine", resp)
	}
}

// Identity comes from the token. A header that contradicts it has no
// legitimate cause.
func TestContradictoryIdentityHeaderIsRefused(t *testing.T) {
	r := NewRegistry("pod-a")
	auth := &liveAuth{token: "good", identity: Identity{MachineID: 7, UserID: 42}}
	h := testHandler(auth, r, nil)
	h.DecodeID = func(s string) int64 {
		if s == "other" {
			return 999
		}
		return 0
	}
	srv := newServer(t, h)

	_, resp, err := dial(t, srv, http.Header{
		"Authorization":        []string{"Bearer good"},
		"X-Agentrq-Machine-Id": []string{"other"},
	})
	if err == nil {
		t.Fatal("a contradictory header was accepted")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %v, want 403", resp)
	}
}

func TestConnectionRegistersAndReleases(t *testing.T) {
	r := NewRegistry("pod-a")
	auth := &liveAuth{token: "good", identity: Identity{MachineID: 7, UserID: 42}}
	srv := newServer(t, testHandler(auth, r, nil))

	ws, _, err := dial(t, srv, http.Header{"Authorization": []string{"Bearer good"}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Registered shortly after the upgrade.
	deadline := time.Now().Add(3 * time.Second)
	for r.Count() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if r.Count() != 1 {
		t.Fatal("the connection was never registered")
	}

	_ = ws.Close()

	// And gone once the daemon disconnects.
	deadline = time.Now().Add(3 * time.Second)
	for r.Count() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if r.Count() != 0 {
		t.Error("the connection was not released after the socket closed")
	}
	if _, released := auth.counts(); released == 0 {
		t.Error("the stored pairing was not cleared")
	}
}

// The protocol is binary. A text frame is a client that has misunderstood it,
// and continuing would mean guessing.
func TestTextFramesEndTheConnection(t *testing.T) {
	r := NewRegistry("pod-a")
	auth := &liveAuth{token: "good", identity: Identity{MachineID: 7}}
	srv := newServer(t, testHandler(auth, r, nil))

	ws, _, err := dial(t, srv, http.Header{"Authorization": []string{"Bearer good"}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := ws.WriteMessage(websocket.TextMessage, []byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, _, err := ws.ReadMessage(); err == nil {
		t.Error("the connection survived a text frame")
	}
}

// gorilla permits one concurrent writer, and this socket is written to by the
// relay, by control messages and by the keepalive at once. Without the mutex
// those interleave and corrupt a frame — a terminal that garbles under load,
// far from the code that caused it.
func TestConcurrentSendsDoNotCorruptFrames(t *testing.T) {
	r := NewRegistry("pod-a")
	auth := &liveAuth{token: "good", identity: Identity{MachineID: 7}}
	srv := newServer(t, testHandler(auth, r, nil))

	ws, _, err := dial(t, srv, http.Header{"Authorization": []string{"Bearer good"}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = ws.Close() }()

	deadline := time.Now().Add(3 * time.Second)
	for r.Count() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	conn, err := r.Get(7)
	if err != nil {
		t.Fatalf("not registered: %v", err)
	}

	const writers, each = 8, 25
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			f, _ := wire.SessionFrame(wire.TypeOutput, uint64(n+1), []byte{byte(n)})
			for j := 0; j < each; j++ {
				_ = conn.Send(f)
			}
		}(i)
	}
	wg.Wait()

	// Every frame that arrives must decode, and carry the session its writer
	// used. A torn write shows up here as a decode failure.
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	for i := 0; i < writers*each; i++ {
		typ, data, err := ws.ReadMessage()
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if typ != websocket.BinaryMessage {
			t.Fatalf("message %d is not binary", i)
		}
		f, err := wire.Decode(data)
		if err != nil {
			t.Fatalf("message %d did not decode — a write was torn: %v", i, err)
		}
		if len(f.Payload) != 1 || uint64(f.Payload[0])+1 != f.SessionID {
			t.Fatalf("message %d is internally inconsistent: %s payload=%v", i, f, f.Payload)
		}
	}
}

// A frame the handler rejects ends the connection rather than being skipped.
func TestRejectedFrameEndsTheConnection(t *testing.T) {
	r := NewRegistry("pod-a")
	auth := &liveAuth{token: "good", identity: Identity{MachineID: 7}}
	srv := newServer(t, testHandler(auth, r, func(context.Context, *Session, wire.Frame) error {
		return errors.New("no")
	}))

	ws, _, err := dial(t, srv, http.Header{"Authorization": []string{"Bearer good"}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	f, _ := wire.SessionFrame(wire.TypeOutput, 1, []byte("x"))
	b, _ := f.Encode()
	if err := ws.WriteMessage(websocket.BinaryMessage, b); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, _, err := ws.ReadMessage(); err == nil {
		t.Error("the connection survived a rejected frame")
	}
}

// The keepalive must fire well before the read deadline, or connections are
// declared dead between pings that were never sent.
func TestKeepaliveTimingIsCoherent(t *testing.T) {
	if pingPeriod >= pongWait {
		t.Errorf("pingPeriod %v >= pongWait %v", pingPeriod, pongWait)
	}
	if pongWait <= HeartbeatInterval {
		t.Errorf("pongWait %v does not outlast a heartbeat interval %v", pongWait, HeartbeatInterval)
	}
}
