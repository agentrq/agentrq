// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package sitetools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mustafaturan/monoflake"
)

const (
	testUser      int64 = 7
	testWorkspace int64 = 70
	otherUserWS   int64 = 80
)

var (
	errStore  = errors.New("site share store unavailable")
	wsID      = monoflake.ID(testWorkspace).String()
	otherWSID = monoflake.ID(otherUserWS).String()
)

// memStore is a Store in memory. Workspaces testWorkspace and 71 belong to
// testUser; otherUserWS belongs to somebody else.
type memStore struct {
	mu     sync.Mutex
	shares map[string]Share // by origin; testUser only
	fail   map[string]bool  // method name → return errStore
}

func newMemStore() *memStore {
	return &memStore{shares: map[string]Share{}, fail: map[string]bool{}}
}

func (m *memStore) OwnsWorkspace(_ context.Context, userID, workspaceID int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail["OwnsWorkspace"] {
		return false, errStore
	}
	return userID == testUser && (workspaceID == testWorkspace || workspaceID == 71), nil
}

func (m *memStore) Upsert(_ context.Context, s Share) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail["Upsert"] {
		return false, errStore
	}
	old, ok := m.shares[s.Origin]
	m.shares[s.Origin] = s
	return !ok || old.WorkspaceID != s.WorkspaceID, nil
}

func (m *memStore) Delete(_ context.Context, userID int64, origin string) (int64, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail["Delete"] {
		return 0, false, errStore
	}
	s, ok := m.shares[origin]
	delete(m.shares, origin)
	return s.WorkspaceID, ok, nil
}

func (m *memStore) ListForUser(_ context.Context, userID int64) ([]Share, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail["ListForUser"] {
		return nil, errStore
	}
	var out []Share
	for _, s := range m.shares {
		out = append(out, s)
	}
	return out, nil
}

func (m *memStore) get(origin string) (Share, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.shares[origin]
	return s, ok
}

type hookCall struct {
	userID, workspaceID int64
	origin              string
}

type rig struct {
	url      string
	hub      *Hub
	store    *memStore
	mu       sync.Mutex
	shared   []hookCall
	unshared []hookCall
}

func newRig(t *testing.T, store *memStore, opts ...func(*Handler)) *rig {
	t.Helper()
	r := &rig{hub: NewHub("instance-a", func() string { return "call-1" }), store: store}
	h := &Handler{
		Hub:   r.hub,
		Store: store,
		Auth: func(req *http.Request) (int64, error) {
			if req.URL.Query().Get("ticket") != "good" {
				return 0, errors.New("bad ticket")
			}
			return testUser, nil
		},
		OnShare: func(u, w int64, o string) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.shared = append(r.shared, hookCall{u, w, o})
		},
		OnUnshare: func(u, w int64, o string) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.unshared = append(r.unshared, hookCall{u, w, o})
		},
	}
	for _, o := range opts {
		o(h)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	r.url = "ws" + strings.TrimPrefix(srv.URL, "http")
	return r
}

func (r *rig) hooks() (shared, unshared []hookCall) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]hookCall(nil), r.shared...), append([]hookCall(nil), r.unshared...)
}

// dial connects as browser b1 and consumes the shares frame.
func (r *rig) dial(t *testing.T) (*websocket.Conn, Frame) {
	t.Helper()
	ws, _, err := websocket.DefaultDialer.Dial(r.url+"/?ticket=good&browser=b1", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	return ws, next(t, ws)
}

func next(t *testing.T, ws *websocket.Conn) Frame {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	var f Frame
	if err := ws.ReadJSON(&f); err != nil {
		t.Fatalf("read: %v", err)
	}
	return f
}

func send(t *testing.T, ws *websocket.Conn, f Frame) {
	t.Helper()
	if err := ws.WriteJSON(f); err != nil {
		t.Fatal(err)
	}
}

// barrier sends a frame that is always refused and waits for the refusal:
// frames are handled in order, so everything sent before it is done.
func barrier(t *testing.T, ws *websocket.Conn) {
	t.Helper()
	send(t, ws, Frame{Type: FrameAnnounce, Origin: "barrier", WorkspaceID: wsID})
	if f := next(t, ws); f.Type != FrameRefused || f.Origin != "barrier" {
		t.Fatalf("barrier: got %+v", f)
	}
}

func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition never held")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func announce(origin, workspaceID string, tools ...Tool) Frame {
	return Frame{Type: FrameAnnounce, Origin: origin, WorkspaceID: workspaceID, LastURL: origin + "/x", Tools: tools}
}

func TestConnectRefusedBeforeUpgrade(t *testing.T) {
	r := newRig(t, newMemStore())
	base := "http" + strings.TrimPrefix(r.url, "ws")
	for _, tc := range []struct {
		name, query string
		want        int
	}{
		{"no ticket", "?browser=b1", http.StatusUnauthorized},
		{"bad ticket", "?ticket=bad&browser=b1", http.StatusUnauthorized},
		{"no browser", "?ticket=good", http.StatusBadRequest},
		{"browser too long", "?ticket=good&browser=" + strings.Repeat("a", 65), http.StatusBadRequest},
		{"browser bad char", "?ticket=good&browser=a_b", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := http.Get(base + "/" + tc.query)
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			if res.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d", res.StatusCode, tc.want)
			}
		})
	}
	if r.hub.Online(testUser, "b1") {
		t.Fatal("a refused connect was registered")
	}
}

func TestConnectAcceptsLongestBrowserID(t *testing.T) {
	r := newRig(t, newMemStore())
	id := strings.Repeat("A-9", 21) + "z"
	ws, _, err := websocket.DefaultDialer.Dial(r.url+"/?ticket=good&browser="+id, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	next(t, ws)
	if !r.hub.Online(testUser, id) {
		t.Fatal("a 64-character browser id was not registered")
	}
}

func TestConnectSendsShares(t *testing.T) {
	store := newMemStore()
	store.shares["https://a.com"] = Share{UserID: testUser, WorkspaceID: testWorkspace, Origin: "https://a.com", AlwaysAllow: []string{"read"}}
	store.shares["https://b.com"] = Share{UserID: testUser, WorkspaceID: 71, Origin: "https://b.com"}
	r := newRig(t, store)

	_, f := r.dial(t)
	if f.Type != FrameShares || len(f.Shares) != 2 {
		t.Fatalf("first frame = %+v, want two shares", f)
	}
	got := map[string]ShareState{}
	for _, s := range f.Shares {
		got[s.Origin] = s
	}
	if a := got["https://a.com"]; a.WorkspaceID != wsID || len(a.AlwaysAllow) != 1 || a.AlwaysAllow[0] != "read" {
		t.Errorf("a.com = %+v", a)
	}
	if b := got["https://b.com"]; b.WorkspaceID != monoflake.ID(71).String() || b.AlwaysAllow == nil {
		t.Errorf("b.com = %+v, want base62 workspace and a non-nil alwaysAllow", b)
	}
	if !r.hub.Online(testUser, "b1") {
		t.Fatal("not registered in the hub")
	}
}

// An empty shares frame would make the extension forget every share, so a
// failed read closes the socket for the extension to retry instead.
func TestConnectClosesWhenSharesCannotBeRead(t *testing.T) {
	store := newMemStore()
	store.fail["ListForUser"] = true
	r := newRig(t, store)

	ws, _, err := websocket.DefaultDialer.Dial(r.url+"/?ticket=good&browser=b1", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, _, err := ws.ReadMessage(); err == nil {
		t.Fatal("got a frame, want the socket closed")
	}
	eventually(t, func() bool { return !r.hub.Online(testUser, "b1") })
}

func TestConnectDisplacesEarlierSocket(t *testing.T) {
	r := newRig(t, newMemStore())
	first, _ := r.dial(t)
	second, _ := r.dial(t)

	_ = first.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, _, err := first.ReadMessage(); err == nil {
		t.Fatal("the displaced socket is still open")
	}
	barrier(t, second)
	if !r.hub.Online(testUser, "b1") {
		t.Fatal("the displaced socket's close unregistered its replacement")
	}
}

func TestAnnounceSharesSite(t *testing.T) {
	r := newRig(t, newMemStore())
	ws, _ := r.dial(t)
	tool := Tool{Name: "search", Description: "Search"}

	send(t, ws, announce("https://a.com", wsID, tool))
	barrier(t, ws)
	s, ok := r.store.get("https://a.com")
	if !ok {
		t.Fatal("not upserted")
	}
	if s.UserID != testUser || s.WorkspaceID != testWorkspace || s.BrowserID != "b1" || s.InstanceID != "instance-a" ||
		s.LastURL != "https://a.com/x" || len(s.Tools) != 1 || s.Tools[0].Name != "search" {
		t.Errorf("share = %+v", s)
	}

	// The same workspace again is a tools update, not a new share.
	send(t, ws, announce("https://a.com", wsID))
	barrier(t, ws)
	// A move to another workspace is a share into that one.
	send(t, ws, announce("https://a.com", monoflake.ID(71).String()))
	barrier(t, ws)

	shared, _ := r.hooks()
	want := []hookCall{{testUser, testWorkspace, "https://a.com"}, {testUser, 71, "https://a.com"}}
	if len(shared) != 2 || shared[0] != want[0] || shared[1] != want[1] {
		t.Fatalf("OnShare calls = %+v, want %+v", shared, want)
	}
}

func TestAnnounceAcceptsLocalhost(t *testing.T) {
	r := newRig(t, newMemStore())
	ws, _ := r.dial(t)
	for _, o := range []string{"http://localhost", "http://localhost:3000", "https://a.com:8443"} {
		send(t, ws, announce(o, wsID))
	}
	barrier(t, ws)
	for _, o := range []string{"http://localhost", "http://localhost:3000", "https://a.com:8443"} {
		if _, ok := r.store.get(o); !ok {
			t.Errorf("%s was not shared", o)
		}
	}
}

func TestAnnounceRefusals(t *testing.T) {
	store := newMemStore()
	r := newRig(t, store)
	ws, _ := r.dial(t)
	tooMany := make([]Tool, MaxTools+1)
	for i := range tooMany {
		tooMany[i] = Tool{Name: "t" + strings.Repeat("x", i)}
	}

	for _, tc := range []struct {
		name  string
		frame Frame
	}{
		{"http not localhost", announce("http://a.com", wsID)},
		{"path", announce("https://a.com/", wsID)},
		{"deep path", announce("https://a.com/p", wsID)},
		{"query", announce("https://a.com?q", wsID)},
		{"fragment", announce("https://a.com#f", wsID)},
		{"userinfo", announce("https://u@a.com", wsID)},
		{"upper case", announce("https://A.com", wsID)},
		{"no host", announce("https://", wsID)},
		{"port only", announce("https://:443", wsID)},
		{"other scheme", announce("ftp://a.com", wsID)},
		{"unparseable", announce("https://a.com:x", wsID)},
		{"empty", announce("", wsID)},
		{"someone else's workspace", announce("https://a.com", otherWSID)},
		{"no workspace", announce("https://a.com", "")},
		{"too many tools", announce("https://a.com", wsID, tooMany...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			send(t, ws, tc.frame)
			f := next(t, ws)
			if f.Type != FrameRefused || f.Origin != tc.frame.Origin || f.Error == "" {
				t.Fatalf("got %+v, want a refusal for %q", f, tc.frame.Origin)
			}
		})
	}
	if len(store.shares) != 0 {
		t.Fatalf("refused announces were stored: %+v", store.shares)
	}
	if shared, _ := r.hooks(); len(shared) != 0 {
		t.Fatalf("OnShare fired for a refusal: %+v", shared)
	}
	if !r.hub.Online(testUser, "b1") {
		t.Fatal("a refusal closed the socket")
	}
}

func TestAnnounceStoreFailuresAreRefused(t *testing.T) {
	for _, method := range []string{"OwnsWorkspace", "Upsert"} {
		t.Run(method, func(t *testing.T) {
			store := newMemStore()
			r := newRig(t, store)
			ws, _ := r.dial(t)
			store.fail[method] = true
			send(t, ws, announce("https://a.com", wsID))
			if f := next(t, ws); f.Type != FrameRefused || f.Origin != "https://a.com" {
				t.Fatalf("got %+v, want a refusal", f)
			}
			if shared, _ := r.hooks(); len(shared) != 0 {
				t.Fatalf("OnShare fired: %+v", shared)
			}
		})
	}
}

func TestWithdraw(t *testing.T) {
	store := newMemStore()
	r := newRig(t, store)
	ws, _ := r.dial(t)
	send(t, ws, announce("https://a.com", wsID))

	send(t, ws, Frame{Type: FrameWithdraw, Origin: "https://a.com"})
	send(t, ws, Frame{Type: FrameWithdraw, Origin: "https://never.com"})
	barrier(t, ws)

	if _, ok := store.get("https://a.com"); ok {
		t.Fatal("still shared")
	}
	_, unshared := r.hooks()
	if len(unshared) != 1 || unshared[0] != (hookCall{testUser, testWorkspace, "https://a.com"}) {
		t.Fatalf("OnUnshare calls = %+v, want one for a.com", unshared)
	}
}

func TestWithdrawStoreFailureIsRefused(t *testing.T) {
	store := newMemStore()
	r := newRig(t, store)
	ws, _ := r.dial(t)
	store.fail["Delete"] = true
	send(t, ws, Frame{Type: FrameWithdraw, Origin: "https://a.com"})
	if f := next(t, ws); f.Type != FrameRefused || f.Origin != "https://a.com" {
		t.Fatalf("got %+v, want a refusal", f)
	}
}

func TestResultReachesPendingCall(t *testing.T) {
	r := newRig(t, newMemStore())
	ws, _ := r.dial(t)

	type out struct {
		f   Frame
		err error
	}
	done := make(chan out, 1)
	go func() {
		f, err := r.hub.Call(context.Background(), testUser, "b1", "https://a.com", "search", json.RawMessage(`{"q":1}`))
		done <- out{f, err}
	}()

	call := next(t, ws)
	if call.Type != FrameCall || call.CallID != "call-1" || call.Tool != "search" || string(call.Arguments) != `{"q":1}` {
		t.Fatalf("call frame = %+v", call)
	}
	send(t, ws, Frame{Type: FrameResult, CallID: call.CallID, Text: "found"})

	select {
	case o := <-done:
		if o.err != nil || o.f.Text != "found" {
			t.Fatalf("Call = %+v, %v", o.f, o.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the result never reached the call")
	}
}

func TestClosedClientGoesOffline(t *testing.T) {
	r := newRig(t, newMemStore())
	ws, _ := r.dial(t)
	ws.Close()
	eventually(t, func() bool { return !r.hub.Online(testUser, "b1") })
}

func TestUnknownFrameIsIgnored(t *testing.T) {
	r := newRig(t, newMemStore())
	ws, _ := r.dial(t)
	send(t, ws, Frame{Type: "nonsense"})
	barrier(t, ws)
}

func TestUndecodableFrameClosesSocket(t *testing.T) {
	r := newRig(t, newMemStore())
	ws, _ := r.dial(t)
	if err := ws.WriteMessage(websocket.TextMessage, []byte("{not json")); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return !r.hub.Online(testUser, "b1") })
}

func TestOversizedFrameClosesSocket(t *testing.T) {
	r := newRig(t, newMemStore())
	ws, _ := r.dial(t)
	big := `{"type":"result","text":"` + strings.Repeat("x", maxFrame) + `"}`
	_ = ws.WriteMessage(websocket.TextMessage, []byte(big))
	eventually(t, func() bool { return !r.hub.Online(testUser, "b1") })
}

// A frame at the limit a result may reach still gets through.
func TestLargestResultIsRead(t *testing.T) {
	r := newRig(t, newMemStore())
	ws, _ := r.dial(t)
	send(t, ws, Frame{Type: FrameResult, CallID: "nobody", Text: strings.Repeat("x", MaxResult)})
	barrier(t, ws)
}

func TestPings(t *testing.T) {
	r := newRig(t, newMemStore(), func(h *Handler) { h.PingPeriod = 20 * time.Millisecond })
	ws, _ := r.dial(t)
	pinged := make(chan struct{}, 1)
	ws.SetPingHandler(func(string) error {
		select {
		case pinged <- struct{}{}:
		default:
		}
		return ws.WriteControl(websocket.PongMessage, nil, time.Now().Add(time.Second))
	})
	go func() {
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}()
	select {
	case <-pinged:
	case <-time.After(5 * time.Second):
		t.Fatal("never pinged")
	}
}

func TestWSConnAfterClose(t *testing.T) {
	r := newRig(t, newMemStore())
	ws, _, err := websocket.DefaultDialer.Dial(r.url+"/?ticket=good&browser=b1", nil)
	if err != nil {
		t.Fatal(err)
	}
	c := &wsConn{ws: ws}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close = %v, want nil", err)
	}
	if err := c.Send(Frame{Type: FrameShares}); err == nil {
		t.Fatal("Send after Close succeeded")
	}
	if err := c.ping(); err == nil {
		t.Fatal("ping after Close succeeded")
	}
}

func TestPlainGetIsNotUpgraded(t *testing.T) {
	r := newRig(t, newMemStore())
	res, err := http.Get("http" + strings.TrimPrefix(r.url, "ws") + "/?ticket=good&browser=b1")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want the upgrader's 400", res.StatusCode)
	}
	if r.hub.Online(testUser, "b1") {
		t.Fatal("a request that never upgraded was registered")
	}
}

func TestKeepaliveStopsOnClosedSocket(t *testing.T) {
	done := make(chan struct{})
	go func() {
		keepalive(context.Background(), &wsConn{closed: true}, time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("keepalive kept pinging a closed socket")
	}
}
