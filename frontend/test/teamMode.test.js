// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

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
  mePath,
  teamPath,
  legacyTeamLink,
  teamHome,
  teamLanding,
  sharedNameOf,
  sameSharedName,
  teamWorkspaceList,
  teamOwners,
  memberChoices,
  workspaceChoices,
  preferredWorkspace,
  ownTeamWorkspace,
  isVersionConflict,
  CONFLICT_NOTICE,
  SETTINGS_TABS,
  teamSettings,
  settingsTabOf,
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
  it('is the /teams/<id> the path starts with, and nothing else', () => {
    expect(teamIdOf({ path: '/teams/tm1/tasks/active', query: {} })).toBe('tm1');
    expect(teamIdOf({ path: '/teams/tm1' })).toBe('tm1');
    expect(teamIdOf({ path: '/teams/a%20b/kanban' })).toBe('a b');
    expect(teamIdOf({ path: '/tasks/active', query: { team: 'tm1' } })).toBe('');
    expect(teamIdOf({ path: '/teamsx/tm1' })).toBe('');
    expect(teamIdOf(undefined)).toBe('');
  });

  it('is the page with no team, for Me', () => {
    expect(mePath('/teams/tm1/tasks/active')).toBe('/tasks/active');
    expect(mePath('/teams/tm1')).toBe('/');
    expect(mePath('/kanban')).toBe('/kanban');
    expect(mePath()).toBe('/');
  });

  it('puts the page under the team, or takes it out for Me', () => {
    expect(withTeam('/tasks/active', 'tm1')).toEqual({ path: '/teams/tm1/tasks/active', query: {} });
    expect(withTeam('/tasks/active?group=docs', 'tm1')).toEqual({ path: '/teams/tm1/tasks/active', query: { group: 'docs' } });
    expect(withTeam({ path: '/teams/tm1/kanban', query: { member: 'u1' } }, '')).toEqual({ path: '/kanban', query: { member: 'u1' } });
    expect(withTeam({ path: '/teams/tm1/kanban' }, 'tm2')).toEqual({ path: '/teams/tm2/kanban', query: {} });
    expect(withTeam({ path: '/kanban' }, 7)).toEqual({ path: '/teams/7/kanban', query: {} });
    expect(withTeam('/', 'tm1')).toEqual({ path: '/teams/tm1', query: {} });
    expect(teamPath('a/b', '/pano')).toBe('/teams/a%2Fb/pano');
  });

  it('starts team mode on the team\'s active tasks', () => {
    expect(teamHome('tm1')).toEqual({ path: '/teams/tm1/tasks/active' });
  });

  it('sends an old ?team= link to its page under the team', () => {
    expect(legacyTeamLink({ path: '/tasks/active/w1/t1', query: { team: 'tm1', workspace: 'api' }, hash: '#c' }))
      .toEqual({ path: '/teams/tm1/tasks/active/w1/t1', query: { workspace: 'api' }, hash: '#c', replace: true });
    expect(legacyTeamLink({ path: '/team/settings', query: { team: 'tm1', tab: 'invitations' } }).path).toBe('/teams/tm1/settings');
    expect(legacyTeamLink({ path: '/team/pano/d1', query: { team: 'tm1' } }).path).toBe('/teams/tm1/pano/d1');
    expect(legacyTeamLink({ path: '/team/analytics', query: { team: 'tm1' } }).path).toBe('/teams/tm1/analytics');
    expect(legacyTeamLink({ path: '/', query: { team: 'tm1' } }).path).toBe('/teams/tm1/tasks/active');
    expect(legacyTeamLink({ path: '/kanban', query: {} })).toBeUndefined();
    expect(legacyTeamLink({ path: '/kanban', query: { team: '' } })).toBeUndefined();
    expect(legacyTeamLink({ path: '/kanban', query: { team: ['a', 'b'] } })).toBeUndefined();
  });
});

describe('landing on the home page', () => {
  const first = { matched: [] };
  const later = { matched: [{}] };

  it('follows an old ?team= link, wherever it points', () => {
    expect(teamLanding({ path: '/', query: { team: 'tm1' } }, later, '')).toMatchObject(teamHome('tm1'));
    expect(teamLanding({ path: '/kanban', query: { team: 'tm1' } }, first, 'tm2')).toMatchObject({ path: '/teams/tm1/kanban' });
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

describe('your own workspace in a team workspace', () => {
  it('is yours of its name, never archived or a fork, else none', () => {
    const mine = [
      { id: 'a', name: 'backend', archivedAt: '2026-01-01' },
      { id: 'f', name: 'Backend', forkOfId: 'c' },
      { id: 'c', name: ' Backend ' },
    ];
    expect(ownTeamWorkspace(mine, 'BACKEND')).toBe('c');
    expect(ownTeamWorkspace(mine, 'docs')).toBe('');
    expect(ownTeamWorkspace(mine, '')).toBe('');
    expect(ownTeamWorkspace()).toBe('');
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

describe('the team settings page in the URL', () => {
  it('is /teams/<id>/settings?tab=<tab>, on members by default', () => {
    expect(teamSettings('tm1', 'invitations')).toEqual({ path: '/teams/tm1/settings', query: { tab: 'invitations' } });
    expect(teamSettings(7)).toEqual({ path: '/teams/7/settings', query: { tab: 'members' } });
  });

  it('opens on one of its tabs, and on members for anything else', () => {
    for (const tab of SETTINGS_TABS) expect(settingsTabOf({ query: { tab } })).toBe(tab);
    expect(SETTINGS_TABS).toEqual(['members', 'invitations', 'activity', 'workspaces', 'danger']);
    expect(settingsTabOf({ query: { tab: 'settings' } })).toBe('members');
    expect(settingsTabOf({ query: {} })).toBe('members');
    expect(settingsTabOf(undefined)).toBe('members');
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
    expect(line('workspace.shareChanged', { details: { workspaceName: 'docs', tasks: 'on', skills: 'off', memory: 'on' } }))
      .toBe('Alice changed what is shared of docs: tasks, memory');
    expect(line('workspace.shareChanged', { details: { tasks: 'off', skills: 'off', memory: 'off' } }))
      .toBe('Alice changed what is shared of a workspace: nothing but the workspace');
    expect(line('workspace.shareChanged', { details: { workspaceName: 'docs', tasks: 'on', skills: 'on', memory: 'on', autoOptInSkills: 'off' } }))
      .toBe('Alice changed what is shared of docs: tasks, memory; new team skills stay off');
    expect(line('workspace.shareChanged', { details: { workspaceName: 'docs', tasks: 'on', autoOptInSkills: 'on' } }))
      .toBe('Alice changed what is shared of docs: tasks; new team skills are turned on');
    expect(line('workspace.shared', { details: { workspaceName: 'docs', autoOptInSkills: 'on' } })).toBe('Alice shared docs');
    expect(line('workspace.shared', { details: { workspaceName: 'docs', autoOptInSkills: 'off' } })).toBe('Alice shared docs; new team skills stay off');
    expect(line('workspace.unshared')).toBe('Alice stopped sharing a workspace');
    const skill = { skill: 'deploy', teamWorkspace: 'backend' };
    expect(line('teamSkill.created', { details: skill })).toBe('Alice created the team skill deploy in backend');
    expect(line('teamSkill.created')).toBe('Alice created a team skill');
    expect(line('teamSkill.deleted', { details: skill })).toBe('Alice deleted the team skill deploy from backend');
    expect(line('teamSkill.given', { details: skill })).toBe('Alice gave the skill deploy to backend');
    expect(line('teamSkill.given')).toBe('Alice gave a skill to a team workspace');
    expect(line('teamSkill.optedIn', { details: skill })).toBe('Alice opted a workspace into the team skill deploy');
    expect(line('teamSkill.optedOut', { details: skill })).toBe('Alice opted a workspace out of the team skill deploy');
    expect(line('teamWorkspace.created', { details: { name: 'api' } })).toBe('Alice started the team workspace api');
    expect(line('teamWorkspace.created')).toBe('Alice started a team workspace');
    expect(line('pano.shared', { details: { docTitle: 'Plan' } })).toBe('Alice shared the Pano doc Plan');
    expect(line('pano.unshared')).toBe('Alice stopped sharing a Pano doc');
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
    const { mode, store, app } = await mountAt('/teams/tm1/tasks/active');
    store.teams = [{ id: 'tm1', name: 'Core' }];

    expect(mode.teamId.value).toBe('tm1');
    expect(mode.isTeamMode.value).toBe(true);
    expect(mode.team.value.name).toBe('Core');
    expect(mode.link('/kanban')).toEqual({ path: '/teams/tm1/kanban', query: {} });
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
    const { mode, store, router, app } = await mountAt('/teams/tm1/tasks/active');
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
    const { mode, app } = await mountAt('/teams/gone/tasks/active');
    const { toasts } = useToasts();

    await mode.leaveTeam();

    expect(toasts.value.at(-1).message).toBe("You're no longer in that team");
    app.unmount();
  });

  it('leaves once when a page\'s reads fail together', async () => {
    const { mode, router, app } = await mountAt('/teams/tm1/tasks/active');
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
    const { mode, store, app } = await mountAt('/teams/tm1/tasks/active');
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
    const { mode, app } = await mountAt('/teams/tm1/tasks/active');
    api.getTeam.mockRejectedValue(new Error('down'));
    const { toasts } = useToasts();

    expect(await mode.changeFailed(conflict, 'tm1')).toBe('conflict');
    expect(toasts.value.at(-1).message).toBe('Someone changed this team, refreshed');
    app.unmount();
  });

  it('goes back to Me when the team on screen is gone', async () => {
    const { mode, router, app } = await mountAt('/teams/tm1/tasks/active');

    expect(await mode.changeFailed({ status: 404 })).toBe('lost');
    expect(router.currentRoute.value.fullPath).toBe('/');
    app.unmount();
  });

  it('shows anything else in the server\'s words, as for another team\'s 404', async () => {
    const { mode, router, app } = await mountAt('/teams/tm1/tasks/active');
    const { toasts } = useToasts();

    expect(await mode.changeFailed(Object.assign(new Error('gone'), { status: 404 }), 'tm2')).toBe('error');
    expect(toasts.value.at(-1)).toMatchObject({ message: 'gone', type: 'error' });
    expect(await mode.changeFailed(undefined)).toBe('error');
    expect(toasts.value.at(-1).message).toBe('Something went wrong');
    expect(router.currentRoute.value.fullPath).toBe('/teams/tm1/tasks/active');
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
  it('are in the one route table, with no page for memory', () => {
    const paths = routes.map((r) => r.path);
    for (const p of ['/team/new', '/teams/:teamId/settings', '/teams/:teamId/analytics', '/teams/:teamId/pano', '/teams/:teamId/pano/:docId', '/invitations', '/invite']) {
      expect(paths).toContain(p);
    }
    expect(paths.filter((p) => p.endsWith('/memory'))).toEqual([]);
    expect(paths.filter((p) => p.startsWith('/team/'))).toEqual(['/team/new']);
  });

  it('opens a team on its active tasks', () => {
    const { redirect } = routes.find((r) => r.path === '/teams/:teamId');
    expect(redirect({ path: '/teams/tm1' })).toBe('/teams/tm1/tasks/active');
  });

  it('shows the pages a team shares with Me under /teams/<id>, as the same page', () => {
    for (const p of ['/tasks/:filter', '/kanban']) {
      const mine = routes.find((r) => r.path === p);
      const team = routes.find((r) => r.path === `/teams/:teamId${p}`);
      expect(team).toEqual({ ...mine, path: `/teams/:teamId${p}` });
    }
    // Only a team has a Skills page: your own are on each workspace's Skills tab.
    for (const p of ['/skills', '/skills/:name']) {
      expect(routes.find((r) => r.path === p), p).toBeUndefined();
      expect(routes.find((r) => r.path === `/teams/:teamId${p}`), p).toBeDefined();
    }
    const workspace = routes.find((r) => r.path === '/workspaces/:id');
    const teamWorkspace = routes.find((r) => r.path === '/teams/:teamId/workspaces/:id');
    expect(teamWorkspace.component).toBe(workspace.component);
    expect(teamWorkspace.children).toEqual([workspace.children.find((c) => c.path === 'tasks/:taskId')]);
    expect(routes.find((r) => r.path === '/teams/:teamId/pano').component).not.toBe(routes.find((r) => r.path === '/pano').component);
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
