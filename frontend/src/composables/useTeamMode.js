// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { computed } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useTeamStore, rememberTeam } from '../stores/teamStore';
import { useToasts } from './useToasts';

/**
 * Team mode: the same pages, showing a team's shared work, read-only.
 *
 * The team on screen is the route's `?team=<id>` and nothing else, so a link
 * always says what it shows and the back button leaves team mode the way it
 * entered it. Write controls are hidden (not disabled) while it is set.
 */

/** The team a route shows, '' in Me mode. */
export function teamIdOf(route) {
  const team = route?.query?.team;
  return typeof team === 'string' ? team : '';
}

/**
 * A location in the same mode as `teamId`: the team kept on it, or taken off
 * for Me ('').
 */
export function withTeam(to, teamId) {
  const loc = typeof to === 'string' ? splitPath(to) : { ...to, query: { ...(to.query || {}) } };
  if (teamId) loc.query.team = String(teamId);
  else delete loc.query.team;
  return loc;
}

function splitPath(path) {
  const [p, search = ''] = path.split('?');
  return { path: p, query: Object.fromEntries(new URLSearchParams(search)) };
}

/** Where team mode starts: the team's active tasks. */
export const teamHome = (teamId) => ({ path: '/tasks/active', query: { team: String(teamId) } });

/** Where accepting an invitation lands: the team, with its one-time welcome. */
export const joinedHome = (teamId) => ({ path: '/tasks/active', query: { team: String(teamId), joined: '1' } });

/**
 * The redirect, if any, for a navigation to the home page: in team mode the
 * home is the team's tasks; and the first page this browser opens follows
 * the switch's last choice. A deep link is never redirected.
 */
export function teamLanding(to, from, remembered) {
  if (to.path !== '/') return undefined;
  const team = teamIdOf(to);
  if (team) return teamHome(team);
  if (remembered && from.matched.length === 0) return teamHome(remembered);
  return undefined;
}

/** The group a shared workspace shows in: its "show in team as", else its name. */
export const groupOf = (w) => (w?.group || w?.name || '').trim();

const ownerKey = (o) => String(o?.userId ?? o?.id ?? '');

/**
 * Shared workspaces as the sidebar draws them: one row per group, by name,
 * with the owners whose workspaces are in it.
 */
export function teamGroups(workspaces = []) {
  const byName = new Map();
  for (const w of workspaces) {
    const name = groupOf(w);
    const key = name.toLowerCase();
    if (!byName.has(key)) byName.set(key, { name, owners: [], workspaceIds: [] });
    const g = byName.get(key);
    g.workspaceIds.push(String(w.id));
    if (w.owner && !g.owners.some((o) => ownerKey(o) === ownerKey(w.owner))) g.owners.push(w.owner);
  }
  return [...byName.values()].sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }));
}

/** Every owner with a shared workspace, by name: the member filter. */
export function teamOwners(workspaces = []) {
  const seen = new Map();
  for (const w of workspaces) if (w.owner && !seen.has(ownerKey(w.owner))) seen.set(ownerKey(w.owner), w.owner);
  return [...seen.values()].sort((a, b) => (a.name || '').localeCompare(b.name || ''));
}

/**
 * The group a workspace is offered when it is shared: a team group of the
 * same name (ignoring case and spaces), else its own name.
 */
export function suggestGroup(workspaceName, groups = []) {
  const want = (workspaceName || '').trim().toLowerCase();
  return groups.find((g) => g.trim().toLowerCase() === want) || (workspaceName || '').trim();
}

/**
 * The workspace of yours a teammate's task is cloned into by default: one with
 * the same name as theirs, else as its group, else the first that is not
 * archived. '' when you have none.
 */
export function preferredWorkspace(mine = [], { workspaceName = '', group = '' } = {}) {
  const open = mine.filter((w) => !w.archivedAt);
  const named = (n) => open.find((w) => n && w.name.trim().toLowerCase() === n.trim().toLowerCase());
  return String((named(workspaceName) || named(group) || open[0])?.id ?? '');
}

/** Owners and admins manage a team; members only read it. */
export const canManage = (role) => role === 'owner' || role === 'admin';

/** The roles someone may hand out: only an owner makes another owner. */
export const assignableRoles = (role) => (role === 'owner' ? ['member', 'admin', 'owner'] : canManage(role) ? ['member', 'admin'] : []);

/** "in 6 days", "in 3 hours", "in 12 minutes", or "expired". */
export function expiresIn(expiresAt, now = Date.now()) {
  const ms = new Date(expiresAt).getTime() - now;
  if (!(ms > 0)) return 'expired';
  const minutes = Math.ceil(ms / 60000);
  if (minutes < 60) return `in ${minutes} minute${minutes === 1 ? '' : 's'}`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `in ${hours} hour${hours === 1 ? '' : 's'}`;
  const days = Math.floor(hours / 24);
  return `in ${days} day${days === 1 ? '' : 's'}`;
}

/**
 * What the invite form says when the server refuses: the daily cap (429) in
 * the server's own words, since it knows the number.
 */
export function inviteError(err) {
  if (err?.status === 429) return err.message || 'This team has sent as many invitations as it may today. Try again tomorrow.';
  return err?.message || 'Failed to send the invitation';
}

/** Who sent an invitation: its inviter's name, else a member's by id, else the id. */
export function inviterName(invitation, members = []) {
  if (invitation?.invitedByName) return invitation.invitedByName;
  const by = invitation?.invitedBy;
  return members.find((m) => String(m.userId) === String(by))?.name || by || '';
}

/** The team in an answer: `{ team }`, or the team itself. */
export const teamOf = (res) => res?.team ?? res ?? null;

/** A team answer that means you can no longer see it: removed, or it is gone. */
export const lostTeam = (err) => err?.status === 404 || err?.status === 403;

/** One of the accept page's states, from what the preview or accept answered. */
export function invitationState(preview, err) {
  if (err?.status === 403) return 'wrongEmail';
  if (err?.status === 404 || err?.status === 410) return 'invalid';
  if (err) return 'error';
  if (preview && preview.emailMatches === false) return 'wrongEmail';
  return preview ? 'ready' : 'loading';
}

/** The team on screen, and links that stay in its mode. */
export function useTeamMode() {
  const route = useRoute();
  const router = useRouter();
  const store = useTeamStore();
  const { notifyInfo } = useToasts();

  const teamId = computed(() => teamIdOf(route));
  const isTeamMode = computed(() => teamId.value !== '');
  const team = computed(() => (isTeamMode.value ? store.getTeam(teamId.value) : null));

  /** `to` in the mode on screen. */
  const link = (to) => withTeam(to, teamId.value);

  /**
   * You can no longer see the team on screen: say so, forget it, and go back
   * to Me (§9 of the flows).
   */
  function leaveTeam(name = team.value?.name) {
    const id = teamId.value;
    notifyInfo(name ? `You're no longer in ${name}` : "You're no longer in that team");
    store.drop(id);
    rememberTeam('');
    return router.replace('/');
  }

  return { teamId, isTeamMode, team, link, leaveTeam };
}
