// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The official ACP registry's agents, for every launch form at once.
 *
 * Loaded when the app starts and again every hour, into one module-level list
 * the forms read, so opening a form never waits on it. A failed or empty
 * reload keeps the last good list: the server answers an empty list for a
 * registry it could not reach, and that is not news worth dropping agents for.
 */

import { readonly, ref } from 'vue'
import * as api from '../api'

/** How often the list is reloaded while the app is open. */
export const ACP_REGISTRY_REFRESH_MS = 60 * 60 * 1000

const agents = ref([])
let inflight = null

/**
 * Reload the list. Concurrent calls share one request; resolves to the list
 * held afterwards.
 */
export function loadAcpRegistry(fetchAgents = api.fetchAcpRegistryAgents) {
  if (!inflight) {
    inflight = (async () => {
      try {
        const data = await fetchAgents()
        if (data?.agents?.length) agents.value = data.agents
      } catch {
        // Keep the last good list; the next reload tries again.
      } finally {
        inflight = null
      }
      return agents.value
    })()
  }
  return inflight
}

/**
 * Load now and every `ACP_REGISTRY_REFRESH_MS`. Returns the function that
 * stops the reloads.
 */
export function startAcpRegistry({
  fetchAgents = api.fetchAcpRegistryAgents,
  setInterval: every = setInterval,
  clearInterval: stopEvery = clearInterval,
} = {}) {
  loadAcpRegistry(fetchAgents)
  const timer = every(() => loadAcpRegistry(fetchAgents), ACP_REGISTRY_REFRESH_MS)
  return () => stopEvery(timer)
}

/** The list, read-only. */
export function useAcpRegistry() {
  return { agents: readonly(agents) }
}

/** Empties the list, for tests. */
export function resetAcpRegistry() {
  agents.value = []
  inflight = null
}
