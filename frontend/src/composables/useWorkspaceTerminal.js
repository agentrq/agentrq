// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * Reaching the terminal of the agent working in this workspace.
 *
 * The workspace page already knows an agent is *connected* — that arrives over
 * the event stream and turns the header dot green — but a connection is not
 * something you can open. The terminal page is addressed by a session id, and
 * that is the one thing the page does not have.
 *
 * So this asks for it, and the asking is deliberately one request: the same
 * answer could be assembled from the machines list plus a session list per
 * machine, but that is one request per machine somebody owns to find at most
 * one row, on the page they sit on all day.
 */

import { computed, ref, watch } from 'vue'
import * as api from '../api'
import { terminalPath } from './useTerminalView'

/**
 * Whether a session is one this workspace should offer a terminal for.
 *
 * Any session with an id — and the absence of a kind check is the decision,
 * not an omission.
 *
 * This listed `claude-code` alone until somebody hit the case it misses. The
 * argument for excluding the gateway was that it is already driveable from the
 * page: its turns arrive in the task composer, with a Stop button, because it
 * speaks ACP a turn at a time. That is true of the gateway's *turns* and of
 * nothing else. The gateway is also a process in a pseudo-terminal, and a
 * process asks its own questions on the way up — install this version (y/n),
 * trust this folder, paste a key. None of those are ACP, none of them reach
 * the composer, and until one is answered the agent is stopped. Excluding a
 * kind here excluded the only place those questions can be seen or answered.
 *
 * So the rule is the session, not the kind, which also means the next kind the
 * daemon learns to run is watchable because it is a terminal rather than
 * because somebody remembered a list.
 *
 * Exported and pure so the rule can be tested on its own. The id is what is
 * really being asked for: a row with none is what a backend answering `{}`
 * instead of `null` looks like by the time it reaches here, and offering a
 * button to `/sessions/undefined` is worse than offering nothing.
 */
export function isWatchable(session) {
  return !!terminalPath(session)
}

/**
 * The terminal affordance for one workspace.
 *
 * @param {object} deps `workspaceId` and `agentConnected` (refs), plus, in
 *   tests, the API function.
 */
export function useWorkspaceTerminal(deps = {}) {
  const {
    workspaceId,
    agentConnected,
    fetchWorkspaceSession = api.fetchWorkspaceSession,
  } = deps

  const session = ref(null)
  const loading = ref(false)

  /** Whether to draw the button at all. */
  const offered = computed(() => isWatchable(session.value))

  /**
   * Where it goes. Empty when there is nothing to go to.
   *
   * Built by the same function `offered` is decided with, so the two cannot
   * disagree: a button that is drawn always has somewhere to send you.
   */
  const to = computed(() => terminalPath(session.value))

  /**
   * What the button says it will show.
   *
   * A machine runs agents for several workspaces and most are the same kind,
   * so the kind alone is not an answer — but here the workspace is implied by
   * the page, and the status is the part somebody cannot otherwise see: a
   * session that is still starting is worth opening precisely because it might
   * be about to fail.
   */
  const label = computed(() =>
    offered.value
      ? session.value.status === 'starting'
        ? 'Agent terminal (starting)'
        : 'Agent terminal'
      : ''
  )

  /**
   * Ask which session is running here.
   *
   * A failure leaves no button rather than a broken one, and says nothing: the
   * person did not ask for this, so there is no news to report — an affordance
   * that cannot be offered should simply not be offered. The header dot still
   * tells them whether an agent is there.
   */
  async function load() {
    const id = workspaceId?.value
    if (!id) {
      session.value = null
      return
    }
    loading.value = true
    try {
      session.value = await fetchWorkspaceSession(id)
    } catch {
      session.value = null
    } finally {
      loading.value = false
    }
  }

  /**
   * Re-ask when the workspace changes, and when an agent comes or goes.
   *
   * The second half is what keeps the button honest without polling and
   * without a second event stream: `agentConnected` already flips on this page
   * from the workspace's own stream, and it flipping is the one cheap signal
   * that the session table has probably changed. Both directions matter — one
   * brings the button, the other takes it away.
   *
   * Deliberately *not* gated on the agent being connected. A session in
   * `starting` has no agent attached yet, and that is exactly the moment
   * somebody wants to watch it.
   */
  watch(
    () => [workspaceId?.value, agentConnected?.value],
    () => {
      load()
    },
    { immediate: true }
  )

  return { session, loading, offered, to, label, load }
}
