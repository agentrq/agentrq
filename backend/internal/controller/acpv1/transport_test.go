// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func frames(n int) [][]byte {
	out := make([][]byte, n)
	for i := range out {
		out[i] = []byte(fmt.Sprintf(`{"jsonrpc":"2.0","method":"m","params":{"n":%d}}`, i+1))
	}
	return out
}

func TestReplayRing(t *testing.T) {
	t.Run("frames past what the peer received come back in order", func(t *testing.T) {
		r := newRing()
		for _, f := range frames(5) {
			r.push(f)
		}
		got, ok := r.after(2)
		if !ok || len(got) != 3 || string(got[0]) != string(frames(5)[2]) {
			t.Fatalf("after(2) returned %d frames, ok=%v, want frames 3 to 5", len(got), ok)
		}
	})

	t.Run("an acknowledged frame is gone and asking for it is a gap", func(t *testing.T) {
		r := newRing()
		for _, f := range frames(5) {
			r.push(f)
		}
		r.ack(3)
		if got, ok := r.after(3); !ok || len(got) != 2 {
			t.Fatalf("after(3) returned %d frames, ok=%v, want frames 4 and 5", len(got), ok)
		}
		if _, ok := r.after(1); ok {
			t.Fatal("after(1) said ok for frames already acknowledged, want a gap")
		}
	})

	t.Run("a peer claiming more than was sent cannot resume", func(t *testing.T) {
		r := newRing()
		r.push(frames(1)[0])
		if _, ok := r.after(5); ok {
			t.Fatal("after(5) with one frame sent said ok, want refused")
		}
	})

	t.Run("overflowing the ring makes the connection unresumable but keeps counting", func(t *testing.T) {
		r := newRing()
		for i := 0; i <= replayMaxFrames; i++ {
			r.push([]byte("{}"))
		}
		if _, ok := r.after(0); ok {
			t.Fatal("after(0) on an overflowed ring said ok, want refused")
		}
		if r.sent != replayMaxFrames+1 {
			t.Fatalf("sent was %d, want %d", r.sent, replayMaxFrames+1)
		}
	})

	t.Run("acknowledging everything then pushing keeps sequence numbers", func(t *testing.T) {
		r := newRing()
		for _, f := range frames(3) {
			r.push(f)
		}
		r.ack(3)
		r.push(frames(4)[3])
		got, ok := r.after(3)
		if !ok || len(got) != 1 || string(got[0]) != string(frames(4)[3]) {
			t.Fatalf("after(3) returned %q, ok=%v, want frame 4 alone", got, ok)
		}
	})

	t.Run("the ring compacts once half of it is spent", func(t *testing.T) {
		r := newRing()
		for _, f := range frames(10) {
			r.push(f)
		}
		r.ack(6)
		if len(r.frames) != 4 || r.head != 0 {
			t.Fatalf("after acknowledging 6 of 10 the slice held %d frames from %d, want 4 from 0", len(r.frames), r.head)
		}
		want := 0
		for _, f := range frames(10)[6:] {
			want += len(f)
		}
		if r.bytes != want {
			t.Fatalf("the ring counted %d bytes, want %d", r.bytes, want)
		}
	})

	t.Run("the byte cap breaks the ring as the frame cap does", func(t *testing.T) {
		r := newRing()
		r.push(make([]byte, replayMaxBytes))
		if _, ok := r.after(0); !ok {
			t.Fatal("after(0) at exactly the byte cap was refused, want ok")
		}
		r.push([]byte("{"))
		if _, ok := r.after(0); ok {
			t.Fatal("after(0) one byte past the cap said ok, want refused")
		}
		r.push([]byte("{}"))
		if !r.broken || r.sent != 3 || r.frames != nil {
			t.Fatalf("a broken ring kept frames=%d sent=%d broken=%v, want none kept, 3 counted", len(r.frames), r.sent, r.broken)
		}
	})
}

func TestControlFrames(t *testing.T) {
	t.Run("a welcome is written with the agentrqBridge key first", func(t *testing.T) {
		got := string(encodeControl(control{Kind: "welcome", ConnectionID: "AB", Resumed: ptr(true), Received: ptr(int64(7)), GraceSeconds: 120}))
		want := `{"agentrqBridge":"welcome","connectionId":"AB","resumed":true,"received":7,"graceSeconds":120}`
		if got != want {
			t.Fatalf("welcome was %s, want %s", got, want)
		}
		if !isControl([]byte(got)) {
			t.Fatal("isControl said no to a welcome")
		}
	})

	t.Run("an ACP frame is never mistaken for a control frame", func(t *testing.T) {
		if isControl([]byte(`{"jsonrpc":"2.0","method":"session/update","params":{"agentrqBridge":"ack"}}`)) {
			t.Fatal("isControl said yes to an ACP frame that mentions the key inside its params")
		}
	})

	t.Run("every control kind round-trips", func(t *testing.T) {
		for _, c := range []control{
			{Kind: "welcome", ConnectionID: "AB", Resumed: ptr(false), Received: ptr(int64(0)), GraceSeconds: 120},
			{Kind: "ack", Received: ptr(int64(64))},
			{Kind: "stderr", Lines: []string{"panic: database unavailable", "exit status 2"}},
			{Kind: "exit", Code: ptr(1)},
			{Kind: "exit", Signal: ptr("SIGKILL")},
			{Kind: "restart", Reason: "the agent did not answer initialize within a minute"},
			{Kind: "error", Error: "server_shutdown", Message: "the server is restarting"},
			{Kind: "bye"},
		} {
			got, err := decodeControl(encodeControl(c))
			if err != nil || !reflect.DeepEqual(got, c) {
				t.Errorf("%s came back as %+v (error %v), want %+v", encodeControl(c), got, err, c)
			}
		}
	})

	t.Run("a control frame that is not valid JSON is a protocol error", func(t *testing.T) {
		if _, err := decodeControl([]byte(`{"agentrqBridge":`)); err == nil {
			t.Fatal("decodeControl accepted a cut-off frame, want an error")
		}
	})
}

func ptr[T any](v T) *T { return &v }

func TestConnection(t *testing.T) {
	t.Run("a frame read on a socket that was replaced is neither counted nor delivered", func(t *testing.T) {
		c := newConnection("A", 100, "user-1", bridgeMeta{})
		delivered := false
		c.OnFrame(func([]byte) { delivered = true })
		c.deliver(new(websocket.Conn), []byte(`{"jsonrpc":"2.0","method":"b1"}`))
		if delivered || c.received != 0 {
			t.Fatalf("a frame from a socket that is not the connection's was delivered=%v and counted %d, want neither", delivered, c.received)
		}
	})

	t.Run("state changes the engine has not heard of yet are told once, as the latest", func(t *testing.T) {
		c := &connection{stateKick: make(chan struct{}, 1), done: make(chan struct{})}
		told := make(chan bool, 4)
		c.onState = func(attached bool) { told <- attached }
		c.setStateLocked(true)
		c.setStateLocked(false)
		c.setStateLocked(true)
		go c.dispatchState()
		if got := <-told; !got {
			t.Fatal("the engine was told detached, want the latest state, attached")
		}
		close(c.done)
		if len(told) != 0 {
			t.Fatalf("the engine was told %d more times, want once", len(told))
		}
	})

	t.Run("a grace timer overtaken by a resume ends nothing", func(t *testing.T) {
		c := newConnection("A", 100, "user-1", bridgeMeta{})
		c.graceGen = 2
		c.expireGrace(1)
		select {
		case <-c.Done():
			t.Fatalf("an old grace timer ended the connection with %v, want it left alone", c.Err())
		default:
		}
	})

	t.Run("an ended connection refuses frames and sockets and keeps its first reason", func(t *testing.T) {
		c := newConnection("A", 100, "user-1", bridgeMeta{})
		c.End(errBridgeBye)
		c.End(errGraceExpired)
		if err := c.Send([]byte(`{}`)); !errors.Is(err, errPeerClosed) {
			t.Errorf("Send after End returned %v, want %v", err, errPeerClosed)
		}
		if err := c.attach(nil, 0, true); !errors.Is(err, errPeerClosed) {
			t.Errorf("attach after End returned %v, want %v", err, errPeerClosed)
		}
		if !errors.Is(c.Err(), errBridgeBye) {
			t.Errorf("Err was %v, want the first reason, %v", c.Err(), errBridgeBye)
		}
		c.Control(control{Kind: "ack"}) // no socket: nothing to do, and no panic
	})

	t.Run("a socket cannot attach with a count the ring cannot serve", func(t *testing.T) {
		c := newConnection("A", 100, "user-1", bridgeMeta{})
		if err := c.attach(nil, 1, true); !errors.Is(err, errNotResumable) {
			t.Fatalf("attach claiming one frame when none was sent returned %v, want %v", err, errNotResumable)
		}
	})

	t.Run("frames sent while away past the ring's cap end the connection at once", func(t *testing.T) {
		c := newConnection("A", 100, "user-1", bridgeMeta{})
		for _, f := range frames(replayMaxFrames + 1) {
			_ = c.Send(f)
		}
		select {
		case <-c.Done():
		default:
			t.Fatal("the connection was still open after its ring overflowed, want it ended")
		}
		if !errors.Is(c.Err(), errNotResumable) {
			t.Fatalf("Err was %v, want %v", c.Err(), errNotResumable)
		}
	})

	t.Run("control frames the transport does not handle reach the engine", func(t *testing.T) {
		c := newConnection("A", 100, "user-1", bridgeMeta{})
		var got []control
		c.OnControl(func(ctl control) { got = append(got, ctl) })
		c.handleControl([]byte(`{"agentrqBridge":"exit","code":1}`))
		c.handleControl([]byte(`{"agentrqBridge":"ack"}`)) // no count: nothing to trim
		c.handleControl([]byte(`{"agentrqBridge":`))       // not JSON: logged and ignored
		if len(got) != 1 || got[0].Kind != "exit" || *got[0].Code != 1 {
			t.Fatalf("the engine saw %+v, want the exit frame alone", got)
		}
	})

	t.Run("a control frame for a bridge that stopped reading drops it into grace", func(t *testing.T) {
		srvWS, _ := socketPair(t)
		c := newConnection("A", 100, "user-1", bridgeMeta{})
		c.ws, c.out = srvWS, make(chan []byte) // nobody reads out
		c.Control(control{Kind: "ack", Received: ptr(int64(1))})
		if attachedNow(c) {
			t.Fatal("the socket was still attached after its queue was full, want it dropped")
		}
		select {
		case <-c.Done():
			t.Fatalf("the connection ended with %v, want it held for its grace window", c.Err())
		default:
		}
		c.End(errBridgeBye)
	})
}

// socketPair is a real server-side socket and the client talking to it.
func socketPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	got := make(chan *websocket.Conn, 1)
	srv := newRawServer(t, func(ws *websocket.Conn) { got <- ws })
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return <-got, client
}

func attachedNow(c *connection) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ws != nil
}
