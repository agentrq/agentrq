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

/**
 * The team workspace a shared workspace is part of: the name it is shared
 * under (its parent's, for a fork), else its own.
 */
export const sharedNameOf = (w) => (w?.sharedName || w?.name || '').trim();

/** Two names are one team workspace when they match ignoring case and spaces. */
export const sameSharedName = (a, b) => (a || '').trim().toLowerCase() === (b || '').trim().toLowerCase();

const ownerKey = (o) => String(o?.userId ?? o?.id ?? '');

/**
 * The team's workspaces as the sidebar draws them: members' workspaces of the
 * same name are one, by name, with each owner whose workspace is in it.
 */
export function teamWorkspaceList(workspaces = []) {
  const byName = new Map();
  for (const w of workspaces) {
    const name = sharedNameOf(w);
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

/** The member filter's choices: anyone, then each owner with their avatar. */
export const memberChoices = (owners = []) => [
  { value: '', label: 'All members' },
  ...owners.map((o) => ({ value: String(o.userId), label: o.name || o.email || String(o.userId), owner: o })),
];

/** The workspace filter's choices: any, then each team workspace with its owners. */
export const workspaceChoices = (list = []) => [
  { value: '', label: 'All workspaces' },
  ...list.map((g) => ({ value: g.name, label: g.name, owners: g.owners })),
];

/**
 * The workspace of yours a teammate's task is cloned into by default: one with
 * the same name as theirs, else as the team workspace it is in, else the first
 * that is not archived. '' when you have none.
 */
export function preferredWorkspace(mine = [], { workspaceName = '', sharedName = '' } = {}) {
  const open = mine.filter((w) => !w.archivedAt);
  const named = (n) => open.find((w) => n && sameSharedName(w.name, n));
  return String((named(workspaceName) || named(sharedName) || open[0])?.id ?? '');
}

/** A change refused because someone changed the team since it was read. */
export const isVersionConflict = (err) => err?.status === 409 && err?.conflict === true;

/** What a conflict tells the person, once the team is read again. */
export const CONFLICT_NOTICE = 'Someone changed this team, refreshed';

/** The Manage team dialog's tabs; `?manage=<tab>` opens it on one. */
export const MANAGE_TABS = ['members', 'invitations', 'activity', 'workspaces'];

/** `route`'s location with the Manage team dialog open on `tab`, or shut (''). */
export function withManage(route, tab) {
  const query = { ...(route?.query || {}) };
  if (tab) query.manage = tab;
  else delete query.manage;
  return { path: route?.path || '/', query };
}

/** The dialog's tab a route asks for, '' when it is shut. */
export const manageTabOf = (route) => (MANAGE_TABS.includes(route?.query?.manage) ? route.query.manage : '');

const personName = (p) => p?.name || p?.email || 'Someone';

/**
 * One line of a team's activity log: who did what. The workspace a share
 * names is the one it had then, as the server kept it.
 */
export function describeActivity(a = {}) {
  const who = personName(a.actor);
  const d = a.details || {};
  const target = a.targetUser ? personName(a.targetUser) : a.targetEmail || '';
  const workspace = d.workspaceName || 'a workspace';
  switch (a.action) {
    case 'team.created': return `${who} created the team`;
    case 'team.renamed': return d.from && d.to ? `${who} renamed the team from ${d.from} to ${d.to}` : `${who} renamed the team`;
    case 'member.invited': return `${who} invited ${target || 'someone'}`;
    case 'invitation.revoked': return `${who} revoked the invitation to ${target || 'someone'}`;
    case 'invitation.accepted': return `${who} joined`;
    case 'invitation.declined': return `${who} declined the invitation`;
    case 'member.removed': return `${who} removed ${target || 'a member'}`;
    case 'member.left': return `${who} left`;
    case 'workspace.shared': return `${who} shared ${workspace}`;
    case 'workspace.unshared': return `${who} stopped sharing ${workspace}`;
    case 'team.deleted': return `${who} deleted the team`;
    default: return `${who}: ${a.action || 'a change'}`;
  }
}

/** "just now", "12 minutes ago", "3 hours ago", "6 days ago", else the date. */
export function timeAgo(at, now = Date.now()) {
  const ms = now - new Date(at).getTime();
  if (!(ms >= 0)) return '';
  const minutes = Math.floor(ms / 60000);
  if (minutes < 1) return 'just now';
  if (minutes < 60) return `${minutes} minute${minutes === 1 ? '' : 's'} ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} hour${hours === 1 ? '' : 's'} ago`;
  const days = Math.floor(hours / 24);
  if (days < 30) return `${days} day${days === 1 ? '' : 's'} ago`;
  return new Date(at).toLocaleDateString();
}

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
 * What the invite form says when the server refuses: a daily cap (429), the
 * team's or the inviter's across all their teams, in the server's own words,
 * since it knows which and the number.
 */
export function inviteError(err) {
  if (err?.status === 429) return err.message || 'That is as many invitations as may be sent today. Try again tomorrow.';
  return err?.message || 'Failed to send the invitation';
}

/** Who sent an invitation: its inviter's name, else a member's by id, else the id. */
export function inviterName(invitation, members = []) {
  if (invitation?.invitedByName) return invitation.invitedByName;
  const by = invitation?.invitedBy;
  return members.find((m) => String(m.userId) === String(by))?.name || by || '';
}

/**
 * A teammate's task event on your account stream, as the event the page would
 * get for its own task: `team.task.updated` for team `teamId` reads as
 * `task.updated`. null for any other team's, and for your own events.
 */
export function teamEvent(ev, teamId) {
  if (!teamId || !ev?.type?.startsWith('team.') || String(ev.teamId) !== String(teamId)) return null;
  return { ...ev, type: ev.type.slice('team.'.length) };
}

/** Whether a team task is in the list's workspace and member filters ('' = any). */
export function inTeamFilter(task, { workspace = '', member = '' } = {}) {
  if (member && String(task?.owner?.userId) !== String(member)) return false;
  return !workspace || sameSharedName(task?.sharedName, workspace);
}

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

// The team being left, while the way back to Me is under way.
let leaving = null;

/** The team on screen, and links that stay in its mode. */
export function useTeamMode() {
  const route = useRoute();
  const router = useRouter();
  const store = useTeamStore();
  const { notifyInfo, notifyError } = useToasts();

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
    // A page's reads fail together (its tasks and the team's workspaces):
    // one notice, one way back.
    if (leaving?.id === id) return leaving.done;
    notifyInfo(name ? `You're no longer in ${name}` : "You're no longer in that team");
    store.drop(id);
    rememberTeam('');
    const done = Promise.resolve(router.replace('/')).finally(() => { leaving = null; });
    leaving = { id, done };
    return done;
  }

  /**
   * A refused change to team `id`: someone changed it since it was read, and
   * it is read again and said so ('conflict'); the team on screen is no longer
   * yours to see, and you go back to Me ('lost'); anything else is shown in
   * the server's words ('error'). The caller reloads its own lists on a
   * conflict.
   */
  async function changeFailed(err, id = teamId.value) {
    if (isVersionConflict(err)) {
      await store.refresh(id).catch(() => {});
      notifyInfo(CONFLICT_NOTICE);
      return 'conflict';
    }
    if (lostTeam(err) && String(id) === teamId.value) {
      await leaveTeam();
      return 'lost';
    }
    notifyError(err?.message || 'Something went wrong');
    return 'error';
  }

  return { teamId, isTeamMode, team, link, leaveTeam, changeFailed };
}
