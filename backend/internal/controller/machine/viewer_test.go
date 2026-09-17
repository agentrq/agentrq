// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package machine

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/agentrq/agentrq/daemon/wire"
)

type fakeLookup struct {
	at  Attachment
	err error
}

func (l fakeLookup) LookupSession(*http.Request, uint64) (Attachment, error) {
	return l.at, l.err
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
		Lookup:    fakeLookup{at: Attachment{MachineID: 11, InstanceID: "pod-a", UserID: 3, ViewerName: "Ada"}},
		SessionID: func(*http.Request) uint64 { return sessionID },
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, relay, daemon
}

// readSession reads the next frame that belongs to the session, skipping the
// presence announcements that arrive on the same socket.
func readSession(t *testing.T, ws *websocket.Conn, name string) wire.Frame {
	t.Helper()
	for {
		_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
		typ, data, err := ws.ReadMessage()
		if err != nil {
			t.Fatalf("%s: read: %v", name, err)
		}
		if typ != websocket.BinaryMessage {
			t.Fatalf("%s: not a binary frame", name)
		}
		f, err := wire.Decode(data)
		if err != nil {
			t.Fatalf("%s: decode: %v", name, err)
		}
		if f.Type != wire.TypeControl {
			return f
		}
	}
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

			got := readSession(t, ws, name)
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
		Lookup:    fakeLookup{at: Attachment{MachineID: 11, InstanceID: "pod-b"}},
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
		h := &ViewerHandler{Relay: NewRelay(reg), Lookup: fakeLookup{at: Attachment{MachineID: 11}},
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

// The audit record is the attach, and only the attach.
//
// Keystrokes are credentials, tokens and file contents. Recording them would
// build the most sensitive log in the system to answer a question nobody
// asked; that somebody opened this session is the fact worth keeping.
func TestTheAttachIsAuditedAndTheKeystrokesAreNot(t *testing.T) {
	const sessionID = 7

	var logged bytes.Buffer
	previous := log.Logger
	log.Logger = zerolog.New(&logged)
	t.Cleanup(func() { log.Logger = previous })

	srv, relay, _ := viewerServer(t, sessionID)
	ws := dialViewer(t, srv)
	waitFor(t, func() bool { return relay.Viewers(sessionID) == 1 }, "the viewer never attached")

	secret := []byte("hunter2-my-actual-password\r")
	f, err := wire.SessionFrame(wire.TypeInput, sessionID, secret)
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
	_ = ws.Close()
	waitFor(t, func() bool { return relay.Viewers(sessionID) == 0 }, "the viewer never detached")

	out := logged.String()
	for _, want := range []string{"terminal attached", "terminal detached", `"user_id":3`, `"session_id":7`, `"machine_id":11`} {
		if !strings.Contains(out, want) {
			t.Errorf("the audit record does not mention %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, "hunter2") {
		t.Errorf("a keystroke reached the log:\n%s", out)
	}
}

// Presence reaches the browser over the same socket as the terminal traffic.
func TestPresenceReachesTheBrowser(t *testing.T) {
	const sessionID = 7
	srv, relay, _ := viewerServer(t, sessionID)
	ws := dialViewer(t, srv)
	waitFor(t, func() bool { return relay.Viewers(sessionID) == 1 }, "the viewer never attached")

	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	frame, err := wire.Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	c, err := wire.ParseControl(frame)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if c.Op != wire.OpPresence {
		t.Fatalf("first frame was %s, want a presence announcement", c.Op)
	}
	var p wire.Presence
	if err := json.Unmarshal(c.Body, &p); err != nil {
		t.Fatalf("presence body: %v", err)
	}
	if p.SessionID != sessionID || len(p.Viewers) != 1 || p.Viewers[0] != "Ada" {
		t.Errorf("presence = %+v", p)
	}
}

// A browser cannot name a session — it holds base62 ids and the header wants a
// number — so it names none, and this side supplies it.
//
// This is the test that was missing. Input over a real socket was covered, but
// always with a frame a Go client had built, and a Go client has the number to
// hand. The browser does not, and the keystroke it actually sends looks like
// this one.
func TestAViewerNeedNotNameItsSession(t *testing.T) {
	const sessionID = 7
	srv, relay, daemon := viewerServer(t, sessionID)
	ws := dialViewer(t, srv)
	waitFor(t, func() bool { return relay.Viewers(sessionID) == 1 }, "the viewer never attached")

	// Exactly what the browser puts on the wire: type, eight zero bytes where
	// the session would be, then the keystroke.
	raw := make([]byte, 9+1)
	raw[0] = byte(wire.TypeInput)
	raw[9] = 'y'
	if err := ws.WriteMessage(websocket.BinaryMessage, raw); err != nil {
		t.Fatal(err)
	}

	waitFor(t, func() bool { return daemon.count() > 0 }, "the keystroke never reached the daemon")

	got := daemon.lastFrame()
	if string(got.Payload) != "y" {
		t.Errorf("the daemon got %q", got.Payload)
	}
	// Supplied here, from the socket the viewer attached to, which is what
	// stops a browser typing into another session by changing a number.
	if got.SessionID != sessionID {
		t.Errorf("session = %d, want %d", got.SessionID, sessionID)
	}
}
