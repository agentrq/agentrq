// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package link

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/agentrq/agentrq/daemon/internal/restore"
	"github.com/agentrq/agentrq/daemon/internal/supervisor"
	"github.com/agentrq/agentrq/daemon/wire"
)

// Dialer opens the socket. An interface so a test can supply one without TLS,
// and so the insecure case is a decision made once, at construction.
type Dialer interface {
	Dial(url string, h http.Header) (*websocket.Conn, *http.Response, error)
}

// Link runs one profile's connection to the backend, for as long as it is
// asked to.
//
// It reconnects rather than exiting. A machine daemon that stopped when the
// server restarted would need somebody to walk over to that machine, which is
// the one thing this whole system exists to avoid.
type Link struct {
	Profile    string
	URL        string
	Identity   Identity
	Dialer     Dialer
	Supervisor *supervisor.Supervisor
	Log        *slog.Logger

	// Rand is the jitter source, injected so backoff can be tested.
	Rand func() float64

	// Updater replaces this daemon's binary when somebody approves it. Nil in
	// a build that cannot update itself, which then simply refuses.
	Updater *Updater

	// Pending are sessions an update stopped, to be started again on the first
	// connection. Each link takes only its own profile's.
	Pending []restore.Session

	// Metrics is the machine's last measurement, and whether there has been
	// one. Read rather than measured: a heartbeat that waited on a disk that
	// had gone away would stop being a heartbeat at exactly the moment it was
	// most informative.
	Metrics func() (wire.Heartbeat, bool)
	// HeartbeatEvery is how often one is sent. Well under the backend's
	// threshold for calling a machine offline, so a machine has to miss
	// several beats before anybody is told it is gone.
	HeartbeatEvery time.Duration

	streams  *streams
	restored bool
}

// New builds a link.
func New(profile, url string, id Identity, d Dialer, s *supervisor.Supervisor, log *slog.Logger) *Link {
	return &Link{
		Profile:        profile,
		URL:            url,
		Identity:       id,
		Dialer:         d,
		Supervisor:     s,
		Log:            log,
		Rand:           rand.Float64,
		HeartbeatEvery: DefaultHeartbeat,
		streams:        newStreams(),
	}
}

// Run connects, serves, and reconnects until the context ends.
func (l *Link) Run(ctx context.Context) error {
	attempt := 0
	for {
		err := l.once(ctx)
		if ctx.Err() != nil {
			return nil
		}

		attempt++
		wait := Backoff(attempt, l.Rand)
		// Logged at the level it deserves: a machine that cannot reach its
		// backend is something the owner of that machine wants to find in the
		// journal, not a debug line.
		// The profile is already on the logger the caller handed in; naming it
		// again here is how a log line ends up saying it twice.
		l.Log.Warn("disconnected from the backend",
			"error", err, "retry_in", wait, "attempt", attempt)

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
	}
}

// once holds a single connection until it ends.
func (l *Link) once(ctx context.Context) error {
	ws, resp, err := l.Dialer.Dial(l.URL, l.Identity.Headers())
	if err != nil {
		if resp != nil {
			// The status is the difference between "the server is down" and
			// "this machine has been disabled", and the person reading the
			// log needs to be able to tell those apart.
			return fmt.Errorf("dial: %w (http %s)", err, resp.Status)
		}
		return fmt.Errorf("dial: %w", err)
	}

	conn := NewConn(ws)
	defer func() {
		_ = conn.Close()
		// The pumps go; the sessions do not. A daemon that killed its agents
		// every time the network blinked would be worse than no daemon.
		l.streams.closeAll()
	}()

	ws.SetReadLimit(maxFrame)
	_ = ws.SetReadDeadline(time.Now().Add(pongWait))
	ws.SetPongHandler(func(string) error {
		_ = ws.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		<-connCtx.Done()
		_ = conn.Close()
	}()

	hostname, _ := os.Hostname()
	if err := conn.Control(wire.Control{Op: wire.OpHello, Body: mustJSON(wire.Hello{
		Version:  l.Identity.Version,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Hostname: hostname,
		// What this daemon is actually supervising. Empty after a restart,
		// which is how the backend learns that rows it thinks are running
		// are not.
		Sessions: l.Supervisor.Running(),
	})}); err != nil {
		return fmt.Errorf("hello: %w", err)
	}
	l.Log.Info("connected to the backend")

	go l.heartbeat(connCtx, conn)
	if l.Updater != nil {
		go l.Updater.Watch(connCtx, conn)
	}

	// Once, on the first connection that works. A reconnect is not a restart,
	// and restoring again would start a second copy of everything.
	if !l.restored {
		l.restored = true
		for _, s := range l.Pending {
			l.Restore(connCtx, conn, s)
		}
		l.Pending = nil
	}

	return l.serve(connCtx, ws, conn)
}

// DefaultHeartbeat is how often the machine reports itself.
const DefaultHeartbeat = 15 * time.Second

// heartbeat reports the machine until the connection ends.
//
// Nothing is sent until there is a measurement to send. An empty heartbeat
// would land on the row as a snapshot of zero memory and an idle CPU, which is
// a confident lie where "we have not heard yet" is the truth — and it would
// defeat the nullable field that exists to tell those apart.
//
// Liveness does not depend on this. The connection's own ping and pong is what
// keeps a machine online, and the backend counts every pong.
func (l *Link) heartbeat(ctx context.Context, conn *Conn) {
	every := l.HeartbeatEvery
	if every <= 0 {
		every = DefaultHeartbeat
	}
	send := func() bool {
		if l.Metrics == nil {
			return false
		}
		hb, ok := l.Metrics()
		if !ok {
			return false
		}
		hb.Sessions = l.Supervisor.Running()
		if err := conn.Control(wire.Control{Op: wire.OpHeartbeat, Body: mustJSON(hb)}); err != nil {
			l.Log.Debug("heartbeat not sent", "error", err)
		}
		return true
	}

	// Connecting and taking the first measurement race, and connecting
	// usually wins. Looking again shortly rather than waiting out a whole
	// interval is what stops a machine showing no metrics at all for the first
	// quarter of a minute after it appears.
	next := time.NewTimer(0)
	defer next.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-next.C:
			if send() {
				next.Reset(every)
			} else {
				next.Reset(WaitingForMetrics)
			}
		}
	}
}

// WaitingForMetrics is how often the heartbeat looks again while the first
// measurement is still being taken.
const WaitingForMetrics = 500 * time.Millisecond

// serve reads frames until the connection ends.
func (l *Link) serve(ctx context.Context, ws *websocket.Conn, conn *Conn) error {
	for {
		typ, data, err := ws.ReadMessage()
		if err != nil {
			return err
		}
		if typ != websocket.BinaryMessage {
			// The protocol is binary. A text frame is a peer that has
			// misunderstood it, and continuing would mean guessing.
			return errors.New("link: non-binary frame from the backend")
		}
		f, err := wire.Decode(data)
		if err != nil {
			return fmt.Errorf("link: undecodable frame: %w", err)
		}
		l.dispatch(ctx, conn, f)
	}
}

// dispatch acts on one frame.
//
// Nothing here returns an error to the caller, and that is deliberate: a bad
// frame must not cost the connection, because every other session on this
// machine is on it.
func (l *Link) dispatch(ctx context.Context, conn *Conn, f wire.Frame) {
	if f.Type != wire.TypeControl {
		if err := l.Supervisor.HandleFrame(f); err != nil {
			l.Log.Warn("could not deliver a frame to its session",
				"type", f.Type.String(), "session", f.SessionID, "error", err)
		}
		return
	}

	c, err := wire.ParseControl(f)
	if err != nil {
		l.Log.Warn("unreadable control message", "error", err)
		return
	}

	switch c.Op {
	case wire.OpAttach, wire.OpDetach:
		l.attach(c)
	case wire.OpStartSession:
		// Handled by the supervisor, and then wired to a pump — the supervisor
		// knows about processes, and this knows about the connection.
		l.start(ctx, conn, c)
	case wire.OpUpdateNow:
		l.updateNow(ctx, conn, c)
	default:
		if err := l.Supervisor.Handle(ctx, l.Profile, c, conn); err != nil {
			l.Log.Warn("control message failed", "op", string(c.Op), "error", err)
		}
	}
}

// capturingReporter passes state reports through and remembers the last
// failure, so it can be logged where the machine's owner will see it.
type capturingReporter struct {
	to   supervisor.Reporter
	mu   sync.Mutex
	last string
}

func (r *capturingReporter) ReportSessionState(st wire.SessionState) error {
	if st.Error != "" {
		r.mu.Lock()
		r.last = st.Error
		r.mu.Unlock()
	}
	return r.to.ReportSessionState(st)
}

func (r *capturingReporter) reason() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.last == "" {
		return "no reason was reported"
	}
	return r.last
}

// updateNow acts on an approval.
//
// Run on its own goroutine, because it ends with this process being replaced
// and the frame loop is what would otherwise be waiting for it.
func (l *Link) updateNow(ctx context.Context, conn *Conn, c wire.Control) {
	var req wire.UpdateNow
	if err := json.Unmarshal(c.Body, &req); err != nil {
		l.Log.Warn("unreadable update approval", "error", err)
		return
	}
	if l.Updater == nil {
		// A build that cannot update itself says so rather than ignoring the
		// request: somebody is watching a button they just pressed.
		l.report(conn, c.ID, "this build cannot update itself")
		return
	}

	go func() {
		if err := l.Updater.Apply(ctx, req.Version); err != nil {
			l.Log.Error("update refused", "approved", req.Version, "error", err)
			l.report(conn, c.ID, err.Error())
		}
	}()
}

// report sends an error back, correlated with whatever provoked it.
func (l *Link) report(conn *Conn, id, message string) {
	_ = conn.Control(wire.Control{
		ID:   id,
		Op:   wire.OpError,
		Body: mustJSON(map[string]string{"error": message}),
	})
}

func (l *Link) attach(c wire.Control) {
	var req wire.KillSession // the attach payload is just a session id
	if err := json.Unmarshal(c.Body, &req); err != nil {
		l.Log.Warn("unreadable attach", "error", err)
		return
	}
	p, ok := l.streams.get(req.SessionID)
	if !ok {
		// A viewer attached to a session that has already gone. Not an error
		// worth shouting about: the backend will learn it ended from the state
		// report that is already on its way.
		l.Log.Debug("attach for a session with no stream", "session", req.SessionID)
		return
	}
	if c.Op == wire.OpDetach {
		p.Detach()
		return
	}
	if err := p.Attach(); err != nil {
		l.Log.Warn("could not send the screen to a new viewer", "session", req.SessionID, "error", err)
	}
}

func (l *Link) start(ctx context.Context, conn *Conn, c wire.Control) {
	var req wire.StartSession
	if err := json.Unmarshal(c.Body, &req); err != nil {
		l.Log.Warn("unreadable start request", "error", err)
		return
	}

	// Wrapped so the reason a start was refused can be logged here as well as
	// sent. The supervisor reports it and then returns nil, which is right for
	// the connection and useless to the person at the machine.
	rep := &capturingReporter{to: conn}
	if err := l.Supervisor.Handle(ctx, l.Profile, c, rep); err != nil {
		l.Log.Warn("start failed", "session", req.SessionID, "error", err)
		return
	}

	// Only a session that actually started has a terminal to read.
	//
	// A refusal has already been reported to the backend by the supervisor,
	// which returns nil for it — closing the socket over one bad launch would
	// take every other session on this machine with it. But the person
	// standing at this machine can see the log and not the control panel, so
	// it is said here too. Without this, a start that fails is completely
	// silent on the machine it failed on.
	sess, err := l.Supervisor.Get(req.SessionID)
	if err != nil {
		l.Log.Warn("a session was refused and never started",
			"session", req.SessionID, "kind", req.Kind, "dir", req.Dir,
			"reason", rep.reason())
		return
	}
	tty := sess.PTY()
	if tty == nil {
		l.Log.Warn("a session started without a terminal", "session", req.SessionID)
		return
	}
	cols, rows := req.Cols, req.Rows
	if cols == 0 || rows == 0 {
		// A terminal with no size renders as one column, which looks like the
		// agent is broken rather than like nobody said how big the window is.
		cols, rows = 80, 24
	}
	l.streams.add(req.SessionID, cols, rows, tty, conn)
}
