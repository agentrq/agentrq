// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
	"github.com/gorilla/websocket"
	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"
)

var (
	errInitTimeout     = errors.New("the agent did not answer initialize")
	errVersion         = errors.New("the agent speaks another ACP version")
	errAuthUnavailable = errors.New("the agent needs a login AgentRQ cannot run")
	errProcessExited   = errors.New("the agent process exited")
	errNoSessionID     = errors.New("the agent opened a session with no id")
)

// maxStderrLines is how much of the agent's stderr is kept for the log line
// written when it exits.
const maxStderrLines = 40

// EngineDeps is what app hands the engine.
type EngineDeps struct {
	Workspace  func(workspaceID int64, userID string) Workspace // app: the MCP manager's Get
	MCPURL     func(workspaceID int64) string                   // app: the launcher's URLs.Workspace
	AppVersion string
	OnConnect  func(ctx context.Context, workspaceID int64, userID string) // telemetry, Task 6
	OnTurn     func(ctx context.Context, workspaceID int64, userID string) // telemetry, Task 6
	OnLogout   func(ctx context.Context, workspaceID int64, userID string) // telemetry
}

// update is one item of the ordered update queue: a notification; a barrier
// a turn waits on so that every update that arrived before the prompt's
// answer is applied before the turn is flushed; or a session to register.
type update struct {
	n          sessionNotification
	barrier    chan struct{}
	register   *session
	registered chan []*session // the sessions registering evicted
}

// engineLimits is the engine's copy of the package's timings and bounds,
// taken when it is created, so that a test shrinking one never races an
// older connection.
type engineLimits struct {
	initialize, idleSession, call, permission, cancelSettle time.Duration
	sessions, reply, thought, tool, early                   int
}

func currentLimits() engineLimits {
	return engineLimits{initialize: initializeTimeout, idleSession: idleSessionWait, call: callTimeout, permission: defaultPermissionWait,
		cancelSettle: cancelSettleWait, sessions: maxSessions, reply: maxReplyBytes, thought: maxThoughtBytes, tool: maxToolCallsKept, early: maxEarlyUpdates}
}

// engineV1 is the ACP client for one bridge connection: the agent behind it,
// its sessions, and the turns they run.
//
// Never call a peer method while holding mu.
type engineV1 struct {
	deps   EngineDeps
	c      *connection
	ws     Workspace
	peer   *peer
	link   *link
	linkID string
	lim    engineLimits
	ctx    context.Context
	stop   context.CancelFunc
	q      *turnQueue

	updates chan update
	early   []sessionNotification // updates for a session not yet registered; the update loop's alone

	// set once by the handshake, read-only afterwards
	agent       agentlink.IdentityReport
	caps        agentCapabilities
	authMethods []authMethod

	mu              sync.Mutex
	sessions        sessionSet
	idle            *session      // opened at connect, not yet adopted by a task
	idlePending     chan struct{} // closed when the idle session has opened (or failed)
	preferredModel  string
	preferredEffort string
	commands        *agentlink.CommandsReport
	attached        bool
	permissions     map[string]*pendingPermission      // by request id
	urlQuestions    map[string]context.CancelCauseFunc // url questions still open, by elicitation id
	stderr          []string
	cause           error // why the connection is ending, once it is
}

// NewEngineFactory is what app hands Handler.NewEngine.
func NewEngineFactory(d EngineDeps) func(c *connection) engine {
	return func(c *connection) engine {
		ctx, stop := context.WithCancel(context.Background())
		e := &engineV1{deps: d, c: c, linkID: "acp1:" + c.ID(), lim: currentLimits(), ctx: ctx, stop: stop,
			updates: make(chan update, updateQueueCap), sessions: newSessionSet(),
			preferredModel: c.Meta().Model, preferredEffort: c.Meta().Effort, permissions: map[string]*pendingPermission{},
			urlQuestions: map[string]context.CancelCauseFunc{}}
		e.link = &link{e: e}
		e.ws = d.Workspace(c.WorkspaceID(), c.UserID())
		e.peer = newPeer(c.Send, e)
		e.q = newTurnQueue(c.Meta().MaxConcurrency, e.runTurn, e.reportConcurrency, e.askForWork)
		c.OnFrame(e.peer.receive)
		c.OnControl(e.onControl)
		return e
	}
}

func (e *engineV1) run(_ context.Context, c *connection) error {
	go e.applyUpdates()
	go func() {
		<-c.Done()
		e.ending(c.Err())
	}()
	defer e.teardown()
	if err := e.handshake(); err != nil {
		return err
	}
	if err := e.openIdleSession(); err != nil {
		return err
	}
	select {
	case <-c.Done():
		return c.Err()
	default:
	}
	e.attach()
	<-c.Done()
	return c.Err()
}

func (e *engineV1) handshake() error {
	ctx, cancel := context.WithTimeout(e.ctx, e.lim.initialize)
	defer cancel()
	var raw json.RawMessage
	err := e.peer.call(ctx, methodInitialize, initializeRequest{
		ProtocolVersion:    ProtocolVersion,
		ClientCapabilities: clientCaps,
		ClientInfo:         implementation{Name: "agentrq", Title: "AgentRQ", Version: e.deps.AppVersion},
	}, &raw)
	if errors.Is(err, context.DeadlineExceeded) {
		e.c.Control(control{Kind: "restart", Reason: "the agent did not answer initialize within a minute"})
		e.c.endWith(closeRestart, "restart", errInitTimeout)
		return errInitTimeout
	}
	if err != nil {
		return err
	}
	var r initializeResponse
	if err := r.decode(raw); err != nil {
		e.refuse("protocol_error", "the agent's initialize answer could not be read: "+err.Error(), websocket.ClosePolicyViolation)
		return err
	}
	if r.ProtocolVersion != ProtocolVersion {
		msg := fmt.Sprintf("this agent speaks ACP v%d; start it with --acp-version %d", r.ProtocolVersion, r.ProtocolVersion)
		e.refuse("version_mismatch", msg, closeVersion)
		return errVersion
	}
	e.caps, e.authMethods = r.AgentCapabilities, r.AuthMethods
	e.agent = agentlink.IdentityReport{Name: e.c.Meta().Agent}
	if r.AgentInfo != nil {
		e.agent = agentlink.IdentityReport{Name: r.AgentInfo.Name, Title: r.AgentInfo.Title, Version: r.AgentInfo.Version}
		if e.agent.Title == e.agent.Name {
			e.agent.Title = ""
		}
	}
	return nil
}

// refuse tells the bridge why with an error frame, then closes with code. The
// close frame carries only the error code: a close reason is capped at 123
// bytes, and the message is often longer.
func (e *engineV1) refuse(errCode, message string, code int) {
	e.c.Control(control{Kind: "error", Error: errCode, Message: message})
	e.c.endWith(code, errCode, errors.New(message))
}

// openIdleSession opens the session the first task adopts, which is also how
// AgentRQ learns the agent's models and commands before any task runs. An
// agent that is slow to open one is waited for at most idleSessionWait.
func (e *engineV1) openIdleSession() error {
	pending := make(chan struct{})
	e.mu.Lock()
	e.idlePending = pending
	e.mu.Unlock()
	result := make(chan error, 1)
	go func() {
		s, err := e.newSession(e.ctx, 0)
		e.mu.Lock()
		if err == nil {
			e.idle = s
		}
		e.idlePending = nil
		e.mu.Unlock()
		close(pending)
		result <- err
	}()
	select {
	case err := <-result:
		if errors.Is(err, errAuthUnavailable) {
			e.refuse("auth_required", e.loginInstructions(), closeAuthRequired)
			return err
		}
		if err != nil {
			zlog.Warn().Str("link", e.linkID).Err(err).Msg("acp v1: could not open a session to learn what the agent offers; the first task will open one")
		}
		return nil
	case <-time.After(e.lim.idleSession):
		zlog.Warn().Str("link", e.linkID).Msg("acp v1: the agent has not opened a session in time; carrying on without knowing what it offers")
		return nil
	}
}

func (e *engineV1) loginInstructions() string {
	agent := e.c.Meta().Agent
	if agent == "" {
		agent = "<agent>"
	}
	return "the agent needs a terminal login: run npx -y @agentrq/acp-gateway@latest --login --agent " + agent +
		" on this machine, then start it again"
}

// attach makes the agent visible: the link first, then everything it reported
// while it was coming up (a report before the link exists would be pruned as
// belonging to no live session), then work for every free slot.
func (e *engineV1) attach() {
	e.ws.AttachAgentLink(e.link)
	e.mu.Lock()
	e.attached = true
	idle, commands := e.idle, e.commands
	e.mu.Unlock()
	if e.agent.Name != "" {
		e.ws.HandleAgentIdentity(e.ctx, e.linkID, e.agent)
	}
	if idle != nil {
		e.reportModels(idle)
		e.reportEffort(idle)
	}
	if commands == nil && supported(e.caps.Auth.Logout) {
		p := agentlink.CommandsReport{Commands: []agentlink.Command{logoutCommand}}
		if idle != nil {
			p.SessionID = idle.id
		}
		commands = &p
	}
	if commands != nil {
		e.ws.HandleAgentCommands(e.ctx, e.linkID, *commands)
	}
	e.reportConcurrency()
	if e.deps.OnConnect != nil {
		e.deps.OnConnect(e.ctx, e.c.WorkspaceID(), e.c.UserID())
	}
	limit, active, queued := e.q.numbers()
	go func() {
		for range limit - active - queued {
			if e.ws.OfferNextTask(e.ctx) == 0 {
				return
			}
		}
	}()
}

func (e *engineV1) isAttached() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.attached
}

func (e *engineV1) reportConcurrency() {
	if !e.isAttached() {
		return
	}
	limit, active, queued := e.q.numbers()
	lo, hi, canSet := 1, maxConcurrency, true
	e.ws.HandleAgentConcurrency(e.ctx, e.linkID, agentlink.ConcurrencyReport{
		MaxConcurrency: &limit, Active: active, Queued: queued, Min: &lo, Max: &hi, CanSet: &canSet})
}

// askForWork is the queue saying a slot is free with nothing waiting. It never
// runs OfferNextTask on the caller's goroutine: that may be a link method the
// poller called while holding the lock OfferNextTask takes.
func (e *engineV1) askForWork() {
	go e.ws.OfferNextTask(e.ctx)
}

func (e *engineV1) onControl(ctl control) {
	switch ctl.Kind {
	case "stderr":
		e.mu.Lock()
		e.stderr = append(e.stderr, ctl.Lines...)
		if n := len(e.stderr) - maxStderrLines; n > 0 {
			e.stderr = append([]string(nil), e.stderr[n:]...)
		}
		e.mu.Unlock()
	case "exit":
		e.mu.Lock()
		lines := e.stderr
		e.mu.Unlock()
		ev := zlog.Warn().Str("link", e.linkID).Strs("stderr", lines)
		if ctl.Code != nil {
			ev = ev.Int("code", *ctl.Code)
		}
		if ctl.Signal != nil {
			ev = ev.Str("signal", *ctl.Signal)
		}
		ev.Msg("acp v1: the agent process exited")
		e.ending(errProcessExited)
		e.c.End(errProcessExited)
	default:
		zlog.Debug().Str("link", e.linkID).Str("kind", ctl.Kind).Msg("acp v1: ignoring a control frame")
	}
}

// ending records why the connection is ending, the first reason given, and
// fails every call still waiting on the agent. The reason is set first, so a
// turn cut off by it posts nothing and teardown posts its note instead.
func (e *engineV1) ending(cause error) {
	if cause == nil {
		cause = errPeerClosed
	}
	e.mu.Lock()
	if e.cause == nil {
		e.cause = cause
	}
	e.mu.Unlock()
	e.peer.close(errPeerClosed)
}

// teardown runs once the connection is over: one note for every turn it cut
// off, every waiting permission answered and its card closed, and the link
// gone.
func (e *engineV1) teardown() {
	e.ending(e.c.Err())
	e.mu.Lock()
	cause, attached := e.cause, e.attached
	var cut []int64
	for _, s := range e.sessions.byID {
		if s.busy {
			cut = append(cut, s.taskID)
		}
	}
	e.mu.Unlock()
	note := noteConnectionLost
	switch {
	case errors.Is(cause, errProcessExited):
		note = noteProcessExited
	case errors.Is(cause, errBridgeBye):
		note = noteCancelled
	}
	for _, taskID := range cut {
		e.note(taskID, note)
	}
	for _, requestID := range e.settlePermissions("") {
		e.ws.ClosePermissionRequest(context.Background(), requestID)
	}
	if attached {
		e.ws.DetachAgentLink(context.Background(), e.linkID)
	}
	e.stop()
}

// request answers the agent's requests: permissions and questions. Every
// other client method is refused, the file system and the terminal among
// them: AgentRQ declared neither, and cannot reach the agent's machine.
func (e *engineV1) request(ctx context.Context, method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case methodRequestPermission:
		return e.requestPermission(ctx, params)
	case methodElicitationCreate:
		return e.createElicitation(ctx, params)
	}
	return nil, &rpcError{Code: codeMethodNotFound, Message: method + " is not supported by AgentRQ"}
}

// notification queues a session update, in arrival order, for applyUpdates.
// A full queue holds the reader back rather than dropping the update: the
// frame has been counted, so a dropped one could never be replayed.
func (e *engineV1) notification(method string, params json.RawMessage) {
	if method == methodElicitationComplete {
		e.completeElicitation(params)
		return
	}
	if method != methodSessionUpdate {
		zlog.Debug().Str("link", e.linkID).Str("method", method).Msg("acp v1: ignoring a notification")
		return
	}
	var n sessionNotification
	if json.Unmarshal(params, &n) != nil {
		zlog.Debug().Str("link", e.linkID).Msg("acp v1: ignoring a session update that is not an object")
		return
	}
	e.enqueue(update{n: n})
}

// enqueue puts an item on the update queue, unless the engine has stopped.
func (e *engineV1) enqueue(u update) bool {
	select {
	case e.updates <- u:
		return true
	case <-e.ctx.Done():
		return false
	}
}

// applyUpdates is the only place session updates are applied, one at a time,
// in the order they arrived.
func (e *engineV1) applyUpdates() {
	for {
		select {
		case <-e.ctx.Done():
			return
		case u := <-e.updates:
			switch {
			case u.register != nil:
				u.registered <- e.register(u.register)
			case u.barrier != nil:
				close(u.barrier)
			default:
				e.applyUpdate(u.n)
			}
		}
	}
}

// register adds a session on the update loop, so that no update can be
// applied between the agent's answer and the session being known: an agent
// commonly reports its commands and options the moment it has answered. What
// came for it before then is applied now, in the order it came.
func (e *engineV1) register(s *session) []*session {
	// No session opens while the idle one waits unadopted (sessionFor adopts
	// it first), so eviction never takes the idle session.
	e.mu.Lock()
	e.sessions.add(s)
	evicted := e.sessions.evict(e.lim.sessions)
	e.mu.Unlock()
	var later []sessionNotification
	for _, n := range e.early {
		if n.SessionID == s.id {
			e.applyUpdate(n)
		} else {
			later = append(later, n)
		}
	}
	e.early = later
	return evicted
}

// holdEarly keeps an update for a session the agent may be about to answer
// for. The oldest goes past maxEarlyUpdates: an agent naming sessions that
// never open cannot grow it.
func (e *engineV1) holdEarly(n sessionNotification) {
	e.early = append(e.early, n)
	if len(e.early) > e.lim.early {
		e.early = append([]sessionNotification(nil), e.early[1:]...)
	}
}

// barrier waits until the update queue has applied everything enqueued before
// this call. The reader enqueues notifications in arrival order and resolves
// responses as they come, so a barrier queued after the prompt's answer comes
// after every update the agent sent before it.
func (e *engineV1) barrier() {
	done := make(chan struct{})
	if e.enqueue(update{barrier: done}) {
		select {
		case <-done:
		case <-e.ctx.Done():
		}
	}
}

// callWithin is a call with the ordinary deadline: everything but a prompt
// and a login, which wait for the agent or a human as long as they take.
func (e *engineV1) callWithin(ctx context.Context, method string, params, result any) error {
	ctx, cancel := context.WithTimeout(ctx, e.lim.call)
	defer cancel()
	return e.peer.call(ctx, method, params, result)
}

// errorText is what a note quotes of an error: the agent's own message when
// the agent answered with one.
func errorText(err error) string {
	var rerr *rpcError
	if errors.As(err, &rerr) {
		return rerr.Message
	}
	return err.Error()
}

func (e *engineV1) note(taskID int64, text string) {
	if _, err := e.ws.PostAgentReply(e.ctx, e.linkID, taskID, text); err != nil {
		zlog.Error().Err(err).Int64("task_id", taskID).Msg("acp v1: could not post a turn note")
	}
}

// b62 is a task id as the workspace's payloads spell it, "" for none.
func b62(id int64) string {
	if id == 0 {
		return ""
	}
	return monoflake.ID(id).String()
}
