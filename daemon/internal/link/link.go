// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package link

import (
	"bytes"
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
	"github.com/agentrq/agentrq/daemon/internal/stream"
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

	// Restarter restarts this daemon when somebody asks, bringing its agents
	// back. Nil when the binary's own path cannot be resolved.
	Restarter *Restarter

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

	// ListAcpAgents and ListAcpModels ask the gateway what it can run,
	// injected so a dispatch test does not have to shell out to npx. Nil
	// defaults to the supervisor package's real implementation.
	ListAcpAgents func(ctx context.Context) []wire.AcpAgent
	ListAcpModels func(ctx context.Context, dir, agent string) []wire.AcpModel

	streams  *streams
	viewers  *viewerCount
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
		viewers:        newViewerCount(),
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
		// The viewers go; the pumps and the sessions do not. A daemon that
		// killed its agents every time the network blinked would be worse than
		// no daemon — and a pump is not the connection's to throw away either:
		// it holds the screen, and it is reading a terminal that is still
		// running.
		l.streams.detachAll()
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
		Sessions:     l.Supervisor.Running(),
		Capabilities: l.capabilities(),
	})}); err != nil {
		return fmt.Errorf("hello: %w", err)
	}
	l.Log.Info("connected to the backend")

	// The sessions that lived through the gap have pumps pointing at the
	// socket that went. They are moved onto this one, and anything somebody is
	// still watching is repainted — the browser never disconnected, so nobody
	// is going to ask to attach again on its behalf.
	for _, err := range l.streams.rebind(conn, func(id uint64) bool { return l.viewers.get(id) > 0 }) {
		l.Log.Warn("could not repaint a terminal after reconnecting", "error", err)
	}

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
	case wire.OpRestart:
		l.restartNow(ctx, conn, c)
	case wire.OpListAcpAgents:
		// Its own goroutine: this shells out and can take tens of seconds on
		// a cold npx cache, and the frame loop reading this connection must
		// not block behind it — every session on the machine rides the same
		// socket.
		go l.listAcpAgents(ctx, conn, c)
	case wire.OpListAcpModels:
		go l.listAcpModels(ctx, conn, c)
	case wire.OpRemoveForkDir:
		// Its own goroutine too: git and deleting a whole tree can take a
		// minute, and every session on the machine rides this socket.
		go l.handle(ctx, conn, c)
	default:
		l.handle(ctx, conn, c)
	}
}

// handle passes a control message to the supervisor.
func (l *Link) handle(ctx context.Context, conn *Conn, c wire.Control) {
	if err := l.Supervisor.Handle(ctx, l.Profile, c, conn); err != nil {
		l.Log.Warn("control message failed", "op", string(c.Op), "error", err)
	}
}

// capturingReporter passes state reports through and remembers the last
// failure, so it can be logged where the machine's owner will see it.
type capturingReporter struct {
	to   supervisor.Reporter
	mu   sync.Mutex
	last string
	// beforeRunning runs just before "running" is sent, so a viewer the
	// backend lets in on hearing it finds the terminal's stream there.
	beforeRunning func()
	// progress shows the launch in its terminal before the agent is running;
	// nil when its stream is made only once it is.
	progress supervisor.Progress
}

func (r *capturingReporter) Progress() supervisor.Progress { return r.progress }

func (r *capturingReporter) ReportSessionState(st wire.SessionState) error {
	if st.Error != "" {
		r.mu.Lock()
		r.last = st.Error
		r.mu.Unlock()
	}
	if st.State == string(supervisor.StateRunning) && r.beforeRunning != nil {
		r.beforeRunning()
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
			l.recoverFrom(ctx, conn, err)
		}
	}()
}

// restartNow acts on a request to restart, which brings the agents back.
func (l *Link) restartNow(ctx context.Context, conn *Conn, c wire.Control) {
	if l.Restarter == nil {
		l.report(conn, c.ID, "this daemon cannot restart itself")
		return
	}
	go func() {
		if err := l.Restarter.Now(ctx); err != nil {
			l.Log.Error("restart failed", "error", err)
			l.report(conn, c.ID, err.Error())
			l.recoverFrom(ctx, conn, err)
		}
	}()
}

// recoverFrom starts again, in this process, the agents a handover stopped for
// a restart that did not happen — the next daemon was going to, and there is
// not one. Only this link's; another profile's are reported lost.
func (l *Link) recoverFrom(ctx context.Context, conn *Conn, err error) {
	if !errors.Is(err, ErrNotRestarted) {
		return
	}
	note, takeErr := restore.Take(l.Restarter.StateDir, time.Now())
	if takeErr != nil {
		l.Log.Warn("cannot bring the stopped sessions back", "error", takeErr)
	}
	for _, s := range note.Sessions {
		if s.Profile != l.Profile {
			l.Log.Warn("a session of another profile was stopped and is not coming back", "session", s.ID)
			continue
		}
		l.Restore(ctx, conn, s)
	}
	l.Supervisor.Abandon()
}

// capabilities is what the hello says this daemon can do.
func (l *Link) capabilities() []string {
	caps := []string{wire.CapabilityFork, wire.CapabilityForkCleanup}
	if l.Restarter != nil {
		caps = append(caps, wire.CapabilityRestart)
	}
	if l.Updater != nil {
		caps = append(caps, wire.CapabilityUpdate)
	}
	return caps
}

// report sends an error back, correlated with whatever provoked it.
func (l *Link) report(conn *Conn, id, message string) {
	_ = conn.Control(wire.Control{
		ID:   id,
		Op:   wire.OpError,
		Body: mustJSON(map[string]string{"error": message}),
	})
}

// listAcpAgents answers a request for the gateway's agent catalogue.
//
// Always answered, even with nothing to say: an empty AcpAgentsList is a
// normal reply here, not a failure, since [supervisor.ListAcpAgents] already
// turned every failure it could hit into that same empty list.
func (l *Link) listAcpAgents(ctx context.Context, conn *Conn, c wire.Control) {
	fn := l.ListAcpAgents
	if fn == nil {
		fn = supervisor.ListAcpAgents
	}
	agents := fn(ctx)
	_ = conn.Control(wire.Control{
		ID:   c.ID,
		Op:   wire.OpAcpAgents,
		Body: mustJSON(wire.AcpAgentsList{Agents: agents}),
	})
}

// listAcpModels answers a request for one agent's models.
func (l *Link) listAcpModels(ctx context.Context, conn *Conn, c wire.Control) {
	var req wire.ListAcpModels
	if err := json.Unmarshal(c.Body, &req); err != nil {
		l.Log.Warn("unreadable list-models request", "error", err)
		_ = conn.Control(wire.Control{ID: c.ID, Op: wire.OpAcpModels, Body: mustJSON(wire.AcpModelsList{})})
		return
	}
	fn := l.ListAcpModels
	if fn == nil {
		fn = supervisor.ListAcpModels
	}
	models := fn(ctx, req.Dir, req.Agent)
	_ = conn.Control(wire.Control{
		ID:   c.ID,
		Op:   wire.OpAcpModels,
		Body: mustJSON(wire.AcpModelsList{Agent: req.Agent, Models: models}),
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
	// Logged on the machine, not only on the server. Somebody at this keyboard
	// must be able to find out that a terminal here is being watched, without
	// having to ask the account that is watching it — which is the whole point
	// of a local record. The keystrokes are not logged: those carry secrets,
	// and the fact of the attach is what belongs in the record.
	if c.Op == wire.OpDetach {
		p.Detach()
		l.Log.Info("a viewer stopped watching a terminal on this machine", "session", req.SessionID)
		l.viewers.leave(req.SessionID)
		return
	}
	l.Log.Warn("a viewer is watching a terminal on this machine", "session", req.SessionID)
	l.viewers.join(req.SessionID)
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
	if req.Fork != nil {
		// Its own goroutine: a fork's first launch makes its folder, a git
		// worktree or a copy of a whole project, and every session on the
		// machine rides this socket.
		go l.launch(ctx, conn, c, req)
		return
	}
	l.launch(ctx, conn, c, req)
}

func (l *Link) launch(ctx context.Context, conn *Conn, c wire.Control, req wire.StartSession) {
	// Wrapped so the reason a start was refused can be logged here as well as
	// sent. The supervisor reports it and then returns nil, which is right for
	// the connection and useless to the person at the machine.
	rep := &capturingReporter{to: conn}
	var once sync.Once
	rep.beforeRunning = func() { once.Do(func() { l.stream(conn, req) }) }
	if req.Fork != nil {
		// Streamed from the start: making a fork's folder can take minutes,
		// and a viewer let in while it is "starting" watches the checkout
		// rather than a blank terminal.
		cols, rows := terminalSize(req)
		if p, ok := l.streams.openNew(req.SessionID, cols, rows, conn); ok {
			rep.progress = &terminalProgress{pump: p, log: l.Log}
		}
	}
	err := l.Supervisor.Handle(ctx, l.Profile, c, rep)
	if _, getErr := l.Supervisor.Get(req.SessionID); getErr != nil && rep.progress != nil {
		l.streams.remove(req.SessionID)
	}
	if err != nil {
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
	if _, err := l.Supervisor.Get(req.SessionID); err != nil {
		l.Log.Warn("a session was refused and never started",
			"session", req.SessionID, "kind", req.Kind, "dir", req.Dir,
			"reason", rep.reason())
	}
}

// stream starts reading a session's terminal, once it is running.
func (l *Link) stream(conn *Conn, req wire.StartSession) {
	sess, err := l.Supervisor.Get(req.SessionID)
	if err != nil {
		return
	}
	tty := sess.PTY()
	opened, isOpen := l.streams.get(req.SessionID)
	if tty == nil {
		l.Log.Warn("a session started without a terminal", "session", req.SessionID)
		if isOpen {
			l.streams.remove(req.SessionID)
		}
		return
	}
	if isOpen {
		// Its notices are on the screen already, put there as they happened.
		l.streams.run(req.SessionID, opened, tty)
		return
	}
	cols, rows := terminalSize(req)
	pump := l.streams.add(req.SessionID, cols, rows, tty, conn)
	announce(pump, sess.Notices(), l.Log)
}

// terminalSize is the size a session's screen is made at.
func terminalSize(req wire.StartSession) (cols, rows uint16) {
	if req.Cols == 0 || req.Rows == 0 {
		// A terminal with no size renders as one column, which looks like the
		// agent is broken rather than like nobody said how big the window is.
		return 80, 24
	}
	return req.Cols, req.Rows
}

// terminalProgress shows a launch in its terminal before the agent runs.
type terminalProgress struct {
	pump *stream.Pump
	log  *slog.Logger
}

func (t *terminalProgress) Notice(text string) { announce(t.pump, []string{text}, t.log) }

// Write feeds what git prints into the screen. Never an error: a terminal
// that cannot be drawn must not fail the checkout it is drawing.
func (t *terminalProgress) Write(b []byte) (int, error) {
	// git ends a line with a bare \n, which in a terminal leaves the next one
	// indented; its progress redraws with a bare \r, which is kept.
	if err := t.pump.Feed(bytes.ReplaceAll(b, []byte("\n"), []byte("\r\n"))); err != nil {
		t.log.Warn("could not put a launch's progress in the terminal", "error", err)
	}
	return len(b), nil
}

// announce puts a launch's notices at the top of its terminal.
//
// Fed into the stream rather than written to the terminal: writing to the
// pseudo-terminal would be typing at the agent, and an agent that reads
// "your .mcp.json already had an entry" as input is an agent that may go and
// do something about it. Feeding it reaches the screen and everyone watching,
// and the process never sees a byte of it.
//
// It goes in before the agent has printed anything, so it is the first line of
// the scrollback for whoever opens the terminal later rather than something
// they have to scroll back to find.
func announce(p *stream.Pump, notices []string, log *slog.Logger) {
	for _, n := range notices {
		// Yellow, then reset. CRLF because this is a terminal, and a bare
		// newline leaves the next line indented to wherever this one ended.
		if err := p.Feed([]byte("\x1b[33magentrqd: " + n + "\x1b[0m\r\n")); err != nil {
			log.Warn("could not put a launch notice in the terminal", "error", err)
		}
	}
}
