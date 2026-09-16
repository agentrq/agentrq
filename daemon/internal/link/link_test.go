// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package link

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/agentrq/agentrq/daemon/internal/pty"
	"github.com/agentrq/agentrq/daemon/internal/stream"
	"github.com/agentrq/agentrq/daemon/internal/supervisor"
	"github.com/agentrq/agentrq/daemon/wire"
)

// ── a backend, close enough to the real one to be worth testing against ──────

type fakeBackend struct {
	srv *httptest.Server

	mu       sync.Mutex
	got      []wire.Frame
	headers  http.Header
	conns    int
	sockets  []*websocket.Conn
	rejectAt int // reject this many connections before accepting
}

func newBackend(t *testing.T) *fakeBackend {
	t.Helper()
	b := &fakeBackend{}
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

	b.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		b.headers = r.Header.Clone()
		b.conns++
		reject := b.conns <= b.rejectAt
		b.mu.Unlock()
		if reject {
			http.Error(w, "not yet", http.StatusServiceUnavailable)
			return
		}

		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		b.mu.Lock()
		b.sockets = append(b.sockets, ws)
		b.mu.Unlock()

		for {
			typ, data, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if typ != websocket.BinaryMessage {
				continue
			}
			f, err := wire.Decode(data)
			if err != nil {
				return
			}
			b.mu.Lock()
			b.got = append(b.got, f)
			b.mu.Unlock()
		}
	}))
	t.Cleanup(b.srv.Close)
	return b
}

func (b *fakeBackend) url() string {
	return "ws" + strings.TrimPrefix(b.srv.URL, "http") + Path
}

func (b *fakeBackend) frames() []wire.Frame {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]wire.Frame(nil), b.got...)
}

func (b *fakeBackend) controls(t *testing.T, op wire.Op) []wire.Control {
	t.Helper()
	var out []wire.Control
	for _, f := range b.frames() {
		if f.Type != wire.TypeControl {
			continue
		}
		c, err := wire.ParseControl(f)
		if err == nil && c.Op == op {
			out = append(out, c)
		}
	}
	return out
}

// send pushes a frame from the backend to the daemon.
func (b *fakeBackend) send(t *testing.T, f wire.Frame) {
	t.Helper()
	encoded, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.sockets) == 0 {
		t.Fatal("nothing is connected")
	}
	if err := b.sockets[len(b.sockets)-1].WriteMessage(websocket.BinaryMessage, encoded); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func (b *fakeBackend) connections() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.conns
}

// ── a pseudo-terminal that is a pipe ─────────────────────────────────────────

type fakeTTY struct {
	out    *io.PipeReader
	outW   *io.PipeWriter
	mu     sync.Mutex
	wrote  []byte
	cols   uint16
	rows   uint16
	done   chan struct{}
	code   int
	closed bool
}

func newTTY() *fakeTTY {
	r, w := io.Pipe()
	return &fakeTTY{out: r, outW: w, done: make(chan struct{})}
}

func (f *fakeTTY) Read(p []byte) (int, error) { return f.out.Read(p) }

func (f *fakeTTY) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wrote = append(f.wrote, p...)
	return len(p), nil
}

func (f *fakeTTY) written() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.wrote...)
}

func (f *fakeTTY) Resize(cols, rows uint16) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cols, f.rows = cols, rows
	return nil
}

func (f *fakeTTY) size() (uint16, uint16) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cols, f.rows
}

func (f *fakeTTY) Wait() (int, error) {
	<-f.done
	return f.code, nil
}

func (f *fakeTTY) Close() error {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return nil
	}
	f.closed = true
	f.mu.Unlock()
	_ = f.outW.Close()
	close(f.done)
	return nil
}

func (f *fakeTTY) Kill() error { return f.Close() }

// ── the harness ──────────────────────────────────────────────────────────────

type harness struct {
	backend *fakeBackend
	link    *Link
	sup     *supervisor.Supervisor
	tty     *fakeTTY
	cancel  context.CancelFunc
}

func start(t *testing.T, b *fakeBackend) *harness {
	t.Helper()
	tty := newTTY()
	sup := supervisor.New(starter(tty), 0, 0)

	l := New("work", b.url(), Identity{MachineID: "m1", UserID: "u1", Token: "tkn", Version: "test"},
		realDialer{}, sup, slog.New(slog.NewTextHandler(io.Discard, nil)))
	l.Rand = func() float64 { return 0 }

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = l.Run(ctx) }()

	h := &harness{backend: b, link: l, sup: sup, tty: tty, cancel: cancel}
	waitFor(t, func() bool { return len(b.controls(t, wire.OpHello)) > 0 }, "the daemon never said hello")
	return h
}

// starter stands in for pty.Start, and checks the working directory the way it
// does — a fake that accepted any path would make the refusal test pass
// without the refusal ever happening.
func starter(tty *fakeTTY) supervisor.Starter {
	return func(_ context.Context, spec pty.Spec) (pty.Session, error) {
		if spec.Dir == "" {
			return nil, pty.ErrNoDir
		}
		if _, err := os.Stat(spec.Dir); err != nil {
			return nil, fmt.Errorf("%w: %s", pty.ErrDirMissing, spec.Dir)
		}
		return tty, nil
	}
}

type realDialer struct{}

func (realDialer) Dial(url string, h http.Header) (*websocket.Conn, *http.Response, error) {
	return websocket.DefaultDialer.Dial(url, h)
}

func waitFor(t *testing.T, ok func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal(msg)
}

func controlFrame(t *testing.T, op wire.Op, body any) wire.Frame {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	f, err := wire.ControlFrame(wire.Control{Op: op, Body: b})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// ── the tests ────────────────────────────────────────────────────────────────

// The one that matters: a keystroke from the backend reaches the terminal, and
// the terminal's output reaches the backend, over a real socket.
func TestAKeystrokeReachesTheTerminalAndOutputComesBack(t *testing.T) {
	b := newBackend(t)
	h := start(t, b)

	dir := t.TempDir()
	b.send(t, controlFrame(t, wire.OpStartSession, wire.StartSession{
		SessionID: 7, Kind: "acp-gateway", Dir: dir, Model: "m", Agent: "a", Cols: 80, Rows: 24,
	}))
	waitFor(t, func() bool {
		for _, c := range b.controls(t, wire.OpSessionState) {
			var st wire.SessionState
			_ = json.Unmarshal(c.Body, &st)
			if st.State == "running" {
				return true
			}
		}
		return false
	}, "the session never reported running")

	// A viewer attaches, which is what starts the output flowing.
	b.send(t, controlFrame(t, wire.OpAttach, wire.KillSession{SessionID: 7}))

	// Esc, then a key. Byte-identical or nothing.
	keys := []byte{0x1b, 'b', '\r'}
	f, err := wire.SessionFrame(wire.TypeInput, 7, keys)
	if err != nil {
		t.Fatal(err)
	}
	b.send(t, f)
	waitFor(t, func() bool { return len(h.tty.written()) == len(keys) }, "the keystrokes never reached the terminal")
	if got := h.tty.written(); string(got) != string(keys) {
		t.Errorf("the terminal got %#v, want %#v", got, keys)
	}

	// And output makes the return trip.
	if _, err := h.tty.outW.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		for _, fr := range b.frames() {
			if fr.Type == wire.TypeOutput && strings.Contains(string(fr.Payload), "hello") {
				return true
			}
		}
		return false
	}, "the terminal's output never reached the backend")
}

// Resize travels the same path and lands on the pseudo-terminal.
func TestResizeReachesTheTerminal(t *testing.T) {
	b := newBackend(t)
	h := start(t, b)

	b.send(t, controlFrame(t, wire.OpStartSession, wire.StartSession{
		SessionID: 7, Kind: "acp-gateway", Dir: t.TempDir(), Model: "m", Agent: "a", Cols: 80, Rows: 24,
	}))
	waitFor(t, func() bool { _, err := h.sup.Get(7); return err == nil }, "the session never started")

	body, err := json.Marshal(wire.Resize{Cols: 132, Rows: 43})
	if err != nil {
		t.Fatal(err)
	}
	f, err := wire.SessionFrame(wire.TypeResize, 7, body)
	if err != nil {
		t.Fatal(err)
	}
	b.send(t, f)

	waitFor(t, func() bool { c, r := h.tty.size(); return c == 132 && r == 43 }, "the terminal was never resized")
}

// The hello says what this daemon is running and what it is supervising, so a
// backend that restarted can correct rows it believes are alive.
func TestHelloDescribesTheMachine(t *testing.T) {
	b := newBackend(t)
	start(t, b)

	var hello wire.Hello
	if err := json.Unmarshal(b.controls(t, wire.OpHello)[0].Body, &hello); err != nil {
		t.Fatal(err)
	}
	if hello.Version != "test" {
		t.Errorf("version = %q", hello.Version)
	}
	if hello.OS == "" || hello.Arch == "" {
		t.Errorf("hello did not say what platform this is: %+v", hello)
	}
	if len(hello.Sessions) != 0 {
		t.Errorf("a fresh daemon claimed sessions: %v", hello.Sessions)
	}
}

// Who the daemon claims to be travels on the upgrade request.
func TestTheClaimIsSentOnConnect(t *testing.T) {
	b := newBackend(t)
	start(t, b)

	b.mu.Lock()
	defer b.mu.Unlock()
	if got := b.headers.Get("Authorization"); got != "Bearer tkn" {
		t.Errorf("Authorization = %q", got)
	}
	if got := b.headers.Get("X-AgentRQ-Machine-Id"); got != "m1" {
		t.Errorf("machine header = %q", got)
	}
	if got := b.headers.Get("X-AgentRQ-User-Id"); got != "u1" {
		t.Errorf("user header = %q", got)
	}
}

// A daemon that stopped when the server restarted would need somebody to walk
// over to that machine, which is the thing this exists to avoid.
func TestItReconnects(t *testing.T) {
	b := newBackend(t)
	b.rejectAt = 2 // refuse the first two attempts

	tty := newTTY()
	sup := supervisor.New(starter(tty), 0, 0)
	l := New("work", b.url(), Identity{Token: "tkn", Version: "test"}, realDialer{}, sup,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	// No jitter, so the retries are the fixed half of the window and this test
	// waits for the loop rather than for a random number.
	l.Rand = func() float64 { return 0 }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = l.Run(ctx) }()

	waitFor(t, func() bool { return len(b.controls(t, wire.OpHello)) > 0 }, "it never got through")
	if b.connections() < 3 {
		t.Errorf("connected after %d attempts, want at least 3", b.connections())
	}
}

// Nothing the backend can send should cost the connection: every other session
// on this machine is on it.
func TestBadMessagesDoNotDropTheConnection(t *testing.T) {
	b := newBackend(t)
	h := start(t, b)
	before := b.connections()

	b.send(t, wire.Frame{Type: wire.TypeControl, Payload: []byte("{not json")})
	b.send(t, controlFrame(t, "somethingNewer", map[string]string{}))
	b.send(t, controlFrame(t, wire.OpAttach, wire.KillSession{SessionID: 999}))
	inputToNowhere, err := wire.SessionFrame(wire.TypeInput, 999, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	b.send(t, inputToNowhere)

	// Still the same connection, and still able to do the ordinary thing.
	b.send(t, controlFrame(t, wire.OpStartSession, wire.StartSession{
		SessionID: 7, Kind: "acp-gateway", Dir: t.TempDir(), Model: "m", Agent: "a",
	}))
	waitFor(t, func() bool { _, err := h.sup.Get(7); return err == nil }, "the connection did not survive")
	if b.connections() != before {
		t.Errorf("the connection was dropped and remade (%d → %d)", before, b.connections())
	}
}

// A refused start is reported, not swallowed: a row left in "starting" blocks
// the workspace's next launch forever.
func TestARefusedStartIsReported(t *testing.T) {
	b := newBackend(t)
	start(t, b)

	b.send(t, controlFrame(t, wire.OpStartSession, wire.StartSession{
		SessionID: 7, Kind: "acp-gateway", Dir: "/definitely/not/a/directory", Model: "m", Agent: "a",
	}))

	waitFor(t, func() bool {
		for _, c := range b.controls(t, wire.OpSessionState) {
			var st wire.SessionState
			_ = json.Unmarshal(c.Body, &st)
			if st.State == "failed" && strings.Contains(st.Error, "/definitely/not/a/directory") {
				return true
			}
		}
		return false
	}, "the refusal never reached the backend, or did not name the path")
}

// A full send queue is backpressure, not a wait: blocking here would stall the
// terminal read loop, and a slow network would become a slow machine.
func TestAFullQueueIsBackpressure(t *testing.T) {
	c := &Conn{out: make(chan []byte, 1), done: make(chan struct{})}
	f, err := wire.SessionFrame(wire.TypeOutput, 1, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Send(f); err != nil {
		t.Fatalf("the first send failed: %v", err)
	}
	if err := c.Send(f); !errors.Is(err, stream.ErrBackpressure) {
		t.Errorf("a full queue returned %v, want backpressure", err)
	}
}

// Detaching stops the output without stopping the session: the screen keeps
// being fed, or the next viewer sees whatever was there when the last one left.
func TestDetachStopsTheOutputButNotTheSession(t *testing.T) {
	b := newBackend(t)
	h := start(t, b)

	b.send(t, controlFrame(t, wire.OpStartSession, wire.StartSession{
		SessionID: 7, Kind: "acp-gateway", Dir: t.TempDir(), Model: "m", Agent: "a", Cols: 80, Rows: 24,
	}))
	waitFor(t, func() bool { _, err := h.sup.Get(7); return err == nil }, "the session never started")

	b.send(t, controlFrame(t, wire.OpAttach, wire.KillSession{SessionID: 7}))
	if _, err := h.tty.outW.Write([]byte("while watching")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return outputContains(b, "while watching") }, "output never arrived")

	b.send(t, controlFrame(t, wire.OpDetach, wire.KillSession{SessionID: 7}))
	// Given time to land before writing, so this tests the detach rather than
	// racing it.
	time.Sleep(50 * time.Millisecond)
	if _, err := h.tty.outW.Write([]byte("after leaving")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if outputContains(b, "after leaving") {
		t.Error("output was sent to a session nobody is watching")
	}

	// And the session is still alive, which is the half that matters.
	if state, _, _ := mustSession(t, h).State(); state != "running" {
		t.Errorf("the session is %q after a detach", state)
	}
}

func mustSession(t *testing.T, h *harness) *supervisor.Session {
	t.Helper()
	sess, err := h.sup.Get(7)
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

func outputContains(b *fakeBackend, want string) bool {
	for _, f := range b.frames() {
		if (f.Type == wire.TypeOutput || f.Type == wire.TypeReplay) && strings.Contains(string(f.Payload), want) {
			return true
		}
	}
	return false
}

// A text frame is a peer that has misunderstood the protocol, and continuing
// would mean guessing what it meant.
func TestANonBinaryFrameEndsTheConnection(t *testing.T) {
	b := newBackend(t)
	start(t, b)
	before := b.connections()

	b.mu.Lock()
	ws := b.sockets[len(b.sockets)-1]
	b.mu.Unlock()
	if err := ws.WriteMessage(websocket.TextMessage, []byte("hello?")); err != nil {
		t.Fatal(err)
	}

	waitFor(t, func() bool { return b.connections() > before }, "the daemon kept a connection it could not parse")
}

// A dial refused with a status says something a person can act on — "disabled"
// and "the server is down" are different problems.
func TestARefusedDialNamesTheStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "machine disabled", http.StatusForbidden)
	}))
	defer srv.Close()

	l := New("work", "ws"+strings.TrimPrefix(srv.URL, "http")+Path, Identity{Token: "t"},
		realDialer{}, supervisor.New(starter(newTTY()), 0, 0), slog.New(slog.NewTextHandler(io.Discard, nil)))

	err := l.once(context.Background())
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("a refused dial reported %v, want the status in it", err)
	}
}

// A connection that has gone takes no more frames, rather than blocking a
// caller on a queue nobody is draining.
func TestSendingOnAClosedConnectionFails(t *testing.T) {
	c := &Conn{out: make(chan []byte, 1), done: make(chan struct{})}
	close(c.done)

	f, err := wire.SessionFrame(wire.TypeOutput, 1, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Send(f); err == nil {
		t.Error("a closed connection accepted a frame")
	}
	if err := c.Control(wire.Control{Op: wire.OpHeartbeat}); err == nil {
		t.Error("a closed connection accepted a control message")
	}
}

// A malformed frame is a protocol the daemon cannot follow, so the connection
// ends and the backoff loop makes a fresh one.
func TestAnUndecodableFrameEndsTheConnection(t *testing.T) {
	b := newBackend(t)
	start(t, b)
	before := b.connections()

	b.mu.Lock()
	ws := b.sockets[len(b.sockets)-1]
	b.mu.Unlock()
	// Shorter than a header: nothing that could be a frame.
	if err := ws.WriteMessage(websocket.BinaryMessage, []byte{0x01, 0x02}); err != nil {
		t.Fatal(err)
	}

	waitFor(t, func() bool { return b.connections() > before }, "the daemon kept a connection carrying frames it cannot read")
}
