// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeAuth gives the token "other" workspace 200 and every other token
// workspace 100, or fails with err.
type fakeAuth struct{ err error }

func (a fakeAuth) WorkspaceForToken(_ context.Context, token string) (int64, string, error) {
	if token == "other" {
		return 200, "user-1", a.err
	}
	return 100, "user-1", a.err
}

// echoEngine records the bridge's ACP frames and socket states, and lets a
// test send its own frames or end the run.
type echoEngine struct {
	c      *connection
	stop   chan error
	mu     sync.Mutex
	got    []string
	states []bool
}

func (e *echoEngine) run(ctx context.Context, c *connection) error {
	select {
	case <-c.Done():
		return c.Err()
	case err := <-e.stop:
		return err
	}
}

func (e *echoEngine) frames() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.got...)
}

func (e *echoEngine) seenStates() []bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]bool(nil), e.states...)
}

func serveHub(t *testing.T) (*httptest.Server, chan *echoEngine, *Handler) {
	t.Helper()
	engines := make(chan *echoEngine, 8)
	h := &Handler{Hub: NewHub(), Auth: fakeAuth{}, Enabled: func() bool { return true },
		NewEngine: func(c *connection) engine {
			e := &echoEngine{c: c, stop: make(chan error, 1)}
			c.OnFrame(func(f []byte) { e.mu.Lock(); e.got = append(e.got, string(f)); e.mu.Unlock() })
			c.OnState(func(attached bool) { e.mu.Lock(); e.states = append(e.states, attached); e.mu.Unlock() })
			engines <- e
			return e
		}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, engines, h
}

func serve(t *testing.T) (*httptest.Server, chan *echoEngine) {
	t.Helper()
	srv, engines, _ := serveHub(t)
	return srv, engines
}

// newRawServer upgrades every request and hands the socket to fn.
func newRawServer(t *testing.T, fn func(*websocket.Conn)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err == nil {
			fn(ws)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func bridgeHeader(resume string) http.Header {
	hdr := http.Header{}
	hdr.Set("Authorization", "Bearer t")
	hdr.Set("X-AgentRQ-Cwd", "/home/me/work")
	if resume != "" {
		hdr.Set("X-AgentRQ-Resume", resume)
	}
	return hdr
}

func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/acp/v1"
}

func dialWith(srv *httptest.Server, hdr http.Header) (*websocket.Conn, *http.Response, error) {
	d := websocket.Dialer{Subprotocols: []string{bridgeSubprotocol}}
	return d.Dial(wsURL(srv), hdr)
}

func dial(t *testing.T, srv *httptest.Server, resume string) *websocket.Conn {
	t.Helper()
	ws, resp, err := dialWith(srv, bridgeHeader(resume))
	if err != nil {
		t.Fatalf("dial failed: %v (status %v)", err, resp)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return ws
}

func readFrame(t *testing.T, ws *websocket.Conn) string {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, b, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("reading a frame failed: %v", err)
	}
	return string(b)
}

// closeCodeOf reads until the socket closes and returns the close code.
func closeCodeOf(t *testing.T, ws *websocket.Conn) int {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		if _, _, err := ws.ReadMessage(); err != nil {
			var ce *websocket.CloseError
			if !errors.As(err, &ce) {
				t.Fatalf("the socket failed with %v, want a close frame", err)
			}
			return ce.Code
		}
	}
}

func welcomeOf(t *testing.T, frame string) control {
	t.Helper()
	c, err := decodeControl([]byte(frame))
	if err != nil || c.Kind != "welcome" {
		t.Fatalf("first frame was %s, want a welcome", frame)
	}
	return c
}

// shrink sets a package variable for one test.
func shrink[T any](t *testing.T, v *T, to T) {
	t.Helper()
	old := *v
	*v = to
	t.Cleanup(func() { *v = old })
}

func noEngine(t *testing.T, engines chan *echoEngine) {
	t.Helper()
	select {
	case extra := <-engines:
		t.Fatalf("a second engine started (%p); a resume must not start one", extra)
	default:
	}
}

func TestSocket(t *testing.T) {
	t.Run("a socket dropped between two chunks resumes without loss or duplication", func(t *testing.T) {
		srv, engines := serve(t)
		ws := dial(t, srv, "")
		w := welcomeOf(t, readFrame(t, ws))
		if *w.Resumed || *w.Received != 0 {
			t.Fatalf("welcome was resumed=%v received=%d, want a fresh connection", *w.Resumed, *w.Received)
		}
		e := <-engines

		_ = e.c.Send([]byte(`{"jsonrpc":"2.0","method":"f1"}`))
		_ = e.c.Send([]byte(`{"jsonrpc":"2.0","method":"f2"}`))
		readFrame(t, ws)
		readFrame(t, ws)
		_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"jsonrpc":"2.0","method":"b1"}`))
		_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"jsonrpc":"2.0","method":"b2"}`))
		waitFor(t, func() bool { return len(e.frames()) == 2 })

		_ = ws.UnderlyingConn().Close() // the network goes, no close frame
		waitFor(t, func() bool { e.c.mu.Lock(); defer e.c.mu.Unlock(); return e.c.ws == nil })
		_ = e.c.Send([]byte(`{"jsonrpc":"2.0","method":"f3"}`)) // sent while away

		ws2 := dial(t, srv, w.ConnectionID+":2")
		w2 := welcomeOf(t, readFrame(t, ws2))
		if !*w2.Resumed || *w2.Received != 2 {
			t.Fatalf("resume welcome was resumed=%v received=%d, want resumed with 2 received", *w2.Resumed, *w2.Received)
		}
		if got := readFrame(t, ws2); !strings.Contains(got, `"f3"`) {
			t.Fatalf("first frame after the resume was %s, want f3 exactly once", got)
		}
		_ = ws2.WriteMessage(websocket.TextMessage, []byte(`{"jsonrpc":"2.0","method":"b3"}`))
		waitFor(t, func() bool { return len(e.frames()) == 3 })
		if got := e.frames(); !strings.Contains(got[2], `"b3"`) {
			t.Fatalf("engine saw %v, want b1, b2, b3 once each", got)
		}
		noEngine(t, engines)
		waitFor(t, func() bool { return len(e.seenStates()) == 3 })
		if got := e.seenStates(); !got[0] || got[1] || !got[2] {
			t.Fatalf("the engine was told %v, want attached, detached, attached", got)
		}
	})

	t.Run("a switched-off endpoint answers 404", func(t *testing.T) {
		srv, _, h := serveHub(t)
		h.Enabled = func() bool { return false }
		if code, _ := get(t, srv, "Bearer t"); code != http.StatusNotFound {
			t.Fatalf("status was %d, want 404", code)
		}
	})

	t.Run("a plain GET with a good token is told to upgrade", func(t *testing.T) {
		srv, _ := serve(t)
		code, hdr := get(t, srv, "Bearer t")
		if code != http.StatusUpgradeRequired || hdr.Get("Upgrade") != "websocket" {
			t.Fatalf("status was %d with Upgrade %q, want 426 with Upgrade websocket", code, hdr.Get("Upgrade"))
		}
	})

	t.Run("no token is 401 with a Bearer challenge, even on a plain GET", func(t *testing.T) {
		srv, _ := serve(t)
		for _, auth := range []string{"", "Bearer ", "Basic dTpw"} {
			code, hdr := get(t, srv, auth)
			if code != http.StatusUnauthorized || !strings.HasPrefix(hdr.Get("WWW-Authenticate"), "Bearer") {
				t.Errorf("Authorization %q got %d with challenge %q, want 401 with a Bearer challenge", auth, code, hdr.Get("WWW-Authenticate"))
			}
		}
	})

	t.Run("a token that is not a workspace access token is 401", func(t *testing.T) {
		srv, _, h := serveHub(t)
		h.Auth = fakeAuth{ErrUnauthorized}
		if code, hdr := get(t, srv, "Bearer t"); code != http.StatusUnauthorized || !strings.HasPrefix(hdr.Get("WWW-Authenticate"), "Bearer") {
			t.Fatalf("status was %d with challenge %q, want 401 with a Bearer challenge", code, hdr.Get("WWW-Authenticate"))
		}
	})

	t.Run("a token for a workspace the user cannot open is 403", func(t *testing.T) {
		srv, _, h := serveHub(t)
		h.Auth = fakeAuth{ErrForbidden}
		if code, _ := get(t, srv, "Bearer t"); code != http.StatusForbidden {
			t.Fatalf("status was %d, want 403", code)
		}
	})

	t.Run("an archived workspace is 403", func(t *testing.T) {
		srv, _, h := serveHub(t)
		h.Auth = fakeAuth{ErrArchived}
		_, resp, _ := dialWith(srv, bridgeHeader(""))
		if body := bodyOf(resp); resp.StatusCode != http.StatusForbidden || body != "workspace is archived\n" {
			t.Fatalf("status was %d with %q, want 403 workspace is archived", resp.StatusCode, body)
		}
	})

	t.Run("a bridge that does not offer the subprotocol is 400", func(t *testing.T) {
		srv, _ := serve(t)
		_, resp, _ := (&websocket.Dialer{}).Dial(wsURL(srv), bridgeHeader(""))
		if body := bodyOf(resp); resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, "agentrq.bridge.1") {
			t.Fatalf("status was %d with %q, want 400 naming agentrq.bridge.1", resp.StatusCode, body)
		}
	})

	t.Run("a relative working directory is 400", func(t *testing.T) {
		srv, _ := serve(t)
		hdr := bridgeHeader("")
		hdr.Set("X-AgentRQ-Cwd", "work")
		_, resp, _ := dialWith(srv, hdr)
		if body := bodyOf(resp); resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, "X-AgentRQ-Cwd") {
			t.Fatalf("status was %d with %q, want 400 naming X-AgentRQ-Cwd", resp.StatusCode, body)
		}
	})

	t.Run("a connection past the limits is 429", func(t *testing.T) {
		srv, _ := serve(t)
		for range maxConnsPerWS {
			dial(t, srv, "")
		}
		_, resp, _ := dialWith(srv, bridgeHeader(""))
		if body := bodyOf(resp); resp.StatusCode != http.StatusTooManyRequests || body != "too many agent connections\n" {
			t.Fatalf("status was %d with %q, want 429 too many agent connections", resp.StatusCode, body)
		}
	})

	t.Run("a failed upgrade gives its place back", func(t *testing.T) {
		srv, _, h := serveHub(t)
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/acp/v1", nil)
		req.Header = bridgeHeader("")
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Sec-WebSocket-Protocol", bridgeSubprotocol)
		resp, err := http.DefaultClient.Do(req) // no Sec-WebSocket-Key
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status was %d, want gorilla's 400", resp.StatusCode)
		}
		waitFor(t, func() bool { all, _, _ := h.Hub.count(); return all == 0 })
	})

	t.Run("the welcome is always the first frame", func(t *testing.T) {
		srv, _ := serve(t)
		ws := dial(t, srv, "")
		if w := welcomeOf(t, readFrame(t, ws)); len(w.ConnectionID) != 26 || w.GraceSeconds != 120 {
			t.Fatalf("welcome had id %q and grace %d, want a 26-character id and 120", w.ConnectionID, w.GraceSeconds)
		}
		if ws.Subprotocol() != bridgeSubprotocol {
			t.Fatalf("the server chose subprotocol %q, want %s", ws.Subprotocol(), bridgeSubprotocol)
		}
	})

	t.Run("a binary frame closes the socket with 1003", func(t *testing.T) {
		srv, _ := serve(t)
		ws := dial(t, srv, "")
		readFrame(t, ws)
		_ = ws.WriteMessage(websocket.BinaryMessage, []byte{1, 2, 3})
		if code := closeCodeOf(t, ws); code != websocket.CloseUnsupportedData {
			t.Fatalf("the socket closed with %d, want 1003", code)
		}
	})

	t.Run("a frame over 16 MiB closes the socket with 1009", func(t *testing.T) {
		shrink(t, &frameReadLimit, 1<<10)
		srv, _ := serve(t)
		ws := dial(t, srv, "")
		readFrame(t, ws)
		_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"jsonrpc":"2.0","params":"`+strings.Repeat("x", 2<<10)+`"}`))
		if code := closeCodeOf(t, ws); code != websocket.CloseMessageTooBig {
			t.Fatalf("the socket closed with %d, want 1009", code)
		}
	})

	t.Run("a resume after the grace window starts over", func(t *testing.T) {
		shrink(t, &graceWindow, 50*time.Millisecond)
		srv, engines := serve(t)
		ws := dial(t, srv, "")
		w := welcomeOf(t, readFrame(t, ws))
		e := <-engines
		_ = ws.UnderlyingConn().Close()
		<-e.c.Done()
		if !errors.Is(e.c.Err(), errGraceExpired) {
			t.Fatalf("the connection ended with %v, want %v", e.c.Err(), errGraceExpired)
		}
		ws2 := dial(t, srv, w.ConnectionID+":0")
		if w2 := welcomeOf(t, readFrame(t, ws2)); *w2.Resumed || w2.ConnectionID == w.ConnectionID {
			t.Fatalf("welcome after the grace window was resumed=%v id=%s, want a fresh connection", *w2.Resumed, w2.ConnectionID)
		}
		<-engines
	})

	t.Run("a resume past what the ring kept starts over and ends the old connection", func(t *testing.T) {
		srv, engines := serve(t)
		ws := dial(t, srv, "")
		w := welcomeOf(t, readFrame(t, ws))
		e := <-engines
		_ = ws.UnderlyingConn().Close()
		waitFor(t, func() bool { return !attachedNow(e.c) })
		for _, f := range frames(replayMaxFrames + 1) {
			_ = e.c.Send(f)
		}
		ws2 := dial(t, srv, w.ConnectionID+":0")
		if w2 := welcomeOf(t, readFrame(t, ws2)); *w2.Resumed {
			t.Fatal("the welcome said resumed after the ring overflowed, want a fresh connection")
		}
		<-engines
		if err := e.c.Err(); !errors.Is(err, errReplaced) && !errors.Is(err, errNotResumable) {
			t.Fatalf("the old connection ended with %v, want %v or %v", err, errReplaced, errNotResumable)
		}
	})

	t.Run("a resume counting frames the server never sent starts over in the old one's place", func(t *testing.T) {
		srv, engines, h := serveHub(t)
		ws := dial(t, srv, "")
		w := welcomeOf(t, readFrame(t, ws))
		e := <-engines
		for range maxConnsPerWS - 1 {
			dial(t, srv, "")
			<-engines
		}
		_ = ws.UnderlyingConn().Close()
		waitFor(t, func() bool { return !attachedNow(e.c) })
		ws2 := dial(t, srv, w.ConnectionID+":5")
		if w2 := welcomeOf(t, readFrame(t, ws2)); *w2.Resumed {
			t.Fatal("the welcome said resumed for a count the server never reached, want a fresh connection")
		}
		<-engines
		if !errors.Is(e.c.Err(), errReplaced) {
			t.Fatalf("the old connection ended with %v, want %v", e.c.Err(), errReplaced)
		}
		if all, inWS, _ := h.Hub.count(); all != maxConnsPerWS || inWS != maxConnsPerWS {
			t.Fatalf("the hub held %d (%d in the workspace), want %d: the fresh connection takes the old one's place", all, inWS, maxConnsPerWS)
		}
	})

	t.Run("a resume naming another workspace's connection starts over", func(t *testing.T) {
		srv, engines := serve(t)
		ws := dial(t, srv, "")
		w := welcomeOf(t, readFrame(t, ws))
		<-engines
		hdr := bridgeHeader(w.ConnectionID + ":0")
		hdr.Set("Authorization", "Bearer other")
		ws2, _, err := dialWith(srv, hdr)
		if err != nil {
			t.Fatalf("dial failed: %v", err)
		}
		defer ws2.Close()
		if w2 := welcomeOf(t, readFrame(t, ws2)); *w2.Resumed || w2.ConnectionID == w.ConnectionID {
			t.Fatalf("a resume from another workspace was resumed=%v id=%s, want a fresh connection", *w2.Resumed, w2.ConnectionID)
		}
		<-engines
	})

	t.Run("a resume while the old socket is still open moves to the new socket", func(t *testing.T) {
		srv, engines := serve(t)
		ws := dial(t, srv, "")
		w := welcomeOf(t, readFrame(t, ws))
		e := <-engines
		_ = e.c.Send([]byte(`{"jsonrpc":"2.0","method":"f1"}`))
		readFrame(t, ws)

		ws2 := dial(t, srv, w.ConnectionID+":1")
		if w2 := welcomeOf(t, readFrame(t, ws2)); !*w2.Resumed {
			t.Fatal("the welcome said fresh, want resumed")
		}
		_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, _, err := ws.ReadMessage(); err == nil {
			t.Fatal("the old socket still delivered a frame, want it closed")
		}
		_ = e.c.Send([]byte(`{"jsonrpc":"2.0","method":"f2"}`))
		if got := readFrame(t, ws2); !strings.Contains(got, `"f2"`) {
			t.Fatalf("the new socket read %s, want f2", got)
		}
		noEngine(t, engines)
	})

	t.Run("acks from the bridge trim what would be replayed", func(t *testing.T) {
		srv, engines := serve(t)
		ws := dial(t, srv, "")
		w := welcomeOf(t, readFrame(t, ws))
		e := <-engines
		for _, f := range frames(3) {
			_ = e.c.Send(f)
			readFrame(t, ws)
		}
		_ = ws.WriteMessage(websocket.TextMessage, encodeControl(control{Kind: "ack", Received: ptr(int64(3))}))
		waitFor(t, func() bool { e.c.mu.Lock(); defer e.c.mu.Unlock(); return e.c.sent.first == 4 })
		_ = ws.UnderlyingConn().Close()
		waitFor(t, func() bool { return !attachedNow(e.c) })
		e.c.mu.Lock()
		kept := len(e.c.sent.frames) - e.c.sent.head
		e.c.mu.Unlock()
		if kept != 0 {
			t.Fatalf("the ring kept %d frames after the bridge acknowledged all 3, want none", kept)
		}
		ws2 := dial(t, srv, w.ConnectionID+":3")
		welcomeOf(t, readFrame(t, ws2))
		_ = e.c.Send(frames(4)[3])
		if got := readFrame(t, ws2); got != string(frames(4)[3]) {
			t.Fatalf("the first frame after the resume was %s, want frame 4 and nothing replayed", got)
		}
	})

	t.Run("the server acks every 64 frames", func(t *testing.T) {
		shrink(t, &ackEvery, time.Hour)
		srv, _ := serve(t)
		ws := dial(t, srv, "")
		readFrame(t, ws)
		for _, f := range frames(ackEveryFrames) {
			_ = ws.WriteMessage(websocket.TextMessage, f)
		}
		if got := readFrame(t, ws); got != `{"agentrqBridge":"ack","received":64}` {
			t.Fatalf("the bridge read %s, want an ack of 64", got)
		}
	})

	t.Run("the server acks within ackEvery when fewer frames arrive", func(t *testing.T) {
		shrink(t, &ackEvery, 20*time.Millisecond)
		srv, _ := serve(t)
		ws := dial(t, srv, "")
		readFrame(t, ws)
		_ = ws.WriteMessage(websocket.TextMessage, frames(1)[0])
		if got := readFrame(t, ws); got != `{"agentrqBridge":"ack","received":1}` {
			t.Fatalf("the bridge read %s, want an ack of 1", got)
		}
	})

	t.Run("bye ends the connection at once, with no grace", func(t *testing.T) {
		srv, engines := serve(t)
		ws := dial(t, srv, "")
		readFrame(t, ws)
		e := <-engines
		_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"agentrqBridge":"bye"}`))
		select {
		case <-e.c.Done():
		case <-time.After(100 * time.Millisecond):
			t.Fatal("the connection was still open 100 ms after bye")
		}
		if !errors.Is(e.c.Err(), errBridgeBye) {
			t.Fatalf("Err was %v, want %v", e.c.Err(), errBridgeBye)
		}
		if code := closeCodeOf(t, ws); code != websocket.CloseNormalClosure {
			t.Fatalf("the socket closed with %d, want 1000", code)
		}
	})

	t.Run("an engine that stops ends the connection and closes the socket", func(t *testing.T) {
		srv, engines := serve(t)
		ws := dial(t, srv, "")
		readFrame(t, ws)
		e := <-engines
		e.stop <- errors.New("the agent answered another protocol version")
		<-e.c.Done()
		if code := closeCodeOf(t, ws); code != websocket.CloseNormalClosure {
			t.Fatalf("the socket closed with %d, want 1000", code)
		}
	})

	t.Run("a bridge that stops reading is dropped, not waited for", func(t *testing.T) {
		srv, engines := serve(t)
		ws := dial(t, srv, "") // never read from again
		e := <-engines
		big := make([]byte, 64<<10)
		start := time.Now()
		for i := 0; attachedNow(e.c); i++ {
			if i > 1<<14 {
				t.Fatal("the socket was still attached after 16,384 unread frames")
			}
			if err := e.c.Send(big); err != nil {
				break
			}
		}
		if took := time.Since(start); took > 5*time.Second {
			t.Fatalf("sending to a bridge that does not read took %v, want Send never to wait", took)
		}
		<-e.c.Done() // what was queued overflowed the ring, so no resume could follow
		if !errors.Is(e.c.Err(), errNotResumable) {
			t.Fatalf("the connection ended with %v, want %v", e.c.Err(), errNotResumable)
		}
		_ = ws
	})

	t.Run("a socket that stops answering pings is dropped into grace", func(t *testing.T) {
		shrink(t, &pingEvery, 10*time.Millisecond)
		shrink(t, &pongWait, 100*time.Millisecond)
		srv, engines := serve(t)
		ws := dial(t, srv, "")
		e := <-engines
		reading := make(chan struct{})
		go func() { // answers pings while it reads
			defer close(reading)
			_ = ws.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
			for {
				if _, _, err := ws.ReadMessage(); err != nil {
					return
				}
			}
		}()
		<-reading
		if !attachedNow(e.c) {
			t.Fatal("a bridge that answered every ping was dropped")
		}
		waitFor(t, func() bool { return !attachedNow(e.c) })
		select {
		case <-e.c.Done():
			t.Fatalf("the connection ended with %v, want it held for its grace window", e.c.Err())
		default:
		}
	})
}

func get(t *testing.T, srv *httptest.Server, auth string) (int, http.Header) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/acp/v1", nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode, resp.Header
}

func bodyOf(resp *http.Response) string {
	if resp == nil {
		return ""
	}
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func TestParseMeta(t *testing.T) {
	t.Run("every header is read and trimmed", func(t *testing.T) {
		m, err := parseMeta(http.Header{
			"X-Agentrq-Bridge-Version":     {"0.3.0"},
			"X-Agentrq-Agent":              {" codex "},
			"X-Agentrq-Agent-Model":        {"gpt-5"},
			"X-Agentrq-Agent-Effort":       {" high "},
			"X-Agentrq-Auth-Method":        {"chatgpt"},
			"X-Agentrq-Platform":           {"linux/arm64"},
			"X-Agentrq-Cwd":                {"/home/me/work"},
			"X-Agentrq-Max-Concurrency":    {"3"},
			"X-Agentrq-Permission-Timeout": {"0"},
		})
		want := bridgeMeta{BridgeVersion: "0.3.0", Agent: "codex", Model: "gpt-5", Effort: "high", AuthMethod: "chatgpt",
			Platform: "linux/arm64", Cwd: "/home/me/work", MaxConcurrency: 3, PermissionTimeout: 0}
		if err != nil || m != want {
			t.Fatalf("parseMeta returned %+v (error %v), want %+v", m, err, want)
		}
	})

	t.Run("missing optional headers mean their defaults", func(t *testing.T) {
		m, err := parseMeta(http.Header{"X-Agentrq-Cwd": {"/w"}})
		if err != nil || m.MaxConcurrency != 0 || m.PermissionTimeout != -1 {
			t.Fatalf("parseMeta returned %+v (error %v), want concurrency 0 and timeout -1", m, err)
		}
	})

	t.Run("no working directory is accepted: the gateway writes its own", func(t *testing.T) {
		m, err := parseMeta(http.Header{})
		if err != nil || m.Cwd != "" {
			t.Fatalf("parseMeta returned %+v (error %v), want no cwd and no error", m, err)
		}
	})

	t.Run("concurrency is clamped and a timeout out of range is ignored", func(t *testing.T) {
		for _, c := range []struct {
			concurrency, timeout string
			wantN                int
			wantT                time.Duration
		}{
			{"0", "1441", 1, -1},
			{"500", "-1", 64, -1},
			{"many", "forever", 0, -1},
			{"1", "1440", 1, 24 * time.Hour},
		} {
			m, _ := parseMeta(http.Header{"X-Agentrq-Cwd": {"/w"}, "X-Agentrq-Max-Concurrency": {c.concurrency}, "X-Agentrq-Permission-Timeout": {c.timeout}})
			if m.MaxConcurrency != c.wantN || m.PermissionTimeout != c.wantT {
				t.Errorf("concurrency %q and timeout %q gave %d and %v, want %d and %v", c.concurrency, c.timeout, m.MaxConcurrency, m.PermissionTimeout, c.wantN, c.wantT)
			}
		}
	})

	t.Run("a header past its length is refused by name", func(t *testing.T) {
		_, err := parseMeta(http.Header{"X-Agentrq-Cwd": {"/w"}, "X-Agentrq-Agent": {strings.Repeat("a", 129)}})
		if err == nil || err.Error() != "X-AgentRQ-Agent is too long" {
			t.Fatalf("parseMeta returned %v, want X-AgentRQ-Agent is too long", err)
		}
	})

	t.Run("Windows and UNC working directories are absolute, relative ones are not", func(t *testing.T) {
		for p, want := range map[string]bool{
			`C:\Users\me`: true, `d:/work`: true, `\\server\share`: true, "/home/me": true,
			"work": false, "": false, `C:work`: false, `~/work`: false,
		} {
			if got := isAbsolutePath(p); got != want {
				t.Errorf("isAbsolutePath(%q) was %v, want %v", p, got, want)
			}
		}
	})
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 2 seconds")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
