// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createApp, h, nextTick } from 'vue';
import { createPinia, setActivePinia } from 'pinia';
import { createRouter, createMemoryHistory } from 'vue-router';

vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  listTeams: vi.fn(),
  listMyInvitations: vi.fn(),
  fetchTeamWorkspaces: vi.fn(),
  getTeam: vi.fn(),
}));

import * as api from '../src/api';
import { useTeamStore, rememberedTeam, rememberTeam, LAST_TEAM_KEY } from '../src/stores/teamStore';
import {
  teamIdOf,
  withTeam,
  teamHome,
  teamLanding,
  sharedNameOf,
  sameSharedName,
  teamWorkspaceList,
  teamOwners,
  memberChoices,
  workspaceChoices,
  preferredWorkspace,
  isVersionConflict,
  CONFLICT_NOTICE,
  MANAGE_TABS,
  withManage,
  manageTabOf,
  describeActivity,
  timeAgo,
  expiresIn,
  inviteError,
  lostTeam,
  invitationState,
  useTeamMode,
  teamEvent,
  inTeamFilter,
} from '../src/composables/useTeamMode';
import { useToasts } from '../src/composables/useToasts';
import { routes, createAgentRQApp } from '../src/app';

const alice = { userId: 'u1', name: 'Alice' };
const bob = { userId: 'u2', name: 'Bob' };

beforeEach(() => {
  setActivePinia(createPinia());
  localStorage.clear();
  vi.clearAllMocks();
});

describe('the remembered team', () => {
  it('is kept per browser, and forgotten for Me', () => {
    expect(rememberedTeam()).toBe('');
    rememberTeam('tm1');
    expect(localStorage.getItem(LAST_TEAM_KEY)).toBe('tm1');
    expect(rememberedTeam()).toBe('tm1');
    rememberTeam('');
    expect(rememberedTeam()).toBe('');
  });

  it('survives storage that throws, as in a private window', () => {
    const blocked = {
      getItem: () => { throw new Error('SecurityError: storage is blocked'); },
      setItem: () => { throw new Error('SecurityError: storage is blocked'); },
      removeItem: () => { throw new Error('SecurityError: storage is blocked'); },
    };
    expect(rememberedTeam(blocked)).toBe('');
    expect(() => rememberTeam('tm1', blocked)).not.toThrow();
    expect(() => rememberTeam('', blocked)).not.toThrow();
  });

  it('reads as Me where there is no storage at all', () => {
    expect(rememberedTeam(undefined)).toBe('');
    expect(() => rememberTeam('tm1', undefined)).not.toThrow();
  });
});

describe('the team store', () => {
  it('loads teams and invitations together, and counts the pending ones', async () => {
    api.listTeams.mockResolvedValue({ teams: [{ id: 'tm1', name: 'Core', role: 'owner' }] });
    api.listMyInvitations.mockResolvedValue({ invitations: [{ id: 'i1' }, { id: 'i2' }] });
    const store = useTeamStore();

    await store.load();

    expect(store.loaded).toBe(true);
    expect(store.teams.map((t) => t.name)).toEqual(['Core']);
    expect(store.pendingCount).toBe(2);
    expect(store.getTeam('tm1').role).toBe('owner');
    expect(store.getTeam('nope')).toBeNull();
  });

  it('keeps what it had when the team API is down, rather than emptying the switch', async () => {
    const store = useTeamStore();
    store.teams = [{ id: 'tm1', name: 'Core' }];
    store.invitations = [{ id: 'i1' }];
    api.listTeams.mockRejectedValue(new Error('network unreachable'));
    api.listMyInvitations.mockRejectedValue(new Error('network unreachable'));

    await store.load();

    expect(store.teams).toHaveLength(1);
    expect(store.invitations).toHaveLength(1);
    expect(store.loaded).toBe(true);
  });

  it('reads an answer with no list as an empty one', async () => {
    api.listTeams.mockResolvedValue({});
    api.listMyInvitations.mockResolvedValue(null);
    const store = useTeamStore();

    await store.load();

    expect(store.teams).toEqual([]);
    expect(store.invitations).toEqual([]);
  });

  it('lets only the latest of two overlapping loads write', async () => {
    const resolvers = [];
    api.listTeams.mockImplementation(() => new Promise((r) => resolvers.push(r)));
    api.listMyInvitations.mockResolvedValue({ invitations: [] });
    const store = useTeamStore();

    const first = store.load();
    const second = store.load();
    resolvers[1]({ teams: [{ id: 'new', name: 'New' }] });
    await second;
    resolvers[0]({ teams: [{ id: 'old', name: 'Old' }] });
    await first;

    expect(store.teams.map((t) => t.id)).toEqual(['new']);
  });

  it('keeps each team\'s shared workspaces apart', async () => {
    api.fetchTeamWorkspaces.mockImplementation(async (id) => ({ workspaces: [{ id: `${id}-w` }] }));
    const store = useTeamStore();

    expect(await store.loadWorkspaces('a')).toEqual([{ id: 'a-w' }]);
    await store.loadWorkspaces('b');

    expect(store.workspacesOf('a')).toEqual([{ id: 'a-w' }]);
    expect(store.workspacesOf('b')).toEqual([{ id: 'b-w' }]);
    expect(store.workspacesOf('c')).toEqual([]);
  });

  it('adds or replaces a team in name order, merging what it knew', () => {
    const store = useTeamStore();
    store.teams = [{ id: 'tm2', name: 'Zeta', role: 'member' }];

    store.upsert({ id: 'tm1', name: 'Core', role: 'owner' });
    store.upsert({ id: 'tm2', name: 'Alpha' });
    store.upsert(null);
    store.upsert({ name: 'no id' });

    expect(store.teams).toEqual([
      { id: 'tm2', name: 'Alpha', role: 'member' },
      { id: 'tm1', name: 'Core', role: 'owner' },
    ]);
  });

  it('drops a team you left, with its workspaces and the remembered choice', async () => {
    api.fetchTeamWorkspaces.mockResolvedValue({ workspaces: [{ id: 'w1' }] });
    const store = useTeamStore();
    store.teams = [{ id: 'tm1', name: 'Core' }, { id: 'tm2', name: 'Infra' }];
    await store.loadWorkspaces('tm1');
    rememberTeam('tm1');

    store.drop('tm1');

    expect(store.teams.map((t) => t.id)).toEqual(['tm2']);
    expect(store.workspacesOf('tm1')).toEqual([]);
    expect(rememberedTeam()).toBe('');
  });

  it('keeps the remembered choice when another team is dropped', () => {
    const store = useTeamStore();
    rememberTeam('tm2');

    store.drop('tm1');

    expect(rememberedTeam()).toBe('tm2');
  });

  it('drops an invitation once answered', () => {
    const store = useTeamStore();
    store.invitations = [{ id: 'i1' }, { id: 'i2' }];

    store.dropInvitation('i1');

    expect(store.invitations).toEqual([{ id: 'i2' }]);
  });
});

describe('the team in the URL', () => {
  it('is the ?team= query, and nothing else', () => {
    expect(teamIdOf({ query: { team: 'tm1' } })).toBe('tm1');
    expect(teamIdOf({ query: {} })).toBe('');
    expect(teamIdOf({ query: { team: ['a', 'b'] } })).toBe('');
    expect(teamIdOf(undefined)).toBe('');
  });

  it('is put on a link, or taken off it for Me', () => {
    expect(withTeam('/tasks/active', 'tm1')).toEqual({ path: '/tasks/active', query: { team: 'tm1' } });
    expect(withTeam('/tasks/active?group=docs', 'tm1')).toEqual({ path: '/tasks/active', query: { group: 'docs', team: 'tm1' } });
    expect(withTeam({ path: '/kanban', query: { team: 'tm1' } }, '')).toEqual({ path: '/kanban', query: {} });
    expect(withTeam({ path: '/kanban' }, 7)).toEqual({ path: '/kanban', query: { team: '7' } });
  });

  it('starts team mode on the team\'s active tasks', () => {
    expect(teamHome('tm1')).toEqual({ path: '/tasks/active', query: { team: 'tm1' } });
  });
});

describe('landing on the home page', () => {
  const first = { matched: [] };
  const later = { matched: [{}] };

  it('shows a team\'s tasks when the home page is asked for in team mode', () => {
    expect(teamLanding({ path: '/', query: { team: 'tm1' } }, later, '')).toEqual(teamHome('tm1'));
  });

  it('follows the last choice on the first page this browser opens', () => {
    expect(teamLanding({ path: '/', query: {} }, first, 'tm2')).toEqual(teamHome('tm2'));
  });

  it('stays in Me when Me was chosen, or once the app is open', () => {
    expect(teamLanding({ path: '/', query: {} }, first, '')).toBeUndefined();
    expect(teamLanding({ path: '/', query: {} }, later, 'tm2')).toBeUndefined();
  });

  it('never redirects a deep link', () => {
    expect(teamLanding({ path: '/workspaces/w1', query: {} }, first, 'tm2')).toBeUndefined();
  });

  it('is wired into the app\'s router', async () => {
    rememberTeam('tm2');
    const { router } = createAgentRQApp({ history: createMemoryHistory() });

    // Public, so the sign-in check lets it through without a server.
    await router.push('/invite#t');

    expect(router.currentRoute.value.path).toBe('/invite');
  });
});

describe('team workspaces and owners', () => {
  const shared = [
    { id: 'w1', name: 'backend', owner: alice },
    { id: 'w2', name: 'api', sharedName: 'Backend ', owner: bob },
    { id: 'w3', name: 'docs', owner: alice },
    { id: 'w4', name: 'backend', owner: alice },
    { id: 'w5', name: 'orphan' },
  ];

  it('names a team workspace by the name it is shared under, else its own', () => {
    expect(sharedNameOf({ name: 'x', sharedName: 'y' })).toBe('y');
    expect(sharedNameOf({ name: ' x ' })).toBe('x');
    expect(sharedNameOf(null)).toBe('');
  });

  it('takes two names as one team workspace ignoring case and spaces', () => {
    expect(sameSharedName(' Backend', 'backend ')).toBe(true);
    expect(sameSharedName('backend', 'web')).toBe(false);
    expect(sameSharedName(undefined, '')).toBe(true);
  });

  it('lists workspaces by name, ignoring case, each owner once', () => {
    expect(teamWorkspaceList(shared)).toEqual([
      { name: 'backend', owners: [alice, bob], workspaceIds: ['w1', 'w2', 'w4'] },
      { name: 'docs', owners: [alice], workspaceIds: ['w3'] },
      { name: 'orphan', owners: [], workspaceIds: ['w5'] },
    ]);
    expect(teamWorkspaceList()).toEqual([]);
  });

  it('lists each owner once, by name, for the member filter', () => {
    expect(teamOwners(shared)).toEqual([alice, bob]);
    expect(teamOwners([{ owner: { id: 'x' } }, { owner: { userId: 'y', name: 'Y' } }])).toEqual([{ id: 'x' }, { userId: 'y', name: 'Y' }]);
    expect(teamOwners()).toEqual([]);
  });

  it('offers any member, then each owner with their avatar, to the member picker', () => {
    expect(memberChoices([alice, { userId: 7, email: 'c@x.io' }, { userId: 8 }])).toEqual([
      { value: '', label: 'All members' },
      { value: 'u1', label: 'Alice', owner: alice },
      { value: '7', label: 'c@x.io', owner: { userId: 7, email: 'c@x.io' } },
      { value: '8', label: '8', owner: { userId: 8 } },
    ]);
    expect(memberChoices()).toEqual([{ value: '', label: 'All members' }]);
  });

  it('offers any workspace, then each team workspace with its owners, to the workspace picker', () => {
    expect(workspaceChoices(teamWorkspaceList(shared.slice(2, 3)))).toEqual([
      { value: '', label: 'All workspaces' },
      { value: 'docs', label: 'docs', owners: [alice] },
    ]);
    expect(workspaceChoices()).toEqual([{ value: '', label: 'All workspaces' }]);
  });
});

describe('the workspace a clone goes to', () => {
  const mine = [
    { id: 'a', name: 'archived', archivedAt: '2026-01-01' },
    { id: 'b', name: 'scratch' },
    { id: 'c', name: 'Backend' },
    { id: 'd', name: 'Docs' },
  ];

  it('prefers one named like theirs, then like the team workspace, then the first open one', () => {
    expect(preferredWorkspace(mine, { workspaceName: 'backend', sharedName: 'docs' })).toBe('c');
    expect(preferredWorkspace(mine, { workspaceName: 'api', sharedName: 'DOCS' })).toBe('d');
    expect(preferredWorkspace(mine, { workspaceName: 'api' })).toBe('b');
    expect(preferredWorkspace(mine)).toBe('b');
  });

  it('is none when every workspace is archived, or there are none', () => {
    expect(preferredWorkspace([mine[0]])).toBe('');
    expect(preferredWorkspace()).toBe('');
  });
});

describe('a change made over someone else\'s', () => {
  it('is a 409 the server marks as a conflict, and nothing else', () => {
    expect(isVersionConflict({ status: 409, conflict: true })).toBe(true);
    expect(isVersionConflict({ status: 409 })).toBe(false);
    expect(isVersionConflict({ status: 400, conflict: true })).toBe(false);
    expect(isVersionConflict(undefined)).toBe(false);
    expect(CONFLICT_NOTICE).toBe('Someone changed this team, refreshed');
  });
});

describe('the Manage team dialog in the URL', () => {
  it('is ?manage=<tab>, kept beside the rest of the query', () => {
    const route = { path: '/kanban', query: { team: 'tm1', workspace: 'docs' } };
    expect(withManage(route, 'invitations')).toEqual({ path: '/kanban', query: { team: 'tm1', workspace: 'docs', manage: 'invitations' } });
    expect(withManage({ path: '/kanban', query: { team: 'tm1', manage: 'members' } }, '')).toEqual({ path: '/kanban', query: { team: 'tm1' } });
    expect(withManage(undefined, 'members')).toEqual({ path: '/', query: { manage: 'members' } });
  });

  it('opens on one of its tabs, and is shut for anything else', () => {
    for (const tab of MANAGE_TABS) expect(manageTabOf({ query: { manage: tab } })).toBe(tab);
    expect(MANAGE_TABS).toEqual(['members', 'invitations', 'activity', 'workspaces']);
    expect(manageTabOf({ query: { manage: 'settings' } })).toBe('');
    expect(manageTabOf({ query: {} })).toBe('');
    expect(manageTabOf(undefined)).toBe('');
  });
});

describe('the activity log', () => {
  const actor = { userId: 'u1', name: 'Alice' };
  const line = (action, more = {}) => describeActivity({ action, actor, ...more });

  it('says who did what, in a sentence', () => {
    expect(line('team.created')).toBe('Alice created the team');
    expect(line('team.renamed', { details: { from: 'Core', to: 'Infra' } })).toBe('Alice renamed the team from Core to Infra');
    expect(line('team.renamed')).toBe('Alice renamed the team');
    expect(line('member.invited', { targetEmail: 'b@x.io' })).toBe('Alice invited b@x.io');
    expect(line('member.invited')).toBe('Alice invited someone');
    expect(line('invitation.revoked', { targetEmail: 'b@x.io' })).toBe('Alice revoked the invitation to b@x.io');
    expect(line('invitation.revoked')).toBe('Alice revoked the invitation to someone');
    expect(line('invitation.accepted')).toBe('Alice joined');
    expect(line('invitation.declined')).toBe('Alice declined the invitation');
    expect(line('member.removed', { targetUser: { email: 'bob@x.io' } })).toBe('Alice removed bob@x.io');
    expect(line('member.removed', { targetUser: {} })).toBe('Alice removed Someone');
    expect(line('member.removed')).toBe('Alice removed a member');
    expect(line('member.left')).toBe('Alice left');
    expect(line('workspace.shared', { details: { workspaceName: 'docs' } })).toBe('Alice shared docs');
    expect(line('workspace.unshared')).toBe('Alice stopped sharing a workspace');
    expect(line('team.deleted', { details: { name: 'Core' } })).toBe('Alice deleted the team');
    expect(line('team.archived')).toBe('Alice: team.archived');
    expect(describeActivity()).toBe('Someone: a change');
    expect(describeActivity({ action: 'member.left', actor: { email: 'c@x.io' } })).toBe('c@x.io left');
  });

  it('says when, in words, then as a date', () => {
    const now = Date.parse('2026-11-13T12:00:00Z');
    const ago = (ms) => new Date(now - ms).toISOString();
    expect(timeAgo(ago(10000), now)).toBe('just now');
    expect(timeAgo(ago(60000), now)).toBe('1 minute ago');
    expect(timeAgo(ago(12 * 60000), now)).toBe('12 minutes ago');
    expect(timeAgo(ago(3600000), now)).toBe('1 hour ago');
    expect(timeAgo(ago(3 * 3600000), now)).toBe('3 hours ago');
    expect(timeAgo(ago(86400000), now)).toBe('1 day ago');
    expect(timeAgo(ago(6 * 86400000), now)).toBe('6 days ago');
    expect(timeAgo(ago(40 * 86400000), now)).toBe(new Date(now - 40 * 86400000).toLocaleDateString());
    expect(timeAgo(ago(-60000), now)).toBe('');
    expect(timeAgo('not a date', now)).toBe('');
    expect(timeAgo(new Date().toISOString())).toBe('just now');
  });
});

describe('invitations', () => {
  const now = Date.parse('2026-11-13T00:00:00Z');
  const at = (ms) => new Date(now + ms).toISOString();

  it('says when one expires', () => {
    expect(expiresIn(at(6 * 86400000 + 1000), now)).toBe('in 6 days');
    expect(expiresIn(at(86400000), now)).toBe('in 1 day');
    expect(expiresIn(at(3 * 3600000), now)).toBe('in 3 hours');
    expect(expiresIn(at(3600000), now)).toBe('in 1 hour');
    expect(expiresIn(at(12 * 60000), now)).toBe('in 12 minutes');
    expect(expiresIn(at(30000), now)).toBe('in 1 minute');
    expect(expiresIn(at(-1), now)).toBe('expired');
    expect(expiresIn('not a date', now)).toBe('expired');
    expect(expiresIn(new Date(Date.now() + 3 * 86400000 + 60000).toISOString())).toBe('in 3 days');
  });

  it('shows the daily cap in the server\'s words, with a fallback', () => {
    const cap = Object.assign(new Error('this team has reached its limit of 10 invitations per day'), { status: 429 });
    expect(inviteError(cap)).toBe(cap.message);
    const mine = Object.assign(new Error('you have reached your limit of 20 invitations per day across all your teams'), { status: 429 });
    expect(inviteError(mine)).toBe(mine.message);
    expect(inviteError(Object.assign(new Error(''), { status: 429 }))).toMatch(/as many invitations as may be sent today/);
    expect(inviteError(new Error('email is not valid'))).toBe('email is not valid');
    expect(inviteError(null)).toBe('Failed to send the invitation');
  });

  it('reads 404 and 403 as a team you can no longer see', () => {
    expect(lostTeam({ status: 404 })).toBe(true);
    expect(lostTeam({ status: 403 })).toBe(true);
    expect(lostTeam({ status: 500 })).toBe(false);
    expect(lostTeam(undefined)).toBe(false);
  });

  it('names the accept page\'s state', () => {
    expect(invitationState(null, null)).toBe('loading');
    expect(invitationState({ emailMatches: true }, null)).toBe('ready');
    expect(invitationState({ teamName: 'Core' }, null)).toBe('ready');
    expect(invitationState({ emailMatches: false }, null)).toBe('wrongEmail');
    expect(invitationState(null, { status: 403 })).toBe('wrongEmail');
    expect(invitationState(null, { status: 410 })).toBe('invalid');
    expect(invitationState(null, { status: 404 })).toBe('invalid');
    expect(invitationState(null, { status: 500 })).toBe('error');
  });
});

describe('useTeamMode', () => {
  async function mountAt(path) {
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:p(.*)*', component: { render: () => null } }] });
    await router.push(path);
    const pinia = createPinia();
    setActivePinia(pinia);
    let mode;
    const app = createApp({ setup() { mode = useTeamMode(); return () => h('div'); } });
    app.use(router).use(pinia).mount(document.createElement('div'));
    return { mode, router, app, store: useTeamStore() };
  }

  it('reads the team from the route, and keeps links in its mode', async () => {
    const { mode, store, app } = await mountAt('/tasks/active?team=tm1');
    store.teams = [{ id: 'tm1', name: 'Core' }];

    expect(mode.teamId.value).toBe('tm1');
    expect(mode.isTeamMode.value).toBe(true);
    expect(mode.team.value.name).toBe('Core');
    expect(mode.link('/kanban')).toEqual({ path: '/kanban', query: { team: 'tm1' } });
    app.unmount();
  });

  it('is Me with no team in the route', async () => {
    const { mode, app } = await mountAt('/tasks/active');

    expect(mode.isTeamMode.value).toBe(false);
    expect(mode.team.value).toBeNull();
    expect(mode.link('/kanban?x=1')).toEqual({ path: '/kanban', query: { x: '1' } });
    app.unmount();
  });

  it('returns to Me, saying why, when you are no longer in the team', async () => {
    const { mode, store, router, app } = await mountAt('/tasks/active?team=tm1');
    store.teams = [{ id: 'tm1', name: 'Core' }];
    rememberTeam('tm1');
    const { toasts } = useToasts();

    await mode.leaveTeam();
    await nextTick();

    expect(toasts.value.at(-1).message).toBe("You're no longer in Core");
    expect(store.teams).toEqual([]);
    expect(rememberedTeam()).toBe('');
    expect(router.currentRoute.value.fullPath).toBe('/');
    app.unmount();
  });

  it('says so without a name when the team was never loaded', async () => {
    const { mode, app } = await mountAt('/tasks/active?team=gone');
    const { toasts } = useToasts();

    await mode.leaveTeam();

    expect(toasts.value.at(-1).message).toBe("You're no longer in that team");
    app.unmount();
  });

  it('leaves once when a page\'s reads fail together', async () => {
    const { mode, router, app } = await mountAt('/tasks/active?team=tm1');
    const { toasts } = useToasts();
    const before = toasts.value.length;

    const first = mode.leaveTeam();
    expect(mode.leaveTeam()).toBe(first);
    await first;

    expect(toasts.value.length).toBe(before + 1);
    expect(router.currentRoute.value.fullPath).toBe('/');
    app.unmount();
  });
});

describe('changeFailed', () => {
  async function mountAt(path) {
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:p(.*)*', component: { render: () => null } }] });
    await router.push(path);
    const pinia = createPinia();
    setActivePinia(pinia);
    let mode;
    const app = createApp({ setup() { mode = useTeamMode(); return () => h('div'); } });
    app.use(router).use(pinia).mount(document.createElement('div'));
    return { mode, router, app, store: useTeamStore() };
  }
  const conflict = Object.assign(new Error('someone changed this team'), { status: 409, conflict: true, version: 9 });

  it('reads the team again on a conflict, and says so', async () => {
    const { mode, store, app } = await mountAt('/tasks/active?team=tm1');
    store.teams = [{ id: 'tm1', name: 'Core', version: 8 }];
    api.getTeam.mockResolvedValue({ id: 'tm1', name: 'Infra', version: 9 });
    const { toasts } = useToasts();

    expect(await mode.changeFailed(conflict)).toBe('conflict');

    expect(api.getTeam).toHaveBeenCalledWith('tm1');
    expect(store.getTeam('tm1')).toMatchObject({ name: 'Infra', version: 9 });
    expect(toasts.value.at(-1)).toMatchObject({ message: 'Someone changed this team, refreshed', type: 'info' });
    app.unmount();
  });

  it('still says so when the team cannot be read again', async () => {
    const { mode, app } = await mountAt('/tasks/active?team=tm1');
    api.getTeam.mockRejectedValue(new Error('down'));
    const { toasts } = useToasts();

    expect(await mode.changeFailed(conflict, 'tm1')).toBe('conflict');
    expect(toasts.value.at(-1).message).toBe('Someone changed this team, refreshed');
    app.unmount();
  });

  it('goes back to Me when the team on screen is gone', async () => {
    const { mode, router, app } = await mountAt('/tasks/active?team=tm1');

    expect(await mode.changeFailed({ status: 404 })).toBe('lost');
    expect(router.currentRoute.value.fullPath).toBe('/');
    app.unmount();
  });

  it('shows anything else in the server\'s words, as for another team\'s 404', async () => {
    const { mode, router, app } = await mountAt('/tasks/active?team=tm1');
    const { toasts } = useToasts();

    expect(await mode.changeFailed(Object.assign(new Error('gone'), { status: 404 }), 'tm2')).toBe('error');
    expect(toasts.value.at(-1)).toMatchObject({ message: 'gone', type: 'error' });
    expect(await mode.changeFailed(undefined)).toBe('error');
    expect(toasts.value.at(-1).message).toBe('Something went wrong');
    expect(router.currentRoute.value.fullPath).toBe('/tasks/active?team=tm1');
    app.unmount();
  });
});

describe('the team store\'s version', () => {
  it('reads one team again, adding it when it was not known', async () => {
    const store = useTeamStore();
    store.teams = [{ id: 'tm1', name: 'Core', version: 1, memberCount: 2 }];
    api.getTeam.mockResolvedValueOnce({ id: 'tm1', name: 'Core', version: 3 });

    await store.refresh('tm1');
    expect(store.getTeam('tm1')).toMatchObject({ version: 3, memberCount: 2 });

    api.getTeam.mockResolvedValueOnce({ id: 'tm2', name: 'Alpha', version: 1 });
    await store.refresh('tm2');
    expect(store.teams.map((t) => t.id)).toEqual(['tm2', 'tm1']);
  });

  it('moves a team on to the version an answer names, and ignores one that names none', () => {
    const store = useTeamStore();
    store.teams = [{ id: 'tm1', name: 'Core', version: 1 }];

    store.setVersion('tm1', 4);
    expect(store.getTeam('tm1').version).toBe(4);
    store.setVersion('tm1', undefined);
    store.setVersion('tm1', null);
    store.setVersion('tm9', 2);
    expect(store.getTeam('tm1').version).toBe(4);
    expect(store.getTeam('tm9')).toBeFalsy();
  });
});

describe('the team routes', () => {
  it('are in the one route table, with no page for settings, analytics or memory', () => {
    const paths = routes.map((r) => r.path);
    for (const p of ['/team/new', '/team/settings', '/invitations', '/invite']) {
      expect(paths).toContain(p);
    }
    expect(paths).not.toContain('/team/analytics');
    expect(paths).not.toContain('/team/memory');
    expect(routes.find((r) => r.path === '/team/settings').component).toBeUndefined();
  });
});

describe('a team\'s events on your account stream', () => {
  it('reads team.<type> for the team on screen as <type>, and nothing else', () => {
    const ev = { type: 'team.task.updated', teamId: 'tm1', payload: { id: 't1' } };
    expect(teamEvent(ev, 'tm1')).toEqual({ type: 'task.updated', teamId: 'tm1', payload: { id: 't1' } });
    expect(teamEvent(ev, 'tm2')).toBe(null);
    expect(teamEvent(ev, '')).toBe(null);
    expect(teamEvent({ type: 'task.updated', payload: {} }, 'tm1')).toBe(null);
    expect(teamEvent(undefined, 'tm1')).toBe(null);
  });

  it('keeps a task to the workspace and member filters, any when unset', () => {
    const t = { sharedName: ' Backend', owner: { userId: 'u2' } };
    expect(inTeamFilter(t)).toBe(true);
    expect(inTeamFilter(t, { workspace: 'backend' })).toBe(true);
    expect(inTeamFilter(t, { workspace: 'Web' })).toBe(false);
    expect(inTeamFilter(t, { member: 'u2' })).toBe(true);
    expect(inTeamFilter(t, { member: 'u3' })).toBe(false);
    expect(inTeamFilter({}, { workspace: 'Web' })).toBe(false);
    expect(inTeamFilter(undefined, { member: 'u2' })).toBe(false);
  });
});
