// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"container/list"
	"context"
	"errors"
	"strings"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
	zlog "github.com/rs/zerolog/log"
)

// session is one ACP session: a task's conversation with the agent, or the
// idle one opened at connect that the first task adopts. Every field but id
// and proj is guarded by the engine's mu.
type session struct {
	id          string
	taskID      int64 // 0 while idle
	busy        bool  // a turn is running on it
	elem        *list.Element
	modes       *sessionModeState
	reenforced  int // times the agent was put back into a mode that asks
	configID    string
	current     string
	models      []agentlink.Model
	effortID    string
	effort      string
	efforts     []agentlink.Effort
	effortShown bool // the workspace was last told the session offers levels
	proj        *projector
	stopTurn    context.CancelFunc // gives up on the running turn's prompt; nil between turns
}

// sessionSet finds a session by its id or its task in O(1), and keeps them in
// least-recently-used order (container/list, front = most recent) so that
// eviction is O(1) too.
type sessionSet struct {
	byID   map[string]*session
	byTask map[int64]*session
	lru    *list.List // of *session
}

func newSessionSet() sessionSet {
	return sessionSet{byID: map[string]*session{}, byTask: map[int64]*session{}, lru: list.New()}
}

func (ss *sessionSet) add(s *session) {
	ss.byID[s.id] = s
	if s.taskID != 0 {
		ss.byTask[s.taskID] = s
	}
	s.elem = ss.lru.PushFront(s)
}

func (ss *sessionSet) touch(s *session) { ss.lru.MoveToFront(s.elem) }

func (ss *sessionSet) remove(s *session) {
	delete(ss.byID, s.id)
	if ss.byTask[s.taskID] == s {
		delete(ss.byTask, s.taskID)
	}
	ss.lru.Remove(s.elem)
}

// evict removes least recently used sessions with no turn running until at
// most limit are left, and returns them. Running sessions are never evicted,
// so the set may stay over the limit while every session is busy.
func (ss *sessionSet) evict(limit int) []*session {
	var out []*session
	for el := ss.lru.Back(); el != nil && ss.lru.Len() > limit; {
		s := el.Value.(*session)
		el = el.Prev()
		if !s.busy {
			ss.remove(s)
			out = append(out, s)
		}
	}
	return out
}

// mcpServers is the workspace's own MCP server, handed to every session when
// the agent advertised http MCP: the spec forbids an http server otherwise.
func (e *engineV1) mcpServers() []mcpServerHTTP {
	if !e.caps.MCPCapabilities.HTTP {
		return nil
	}
	return []mcpServerHTTP{{Type: "http", Name: mcpServerName, URL: e.deps.MCPURL(e.c.WorkspaceID()),
		Headers: []httpHeader{{Name: "Authorization", Value: "Bearer " + e.c.Token()}}}}
}

func isAuthRequired(err error) bool {
	var rerr *rpcError
	return errors.As(err, &rerr) && rerr.Code == codeAuthRequired
}

// openSession asks the agent for a new session, logging it in and asking once
// more if it says it needs a login.
func (e *engineV1) openSession(ctx context.Context) (newSessionResponse, error) {
	req := newSessionRequest{MCPServers: e.mcpServers()}
	var resp newSessionResponse
	err := e.callWithin(ctx, methodSessionNew, req, &resp)
	if isAuthRequired(err) {
		if err = e.login(); err != nil {
			return resp, err
		}
		err = e.callWithin(ctx, methodSessionNew, req, &resp)
		if isAuthRequired(err) {
			return resp, errAuthUnavailable
		}
	}
	if err == nil && resp.SessionID == "" {
		err = errNoSessionID
	}
	return resp, err
}

// login runs the agent's own login: the bridge's preferred method, which must
// exist and not be a terminal one, else the first the agent performs itself.
// It has no deadline: it may be waiting for a human to sign in.
func (e *engineV1) login() error {
	method, ok := pickAuthMethod(e.authMethods, e.c.Meta().AuthMethod)
	if !ok {
		return errAuthUnavailable
	}
	return e.peer.call(e.ctx, methodAuthenticate, authenticateRequest{MethodID: method}, nil)
}

func pickAuthMethod(methods []authMethod, preferred string) (string, bool) {
	for _, m := range methods {
		if preferred != "" && m.ID == preferred {
			return m.ID, m.Type != "terminal"
		}
		if preferred == "" && m.Type != "terminal" {
			return m.ID, true
		}
	}
	return "", false
}

// newSession opens a session for a task (0 for the idle one), registers it,
// puts it on the preferred model and into a mode that asks the human.
func (e *engineV1) newSession(ctx context.Context, taskID int64) (*session, error) {
	resp, err := e.openSession(ctx)
	if err != nil {
		return nil, err
	}
	// A task's session is busy from the moment it is registered: marked any
	// later, another session registering past the limit could evict it
	// before its first turn runs.
	s := &session{id: resp.SessionID, taskID: taskID, busy: taskID != 0, modes: resp.Modes,
		proj: newProjector(e.lim.reply, e.lim.thought, e.lim.tool)}
	s.configID, s.current, s.models, _ = modelsFrom(resp.ConfigOptions)
	s.effortID, s.effort, s.efforts, _ = effortFrom(resp.ConfigOptions)
	registered := make(chan []*session, 1)
	if !e.enqueue(update{register: s, registered: registered}) {
		return nil, errPeerClosed
	}
	var evicted []*session
	select {
	case evicted = <-registered:
	case <-e.ctx.Done():
		return nil, errPeerClosed
	}
	e.mu.Lock()
	preferred, models, current := e.preferredModel, s.models, s.current
	e.mu.Unlock()
	for _, old := range evicted {
		e.closeSession(old)
	}
	if id, ok := resolveModel(models, preferred); ok && id != current {
		e.applyModel(ctx, s, preferred)
	} else {
		e.reportModels(s)
	}
	e.mu.Lock()
	preferredEffort, efforts, effort := e.preferredEffort, s.efforts, s.effort
	e.mu.Unlock()
	if preferredEffort != "" && preferredEffort != effort && offersEffort(efforts, preferredEffort) {
		e.applyEffort(ctx, s, preferredEffort)
	} else {
		e.reportEffort(s)
	}
	if s.modes != nil {
		e.mu.Lock()
		current := s.modes.CurrentModeID
		e.mu.Unlock()
		e.enforceHumanMode(ctx, s, current)
	}
	return s, nil
}

// closeSession tells the agent an evicted session is over, when it can be
// told: an agent without session/close just keeps it.
func (e *engineV1) closeSession(s *session) {
	if !supported(e.caps.SessionCapabilities.Close) {
		return
	}
	go func() {
		if err := e.callWithin(e.ctx, methodSessionClose, sessionIDParams{SessionID: s.id}, nil); err != nil {
			zlog.Debug().Str("link", e.linkID).Err(err).Msg("acp v1: could not close an evicted session")
		}
	}()
}

// logoutCommand is AgentRQ's own slash command: typed as a task's message, it
// logs the agent out instead of prompting it. It is listed among the agent's
// commands so the reply reaches the engine as the bare command.
var logoutCommand = agentlink.Command{Name: "logout", Description: "Log the agent out; its next session asks for a login again"}

// isLogout reports whether a delivery is the /logout command, with nothing
// after it on its line.
func isLogout(content string) bool {
	line, _, _ := strings.Cut(strings.TrimSpace(content), "\n")
	return strings.TrimSpace(line) == "/"+logoutCommand.Name
}

// withLogout lists /logout among the agent's commands when the agent can log
// out, in place of any logout command of its own: the engine answers it.
func (e *engineV1) withLogout(commands []agentlink.Command) []agentlink.Command {
	if !supported(e.caps.Auth.Logout) {
		return commands
	}
	out := make([]agentlink.Command, 0, len(commands)+1)
	for _, c := range commands {
		if c.Name != logoutCommand.Name {
			out = append(out, c)
		}
	}
	return append(out, logoutCommand)
}

// logout runs a /logout delivery: the agent is asked to end its login, and
// the note it ends with is what tells the composer this turn is over.
func (e *engineV1) logout(taskID int64) {
	err := e.callWithin(e.ctx, methodLogout, logoutRequest{}, nil)
	if e.isEnding() {
		return
	}
	if err != nil {
		e.note(taskID, noteAnsweredWithError(errorText(err)))
		return
	}
	if e.deps.OnLogout != nil {
		e.deps.OnLogout(e.ctx, e.c.WorkspaceID(), e.c.UserID())
	}
	e.note(taskID, noteLoggedOut)
}

// sessionFor finds or opens the session a task's turn runs on, and marks it
// busy so it cannot be evicted: the task's own, else the idle session (waiting
// for it while it is still opening), else a new one.
func (e *engineV1) sessionFor(ctx context.Context, taskID int64) (*session, error) {
	e.mu.Lock()
	pending := e.idlePending
	e.mu.Unlock()
	if pending != nil {
		select {
		case <-pending:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	e.mu.Lock()
	s := e.sessions.byTask[taskID]
	adopted := s == nil && e.idle != nil
	if adopted {
		s, e.idle = e.idle, nil
		s.taskID = taskID
		e.sessions.byTask[taskID] = s
	}
	if s != nil {
		s.busy = true
		e.sessions.touch(s)
	}
	e.mu.Unlock()
	if adopted {
		e.reportModels(s) // now the task's
		e.reportEffort(s)
	}
	if s != nil {
		return s, nil
	}
	return e.newSession(ctx, taskID) // registered busy
}

// sessionByID is a session by its ACP id, nil when unknown.
func (e *engineV1) sessionByID(acpSessionID string) *session {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.sessions.byID[acpSessionID]
}

// busySession is the task's session while a turn runs on it, else nil.
func (e *engineV1) busySession(taskID int64) *session {
	e.mu.Lock()
	defer e.mu.Unlock()
	if s := e.sessions.byTask[taskID]; s != nil && s.busy {
		return s
	}
	return nil
}

func (e *engineV1) ownsTask(taskID int64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.sessions.byTask[taskID] != nil
}

// reportModels tells the workspace the session's models, once the link is
// attached and if the agent offers any.
func (e *engineV1) reportModels(s *session) {
	e.mu.Lock()
	p := agentlink.ModelsReport{TaskID: b62(s.taskID), SessionID: s.id, ConfigID: s.configID,
		CurrentModel: s.current, Models: s.models, CanSet: s.configID != ""}
	attached := e.attached
	e.mu.Unlock()
	if !attached || len(p.Models) == 0 {
		return
	}
	e.ws.HandleAgentModels(e.ctx, e.linkID, p)
}

// applyModel switches a session to the requested model, found by id or by
// display name among the latest models it reported. The picker is always
// answered: with the agent's new options on success, with what was last known
// otherwise.
func (e *engineV1) applyModel(ctx context.Context, s *session, requested string) {
	e.mu.Lock()
	configID, sessionID, models := s.configID, s.id, s.models
	e.mu.Unlock()
	id, ok := resolveModel(models, requested)
	if ok {
		var resp setConfigOptionResponse
		err := e.callWithin(ctx, methodSessionSetConfig, setConfigOptionRequest{SessionID: sessionID, ConfigID: configID, Value: id}, &resp)
		if err != nil {
			zlog.Warn().Str("link", e.linkID).Err(err).Str("model", id).Msg("acp v1: the agent did not switch models")
		} else {
			e.mu.Lock()
			if cid, current, models, found := modelsFrom(resp.ConfigOptions); found {
				s.configID, s.current, s.models = cid, current, models
			} else {
				s.current, s.models = id, withCurrent(s.models, id)
			}
			e.mu.Unlock()
			if len(resp.ConfigOptions) > 0 {
				e.setEffortOptions(s, resp.ConfigOptions)
			}
		}
	}
	e.reportModels(s)
}

// setEffortOptions takes the session's reasoning level from the agent's full
// list of config options, and tells the workspace. A list without one means
// the current model has no levels, and the picker goes.
func (e *engineV1) setEffortOptions(s *session, opts []configOption) {
	e.mu.Lock()
	s.effortID, s.effort, s.efforts, _ = effortFrom(opts)
	e.mu.Unlock()
	e.reportEffort(s)
}

// reportEffort tells the workspace the session's reasoning level, once the
// link is attached: while the agent offers levels, and once more with none
// when they go.
func (e *engineV1) reportEffort(s *session) {
	e.mu.Lock()
	p := agentlink.EffortReport{TaskID: b62(s.taskID), SessionID: s.id, ConfigID: s.effortID,
		CurrentEffort: s.effort, Efforts: s.efforts, CanSet: s.effortID != ""}
	send := e.attached && (len(s.efforts) > 0 || s.effortShown)
	if send {
		s.effortShown = len(s.efforts) > 0
	}
	e.mu.Unlock()
	if send {
		e.ws.HandleAgentEffort(e.ctx, e.linkID, p)
	}
}

// applyEffort switches a session to a reasoning level it offers. The picker is
// always answered, with what the agent now says or what was last known.
func (e *engineV1) applyEffort(ctx context.Context, s *session, requested string) {
	e.mu.Lock()
	configID, sessionID, offered := s.effortID, s.id, offersEffort(s.efforts, requested)
	e.mu.Unlock()
	if offered {
		var resp setConfigOptionResponse
		err := e.callWithin(ctx, methodSessionSetConfig, setConfigOptionRequest{SessionID: sessionID, ConfigID: configID, Value: requested}, &resp)
		switch {
		case err != nil:
			zlog.Warn().Str("link", e.linkID).Err(err).Str("effort", requested).Msg("acp v1: the agent did not switch reasoning level")
		case len(resp.ConfigOptions) > 0:
			e.setEffortOptions(s, resp.ConfigOptions)
			return
		default:
			e.mu.Lock()
			s.effort = requested
			e.mu.Unlock()
		}
	}
	e.reportEffort(s)
}

// setEffort is the human picking a reasoning level for a session: it becomes
// the default for new sessions that offer it too.
func (e *engineV1) setEffort(acpSessionID, effort string) bool {
	e.mu.Lock()
	e.preferredEffort = effort
	s := e.sessions.byID[acpSessionID]
	e.mu.Unlock()
	if s != nil {
		go e.applyEffort(e.ctx, s, effort)
	}
	return true
}

// setModel is the human picking a model for a session: it becomes the
// default for new sessions too.
func (e *engineV1) setModel(acpSessionID, modelID string) bool {
	e.mu.Lock()
	e.preferredModel = modelID
	s := e.sessions.byID[acpSessionID]
	e.mu.Unlock()
	if s != nil {
		go e.applyModel(e.ctx, s, modelID)
	}
	return true
}

// enforceHumanMode puts a session into the mode that asks the human before a
// tool runs, when the agent offers one and current, the mode the session was
// seen in, is not it. The modes the agent offers never change after the
// session opens, so they are read without the lock.
func (e *engineV1) enforceHumanMode(ctx context.Context, s *session, current string) {
	modes := s.modes
	if len(modes.AvailableModes) == 0 {
		return
	}
	id := humanMode(modes)
	if id == "" {
		ids := make([]string, 0, len(modes.AvailableModes))
		for _, m := range modes.AvailableModes {
			ids = append(ids, m.ID)
		}
		zlog.Warn().Str("link", e.linkID).Str("available", strings.Join(ids, ", ")).
			Msg("acp v1: the agent offers no mode that defers approvals to the human; tool calls may run without AgentRQ approval")
		return
	}
	if id == current {
		return
	}
	if err := e.callWithin(ctx, methodSessionSetMode, setModeRequest{SessionID: s.id, ModeID: id}, nil); err != nil {
		zlog.Warn().Str("link", e.linkID).Err(err).Str("mode", id).Msg("acp v1: could not switch the session to a mode that asks; tool calls may run without AgentRQ approval")
		return
	}
	e.mu.Lock()
	modes.CurrentModeID = id
	e.mu.Unlock()
}

// reenforceMode is the agent moving a session into another mode. One that
// approves on the human's behalf is undone, at most maxModeReenforcements
// times per session; a mode that asks resets the count.
func (e *engineV1) reenforceMode(s *session, modeID string) {
	e.mu.Lock()
	if s.modes == nil || len(s.modes.AvailableModes) == 0 {
		e.mu.Unlock()
		return
	}
	s.modes.CurrentModeID = modeID
	if modeAsks(s.modes, modeID) {
		s.reenforced = 0
		e.mu.Unlock()
		return
	}
	if s.reenforced >= maxModeReenforcements {
		e.mu.Unlock()
		zlog.Warn().Str("link", e.linkID).Str("mode", modeID).
			Msg("acp v1: the agent keeps leaving the mode that asks; giving up, tool calls may run without AgentRQ approval")
		return
	}
	s.reenforced++
	e.mu.Unlock()
	go e.enforceHumanMode(e.ctx, s, modeID)
}
