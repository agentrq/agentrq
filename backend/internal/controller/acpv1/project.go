// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
	zlog "github.com/rs/zerolog/log"
)

// truncatedMarker ends a reply or thought cut at its cap.
const truncatedMarker = "\n\n[truncated]"

// toolCall is what is remembered of a call the agent has not finished, for a
// permission request that names it only by id, and for its tool_calls row.
type toolCall struct {
	id, title, name string
	rawInput        json.RawMessage
	row             int64 // the tool_calls row recorded for it, 0 for none yet
	gated           bool  // a permission request asked about it, and owns its row
}

// label names a call the way its permission card would.
func (tc toolCall) label() string {
	for _, l := range []string{tc.title, tc.name} {
		if l != "" {
			return l
		}
	}
	return "Unknown Tool"
}

// projector turns one session's updates into what the human reads: the
// turn's answer, its reasoning, the latest usage, and the calls in flight.
// Its own lock, because the update loop writes while a turn reads.
type projector struct {
	// rows is held while a call's row is decided and written, so a permission
	// request and a status update never both record the same call.
	rows       sync.Mutex
	mu         sync.Mutex
	replyCap   int
	thoughtCap int
	reply      capped
	thought    capped
	usage      *usageUpdate
	toolReply  *string // the text the agent last sent through the reply tool, this turn
	callCap    int
	calls      map[string]*list.Element // of *toolCall, by id
	callOrder  *list.List               // oldest first, so the oldest is dropped past the cap
}

func newProjector(replyCap, thoughtCap, callCap int) *projector {
	return &projector{replyCap: replyCap, thoughtCap: thoughtCap, callCap: callCap,
		calls: map[string]*list.Element{}, callOrder: list.New()}
}

// capped is a buffer that keeps at most a cap of bytes, then the marker once.
type capped struct {
	b   strings.Builder
	cut bool
}

func (c *capped) add(text string, limit int) {
	if c.cut {
		return
	}
	if room := limit - c.b.Len(); len(text) > room {
		for room > 0 && !utf8.RuneStart(text[room]) {
			room--
		}
		c.b.WriteString(text[:room])
		c.b.WriteString(truncatedMarker)
		c.cut = true
		return
	}
	c.b.WriteString(text)
}

func (c *capped) take() string {
	s := c.b.String()
	c.b.Reset()
	c.cut = false
	return s
}

func (p *projector) appendReply(text string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reply.add(text, p.replyCap)
}

func (p *projector) appendThought(text string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.thought.add(text, p.thoughtCap)
}

// takeReply returns the turn's answer, and whether it is exactly what the
// agent already sent through the reply tool; the next turn starts afresh.
func (p *projector) takeReply() (reply string, duplicate bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	reply = p.reply.take()
	duplicate = p.toolReply != nil && *p.toolReply == reply
	p.toolReply = nil
	return reply, duplicate
}

func (p *projector) takeThought() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.thought.take()
}

func (p *projector) setUsage(u usageUpdate) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.usage = &u
}

// usageForTurnEnd is the latest usage the session ever reported: re-reported
// at every turn's end, so the footer moves past the human's message even when
// a turn sent none.
func (p *projector) usageForTurnEnd() (usageUpdate, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.usage == nil {
		return usageUpdate{}, false
	}
	return *p.usage, true
}

func (p *projector) recordToolReply(text string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.toolReply = &text
}

// rememberCall merges an update into what is known of a call and returns the
// merged call. A finished call is forgotten at once; past the cap the oldest
// unfinished one goes.
func (p *projector) rememberCall(u toolCallUpdate) toolCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	var tc toolCall
	el := p.calls[u.ToolCallID]
	if el != nil {
		tc = *el.Value.(*toolCall)
	}
	tc.id = u.ToolCallID
	if u.Title != nil {
		tc.title = *u.Title
	}
	if u.Name != nil {
		tc.name = *u.Name
	}
	if len(u.RawInput) > 0 && string(u.RawInput) != "null" {
		tc.rawInput = u.RawInput
	}
	finished := u.Status != nil && (*u.Status == "completed" || *u.Status == "failed")
	switch {
	case finished && el != nil:
		p.callOrder.Remove(el)
		delete(p.calls, tc.id)
	case finished:
	case el != nil:
		el.Value = &tc
	default:
		p.keep(&tc)
	}
	return tc
}

// keep remembers a call not yet known; past the cap the oldest one goes.
// p.mu is held.
func (p *projector) keep(tc *toolCall) {
	p.calls[tc.id] = p.callOrder.PushBack(tc)
	if p.callOrder.Len() > p.callCap {
		oldest := p.callOrder.Remove(p.callOrder.Front()).(*toolCall)
		delete(p.calls, oldest.id)
	}
}

// gate marks a call a permission request asks about, remembering it if no
// update named it yet, and returns the row already recorded for it.
func (p *projector) gate(id string) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	el := p.calls[id]
	if el == nil {
		p.keep(&toolCall{id: id, gated: true})
		return 0
	}
	tc := el.Value.(*toolCall)
	tc.gated = true
	return tc.row
}

// setRow remembers the row recorded for a call still in flight.
func (p *projector) setRow(id string, row int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if el := p.calls[id]; el != nil {
		el.Value.(*toolCall).row = row
	}
}

// toolCall is what is known of an unfinished call, zero when nothing is.
func (p *projector) toolCall(id string) toolCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	if el := p.calls[id]; el != nil {
		return *el.Value.(*toolCall)
	}
	return toolCall{}
}

// runTurn is one delivery: find the task's session, mark the task started,
// prompt, and post what the turn produced.
//
// A turn that comes up once the connection is ending is skipped without a
// note: it never started, the task is still notstarted, and the poller pushes
// it again to whichever agent is connected next.
func (e *engineV1) runTurn(d delivery) {
	defer e.q.finish(d.taskID)
	if e.q.cancelledSince(d.taskID, d.seq) || e.isEnding() {
		return
	}
	s, err := e.sessionFor(e.ctx, d.taskID)
	if err != nil {
		if e.isEnding() {
			return
		}
		if errors.Is(err, errAuthUnavailable) {
			err = errors.New(e.loginInstructions())
		}
		e.note(d.taskID, noteAnsweredWithError(errorText(err)))
		return
	}
	if e.q.cancelledSince(d.taskID, d.seq) {
		e.setIdle(s)
		return
	}
	if err := e.ws.MarkTaskOngoing(e.ctx, e.linkID, d.taskID); err != nil {
		zlog.Warn().Err(err).Int64("task_id", d.taskID).Msg("acp v1: could not mark the task ongoing; prompting anyway")
	}
	var resp promptResponse
	turnCtx, stopTurn := context.WithCancel(e.ctx)
	defer stopTurn()
	e.mu.Lock()
	s.stopTurn = stopTurn
	e.mu.Unlock()
	err = e.peer.call(turnCtx, methodSessionPrompt, promptRequest{
		SessionID: s.id,
		Prompt:    []contentBlock{{Type: "text", Text: d.content}},
	}, &resp)
	e.mu.Lock()
	s.stopTurn = nil
	e.mu.Unlock()
	if err != nil && turnCtx.Err() != nil && e.ctx.Err() == nil {
		// Cancelled, and the agent never answered within cancelSettleWait:
		// the turn ends as cancelled rather than holding its slot for ever.
		resp.StopReason, err = "cancelled", nil
	}
	e.barrier() // every update that came before the answer is applied now
	e.endTurn(s, d.taskID, resp.StopReason, err)
	if e.deps.OnTurn != nil {
		e.deps.OnTurn(e.ctx, e.c.WorkspaceID(), e.c.UserID())
	}
}

func (e *engineV1) isEnding() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cause != nil
}

// settleAfterCancel gives a cancelled turn cancelSettleWait for the agent to
// answer its prompt, then stops waiting for it.
func (e *engineV1) settleAfterCancel(s *session) {
	e.mu.Lock()
	stop := s.stopTurn
	e.mu.Unlock()
	if stop != nil {
		time.AfterFunc(e.lim.cancelSettle, stop)
	}
}

func (e *engineV1) setIdle(s *session) {
	e.mu.Lock()
	s.busy = false
	e.mu.Unlock()
}

// endTurn posts what the turn produced, in the order the human reads it:
// reasoning, the answer, the usage footer, then why the turn ended early. A
// turn the connection's end cut off posts nothing: teardown notes it.
func (e *engineV1) endTurn(s *session, taskID int64, stopReason string, err error) {
	e.mu.Lock()
	ending := e.cause != nil
	if !ending {
		s.busy = false // still busy when cut off, so teardown sees the turn
	}
	e.mu.Unlock()
	if ending {
		return
	}
	e.flushThought(s)
	if reply, duplicate := s.proj.takeReply(); reply != "" && !duplicate {
		if _, perr := e.ws.PostAgentReply(e.ctx, e.linkID, taskID, reply); perr != nil {
			zlog.Error().Err(perr).Int64("task_id", taskID).Msg("acp v1: could not post the agent's reply")
		}
	}
	if u, ok := s.proj.usageForTurnEnd(); ok {
		e.ws.HandleAgentTelemetry(e.ctx, e.linkID, agentlink.TelemetryReport{
			TaskID: b62(taskID), SessionID: s.id, Kind: "usage", Text: formatUsage(u), Data: usageData(u)})
	}
	if err != nil {
		e.note(taskID, noteAnsweredWithError(errorText(err)))
	} else if note := stopNote(stopReason); note != "" {
		e.note(taskID, note)
	}
}

// taskOf is the session's task, 0 while it is idle.
func (e *engineV1) taskOf(s *session) int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return s.taskID
}

// flushThought posts the reasoning so far as one block, when the session has
// a task to post it to; the gateway drops an idle session's reasoning too.
func (e *engineV1) flushThought(s *session) {
	thought := s.proj.takeThought()
	taskID := e.taskOf(s)
	if strings.TrimSpace(thought) == "" || taskID == 0 {
		return
	}
	e.ws.HandleAgentTelemetry(e.ctx, e.linkID, agentlink.TelemetryReport{
		TaskID: b62(taskID), SessionID: s.id, Kind: "thought", Text: thought, Data: map[string]any{}})
}

// sendPlan closes the reasoning block and posts a plan, for a session with a
// task.
func (e *engineV1) sendPlan(s *session, text string, data map[string]any) {
	e.flushThought(s)
	if taskID := e.taskOf(s); taskID != 0 {
		e.ws.HandleAgentTelemetry(e.ctx, e.linkID, agentlink.TelemetryReport{
			TaskID: b62(taskID), SessionID: s.id, Kind: "plan", Text: text, Data: data})
	}
}

// applyUpdate is one session/update, as spec §5.8's v1 column says.
func (e *engineV1) applyUpdate(n sessionNotification) {
	s := e.sessionByID(n.SessionID)
	if s == nil {
		e.holdEarly(n)
		return
	}
	switch kindOf(n.Update) {
	case "agent_message_chunk":
		var c contentChunk
		if lenient(n.Update, &c) && c.Content.Type == "text" {
			e.flushThought(s) // the agent has stopped thinking and started answering
			s.proj.appendReply(c.Content.Text)
		}
	case "agent_thought_chunk":
		var c contentChunk
		if lenient(n.Update, &c) && c.Content.Type == "text" {
			s.proj.appendThought(c.Content.Text)
		}
	case "tool_call":
		e.flushThought(s) // reasoning that explains a call belongs in front of it
		e.toolCallUpdate(s, n.Update)
	case "tool_call_update":
		e.toolCallUpdate(s, n.Update)
	case "plan":
		var p legacyPlan
		lenient(n.Update, &p)
		plan := planContent{Type: "items", PlanID: "default", Entries: p.Entries}
		e.sendPlan(s, formatPlan(plan), planData(plan))
	case "plan_update":
		var p planUpdate
		lenient(n.Update, &p)
		e.sendPlan(s, formatPlan(p.Plan), planData(p.Plan))
	case "plan_removed":
		var p planRemoved
		lenient(n.Update, &p)
		e.sendPlan(s, "Plan withdrawn.", map[string]any{"planId": p.PlanID, "removed": true})
	case "available_commands_update":
		var u commandsUpdate
		lenient(n.Update, &u)
		e.setCommands(s, normalizeCommands(u.AvailableCommands))
	case "current_mode_update":
		var u currentModeUpdate
		if lenient(n.Update, &u) {
			e.reenforceMode(s, u.CurrentModeID)
		}
	case "config_option_update":
		var u configOptionUpdate
		lenient(n.Update, &u)
		if id, current, models, ok := modelsFrom(u.ConfigOptions); ok {
			e.mu.Lock()
			s.configID, s.current, s.models = id, current, models
			e.mu.Unlock()
			e.reportModels(s)
		}
		e.setEffortOptions(s, u.ConfigOptions)
	case "usage_update":
		var u usageUpdate
		if lenient(n.Update, &u) {
			s.proj.setUsage(u)
		}
	}
}

// toolCallUpdate remembers a call and keeps its tool_calls row, and records a
// completed reply the agent sent to the session's own task through the
// workspace's reply tool, so the same text is not posted a second time when
// the turn ends.
func (e *engineV1) toolCallUpdate(s *session, raw json.RawMessage) {
	var u toolCallUpdate
	if !lenient(raw, &u) || u.ToolCallID == "" {
		return
	}
	s.proj.rows.Lock()
	tc := s.proj.rememberCall(u)
	tool, ours := workspaceTool(tc.title, e.ws.ToolNames(), tc.name)
	if !ours {
		e.recordCall(s, tc, rowStatus(u.Status))
	}
	s.proj.rows.Unlock()
	if !ours || u.Status == nil || *u.Status != "completed" {
		return
	}
	if taskID, text, ok := agentReply(tool, tc.rawInput); ok && taskID == b62(e.taskOf(s)) {
		s.proj.recordToolReply(text)
	}
}

// rowStatus is what a call's row shows for its ACP status: "" until it runs.
// A pending call may yet be put to the human, whose request records it.
func rowStatus(status *string) string {
	switch deref(status) {
	case "in_progress":
		return "running"
	case "completed", "failed":
		return *status
	}
	return ""
}

// recordCall writes a call the agent runs without asking as its task's
// tool_calls row once it starts, and moves the row on as it finishes. A call
// a permission request asked about is left to that request's row, whose
// verdict says more than how it ended. s.proj.rows is held.
func (e *engineV1) recordCall(s *session, tc toolCall, status string) {
	taskID := e.taskOf(s)
	switch {
	case tc.gated || status == "" || taskID == 0:
	case tc.row == 0:
		row := e.ws.RecordToolCall(e.ctx, taskID, agentlink.ToolCall{
			ToolName: tc.label(), Description: tc.label(), InputPreview: inputPreview(tc.rawInput), Status: status})
		s.proj.setRow(tc.id, row)
	case status != "running":
		e.ws.SetToolCallStatus(e.ctx, tc.row, status)
	}
}

// normalizeCommands is the gateway's: names trimmed, nameless commands
// dropped, a repeated name's last entry kept in the first one's place.
func normalizeCommands(in []availableCommand) []agentlink.Command {
	out := []agentlink.Command{}
	at := map[string]int{}
	for _, c := range in {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}
		cmd := agentlink.Command{Name: name, Description: strings.TrimSpace(c.Description)}
		if c.Input != nil {
			cmd.Hint = strings.TrimSpace(c.Input.Hint)
		}
		if i, seen := at[name]; seen {
			out[i] = cmd
			continue
		}
		at[name] = len(out)
		out = append(out, cmd)
	}
	return out
}

// setCommands keeps the agent's slash commands for attach to report, and
// reports them now when the link is up. An empty list withdraws the menu.
func (e *engineV1) setCommands(s *session, commands []agentlink.Command) {
	e.mu.Lock()
	p := agentlink.CommandsReport{TaskID: b62(s.taskID), SessionID: s.id, Commands: commands}
	e.commands = &p
	attached := e.attached
	e.mu.Unlock()
	if attached {
		e.ws.HandleAgentCommands(e.ctx, e.linkID, p)
	}
}
