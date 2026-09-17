// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

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
 * will do afterwards. The honest interface offers the stop instead of the send,
 * and says so.
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
 * It is only sent when the agent reported any usage at all, so an agent that
 * reports none would leave the composer locked for good. For that case, and
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
    if (isUsageFooter(message)) return false
    if (!publishesFooters && isAPlainReply(message)) return false
    if (isFromAPerson(message)) return true
  }

  // Nothing decisive either way: a task that has been started and has not said
  // anything yet. The agent has the task and is working on it.
  return true
}

/**
 * What the composer says while it is not accepting anything.
 *
 * Names the reason rather than going quietly grey, because an input that stops
 * working without saying why reads as a broken page.
 */
export function workingPlaceholder() {
  return 'The agent is working — stop it to send a message'
}

/**
 * What the button does right now.
 *
 * Returned as a word rather than a boolean so the template reads as what it
 * is, and so a third state later is an addition rather than a rewrite.
 *
 * @returns {'stop' | 'send'}
 */
export function composerAction(working) {
  return working ? 'stop' : 'send'
}
