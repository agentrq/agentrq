// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { defineStore } from 'pinia';
import { computed, ref } from 'vue';
import * as api from '../api';

/**
 * The teams the signed-in user is in, the invitations waiting for them, and
 * the shared workspaces of the team on screen.
 *
 * Which team is on screen is not kept here: it is the route's `?team=`, so a
 * link says what it shows (see useTeamMode). What is kept is the last choice,
 * per browser, so the switch can offer it again.
 */
export const LAST_TEAM_KEY = 'agentrq:team';

/** The last team chosen in this browser, '' for Me. */
export function rememberedTeam(storage = globalThis.localStorage) {
  try {
    return storage?.getItem(LAST_TEAM_KEY) || '';
  } catch {
    // Storage blocked (a private window): the choice lasts this visit.
    return '';
  }
}

export function rememberTeam(teamId, storage = globalThis.localStorage) {
  try {
    if (teamId) storage?.setItem(LAST_TEAM_KEY, teamId);
    else storage?.removeItem(LAST_TEAM_KEY);
  } catch {
    // As above.
  }
}

// The API's lists, or empty: a missing field is not a crash.
const listOf = (res, key) => (Array.isArray(res?.[key]) ? res[key] : []);

export const useTeamStore = defineStore('team', () => {
  const teams = ref([]);
  const invitations = ref([]);
  const loaded = ref(false);
  // Per team, so switching back and forth draws at once.
  const workspacesByTeam = ref({});

  const pendingCount = computed(() => invitations.value.length);

  const getTeam = (teamId) => teams.value.find((t) => String(t.id) === String(teamId)) || null;

  // Reads overlap (an accept reloads while the shell's first read is out), so
  // only the latest may write, as in the workspace store.
  let latest = 0;

  /**
   * Teams and invitations together: the switch shows both. A failure leaves
   * both as they were — teams are an addition to the app, and a team API that
   * is down must not take the sidebar with it.
   */
  async function load() {
    const id = ++latest;
    const [t, i] = await Promise.allSettled([api.listTeams(), api.listMyInvitations()]);
    if (id !== latest) return;
    if (t.status === 'fulfilled') teams.value = listOf(t.value, 'teams');
    if (i.status === 'fulfilled') invitations.value = listOf(i.value, 'invitations');
    loaded.value = true;
  }

  async function loadWorkspaces(teamId) {
    const res = await api.fetchTeamWorkspaces(teamId);
    workspacesByTeam.value = { ...workspacesByTeam.value, [teamId]: listOf(res, 'workspaces') };
    return workspacesByTeam.value[teamId];
  }

  const workspacesOf = (teamId) => workspacesByTeam.value[teamId] || [];

  /** A team created, renamed or joined: in the list, replacing its old copy. */
  function upsert(team) {
    if (!team?.id) return;
    const rest = teams.value.filter((t) => String(t.id) !== String(team.id));
    teams.value = [...rest, { ...getTeam(team.id), ...team }].sort((a, b) => a.name.localeCompare(b.name));
  }

  /** The team read again: after someone else changed it. */
  async function refresh(teamId) {
    const team = await api.getTeam(teamId);
    upsert(team);
    return getTeam(teamId);
  }

  /** The version a change of yours left the team at. */
  function setVersion(teamId, version) {
    if (version === undefined || version === null) return;
    teams.value = teams.value.map((t) => (String(t.id) === String(teamId) ? { ...t, version } : t));
  }

  /** A team deleted, left, or one you were removed from. */
  function drop(teamId) {
    teams.value = teams.value.filter((t) => String(t.id) !== String(teamId));
    const { [teamId]: _gone, ...rest } = workspacesByTeam.value;
    workspacesByTeam.value = rest;
    if (rememberedTeam() === String(teamId)) rememberTeam('');
  }

  function dropInvitation(invitationId) {
    invitations.value = invitations.value.filter((i) => String(i.id) !== String(invitationId));
  }

  return {
    teams,
    invitations,
    loaded,
    workspacesByTeam,
    pendingCount,
    getTeam,
    load,
    loadWorkspaces,
    workspacesOf,
    upsert,
    refresh,
    setVersion,
    drop,
    dropInvitation,
  };
});
