// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package machine

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/agentrq/agentrq/daemon/wire"
)

type fakeLookup struct {
	machineID  int64
	instanceID string
	err        error
}

func (l fakeLookup) LookupSession(*http.Request, uint64) (int64, string, error) {
	return l.machineID, l.instanceID, l.err
}

// viewerServer wires a relay to a real HTTP server, with a real daemon socket
// on the other side.
func viewerServer(t *testing.T, sessionID uint64) (*httptest.Server, *Relay, *fakeConn) {
	t.Helper()
	reg := NewRegistry("pod-a")
	daemon := &fakeConn{}
	reg.Add(11, daemon)
	relay := NewRelay(reg)

	h := &ViewerHandler{
		Relay:     relay,
		Lookup:    fakeLookup{machineID: 11, instanceID: "pod-a"},
		SessionID: func(*http.Request) uint64 { return sessionID },
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, relay, daemon
}

func dialViewer(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return ws
}

// THE test the milestone asks for: bytes through a real WebSocket, both ways,
// byte-for-byte. Esc and invalid UTF-8 are the ones most likely to be
// "helpfully" transformed by something in the middle.
func TestBytesSurviveARealSocketInBothDirections(t *testing.T) {
	const sessionID = 7
	srv, relay, daemon := viewerServer(t, sessionID)
	ws := dialViewer(t, srv)

	waitFor(t, func() bool { return relay.Viewers(sessionID) == 1 }, "the viewer never attached")

	payloads := map[string][]byte{
		"escape":       {0x1b},
		"csi clear":    []byte("\x1b[2K"),
		"invalid utf8": {0xff, 0xfe, 0x80},
		"nul":          {0x00, 0x00},
		"every byte":   allBytes(),
	}

	t.Run("daemon to browser", func(t *testing.T) {
		for name, payload := range payloads {
			f, err := wire.SessionFrame(wire.TypeOutput, sessionID, payload)
			if err != nil {
				t.Fatal(err)
			}
			relay.FromDaemon(f)

			_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
			typ, data, err := ws.ReadMessage()
			if err != nil {
				t.Fatalf("%s: read: %v", name, err)
			}
			if typ != websocket.BinaryMessage {
				t.Fatalf("%s: not a binary frame", name)
			}
			got, err := wire.Decode(data)
			if err != nil {
				t.Fatalf("%s: decode: %v", name, err)
			}
			if !bytes.Equal(got.Payload, payload) {
				t.Errorf("%s changed:\n got %#v\nwant %#v", name, got.Payload, payload)
			}
		}
	})

	t.Run("browser to daemon", func(t *testing.T) {
		for name, payload := range payloads {
			before := daemon.count()

			f, err := wire.SessionFrame(wire.TypeInput, sessionID, payload)
			if err != nil {
				t.Fatal(err)
			}
			b, err := f.Encode()
			if err != nil {
				t.Fatal(err)
			}
			if err := ws.WriteMessage(websocket.BinaryMessage, b); err != nil {
				t.Fatalf("%s: write: %v", name, err)
			}

			waitFor(t, func() bool { return daemon.count() > before }, name+": never reached the daemon")

			got := daemon.lastFrame()
			if !bytes.Equal(got.Payload, payload) {
				t.Errorf("%s changed on the way in:\n got %#v\nwant %#v", name, got.Payload, payload)
			}
			if got.Type != wire.TypeInput {
				t.Errorf("%s arrived as %s", name, got.Type)
			}
		}
	})
}

func allBytes() []byte {
	b := make([]byte, 256)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

// Without this, an attached browser could type into any session on that
// machine by changing a number in the frame it sends.
func TestAViewerCannotTypeIntoAnotherSession(t *testing.T) {
	const mine = 7
	srv, relay, daemon := viewerServer(t, mine)
	ws := dialViewer(t, srv)
	waitFor(t, func() bool { return relay.Viewers(mine) == 1 }, "never attached")

	before := daemon.count()

	// Claim a different session in the frame.
	f, err := wire.SessionFrame(wire.TypeInput, 999, []byte("rm -rf /\r"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteMessage(websocket.BinaryMessage, b); err != nil {
		t.Fatal(err)
	}

	waitFor(t, func() bool { return daemon.count() > before }, "nothing reached the daemon")
	if got := daemon.lastFrame(); got.SessionID != mine {
		t.Errorf("input was delivered to session %d, want %d — a viewer escaped its session",
			got.SessionID, mine)
	}
}

// A terminal panel that connects and then shows nothing is the worst version
// of this failure, because it looks like the agent is simply quiet.
func TestAttachToAMachineHeldElsewhereIsRefusedBeforeUpgrading(t *testing.T) {
	reg := NewRegistry("pod-a") // holds nothing
	h := &ViewerHandler{
		Relay:     NewRelay(reg),
		Lookup:    fakeLookup{machineID: 11, instanceID: "pod-b"},
		SessionID: func(*http.Request) uint64 { return 7 },
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	_, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err == nil {
		t.Fatal("the connection was accepted")
	}
	if resp == nil || resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %v, want 409 naming the problem", resp)
	}
}

func TestViewerHandlerRefusesNonsense(t *testing.T) {
	reg := NewRegistry("pod-a")
	reg.Add(11, &fakeConn{})

	t.Run("no session in the path", func(t *testing.T) {
		h := &ViewerHandler{Relay: NewRelay(reg), Lookup: fakeLookup{machineID: 11},
			SessionID: func(*http.Request) uint64 { return 0 }}
		srv := httptest.NewServer(h)
		defer srv.Close()
		_, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
		if err == nil || resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("status = %v, want 422", resp)
		}
	})

	t.Run("session the caller may not see", func(t *testing.T) {
		h := &ViewerHandler{Relay: NewRelay(reg),
			Lookup:    fakeLookup{err: errors.New("no")},
			SessionID: func(*http.Request) uint64 { return 7 }}
		srv := httptest.NewServer(h)
		defer srv.Close()
		_, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
		if err == nil || resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %v, want 404", resp)
		}
	})
}

// Detaching on disconnect is what stops a dead browser holding a session's
// stream open forever.
func TestClosingTheBrowserDetaches(t *testing.T) {
	const sessionID = 7
	srv, relay, _ := viewerServer(t, sessionID)
	ws := dialViewer(t, srv)
	waitFor(t, func() bool { return relay.Viewers(sessionID) == 1 }, "never attached")

	_ = ws.Close()
	waitFor(t, func() bool { return relay.Viewers(sessionID) == 0 }, "the viewer was never detached")
}
