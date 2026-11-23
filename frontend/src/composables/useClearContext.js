// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * Offering to start a task on a clean context.
 *
 * When a task carries this, the backend types `/clear` into the agent's
 * terminal before it hands the task over, so the agent reads it without the
 * previous task still in its context.
 *
 * ## Why the option is hidden rather than disabled
 *
 * It depends on something the workspace either has or has not: a **running
 * Claude Code session**, on a machine whose daemon is connected. A workspace
 * with no machine has no terminal to type into, and the backend ignores the
 * flag — correctly, because a task must never be withheld from an agent over a
 * convenience. A control that is always shown and silently does nothing half
 * the time is worse than one that appears when it can work.
 *
 * ## Why the kind is checked here, unlike `useWorkspaceTerminal`
 *
 * `isWatchable` there deliberately does *not* check the kind: anything in a
 * pseudo-terminal is worth watching, because a process asks its own questions
 * on the way up whatever it is.
 *
 * This is the opposite case and the opposite answer. `/clear` is a Claude Code
 * command, and it is being *typed* rather than read — offered for a gateway
 * session it would put six stray characters into whatever that agent was
 * doing. Watching is safe for any kind; typing is not.
 */

import { computed, ref } from 'vue'
import * as api from '../api'

/** The only session kind that understands `/clear`. */
export const CLEAR_CONTEXT_KIND = 'claude-code'

/**
 * Whether `/clear` could actually reach this session.
 *
 * `starting` is excluded as well as the finished states, and that is the
 * interesting one: the backend writes to a *running* session's terminal, so a
 * session still coming up would take the flag and drop it. The daemon would
 * also have nothing to type into yet — the process that reads the prompt is
 * the thing still starting.
 *
 * Pure and exported so the rule is testable on its own, and so the condition
 * the icon is shown on is the same one the backend acts on rather than a
 * second opinion about it.
 */
export function canClearContext(session) {
  if (!session?.id) return false
  return session.kind === CLEAR_CONTEXT_KIND && session.status === 'running'
}

/** What the icon says it will do, in each of its two states. */
export function clearContextTooltip(on) {
  return on
    ? 'Clear context: the agent will be sent /clear before it picks this task up'
    : 'Start this task on a clean context — sends /clear to the agent first'
}

/**
 * Whether this workspace can be offered the option, and the asking.
 *
 * One request, on the endpoint the workspace page already uses to find its
 * terminal. Deliberately not assembled from the machines list plus a session
 * list per machine: that is one request per machine somebody owns, to find at
 * most one row.
 *
 * @param {object} deps `workspaceId` (a ref), plus the API function in tests
 */
export function useClearContext(deps = {}) {
  const { workspaceId, fetchWorkspaceSession = api.fetchWorkspaceSession } = deps

  const session = ref(null)

  /** Whether to draw the icon at all. */
  const offered = computed(() => canClearContext(session.value))

  /**
   * Ask what is running.
   *
   * A failure leaves `session` null, which hides the icon. That is the right
   * way round: not knowing whether a terminal is there is not a reason to
   * offer to type into it.
   */
  async function load() {
    const id = workspaceId?.value ?? workspaceId
    if (!id) return
    try {
      session.value = await fetchWorkspaceSession(id)
    } catch {
      session.value = null
    }
  }

  return { session, offered, load }
}
