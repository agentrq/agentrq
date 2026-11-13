// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// The team-mode pieces outside the settings and invitation pages: the switch,
// the sidebar and bar, sharing, cloning, and the team's analytics, memory and
// skills pages. Mounted for real, against a mocked API.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createApp, defineComponent, h, nextTick } from 'vue';
import { createPinia, setActivePinia } from 'pinia';
import { createRouter, createMemoryHistory } from 'vue-router';

vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  listTeams: vi.fn(),
  listMyInvitations: vi.fn(),
  fetchTeamWorkspaces: vi.fn(),
  fetchWorkspaces: vi.fn(),
  listWorkspaceShares: vi.fn(),
  shareWorkspace: vi.fn(),
  unshareWorkspace: vi.fn(),
  cloneTeamTask: vi.fn(),
  fetchTeamStats: vi.fn(),
  getTeamMemory: vi.fn(),
  saveTeamMemory: vi.fn(),
  fetchTeamMemoryIndex: vi.fn(),
  getTeamMemberMemory: vi.fn(),
  fetchTeamSkills: vi.fn(),
  getTeamSkill: vi.fn(),
  addTeamSkill: vi.fn(),
}));

// The panels draw charts jsdom cannot; what matters here is what the page
// hands them and does with what they emit.
vi.mock('../src/components/StatsPanels.vue', async () => {
  const { defineComponent: dc, h: hh } = await import('vue');
  return {
    default: dc({
      props: ['stats', 'loading', 'activeRange', 'customFrom', 'customTo'],
      emits: ['update:activeRange', 'update:customFrom', 'update:customTo', 'apply'],
      setup(props, { emit, slots }) {
        return () => hh('div', { 'data-test': 'stats-panels', 'data-range': props.activeRange }, [
          hh('button', { 'data-test': 'range-custom', onClick: () => emit('update:activeRange', 'custom') }),
          hh('button', { 'data-test': 'custom-from', onClick: () => emit('update:customFrom', '2026-01-01') }),
          hh('button', { 'data-test': 'custom-to', onClick: () => emit('update:customTo', '2026-01-31') }),
          slots['after-summary']?.(),
        ]);
      },
    }),
  };
});

import * as api from '../src/api';
import { useTeamStore, rememberedTeam, rememberTeam } from '../src/stores/teamStore';
import { useWorkspaceStore } from '../src/stores/workspaceStore';
import { useToasts } from '../src/composables/useToasts';
import { inviterName, joinedHome, teamOf, teamOwners, teamLanding } from '../src/composables/useTeamMode';
import TeamSwitcher from '../src/components/TeamSwitcher.vue';
import TeamSidebar from '../src/components/TeamSidebar.vue';
import TeamModeBar from '../src/components/TeamModeBar.vue';
import OwnerAvatar from '../src/components/OwnerAvatar.vue';
import WorkspaceSharing from '../src/components/WorkspaceSharing.vue';
import CloneTaskModal from '../src/components/CloneTaskModal.vue';
import TeamSkills from '../src/components/TeamSkills.vue';
import TeamAnalyticsView from '../src/views/TeamAnalyticsView.vue';
import TeamMemoryView from '../src/views/TeamMemoryView.vue';

const alice = { userId: 'u1', name: 'Alice', picture: 'https://x/a.png' };
const bob = { userId: 'u2', name: 'Bob' };

const flush = async () => {
  for (let i = 0; i < 3; i += 1) {
    await new Promise((resolve) => setTimeout(resolve, 0));
    await nextTick();
  }
};

const failure = (status, message = 'nope') => Object.assign(new Error(message), { status });

let app;
let el;
let router;

async function mount(component, { path = '/', props = {} } = {}) {
  el = document.createElement('div');
  document.body.appendChild(el);
  router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:rest(.*)*', component: { render: () => null } }],
  });
  await router.push(path);
  await router.isReady();
  const pinia = createPinia();
  setActivePinia(pinia);
  const Host = defineComponent({ render: () => h(component, props) });
  app = createApp(Host).use(pinia).use(router);
  app.directive('click-outside', {});
  app.mount(el);
  await flush();
  return el;
}

const $ = (sel) => document.body.querySelector(sel);
const $$ = (sel) => [...document.body.querySelectorAll(sel)];
// Every text node apart, as a reader sees adjacent elements.
const text = (node) => {
  if (!node) return undefined;
  const walker = document.createTreeWalker(node, NodeFilter.SHOW_TEXT);
  const parts = [];
  while (walker.nextNode()) parts.push(walker.currentNode.nodeValue);
  return parts.join(' ').replace(/\s+/g, ' ').trim();
};
const click = async (node) => {
  node.click();
  await flush();
};
const here = () => router.currentRoute.value;
const toasts = () => useToasts().toasts.value.map((t) => t.message);

beforeEach(() => {
  localStorage.clear();
  vi.clearAllMocks();
  useToasts().toasts.value = [];
  api.listTeams.mockResolvedValue({ teams: [] });
  api.listMyInvitations.mockResolvedValue({ invitations: [] });
  api.fetchTeamWorkspaces.mockResolvedValue({ workspaces: [] });
});

afterEach(() => {
  app?.unmount();
  app = null;
  el?.remove();
  document.body.innerHTML = '';
});

describe('the rest of the team helpers', () => {
  it('lands a new member on the team with its welcome', () => {
    expect(joinedHome(7)).toEqual({ path: '/tasks/active', query: { team: '7', joined: '1' } });
  });

  it('reads a team answer either way', () => {
    expect(teamOf({ team: { id: 't' } })).toEqual({ id: 't' });
    expect(teamOf({ id: 't' })).toEqual({ id: 't' });
    expect(teamOf(undefined)).toBe(null);
  });

  it('lists owners by name, unnamed ones first', () => {
    expect(teamOwners([{ owner: { userId: 'a' } }, { owner: bob }, { owner: alice }, {}])).toEqual([{ userId: 'a' }, alice, bob]);
    expect(teamOwners([{ owner: bob }, { owner: { userId: 'b' } }])).toEqual([{ userId: 'b' }, bob]);
    expect(teamOwners()).toEqual([]);
    // An owner known by id alone, or by nothing, is still one owner.
    expect(teamOwners([{ owner: { id: 'x', name: 'X' } }, { owner: {} }, { owner: {} }])).toEqual([{}, { id: 'x', name: 'X' }]);
  });

  it('names who sent an invitation', () => {
    const members = [{ userId: 'u1', name: 'Dev' }];
    expect(inviterName({ invitedBy: 'u1', invitedByName: 'Ann' }, members)).toBe('Ann');
    expect(inviterName({ invitedBy: 'u1' }, members)).toBe('Dev');
    expect(inviterName({ invitedBy: 'u9' }, members)).toBe('u9');
    expect(inviterName({ invitedBy: 'u9' })).toBe('u9');
    expect(inviterName(null)).toBe('');
  });

  it('leaves a home page in Me with nothing remembered', () => {
    expect(teamLanding({ path: '/', query: {} }, { matched: [] }, '')).toBeUndefined();
  });
});

describe('an owner\'s avatar', () => {
  it('is a picture, or an initial, with the name when labelled', async () => {
    await mount(defineComponent({
      render: () => h('div', [
        h(OwnerAvatar, { owner: alice, label: true, size: 'md' }),
        h(OwnerAvatar, { owner: { email: 'zed@x.com' }, label: true }),
        h(OwnerAvatar, {}),
      ]),
    }));
    const [a, z, none] = $$('[data-test=owner-avatar]');
    expect(a.querySelector('img').getAttribute('src')).toBe(alice.picture);
    expect(a.getAttribute('title')).toBe('Alice');
    expect(text(a)).toBe('Alice');
    expect(text(z)).toBe('Z zed@x.com');
    expect(z.getAttribute('title')).toBe('zed@x.com');
    expect(text(none)).toBe('?');
    expect(none.getAttribute('title')).toBe('');
  });
});

describe('the Me | Team switch', () => {
  const core = { id: 'tm1', name: 'Core', role: 'owner' };
  const docs = { id: 'tm2', name: 'Docs', role: 'member' };

  async function withTeams(teams, invitations = [], opts = {}) {
    await mount(TeamSwitcher, opts);
    const store = useTeamStore();
    store.teams = teams;
    store.invitations = invitations;
    await flush();
    return store;
  }

  it('offers to create a team when you have none', async () => {
    await withTeams([]);
    expect(text($('[data-test=switch-team]'))).toBe('Team');
    expect($('[data-test=team-badge]')).toBe(null);
    await click($('[data-test=switch-team]'));
    expect(here().path).toBe('/team/new');
  });

  it('opens the remembered team, else the first, and remembers the choice', async () => {
    rememberTeam('tm2');
    await withTeams([core, docs]);
    expect(text($('[data-test=switch-team]'))).toBe('Team: Docs');
    await click($('[data-test=switch-team]'));
    expect(here().fullPath).toBe('/tasks/active?team=tm2');
    expect(rememberedTeam()).toBe('tm2');

    rememberTeam('gone');
    app.unmount();
    await withTeams([core, docs]);
    expect(text($('[data-test=switch-team]'))).toBe('Team: Core');
  });

  it('goes back to Me from team mode, and forgets the team', async () => {
    rememberTeam('tm1');
    await withTeams([core], [], { path: '/tasks/active?team=tm1' });
    expect(text($('[data-test=switch-team]'))).toBe('Team: Core');
    await click($('[data-test=switch-me]'));
    expect(here().path).toBe('/');
    expect(rememberedTeam()).toBe('');
  });

  it('stays put choosing Me while already in Me', async () => {
    await withTeams([core], [], { path: '/workspaces/w1' });
    await click($('[data-test=switch-me]'));
    expect(here().path).toBe('/workspaces/w1');
  });

  it('counts invitations on a badge that opens them', async () => {
    await withTeams([], [{ id: 'i1' }, { id: 'i2' }]);
    const badge = $('[data-test=team-badge]');
    expect(text(badge)).toBe('2');
    expect(badge.getAttribute('title')).toBe('2 pending invitations');
    expect(badge.getAttribute('href')).toBe('/invitations');
    useTeamStore().invitations = [{ id: 'i1' }];
    await flush();
    expect($('[data-test=team-badge]').getAttribute('title')).toBe('1 pending invitation');
  });

  it('lists your teams with your role, and the team pages, in its menu', async () => {
    await withTeams([core, docs], [{ id: 'i1' }], { path: '/tasks/active?team=tm1' });
    await click($('[data-test=switch-menu]'));
    const menu = $('[data-test=team-menu]');
    expect(text(menu)).toContain('Your teams');
    expect(text(menu)).toContain('Core owner');
    expect(text(menu)).toContain('Docs member');
    expect(text(menu)).toContain('Invitations (1)');
    expect(menu.querySelectorAll('svg path[d="M5 13l4 4L19 7"]').length).toBe(1);

    const item = (label) => [...menu.querySelectorAll('[role=menuitem]')].find((b) => text(b).includes(label));
    await click(item('Docs'));
    expect(here().fullPath).toBe('/tasks/active?team=tm2');
    expect($('[data-test=team-menu]')).toBe(null);

    await click($('[data-test=switch-menu]'));
    await click([...$$('[role=menuitem]')].find((b) => text(b) === 'Team settings'));
    expect(here().fullPath).toBe('/team/settings?team=tm2');

    await click($('[data-test=switch-menu]'));
    await click([...$$('[role=menuitem]')].find((b) => text(b).startsWith('Invitations')));
    expect(here().path).toBe('/invitations');

    await click($('[data-test=switch-menu]'));
    await click([...$$('[role=menuitem]')].find((b) => text(b) === 'Create team'));
    expect(here().path).toBe('/team/new');
  });

  it('has no team pages to offer before you are in one', async () => {
    await withTeams([]);
    await click($('[data-test=switch-menu]'));
    expect(text($('[data-test=team-menu]'))).not.toContain('Your teams');
    expect(text($('[data-test=team-menu]'))).not.toContain('Team settings');
  });

  it('is one button with its menu beside it in a folded sidebar', async () => {
    await withTeams([core], [{ id: 'i1' }], { path: '/tasks/active?team=tm1', props: { collapsed: true } });
    const button = $('[data-test=team-switcher] > button');
    expect(button.getAttribute('aria-label')).toBe('Team: Core');
    expect(text($('[data-test=team-badge]'))).toBe('1');
    await click(button);
    const me = [...$$('[role=menuitem]')].find((b) => text(b) === 'Me');
    expect(me.querySelector('svg')).toBe(null);
    await click(me);
    expect(here().path).toBe('/');
  });

  it('marks Me in the folded menu while in Me', async () => {
    await withTeams([], [], { props: { collapsed: true } });
    const button = $('[data-test=team-switcher] > button');
    expect(button.getAttribute('aria-label')).toBe('Me');
    expect($('[data-test=team-badge]')).toBe(null);
    await click(button);
    expect([...$$('[role=menuitem]')].find((b) => text(b) === 'Me').querySelector('svg')).not.toBe(null);
    await click(button);
    expect($('[data-test=team-menu]')).toBe(null);
  });
});

describe('the team sidebar', () => {
  it('lists the team\'s pages and its groups, with owners', async () => {
    api.fetchTeamWorkspaces.mockResolvedValue({
      workspaces: [
        { id: 'w1', name: 'api', group: 'Backend', owner: alice },
        { id: 'w2', name: 'docs', owner: bob },
        { id: 'w6', name: 'docs', owner: { id: 'o9', name: 'Fi' } },
        { id: 'w3', name: 'b2', group: 'Backend', owner: { userId: 'u3', name: 'Cy' } },
        { id: 'w4', name: 'b3', group: 'Backend', owner: { userId: 'u4', name: 'Di' } },
        { id: 'w5', name: 'b4', group: 'Backend', owner: { userId: 'u5', name: 'Ed' } },
      ],
    });
    const showTooltip = vi.fn();
    const hideTooltip = vi.fn();
    await mount(TeamSidebar, { path: '/tasks/active?team=tm1', props: { showTooltip, hideTooltip } });
    expect(api.fetchTeamWorkspaces).toHaveBeenCalledWith('tm1');

    const links = $$('[data-test=team-sidebar] > a').map((a) => [text(a), a.getAttribute('href')]);
    expect(links).toEqual([
      ['Tasks', '/tasks/active?team=tm1'],
      ['Kanban', '/kanban?team=tm1'],
      ['Analytics', '/team/analytics?team=tm1'],
      ['Memory', '/team/memory?team=tm1'],
      ['Skills', '/skills?team=tm1'],
      ['Settings', '/team/settings?team=tm1'],
      ['Backend C D +1', '/tasks/active?group=Backend&team=tm1'],
      ['docs B F', '/tasks/active?group=docs&team=tm1'],
    ]);
    expect($$('[data-test=team-group]')[0].querySelectorAll('[data-test=owner-avatar]').length).toBe(3);
    expect($$('[data-test=team-sidebar] > a')[0].classList.contains('bg-gray-200')).toBe(true);

    const first = $$('[data-test=team-sidebar] > a')[0];
    first.dispatchEvent(new Event('mouseenter'));
    first.dispatchEvent(new Event('mouseleave'));
    $$('[data-test=team-group]')[0].dispatchEvent(new Event('mouseenter'));
    $$('[data-test=team-group]')[0].dispatchEvent(new Event('mouseleave'));
    expect(showTooltip).toHaveBeenCalledTimes(2);
    expect(hideTooltip).toHaveBeenCalledTimes(2);

    for (const [path, label] of [
      ['/tasks/active?team=tm1&group=docs', 'docs B F'],
      ['/kanban?team=tm1', 'Kanban'],
      ['/team/analytics?team=tm1', 'Analytics'],
      ['/team/memory?team=tm1', 'Memory'],
      ['/skills/x?team=tm1', 'Skills'],
      ['/team/settings?team=tm1', 'Settings'],
    ]) {
      await router.push(path);
      await flush();
      const active = [...$$('[data-test=team-sidebar] a')].filter((a) => a.classList.contains('bg-gray-200'));
      expect(active.map(text), path).toEqual([label]);
    }
  });

  it('says when nothing is shared, and folds to icons', async () => {
    await mount(TeamSidebar, { path: '/tasks/active?team=tm1', props: { expanded: false } });
    expect($('[data-test=team-sidebar-empty]')).toBe(null);
    expect(text($('[data-test=team-sidebar] > a'))).toBe('');
    app.unmount();
    await mount(TeamSidebar, { path: '/tasks/active?team=tm1' });
    expect(text($('[data-test=team-sidebar-empty]'))).toBe('Nothing shared yet.');
  });

  it('goes back to Me when you are no longer in the team, and not for other failures', async () => {
    api.fetchTeamWorkspaces.mockRejectedValueOnce(failure(500));
    await mount(TeamSidebar, { path: '/tasks/active?team=tm1' });
    expect(here().path).toBe('/tasks/active');

    api.fetchTeamWorkspaces.mockRejectedValueOnce(failure(403));
    await router.push('/tasks/active?team=tm2');
    await flush();
    expect(here().fullPath).toBe('/');
    expect(toasts()).toContain("You're no longer in that team");
  });
});

describe('the team-mode bar', () => {
  it('says whose work this is, and goes back to Me', async () => {
    rememberTeam('tm1');
    await mount(TeamModeBar, { path: '/tasks/active?team=tm1', props: { name: 'Core' } });
    expect(text($('[data-test=team-mode-bar]'))).toBe('Viewing Core (read-only) Back to Me');
    expect($('[data-test=joined-banner]')).toBe(null);
    await click([...$$('a')].find((a) => text(a) === 'Back to Me'));
    expect(here().path).toBe('/');
    expect(rememberedTeam()).toBe('');
  });

  it('offers to share once, right after joining', async () => {
    await mount(TeamModeBar, { path: '/tasks/active?team=tm1&joined=1' });
    const banner = $('[data-test=joined-banner]');
    expect(banner.textContent.replace(/\s+/g, ' ').trim()).toBe('You joined Team. Share your workspaces?Share');
    expect(banner.querySelector('a').getAttribute('href')).toBe('/team/settings?team=tm1&tab=workspaces');
    await click(banner.querySelector('button'));
    expect(here().fullPath).toBe('/tasks/active?team=tm1');
    expect($('[data-test=joined-banner]')).toBe(null);
  });
});

describe('sharing a workspace', () => {
  const core = { id: 'tm1', name: 'Core', role: 'owner' };
  const docs = { id: 'tm2', name: 'Docs', role: 'member' };
  const api1 = { id: 'w1', name: 'api' };
  const fork = { id: 'w2', name: 'api-fork', forkOfId: 'w1' };

  async function inWorkspace(shares, { teams = [core, docs], workspaces = [api1, fork], id = 'w1' } = {}) {
    api.listWorkspaceShares.mockResolvedValue(shares);
    await mount(defineComponent({
      setup() {
        useTeamStore().teams = teams;
        useWorkspaceStore().workspaces = workspaces;
        return () => h(WorkspaceSharing, { workspaceId: id });
      },
    }));
  }

  const rows = () => $$('[data-test=share-row]');
  const row = (label) => rows().find((r) => text(r).startsWith(label));

  it('lists your teams with the workspace\'s share in each', async () => {
    await inWorkspace({ shares: [{ teamId: 'tm1', group: 'Backend' }, { teamId: 'tm2' }] });
    expect(api.listWorkspaceShares).toHaveBeenCalledWith('w1');
    expect(rows().length).toBe(2);
    expect(row('Core').querySelector('[data-test=share-switch]').getAttribute('aria-checked')).toBe('true');
    expect(row('Core').querySelector('[data-test=share-group]').value).toBe('Backend');
    expect(row('Docs').querySelector('[data-test=share-group]').value).toBe('');
    expect(row('Core').querySelector('[data-test=share-switch]').getAttribute('aria-label')).toBe('Share api with Core');
  });

  it('reads an answer with no shares as none', async () => {
    await inWorkspace({});
    expect(row('Core').querySelector('[data-test=share-switch]').getAttribute('aria-checked')).toBe('false');
    expect(row('Core').querySelector('[data-test=share-group]')).toBe(null);
  });

  it('says so when you are in no team', async () => {
    await inWorkspace({ shares: [] }, { teams: [] });
    expect(text($('[data-test=sharing-empty]'))).toBe('You are not in a team yet. Create one or accept an invitation to share this workspace.');
  });

  it('greys a fork out, with no switch: it follows its parent', async () => {
    await inWorkspace({ shares: [] }, { id: 'w2' });
    expect(text(row('Core').querySelector('[data-test=share-fork]'))).toBe('Shared with parent');
    expect(row('Core').querySelector('[data-test=share-switch]')).toBe(null);
    expect(row('Core').className).toContain('opacity-60');
  });

  it('shares with the team group of the same name prefilled', async () => {
    api.fetchTeamWorkspaces.mockResolvedValue({ workspaces: [{ id: 'x', name: 'other', group: 'API', owner: bob }] });
    api.shareWorkspace.mockResolvedValue({});
    await inWorkspace({ shares: [] });
    await click(row('Core').querySelector('[data-test=share-switch]'));
    expect(api.shareWorkspace).toHaveBeenCalledWith('w1', 'tm1', 'API');
    expect(row('Core').querySelector('[data-test=share-group]').value).toBe('API');
    expect([...row('Core').querySelectorAll('datalist option')].map((o) => o.value)).toEqual(['API']);
    expect(toasts()).toContain('Shared api with Core');
  });

  it('shares under its own name when the team\'s groups cannot be read', async () => {
    api.fetchTeamWorkspaces.mockRejectedValue(failure(500));
    api.shareWorkspace.mockResolvedValue({});
    await inWorkspace({ shares: [] });
    await click(row('Docs').querySelector('[data-test=share-switch]'));
    expect(api.shareWorkspace).toHaveBeenCalledWith('w1', 'tm2', 'api');
  });

  it('says why a share failed, and stays off', async () => {
    api.shareWorkspace.mockRejectedValue(failure(500, 'Share failed'));
    await inWorkspace({ shares: [] });
    await click(row('Core').querySelector('[data-test=share-switch]'));
    expect(toasts()).toEqual(['Share failed']);
    expect(row('Core').querySelector('[data-test=share-switch]').getAttribute('aria-checked')).toBe('false');
    expect(row('Core').querySelector('[data-test=share-switch]').disabled).toBe(false);
  });

  it('renames the group it shows in, or puts back its name when cleared', async () => {
    api.shareWorkspace.mockResolvedValue({});
    await inWorkspace({ shares: [{ teamId: 'tm1', group: 'Backend' }] });
    const input = row('Core').querySelector('[data-test=share-group]');
    input.value = '  Platform ';
    input.dispatchEvent(new Event('change'));
    await flush();
    expect(api.shareWorkspace).toHaveBeenLastCalledWith('w1', 'tm1', 'Platform');
    input.value = 'Platform';
    input.dispatchEvent(new Event('change'));
    await flush();
    expect(api.shareWorkspace).toHaveBeenCalledTimes(1);
    input.value = '';
    input.dispatchEvent(new Event('change'));
    await flush();
    expect(api.shareWorkspace).toHaveBeenLastCalledWith('w1', 'tm1', 'api');
  });

  it('asks before it stops sharing, and stops at once on yes', async () => {
    api.unshareWorkspace.mockResolvedValue({});
    await inWorkspace({ shares: [{ teamId: 'tm1', group: 'api' }] });
    await click(row('Core').querySelector('[data-test=share-switch]'));
    expect(document.body.textContent).toContain('Teammates in Core will stop seeing api and its tasks immediately.');
    const cancel = [...$$('button')].find((b) => text(b) === 'Cancel');
    await click(cancel);
    expect(api.unshareWorkspace).not.toHaveBeenCalled();

    await click(row('Core').querySelector('[data-test=share-switch]'));
    await click([...$$('button')].filter((b) => text(b) === 'Stop sharing').pop());
    expect(api.unshareWorkspace).toHaveBeenCalledWith('w1', 'tm1');
    expect(row('Core').querySelector('[data-test=share-switch]').getAttribute('aria-checked')).toBe('false');
    expect(toasts()).toContain('Stopped sharing api with Core');
  });

  it('draws a workspace or team it does not know yet by id alone', async () => {
    api.listWorkspaceShares.mockResolvedValue({ shares: [] });
    await mount(defineComponent({
      setup() {
        useTeamStore().teams = [core];
        return () => h(WorkspaceSharing, { workspaceId: 'w9' });
      },
    }));
    expect(row('Core').querySelector('[data-test=share-switch]').getAttribute('aria-label')).toBe('Share  with Core');
    app.unmount();

    api.fetchWorkspaces.mockResolvedValue({ workspaces: [api1] });
    await mount(defineComponent({ render: () => h(WorkspaceSharing, { teamId: 'tm9' }) }));
    expect(row('api').querySelector('[data-test=share-switch]').getAttribute('aria-label')).toBe('Share api with ');
  });

  it('says so when the shares cannot be read', async () => {
    api.listWorkspaceShares.mockRejectedValue(failure(500, 'Could not read'));
    await mount(defineComponent({ render: () => h(WorkspaceSharing, { workspaceId: 'w1' }) }));
    expect(toasts()).toEqual(['Could not read']);
  });

  it('lists your workspaces in a team\'s settings, the shared ones on', async () => {
    api.fetchWorkspaces.mockResolvedValue({ workspaces: [api1, fork, { id: 'w3', name: 'old', archivedAt: '2026-01-01' }] });
    api.fetchTeamWorkspaces.mockResolvedValue({ workspaces: [{ id: 'w1', name: 'api', group: 'Backend' }, { id: 'zz', name: 'theirs' }, { id: 'w2', name: 'api-fork' }] });
    await mount(defineComponent({
      setup() {
        useTeamStore().teams = [core];
        return () => h(WorkspaceSharing, { teamId: 'tm1' });
      },
    }));
    expect(rows().map((r) => text(r).split(' ')[0])).toEqual(['api', 'api-fork']);
    expect(row('api ').querySelector('[data-test=share-group]').value).toBe('Backend');
    expect(row('api ').querySelector('[data-test=share-switch]').getAttribute('aria-label')).toBe('Share api with Core');
  });

  it('reads your workspaces once they are known, or says you have none', async () => {
    api.fetchTeamWorkspaces.mockResolvedValue({ workspaces: [{ id: 'w1' }] });
    await mount(defineComponent({
      setup() {
        useWorkspaceStore().workspaces = [api1];
        return () => h(WorkspaceSharing, { teamId: 'tm1' });
      },
    }));
    expect(api.fetchWorkspaces).not.toHaveBeenCalled();
    expect(row('api').querySelector('[data-test=share-group]').value).toBe('');
    app.unmount();

    api.fetchWorkspaces.mockResolvedValue({ workspaces: [] });
    await mount(defineComponent({ render: () => h(WorkspaceSharing, { teamId: 'tm1' }) }));
    expect(text($('[data-test=sharing-empty]'))).toBe('You have no workspaces to share.');
  });
});

describe('cloning a teammate\'s task', () => {
  const task = { id: 't1', title: 'Fix login' };
  const mine = [{ id: 'm1', name: 'scratch' }, { id: 'm2', name: 'API' }, { id: 'm3', name: 'old', archivedAt: 'x' }];

  async function open({ workspaces = mine, workspace = { name: 'api', group: 'Backend' }, show = true } = {}) {
    await mount(defineComponent({
      setup() {
        useWorkspaceStore().workspaces = workspaces;
        return () => h(CloneTaskModal, { show, teamId: 'tm1', task, workspace, onClose: closed });
      },
    }));
  }
  let closed;
  beforeEach(() => { closed = vi.fn(); });

  it('picks your workspace named like theirs, and clones into it', async () => {
    api.cloneTeamTask.mockResolvedValue({ task: { id: 'c1', workspaceId: 'm2' } });
    await open();
    expect(text($('[data-test=clone-modal]'))).toContain('A copy of Fix login');
    expect($('[data-test=clone-workspace]').value).toBe('m2');
    expect([...$('[data-test=clone-workspace]').options].map((o) => o.value)).toEqual(['m1', 'm2']);
    const picker = $('[data-test=clone-workspace]');
    picker.value = 'm1';
    picker.dispatchEvent(new Event('change'));
    picker.value = 'm2';
    picker.dispatchEvent(new Event('change'));
    const status = $('[data-test=clone-status]');
    status.value = 'ongoing';
    status.dispatchEvent(new Event('change'));
    await click($('[data-test=clone-confirm]'));
    expect(api.cloneTeamTask).toHaveBeenCalledWith('tm1', 't1', 'm2', 'ongoing');
    const toast = useToasts().toasts.value.at(-1);
    expect(toast.message).toBe('Task cloned');
    expect(toast.link).toEqual({ taskId: 'c1', workspaceId: 'm2', label: 'Open' });
    expect(closed).toHaveBeenCalled();
  });

  it('links the clone to the workspace chosen when the answer names none, or not at all', async () => {
    api.cloneTeamTask.mockResolvedValueOnce({ id: 'c2' }).mockResolvedValueOnce(undefined);
    await open({ workspace: null });
    expect($('[data-test=clone-workspace]').value).toBe('m1');
    await click($('[data-test=clone-confirm]'));
    expect(useToasts().toasts.value.at(-1).link).toEqual({ taskId: 'c2', workspaceId: 'm1', label: 'Open' });
    await click($('[data-test=clone-confirm]'));
    expect(useToasts().toasts.value.at(-1).link).toBe(null);
  });

  it('says why a clone failed, and stays open', async () => {
    api.cloneTeamTask.mockRejectedValue(failure(404, 'No longer shared'));
    await open();
    await click($('[data-test=clone-confirm]'));
    expect(toasts()).toEqual(['No longer shared']);
    expect(closed).not.toHaveBeenCalled();
    expect($('[data-test=clone-confirm]').disabled).toBe(false);
  });

  it('reads your workspaces when it opens without them, and offers to make one', async () => {
    api.fetchWorkspaces.mockResolvedValueOnce({ workspaces: [] }).mockRejectedValueOnce(failure(500));
    await open({ workspaces: [] });
    expect(api.fetchWorkspaces).toHaveBeenCalled();
    expect(text($('[data-test=clone-no-workspace]'))).toBe('You have no workspace to clone into. Create one first.');
    expect($('[data-test=clone-confirm]').disabled).toBe(true);
    await click($('[data-test=clone-no-workspace] a'));
    expect(closed).toHaveBeenCalled();
    app.unmount();
    await open({ workspaces: [] });
    expect($('[data-test=clone-no-workspace]')).not.toBe(null);
  });

  it('closes on its backdrop, Cancel or Escape, and draws nothing shut', async () => {
    await open();
    await click($('[data-test=clone-modal] > div'));
    await click([...$$('button')].find((b) => text(b) === 'Cancel'));
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    await flush();
    expect(closed).toHaveBeenCalledTimes(3);
    app.unmount();
    closed = vi.fn();
    await open({ show: false });
    expect($('[data-test=clone-modal]')).toBe(null);
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    await flush();
    expect(closed).not.toHaveBeenCalled();
  });
});

describe('a team\'s analytics', () => {
  it('reads the team\'s numbers, by group, with who did what', async () => {
    api.fetchTeamWorkspaces.mockResolvedValue({ workspaces: [{ id: 'w1', name: 'api', group: 'Backend' }, { id: 'w2', name: 'docs' }] });
    api.fetchTeamStats.mockResolvedValue({
      summary: {},
      byMember: [
        { owner: alice, tasksCompleted: 1200, messages: 4 },
        { userId: 'u2', name: 'Bob' },
        { owner: { name: 'Guest' }, tasksCompleted: 1 },
      ],
    });
    await mount(TeamAnalyticsView, { path: '/team/analytics?team=tm1' });
    expect(api.fetchTeamStats).toHaveBeenCalledWith('tm1', expect.any(String), 0, 0, '');
    expect([...$('[data-test=team-analytics-group]').options].map((o) => o.value)).toEqual(['', 'Backend', 'docs']);
    expect($$('[data-test=team-member-row]').map(text)).toEqual([`Alice ${(1200).toLocaleString()} 4`, 'B Bob 0 0', 'G Guest 1 0']);

    const select = $('[data-test=team-analytics-group]');
    select.value = 'Backend';
    select.dispatchEvent(new Event('change'));
    await flush();
    expect(here().fullPath).toBe('/team/analytics?team=tm1&group=Backend');
    expect(api.fetchTeamStats).toHaveBeenLastCalledWith('tm1', expect.any(String), 0, 0, 'Backend');

    select.value = '';
    select.dispatchEvent(new Event('change'));
    await flush();
    expect(here().fullPath).toBe('/team/analytics?team=tm1');
  });

  it('loads a custom range once both ends are set', async () => {
    api.fetchTeamStats.mockResolvedValue({ summary: {} });
    await mount(TeamAnalyticsView, { path: '/team/analytics?team=tm1' });
    expect($('[data-test=team-by-member]')).toBe(null);
    await click($('[data-test=range-custom]'));
    const calls = api.fetchTeamStats.mock.calls.length;
    await click($('[data-test=custom-from]'));
    expect(api.fetchTeamStats.mock.calls.length).toBe(calls);
    await click($('[data-test=custom-to]'));
    expect(api.fetchTeamStats.mock.calls.length).toBe(calls + 1);
  });

  it('goes back to Me without the team, and says other failures', async () => {
    api.fetchTeamWorkspaces.mockRejectedValue(failure(500));
    api.fetchTeamStats.mockRejectedValueOnce(failure(500, 'Stats down'));
    await mount(TeamAnalyticsView, { path: '/team/analytics?team=tm1' });
    expect(toasts()).toContain('Stats down');
    app.unmount();
    api.fetchTeamStats.mockRejectedValueOnce(failure(404));
    await mount(TeamAnalyticsView, { path: '/team/analytics?team=tm1' });
    expect(here().path).toBe('/');
  });
});

describe('a team\'s memory', () => {
  const index = {
    memories: [
      { workspaceId: 'w1', workspaceName: 'api', group: 'Backend', owner: alice, names: ['deploy.md'] },
      { workspaceId: 'w2', workspaceName: 'docs', group: 'docs', owner: bob },
      { workspaceId: 'w3', workspaceName: 'web', owner: bob, names: [] },
    ],
  };

  async function open({ memory = { content: '# Team', updatedAt: '2026-11-01T00:00:00Z' }, memories = index } = {}) {
    api.getTeamMemory.mockResolvedValue(memory);
    api.fetchTeamMemoryIndex.mockResolvedValue(memories);
    await mount(TeamMemoryView, { path: '/team/memory?team=tm1' });
  }

  it('edits the team\'s memory.md, saving only a change', async () => {
    api.saveTeamMemory.mockResolvedValue({ updatedAt: '2026-11-02T00:00:00Z' });
    await open();
    const editor = $('[data-test=team-memory-editor]');
    expect(editor.value).toBe('# Team');
    expect($('[data-test=team-memory-save]').disabled).toBe(true);
    editor.value = '# Team\nMore';
    editor.dispatchEvent(new Event('input'));
    await flush();
    await click($('[data-test=team-memory-save]'));
    expect(api.saveTeamMemory).toHaveBeenCalledWith('tm1', '# Team\nMore', '2026-11-01T00:00:00Z');
    expect(toasts()).toContain('Team memory saved');
    expect($('[data-test=team-memory-save]').disabled).toBe(true);
    expect(text(document.body)).toContain(`Saved ${new Date('2026-11-02T00:00:00Z').toLocaleString()}`);
  });

  it('says when someone saved first, and replaces theirs only on a second save', async () => {
    api.saveTeamMemory.mockRejectedValueOnce(failure(409)).mockResolvedValueOnce(undefined);
    await open();
    const editor = $('[data-test=team-memory-editor]');
    editor.value = 'mine';
    editor.dispatchEvent(new Event('input'));
    await flush();
    await click($('[data-test=team-memory-save]'));
    expect($('[data-test=team-memory-conflict]')).not.toBe(null);
    await click($('[data-test=team-memory-save]'));
    expect(api.saveTeamMemory).toHaveBeenLastCalledWith('tm1', 'mine', '');
    expect($('[data-test=team-memory-conflict]')).toBe(null);
    expect(text(document.body)).toContain(`Saved ${new Date('2026-11-01T00:00:00Z').toLocaleString()}`);
  });

  it('loads theirs over a conflict', async () => {
    api.saveTeamMemory.mockRejectedValue(failure(409));
    await open();
    const editor = $('[data-test=team-memory-editor]');
    editor.value = 'mine';
    editor.dispatchEvent(new Event('input'));
    await flush();
    await click($('[data-test=team-memory-save]'));
    api.getTeamMemory.mockResolvedValue({ content: 'theirs', updatedAt: '2026-11-03T00:00:00Z' });
    await click($('[data-test=team-memory-reload]'));
    expect($('[data-test=team-memory-editor]').value).toBe('theirs');
    expect($('[data-test=team-memory-conflict]')).toBe(null);
  });

  it('says why a save failed, or goes back to Me without the team', async () => {
    api.saveTeamMemory.mockRejectedValueOnce(failure(500, 'Save failed')).mockRejectedValueOnce(failure(403));
    await open();
    const editor = $('[data-test=team-memory-editor]');
    editor.value = 'x';
    editor.dispatchEvent(new Event('input'));
    await flush();
    await click($('[data-test=team-memory-save]'));
    expect(toasts()).toContain('Save failed');
    await click($('[data-test=team-memory-save]'));
    expect(here().path).toBe('/');
  });

  it('starts empty, and says when it cannot be read', async () => {
    await open({ memory: null, memories: null });
    expect($('[data-test=team-memory-editor]').value).toBe('');
    expect(text($('[data-test=team-memory-index-empty]'))).toBe('No shared workspace has memories yet.');
    expect(text(document.body)).not.toContain('Saved');
    app.unmount();
    api.getTeamMemory.mockRejectedValue(failure(500, 'Memory down'));
    await mount(TeamMemoryView, { path: '/team/memory?team=tm1' });
    expect(text($('[data-test=team-memory-error]'))).toBe('Memory down');
    app.unmount();
    api.getTeamMemory.mockRejectedValue(failure(404));
    await mount(TeamMemoryView, { path: '/team/memory?team=tm1' });
    expect(here().path).toBe('/');
  });

  it('lists the members\' memories by group, and opens one read-only', async () => {
    await open();
    expect($$('[data-test=team-memory-workspace]').map(text)).toEqual(['Backend api deploy.md', 'B docs', 'B web']);
    api.getTeamMemberMemory.mockResolvedValueOnce({ content: '# Deploy' }).mockResolvedValueOnce(undefined).mockRejectedValueOnce(failure(500, 'Gone'));
    await click($('[data-test=team-memory-item]'));
    expect(api.getTeamMemberMemory).toHaveBeenCalledWith('tm1', 'w1', 'deploy.md');
    expect($('[data-test=team-memory-opened] .md-body').innerHTML).toContain('Deploy');
    await click($('[data-test=team-memory-item]'));
    expect($('[data-test=team-memory-opened] .md-body').innerHTML).toBe('');
    await click($('[data-test=team-memory-item]'));
    expect(text($('[data-test=team-memory-opened]'))).toContain('Gone');
    await click([...$$('[data-test=team-memory-opened] button')].find((b) => text(b) === 'Close'));
    expect($('[data-test=team-memory-opened]')).toBe(null);
  });

  it('says when the index cannot be read, or goes back to Me', async () => {
    api.getTeamMemory.mockResolvedValue({});
    api.fetchTeamMemoryIndex.mockRejectedValueOnce(failure(500, 'Index down'));
    await mount(TeamMemoryView, { path: '/team/memory?team=tm1' });
    expect(toasts()).toContain('Index down');
    app.unmount();
    api.fetchTeamMemoryIndex.mockRejectedValueOnce(failure(403));
    await mount(TeamMemoryView, { path: '/team/memory?team=tm1' });
    expect(here().path).toBe('/');
  });
});

describe('a team\'s skills', () => {
  it('lists the skills its shared workspaces use, with who has each', async () => {
    api.fetchTeamSkills.mockResolvedValue({ skills: [{ name: 'deploy', description: 'Ship it', owners: [alice, { name: 'Anon' }] }, { name: 'lint' }] });
    await mount(TeamSkills, { path: '/skills?team=tm1' });
    expect(api.fetchTeamSkills).toHaveBeenCalledWith('tm1');
    const rows = $$('[data-test=team-skill-row]');
    expect(rows.map((r) => r.querySelector('a').getAttribute('href'))).toEqual(['/skills/deploy?team=tm1', '/skills/lint?team=tm1']);
    expect(rows[0].querySelectorAll('[data-test=owner-avatar]').length).toBe(2);
  });

  it('says when there are none, or none could be read', async () => {
    api.fetchTeamSkills.mockResolvedValueOnce({}).mockRejectedValueOnce(failure(500, 'Skills down')).mockRejectedValueOnce(failure(404));
    await mount(TeamSkills, { path: '/skills?team=tm1' });
    expect(text($('[data-test=team-skills-empty]'))).toBe('No skill is on in a shared workspace yet.');
    app.unmount();
    await mount(TeamSkills, { path: '/skills?team=tm1' });
    expect(text($('[data-test=team-skill-error]'))).toBe('Skills down');
    app.unmount();
    await mount(TeamSkills, { path: '/skills?team=tm1' });
    expect(here().path).toBe('/');
  });

  it('shows one skill read-only, and adds a copy to yours', async () => {
    api.getTeamSkill.mockResolvedValue({ skill: { name: 'deploy', content: '---\nname: deploy\n---\n# Deploy steps', owners: [alice, { name: 'Anon' }] } });
    api.fetchWorkspaces.mockResolvedValue({ workspaces: [{ id: 'm1', name: 'scratch' }] });
    api.addTeamSkill.mockResolvedValue({});
    await mount(TeamSkills, { path: '/skills/deploy?team=tm1', props: { name: 'deploy' } });
    expect(api.getTeamSkill).toHaveBeenCalledWith('tm1', 'deploy');
    expect($('[data-test=team-skill-body]').innerHTML).toContain('Deploy steps');
    expect($('[data-test=team-skill-body]').innerHTML).not.toContain('name: deploy');
    expect($$('a').find((a) => text(a) === '← All skills').getAttribute('href')).toBe('/skills?team=tm1');

    await click($('[data-test=team-skill-add]'));
    expect(api.fetchWorkspaces).toHaveBeenCalled();
    const box = $('[data-test=team-skill-workspace]');
    box.checked = true;
    box.dispatchEvent(new Event('change'));
    await click($('[data-test=team-skill-add-confirm]'));
    expect(api.addTeamSkill).toHaveBeenCalledWith('tm1', 'deploy', ['m1']);
    const toast = useToasts().toasts.value.at(-1);
    expect(toast.message).toBe('Added deploy to your skills');
    expect(toast.link).toEqual({ path: '/skills/deploy', label: 'Open' });
    expect($('[data-test=team-skill-add-form]')).toBe(null);
  });

  it('says why an add failed, and reads your workspaces only once', async () => {
    api.getTeamSkill.mockResolvedValue({ content: 'Body' });
    api.addTeamSkill.mockRejectedValue(failure(409, 'You already have deploy'));
    await mount(defineComponent({
      setup() {
        useWorkspaceStore().workspaces = [{ id: 'm1', name: 'scratch' }];
        return () => h(TeamSkills, { name: 'deploy' });
      },
    }), { path: '/skills/deploy?team=tm1' });
    expect(text($('[data-test=team-skills] h2'))).toBe('deploy');
    await click($('[data-test=team-skill-add]'));
    expect(api.fetchWorkspaces).not.toHaveBeenCalled();
    await click($('[data-test=team-skill-add-confirm]'));
    expect(text($('[data-test=team-skill-add-error]'))).toBe('You already have deploy');
    await click($('[data-test=team-skill-add]'));
    expect($('[data-test=team-skill-add-form]')).toBe(null);
  });

  it('adds with no workspace to turn it on in, and says when the skill is gone', async () => {
    api.getTeamSkill.mockResolvedValueOnce({ name: 'deploy', content: '' }).mockRejectedValueOnce(failure(404, 'No such skill'));
    api.fetchWorkspaces.mockRejectedValue(failure(500));
    await mount(TeamSkills, { path: '/skills/deploy?team=tm1', props: { name: 'deploy' } });
    await click($('[data-test=team-skill-add]'));
    expect($('[data-test=team-skill-add-form] fieldset')).toBe(null);
    app.unmount();
    await mount(TeamSkills, { path: '/skills/deploy?team=tm1', props: { name: 'deploy' } });
    expect(text($('[data-test=team-skill-error]'))).toBe('No such skill');
    expect(here().path).toBe('/skills/deploy');
  });

  it('says it is loading until the answer arrives', async () => {
    api.fetchTeamSkills.mockReturnValue(new Promise(() => {}));
    api.getTeamSkill.mockReturnValue(new Promise(() => {}));
    await mount(TeamSkills, { path: '/skills?team=tm1' });
    expect(text($('[data-test=team-skills]'))).toBe('Loading skills…');
    app.unmount();
    await mount(TeamSkills, { path: '/skills/x?team=tm1', props: { name: 'x' } });
    expect(text($('[data-test=team-skills]'))).toBe('← All skills Loading…');
  });
});
