// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// The installed app's icon shortcuts (the manifest's `shortcuts`) open
// `?shortcut=<name>`. New task and Find task are overlays with no URL of
// their own, so the shell opens them from the query.

/**
 * Run the shortcut a launch URL names, once.
 *
 * The query comes off first, with a replace, so Back never lands on a URL
 * that opens the overlay again; an action that navigates must also wait for
 * that, or its push cancels the replace.
 */
export async function followLaunchShortcut(router, route, actions) {
  const name = route.query.shortcut
  if (name === undefined) return false
  const { shortcut: _, ...query } = route.query
  await router.replace({ path: route.path, query, hash: route.hash })
  if (typeof name !== 'string' || !Object.hasOwn(actions, name)) return false
  await actions[name]()
  return true
}
