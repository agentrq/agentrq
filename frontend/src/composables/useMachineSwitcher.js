// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * Jumping to a machine from the keyboard, the `M` counterpart of the `W`
 * workspace switcher.
 *
 * The matching lives here rather than in the component so it can be tested as
 * a function, the same split `useWorkspaceSwitcher` uses.
 */

import { ref } from 'vue';

import * as api from '../api';

/**
 * What a machine is called on screen: its name, or the hostname it enrolled
 * with when nobody has named it. The machines page shows the same fallback, so
 * the switcher finds a machine by the word its row there displays.
 *
 * @param {{ name?: string, hostname?: string } | null | undefined} machine
 */
export function machineLabel(machine) {
  return String(machine?.name || machine?.hostname || '');
}

/**
 * Machines matching `query`, best match first.
 *
 * Ranked like workspaces — exact name, then prefix, then substring — and then
 * the hostname, for a machine that was renamed but is still known by the name
 * its terminal prompt prints.
 *
 * **Online machines rank before offline ones of the same quality.** A machine
 * is almost always opened to launch or watch an agent, which only an online
 * one can do; an offline one stays listed so the switcher agrees with the
 * machines page.
 *
 * @param {Array<{ id: string, name?: string, hostname?: string, online?: boolean }>} machines
 * @param {string} query
 * @param {number} [limit]
 */
export function matchMachines(machines, query, limit = 10) {
  const list = Array.isArray(machines) ? machines : [];
  const q = (query ?? '').trim().toLowerCase();

  const rank = (m) => {
    if (!q) return 0;
    const label = machineLabel(m).toLowerCase();
    if (label === q) return 0;
    if (label.startsWith(q)) return 1;
    if (label.includes(q)) return 2;
    if (String(m?.hostname ?? '').toLowerCase().includes(q)) return 3;
    return -1;
  };

  return list
    .map((m, order) => ({ m, order, rank: rank(m), offline: m?.online ? 0 : 1 }))
    .filter((row) => row.rank !== -1)
    .sort((a, b) => a.rank - b.rank || a.offline - b.offline || a.order - b.order)
    .slice(0, limit)
    .map((row) => row.m);
}

/**
 * A machine's page, which is where its row on the machines page opens.
 *
 * @param {{ id?: string } | string | null | undefined} machine
 * @returns {string} a route, or the machines page when there is nothing to open
 */
export function machineRoute(machine) {
  const id = typeof machine === 'string' ? machine : machine?.id;
  return id ? `/machines/${id}` : '/machines';
}

/**
 * The machine on screen, if the route is a machine's page.
 *
 * The shell's route params are shared by every page, and `:id` on a
 * workspace's page is a workspace, so the path decides what the id names.
 *
 * @param {{ path?: string, params?: Record<string, any> } | null | undefined} route
 */
export function currentMachineId(route) {
  if (!route?.path?.startsWith('/machines/')) return '';
  return String(route.params?.id ?? '');
}

/**
 * The switcher's list, fetched each time it opens.
 *
 * Machines are not in a store the way workspaces are — only the machines pages
 * hold them — so there is nothing to read in hand. Fetching on open also means
 * the online dots are current rather than whatever they were at page load.
 *
 * A reopen can overtake a slow earlier fetch, so only the latest answer is
 * kept; otherwise a stale list could land on top of a fresh one.
 *
 * @param {{ fetchMachines?: () => Promise<{ machines?: any[] }> }} [deps]
 */
export function useMachineSwitcher(deps = {}) {
  const { fetchMachines = api.fetchMachines } = deps;

  const machines = ref([]);
  const loading = ref(false);
  const error = ref('');
  let latest = 0;

  async function load() {
    const call = ++latest;
    loading.value = true;
    error.value = '';
    try {
      const data = await fetchMachines();
      if (call !== latest) return;
      machines.value = data?.machines ?? [];
    } catch (e) {
      if (call !== latest) return;
      error.value = e?.message || 'Failed to load machines';
    } finally {
      if (call === latest) loading.value = false;
    }
  }

  return { machines, loading, error, load };
}
