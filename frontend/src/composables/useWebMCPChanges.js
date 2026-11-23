// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * Telling the open page that a browser agent changed something.
 *
 * A WebMCP tool writes through the same API the page reads from, but the page
 * is not told: a view that loaded on mount would go on showing the old data
 * until the person navigated away and back. Tasks and machine status reach the
 * page over SSE; workflows, events and workspaces do not, so those views listen
 * here instead and re-fetch.
 *
 * Views re-fetch rather than the `<router-view>` being remounted, because a
 * remount would throw away whatever the person had in progress on the page.
 */
import { getCurrentInstance, onUnmounted } from 'vue'

const listeners = new Set()

/** Called by the WebMCP wiring after a tool that changes data has succeeded. */
export function notifyWebMCPChange() {
  for (const listener of listeners) listener()
}

/**
 * Run `reload` after each WebMCP change, until the calling component unmounts.
 *
 * An agent's changes come in bursts — nine steps added to a workflow is nine
 * calls — so runs never overlap: a change that lands mid-reload queues exactly
 * one more, and an older response can never land after a newer one. A failed
 * reload is logged and leaves the page as it was.
 *
 * @param {() => unknown} reload
 * @returns {() => void} stop listening
 */
export function onWebMCPChange(reload) {
  let running = false
  let again = false

  async function drain() {
    running = true
    do {
      again = false
      try {
        await reload()
      } catch (err) {
        console.error('Refreshing after a WebMCP change failed:', err)
      }
    } while (again)
    running = false
  }

  const listener = () => {
    if (running) again = true
    else drain()
  }

  listeners.add(listener)
  const off = () => listeners.delete(listener)
  if (getCurrentInstance()) onUnmounted(off)
  return off
}
