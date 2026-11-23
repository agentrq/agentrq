// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The installed app's icon badge: how many tasks are waiting on the person.
 *
 * The same count as the sidebar's marks, from the same task overview: tasks
 * with a permission request awaiting an answer, plus blocked ones. The page
 * sets it while open; the worker sets it on a push, when no page may be.
 */

/** The sum over workspaces, each `{ counts }` as the overview returns it. */
export function waitingOnYou(workspaces) {
  let n = 0
  for (const w of workspaces ?? []) n += (w?.counts?.pending ?? 0) + (w?.counts?.blocked ?? 0)
  return n
}

/**
 * Show `n` on the icon, or clear it at zero. Does nothing where the platform
 * has no badge, and ignores a refusal, which Chrome gives an app that is not
 * installed.
 */
export async function setBadge(nav, n) {
  if (typeof nav?.setAppBadge !== 'function') return
  try {
    await (n > 0 ? nav.setAppBadge(n) : nav.clearAppBadge())
  } catch {
    // Not installed, or not allowed: there is no badge to keep in step.
  }
}

/**
 * The worker's side, on a push: the payload carries no count, so ask for it.
 * The API sits under the worker's scope, which carries any base path. A failed
 * read leaves the badge as it was rather than clearing a count still true.
 */
export async function refreshBadge(fetchImpl, scope, nav) {
  if (typeof nav?.setAppBadge !== 'function') return
  try {
    const res = await fetchImpl(new URL('api/v1/tasks/overview', scope))
    if (!res.ok) return
    await setBadge(nav, waitingOnYou((await res.json()).workspaces))
  } catch {
    // Offline or signed out: keep what is shown.
  }
}
