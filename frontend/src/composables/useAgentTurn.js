// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Whether the agent is in the middle of a turn, and what the composer should
 * do about it.
 *
 * The ACP gateway gives a task one session and a session one turn at a time, so
 * a second message delivered while a turn is running is chained behind it
 * rather than reaching the agent — `runTurnForTask` in the gateway is where
 * that happens, and its comment says exactly why: two prompts on one session
 * are refused or interleave. Someone who types "no, stop, do the other thing"
 * has therefore not stopped anything; they have added to the list of things it
 * will do afterwards.
 *
 * So the composer offers the stop as well as the send, and what the send does
 * changes: mid-turn it holds the message in the browser rather than posting
 * it. Nothing reaches the gateway until the turn ends, which is what makes a
 * queued message editable — see `useQueuedMessages`.
 *
 * ## Why this is derived rather than reported
 *
 * Nothing on the server knows it. What the server tracks is whether a
 * stop-capable agent is *attached*, which is a different question — and
 * `ongoing` is true for the whole life of a task, including every moment the
 * agent is waiting on a reply from the human. Swapping on either would replace
 * Send with Stop permanently and make the task unanswerable, which is worse
 * than the bug.
 *
 * ## What ends a turn
 *
 * Not a reply. The gateway's own instructions tell agents to report progress
 * with `reply` every few steps, so a reply arriving is usually the agent
 * talking *while* it works. Reading one as the end of a turn is what would
 * quietly hand the composer back mid-turn and queue the next message — the bug
 * this exists to fix.
 *
 * What does end one is the usage footer. The gateway flushes it from
 * `flushReply`, which runs when the prompt resolves, and its own comment calls
 * it "the last usage snapshot of the turn" — one per turn, at the end,
 * including a turn that ended because somebody pressed stop.
 *
 * It is only sent when the agent reported any usage at all — and a turn stopped
 * before the agent reported any has none to send. What that turn does get is
 * the gateway's note on why it ended early (`reportStopReason`, after
 * `flushReply`, so it is the turn's last word too), which is therefore an end
 * of a turn as well. Without it, a stop pressed early left the composer locked
 * and the queue unsent.
 *
 * An agent that reports no usage at all would leave the composer locked for
 * good. For that case, and
 * only that case, a plain reply is taken as the end of a turn: worse, but the
 * old behaviour rather than a dead end. A task that has seen a usage footer has
 * an agent that publishes them, so its footers are the only marker used.
 */

import { agentTelemetryKind } from './useAgentTelemetry'

/** Statuses in which a task can have an agent working on it at all. */
const ACTIVE_STATUSES = new Set(['ongoing'])

/**
 * Whether a message is somebody talking to the agent.
 *
 * Slack counts: it is a person sending a message into the task, and it chains
 * behind a turn exactly as the composer does.
 */
function isFromAPerson(message) {
  if (message?._pending) return false
  return message?.sender === 'human' || message?.sender === 'slack'
}

/** The footer the gateway sends when a turn finishes. */
function isUsageFooter(message) {
  return agentTelemetryKind(message) === 'usage'
}

/**
 * The gateway's notes on a turn that ended before the agent finished, in
 * `STOP_REASON_NOTES` and the fallback for a reason it does not know. Matched
 * on the text because that is all a reply carries.
 */
const STOP_REASON_NOTE = /^⚠️ (The turn was cancelled before the agent finished\.|The agent refused to continue this turn\.|The agent stopped (mid-turn: |for a reason this gateway does not recognise: ))/u

/** The note the gateway sends when a turn ends early, stopped or otherwise. */
function isStopReasonNote(message) {
  if (message?.sender !== 'agent' || message?.metadata?.type) return false
  return typeof message.text === 'string' && STOP_REASON_NOTE.test(message.text)
}

/**
 * Whether a message is the agent answering rather than working.
 *
 * Used only as the fallback marker. A permission request or an elicitation is
 * the agent asking for something mid-turn and is never one, nor is telemetry.
 */
function isAPlainReply(message) {
  if (message?.sender !== 'agent') return false
  if (agentTelemetryKind(message) !== null) return false
  return !message?.metadata?.type
}

/**
 * Is the agent working on a turn right now?
 *
 * @param {object} deps
 * @param {object} deps.task      the task being viewed
 * @param {object} deps.workspace the workspace, for whether a stop would work
 * @param {Array}  deps.messages  the task's messages, oldest first
 * @returns {boolean}
 */
export function agentIsWorking({ task, workspace, messages } = {}) {
  // Only where stopping means something. Claude Code speaking MCP directly does
  // not chain turns like this and cannot be stopped, so taking its Send away
  // would leave no way to say anything at all.
  if (!workspace?.agentSupportsStop) return false
  if (task?.assignee === 'human') return false
  if (!ACTIVE_STATUSES.has(task?.status)) return false

  const list = Array.isArray(messages) ? messages : []
  // Whether this agent publishes usage footers at all decides which marker is
  // trusted. One that does gets the accurate answer; one that does not gets the
  // old, loose one rather than a composer that never unlocks.
  const publishesFooters = list.some(isUsageFooter)

  for (let i = list.length - 1; i >= 0; i -= 1) {
    const message = list[i]
    if (isUsageFooter(message) || isStopReasonNote(message)) return false
    if (!publishesFooters && isAPlainReply(message)) return false
    if (isFromAPerson(message)) return true
  }

  // Nothing decisive either way: a task that has been started and has not said
  // anything yet. The agent has the task and is working on it.
  return true
}

/**
 * What the composer says while the agent is mid-turn.
 *
 * Says where the message will go, because a Send that does not send is worse
 * than a Send that is missing: the box still accepts text, and nothing else
 * on screen would explain why nothing happened.
 */
export function workingPlaceholder() {
  return 'The agent is working — your message will be queued until it finishes'
}

/**
 * Whether to tell the person the agent is busy, which is a wider question than
 * `agentIsWorking`: that one decides Send versus Stop and so only holds where
 * the agent can be stopped. This one is only a sign of life, so it also holds
 * for an agent speaking MCP directly — connected, handed the task, and not yet
 * answered. A task waiting on the human, or with nobody connected, is not busy.
 */
export function agentIsBusy({ task, workspace, messages } = {}) {
  if (!workspace?.agentConnected) return false
  if (workspace.agentSupportsStop) return agentIsWorking({ task, workspace, messages })
  if (task?.assignee === 'human') return false
  if (!ACTIVE_STATUSES.has(task?.status)) return false

  const list = Array.isArray(messages) ? messages : []
  for (let i = list.length - 1; i >= 0; i -= 1) {
    const message = list[i]
    // Anything the agent put into the thread but telemetry is it answering or
    // asking, so the next move is the human's.
    if (message?.sender === 'agent' && agentTelemetryKind(message) === null) return false
    if (isFromAPerson(message)) return true
  }
  return true
}

/** When the turn being worked on began: the last thing a person said, if any. */
export function turnStartedAt(messages) {
  const list = Array.isArray(messages) ? messages : []
  for (let i = list.length - 1; i >= 0; i -= 1) {
    if (isFromAPerson(list[i]) && list[i].createdAt) return new Date(list[i].createdAt).getTime()
  }
  return null
}

/** "7s", "1m 05s", "1h 02m": how long the agent has been at it. */
export function formatElapsed(ms) {
  const total = Math.max(0, Math.floor(ms / 1000))
  const s = total % 60
  const m = Math.floor(total / 60) % 60
  const h = Math.floor(total / 3600)
  if (h > 0) return `${h}h ${String(m).padStart(2, '0')}m`
  if (m > 0) return `${m}m ${String(s).padStart(2, '0')}s`
  return `${s}s`
}
