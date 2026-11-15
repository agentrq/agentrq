// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// The team-mode pieces outside the Manage team dialog and the invitation
// pages: the switch, renaming in place, the sidebar and bar, the pickers,
// sharing, cloning, and the team's skills. Mounted for real, against a
// mocked API.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createApp, defineComponent, h, nextTick, ref } from 'vue';
import { createPinia, setActivePinia } from 'pinia';
import { createRouter, createMemoryHistory } from 'vue-router';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  listTeams: vi.fn(),
  listMyInvitations: vi.fn(),
  getTeam: vi.fn(),
  renameTeam: vi.fn(),
  fetchTeamWorkspaces: vi.fn(),
  fetchWorkspaces: vi.fn(),
  listWorkspaceShares: vi.fn(),
  shareWorkspace: vi.fn(),
  unshareWorkspace: vi.fn(),
  cloneTeamTask: vi.fn(),
  fetchTeamStats: vi.fn(),
  searchSkills: vi.fn(),
  listTeamWorkspaceSkills: vi.fn(),
}));

// No stream in a test: the team skills page only listens to it.
vi.mock('../src/useEventBus', () => ({
  useEventBus: () => ({ connect: () => {}, disconnect: () => {}, onEvent: () => () => {} }),
}));

// The panels' own drawing is tested with them; here, only what they emit.
vi.mock('../src/components/StatsPanels.vue', async () => {
  const { defineComponent: dc, h: hh } = await import('vue');
  return {
    default: dc({
      props: ['stats', 'loading', 'activeRange', 'customFrom', 'customTo'],
      emits: ['update:activeRange', 'update:customFrom', 'update:customTo', 'apply'],
      setup(props, { emit }) {
        return () => hh('div', { 'data-test': 'stats-panels', 'data-range': props.activeRange }, [
          hh('button', { 'data-test': 'range-custom', onClick: () => emit('update:activeRange', 'custom') }),
          hh('button', { 'data-test': 'custom-from', onClick: () => emit('update:customFrom', '2026-01-01') }),
          hh('button', { 'data-test': 'custom-to', onClick: () => emit('update:customTo', '2026-01-31') }),
        ]);
      },
    }),
  };
});

import * as api from '../src/api';
import { useTeamStore, rememberedTeam, rememberTeam } from '../src/stores/teamStore';
import { useWorkspaceStore } from '../src/stores/workspaceStore';
import { useToasts } from '../src/composables/useToasts';
import { inviterName, joinedHome, teamOwners, teamLanding } from '../src/composables/useTeamMode';
import { SEARCH_THRESHOLD, filterPickerItems, isTypeaheadKey, stepIndex, typeaheadIndex } from '../src/composables/usePicker';
import TeamSwitcher from '../src/components/TeamSwitcher.vue';
import TeamSidebar from '../src/components/TeamSidebar.vue';
import TeamModeBar from '../src/components/TeamModeBar.vue';
import TeamNameEditor from '../src/components/TeamNameEditor.vue';
import TeamTaskFilters from '../src/components/TeamTaskFilters.vue';
import ItemPicker from '../src/components/ItemPicker.vue';
import OwnerAvatar from '../src/components/OwnerAvatar.vue';
import WorkspaceSharing from '../src/components/WorkspaceSharing.vue';
import CloneTaskModal from '../src/components/CloneTaskModal.vue';
import TeamAnalyticsView from '../src/views/TeamAnalyticsView.vue';
import SkillsView from '../src/views/SkillsView.vue';
import TeamWorkspaceActions from '../src/components/TeamWorkspaceActions.vue';

const alice = { userId: 'u1', name: 'Alice', picture: 'https://x/a.png' };
const bob = { userId: 'u2', name: 'Bob' };

const flush = async () => {
  for (let i = 0; i < 3; i += 1) {
    await new Promise((resolve) => setTimeout(resolve, 0));
    await nextTick();
  }
};

const failure = (status, message = 'nope') => Object.assign(new Error(message), { status });
const conflict = () => Object.assign(new Error('someone changed this team: reload it and try again'), { status: 409, conflict: true, version: 9 });

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
const key = async (node, k, init = {}) => {
  node.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true, ...init }));
  await flush();
};
const type = async (input, value) => {
  input.value = value;
  input.dispatchEvent(new Event('input'));
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
  api.searchSkills.mockResolvedValue({ skills: [] });
});

afterEach(() => {
  app?.unmount();
  app = null;
  el?.remove();
  document.body.innerHTML = '';
  vi.restoreAllMocks();
});

describe('the rest of the team helpers', () => {
  it('lands a new member on the team with its welcome', () => {
    expect(joinedHome(7)).toEqual({ path: '/teams/7/tasks/active', query: { joined: '1' } });
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
        h(OwnerAvatar, { owner: bob, size: 'xs' }),
      ]),
    }));
    const [a, z, none, xs] = $$('[data-test=owner-avatar]');
    const circle = (el) => el.firstElementChild.className;
    expect(circle(a)).toMatch(/\bw-6 h-6\b.*\bring-1\b/);
    expect(circle(z)).toMatch(/\bw-4 h-4\b.*\bring-1\b/);
    // xs matches the 14px icons beside it on a kanban card, with no ring.
    expect(circle(xs)).toMatch(/\bw-3\.5 h-3\.5\b/);
    expect(circle(xs)).not.toMatch(/\bring-1\b/);
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
  const core = { id: 'tm1', name: 'Core', version: 3, memberCount: 2 };
  const docs = { id: 'tm2', name: 'Docs', version: 1, memberCount: 1 };

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
    expect(here().fullPath).toBe('/teams/tm2/tasks/active');
    expect(rememberedTeam()).toBe('tm2');

    rememberTeam('gone');
    app.unmount();
    await withTeams([core, docs]);
    expect(text($('[data-test=switch-team]'))).toBe('Team: Core');
  });

  it('goes back to Me from team mode, and forgets the team', async () => {
    rememberTeam('tm1');
    await withTeams([core], [], { path: '/teams/tm1/tasks/active' });
    expect(text($('[data-test=switch-team-current]'))).toBe('Team: Core');
    expect($('[data-test=switch-team]')).toBe(null);
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

  it('lists your teams by size, never by role, and opens the team settings', async () => {
    await withTeams([core, docs, { id: 'tm3', name: 'Zed' }], [{ id: 'i1' }], { path: '/teams/tm1/kanban' });
    await click($('[data-test=switch-menu]'));
    const menu = $('[data-test=team-menu]');
    expect(text(menu)).toContain('Your teams');
    expect(text(menu)).toContain('Core 2 members');
    expect(text(menu)).toContain('Docs 1 member');
    expect(text(menu)).not.toMatch(/owner|admin/);
    expect(text(menu)).toContain('Invitations (1)');
    expect(menu.querySelectorAll('svg path[d="M5 13l4 4L19 7"]').length).toBe(1);

    await click($('[data-test=manage-team]'));
    expect(here().fullPath).toBe('/teams/tm1/settings?tab=members');
    expect($('[data-test=team-menu]')).toBe(null);

    await click($('[data-test=switch-menu]'));
    const item = (label) => [...$$('[role=menuitem]')].find((b) => text(b).includes(label));
    await click(item('Docs'));
    expect(here().fullPath).toBe('/teams/tm2/tasks/active');

    await click($('[data-test=switch-menu]'));
    await click([...$$('[role=menuitem]')].find((b) => text(b).startsWith('Invitations')));
    expect(here().path).toBe('/invitations');

    await click($('[data-test=switch-menu]'));
    await click([...$$('[role=menuitem]')].find((b) => text(b) === 'Create team'));
    expect(here().path).toBe('/team/new');
  });

  it('opens the settings from Me of the team it would open, remembering it', async () => {
    rememberTeam('tm2');
    await withTeams([core, docs]);
    await click($('[data-test=switch-menu]'));
    await click($('[data-test=manage-team]'));
    expect(here().fullPath).toBe('/teams/tm2/settings?tab=members');
    expect(rememberedTeam()).toBe('tm2');
  });

  it('has no team to manage before you are in one', async () => {
    await withTeams([]);
    await click($('[data-test=switch-menu]'));
    expect(text($('[data-test=team-menu]'))).not.toContain('Your teams');
    expect($('[data-test=manage-team]')).toBe(null);
  });

  it('is one button with its menu beside it in a folded sidebar', async () => {
    await withTeams([core], [{ id: 'i1' }], { path: '/teams/tm1/tasks/active', props: { collapsed: true } });
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

describe('renaming a team in place', () => {
  const core = { id: 'tm1', name: 'Core', version: 3, memberCount: 2 };

  async function inSwitch() {
    await mount(TeamSwitcher, { path: '/teams/tm1/tasks/active' });
    useTeamStore().teams = [{ ...core }];
    await flush();
  }

  it('opens on a click of the name, and Escape or leaving the field puts it back', async () => {
    await inSwitch();
    const name = $('[data-test=team-rename]');
    expect(name.getAttribute('aria-label')).toBe('Rename Core');
    await click(name);
    const input = $('[data-test=team-rename-input]');
    expect(input.value).toBe('Core');
    expect(document.activeElement).toBe(input);

    await type(input, 'Infra');
    await key(input, 'Escape');
    expect($('[data-test=team-rename-input]')).toBe(null);
    expect(text($('[data-test=team-rename]'))).toBe('Core');

    await click($('[data-test=team-rename]'));
    $('[data-test=team-rename-input]').dispatchEvent(new Event('blur'));
    await flush();
    expect($('[data-test=team-rename-input]')).toBe(null);
    expect(api.renameTeam).not.toHaveBeenCalled();
  });

  it('saves on Enter, against the version it has, and moves to the new one', async () => {
    api.renameTeam.mockResolvedValue({ ...core, name: 'Infra', version: 4 });
    await inSwitch();
    await click($('[data-test=team-rename]'));
    await type($('[data-test=team-rename-input]'), '  Infra ');
    $('[data-test=team-rename-form]').dispatchEvent(new Event('submit'));
    await flush();

    expect(api.renameTeam).toHaveBeenCalledWith('tm1', 'Infra', 3);
    expect(useTeamStore().getTeam('tm1')).toMatchObject({ name: 'Infra', version: 4 });
    expect(text($('[data-test=switch-team-current]'))).toBe('Team: Infra');
    expect(toasts()).toEqual(['Team renamed']);
  });

  it('sends nothing for the same name, or none', async () => {
    await inSwitch();
    for (const value of ['Core', '   ']) {
      await click($('[data-test=team-rename]'));
      await type($('[data-test=team-rename-input]'), value);
      $('[data-test=team-rename-form]').dispatchEvent(new Event('submit'));
      await flush();
      expect($('[data-test=team-rename-input]')).toBe(null);
    }
    expect(api.renameTeam).not.toHaveBeenCalled();
  });

  it('reads the team again when someone changed it first, and says so', async () => {
    api.renameTeam.mockRejectedValue(conflict());
    api.getTeam.mockResolvedValue({ ...core, name: 'Platform', version: 9 });
    await inSwitch();
    await click($('[data-test=team-rename]'));
    await type($('[data-test=team-rename-input]'), 'Infra');
    $('[data-test=team-rename-form]').dispatchEvent(new Event('submit'));
    await flush();

    expect(api.getTeam).toHaveBeenCalledWith('tm1');
    expect(toasts()).toEqual(['Someone changed this team, refreshed']);
    expect(text($('[data-test=switch-team-current]'))).toBe('Team: Platform');
    expect(useTeamStore().getTeam('tm1').version).toBe(9);
  });

  it('says why any other rename failed', async () => {
    api.renameTeam.mockRejectedValue(failure(400, 'name is too long'));
    await inSwitch();
    await click($('[data-test=team-rename]'));
    await type($('[data-test=team-rename-input]'), 'Infra');
    $('[data-test=team-rename-form]').dispatchEvent(new Event('submit'));
    await flush();
    expect(toasts()).toEqual(['name is too long']);
    expect(text($('[data-test=team-rename]'))).toBe('Core');
  });

  it('cannot be opened for a team not loaded yet, which shows its fallback', async () => {
    await mount(TeamNameEditor, { props: { team: null, fallback: 'Team' } });
    expect($('[data-test=team-rename]').disabled).toBe(true);
    expect($('[data-test=team-rename]').getAttribute('aria-label')).toBe('Rename the team');
    expect(text($('[data-test=team-rename]'))).toBe('Team');
  });
});

describe('the team sidebar', () => {
  it('lists the team\'s pages, then its workspaces with their owners', async () => {
    api.fetchTeamWorkspaces.mockResolvedValue({
      workspaces: [
        { id: 'w1', name: 'api', sharedName: 'Backend', owner: alice },
        { id: 'w2', name: 'docs', owner: bob },
        { id: 'w6', name: 'docs', owner: { id: 'o9', name: 'Fi' } },
        { id: 'w3', name: 'b2', sharedName: 'backend ', owner: { userId: 'u3', name: 'Cy' } },
        { id: 'w4', name: 'b3', sharedName: 'Backend', owner: { userId: 'u4', name: 'Di' } },
        { id: 'w5', name: 'b4', sharedName: 'Backend', owner: { userId: 'u5', name: 'Ed' } },
      ],
    });
    const showTooltip = vi.fn();
    const hideTooltip = vi.fn();
    await mount(TeamSidebar, { path: '/teams/tm1/tasks/active', props: { showTooltip, hideTooltip } });
    expect(api.fetchTeamWorkspaces).toHaveBeenCalledWith('tm1');

    expect(text($('[data-test=team-sidebar]'))).toMatch(/^Team Tasks Kanban Pano Workspaces /);
    const links = $$('[data-test=team-sidebar] > a').map((a) => [text(a), a.getAttribute('href')]);
    expect(links).toEqual([
      ['Tasks', '/teams/tm1/tasks/active'],
      ['Kanban', '/teams/tm1/kanban'],
      ['Pano', '/teams/tm1/pano'],
      ['Backend C D +1', '/teams/tm1/tasks/active?workspace=Backend'],
      ['docs B F', '/teams/tm1/tasks/active?workspace=docs'],
    ]);
    expect($$('[data-test=team-workspace]')[0].querySelectorAll('[data-test=owner-avatar]').length).toBe(3);
    expect($$('[data-test=team-sidebar] > a')[0].classList.contains('bg-gray-200')).toBe(true);

    const first = $$('[data-test=team-sidebar] > a')[0];
    first.dispatchEvent(new Event('mouseenter'));
    first.dispatchEvent(new Event('mouseleave'));
    $$('[data-test=team-workspace]')[0].dispatchEvent(new Event('mouseenter'));
    $$('[data-test=team-workspace]')[0].dispatchEvent(new Event('mouseleave'));
    expect(showTooltip).toHaveBeenCalledTimes(2);
    expect(hideTooltip).toHaveBeenCalledTimes(2);

    for (const [path, label] of [
      ['/teams/tm1/tasks/active?workspace=docs', 'docs B F'],
      ['/teams/tm1/kanban', 'Kanban'],
      ['/teams/tm1/pano/d1', 'Pano'],
    ]) {
      await router.push(path);
      await flush();
      const active = [...$$('[data-test=team-sidebar] a')].filter((a) => a.classList.contains('bg-gray-200'));
      expect(active.map(text), path).toEqual([label]);
    }
  });

  it('says when nothing is shared, and folds to icons', async () => {
    await mount(TeamSidebar, { path: '/teams/tm1/tasks/active', props: { expanded: false } });
    expect($('[data-test=team-sidebar-empty]')).toBe(null);
    expect(text($('[data-test=team-sidebar] > a'))).toBe('');
    app.unmount();
    await mount(TeamSidebar, { path: '/teams/tm1/tasks/active' });
    expect(text($('[data-test=team-sidebar-empty]'))).toBe('Nothing shared yet.');
  });

  it('goes back to Me when you are no longer in the team, and not for other failures', async () => {
    api.fetchTeamWorkspaces.mockRejectedValueOnce(failure(500));
    await mount(TeamSidebar, { path: '/teams/tm1/tasks/active' });
    expect(here().path).toBe('/teams/tm1/tasks/active');

    api.fetchTeamWorkspaces.mockRejectedValueOnce(failure(403));
    await router.push('/teams/tm2/tasks/active');
    await flush();
    expect(here().fullPath).toBe('/');
    expect(toasts()).toContain("You're no longer in that team");
  });
});

describe('the team-mode bar', () => {
  it('says whose work this is, renames it in place, opens the team settings, and goes back to Me', async () => {
    rememberTeam('tm1');
    await mount(TeamModeBar, { path: '/teams/tm1/kanban', props: { name: 'Core' } });
    expect(text($('[data-test=team-mode-bar]'))).toBe('Viewing Core (read-only) Manage Back to Me');
    expect($('[data-test=team-rename]').disabled).toBe(true);
    useTeamStore().teams = [{ id: 'tm1', name: 'Core', version: 1 }];
    await flush();
    expect($('[data-test=team-rename]').disabled).toBe(false);
    expect($('[data-test=joined-banner]')).toBe(null);
    expect($('[data-test=team-bar-manage]').getAttribute('href')).toBe('/teams/tm1/settings?tab=members');

    await click([...$$('a')].find((a) => text(a) === 'Back to Me'));
    expect(here().path).toBe('/');
    expect(rememberedTeam()).toBe('');
  });

  it('offers to share once, right after joining, in the team settings', async () => {
    await mount(TeamModeBar, { path: '/teams/tm1/tasks/active?joined=1' });
    const banner = $('[data-test=joined-banner]');
    expect(banner.textContent.replace(/\s+/g, ' ').trim()).toBe('You joined Team. Share your workspaces?Share');
    expect(banner.querySelector('a').getAttribute('href')).toBe('/teams/tm1/settings?tab=workspaces');
    await click(banner.querySelector('button'));
    expect(here().fullPath).toBe('/teams/tm1/tasks/active');
    expect($('[data-test=joined-banner]')).toBe(null);
  });

  // A sticky footer under the page on desktop; on a phone the bottom is the
  // Menu pill's, so there it is the first thing in the card.
  it('follows the page as a sticky footer, and leads it on a phone', async () => {
    const app = readFileSync(resolve(__dirname, '../src/App.vue'), 'utf-8');
    expect(app.indexOf('<TeamModeBar')).toBeGreaterThan(app.indexOf('data-terminal-launch-area class'));
    await mount(TeamModeBar, { path: '/teams/tm1/kanban?joined=1' });
    const bar = $('[data-test=team-mode-bar]');
    expect(bar.className.split(' ')).toEqual(expect.arrayContaining(['order-first', 'md:order-none', 'md:sticky', 'md:bottom-0', 'page-surface']));
    expect(bar.firstElementChild.className.split(' ')).toEqual(expect.arrayContaining(['md:order-last', 'md:border-t']));
  });
});

describe('the picker', () => {
  const people = [
    { value: '', label: 'All members' },
    { value: 'u1', label: 'Alice', owner: alice },
    { value: 'u2', label: 'Bob', owner: bob },
  ];
  const many = 'Alpha Bravo Charlie Delta Echo Foxtrot Golf Hotel India'.split(' ').map((l) => ({ value: l.toLowerCase(), label: l }));

  let chosen;
  async function pick(items, props = {}, start = '') {
    chosen = ref(start);
    await mount(defineComponent({
      render: () => h('div', [
        h(ItemPicker, { items, modelValue: chosen.value, 'onUpdate:modelValue': (v) => { chosen.value = v; }, ariaLabel: 'Member', ...props }),
        h('p', { 'data-test': 'outside' }, 'elsewhere'),
      ]),
    }));
  }
  const trigger = () => $('[data-test=picker]');
  const options = () => $$('[data-test=picker-option]');
  const active = () => options().find((o) => o.dataset.active === 'true');

  it('shows the choice with its avatar, and ticks it in the open menu', async () => {
    await pick(people, {}, 'u1');
    expect(text(trigger())).toBe('Alice');
    expect(trigger().querySelector('[data-test=owner-avatar]')).not.toBe(null);
    expect(trigger().getAttribute('aria-expanded')).toBe('false');

    await click(trigger());
    expect(trigger().getAttribute('aria-expanded')).toBe('true');
    expect(options().map((o) => o.getAttribute('aria-selected'))).toEqual(['false', 'true', 'false']);
    expect(options()[1].querySelector('[data-test=picker-check]')).not.toBe(null);
    expect(active().dataset.value).toBe('u1');
    expect($('[data-test=picker-search]')).toBe(null);

    options()[2].dispatchEvent(new Event('mousemove'));
    await flush();
    expect(active().dataset.value).toBe('u2');
    await click(options()[2]);
    expect(chosen.value).toBe('u2');
    expect($('[data-test=picker-menu]')).toBe(null);
    expect(document.activeElement).toBe(trigger());
  });

  it('draws a team workspace with its owners, a few and a count', async () => {
    const owners = [alice, bob, { userId: 'u3', name: 'Cy' }, { userId: 'u4', name: 'Di' }];
    await pick([{ value: '', label: 'All workspaces' }, { value: 'api', label: 'api', owners }], { align: 'right' }, 'api');
    expect(trigger().querySelectorAll('[data-test=owner-avatar]').length).toBe(3);
    await click(trigger());
    expect($('[data-test=picker-menu]').className).toContain('right-0');
    expect(text(options()[1])).toContain('+1');
  });

  it('says what to choose when nothing is chosen, and fills a form row as a block', async () => {
    await pick([{ value: 'a', label: 'A' }], { block: true, placeholder: 'Pick one' }, 'zz');
    expect(text(trigger())).toBe('Pick one');
    expect(trigger().parentElement.className).toContain('w-full');
    expect(trigger().classList.contains('h-9')).toBe(true);
    await click(trigger());
    expect(active().dataset.value).toBe('a');
  });

  // In a toolbar, as tall as the status tabs beside it: 38px.
  it('stands as tall as the status tabs in a toolbar', async () => {
    await pick(people);
    expect(trigger().classList.contains('h-9.5')).toBe(true);
  });

  // On a narrow page: the icon, or the choice's avatar, and no label.
  it('folds to its icon on a narrow page', async () => {
    await pick(people, { icon: 'M1 1h2' });
    expect(trigger().classList.contains('@max-[48rem]:h-8.5')).toBe(true);
    expect($('[data-test=picker-icon] path').getAttribute('d')).toBe('M1 1h2');
    expect(trigger().querySelector('.truncate').classList.contains('@max-[48rem]:hidden')).toBe(true);
  });

  it('folds to the chosen avatar rather than its icon', async () => {
    await pick(people, { icon: 'M1 1h2' }, 'u1');
    expect($('[data-test=picker-icon]')).toBe(null);
    expect(trigger().querySelector('[data-test=owner-avatar]')).not.toBe(null);
  });

  it('never folds without an icon', async () => {
    await pick(people);
    expect($('[data-test=picker-icon]')).toBe(null);
    expect(trigger().className).not.toContain('@max-[48rem]');
  });

  it('opens from the keyboard, and moves, picks and closes as a select does', async () => {
    await pick(people);
    await key(trigger(), 'x');
    expect($('[data-test=picker-menu]')).toBe(null);
    await key(trigger(), 'ArrowDown');
    expect(active().dataset.value).toBe('');
    await key(trigger(), 'ArrowDown');
    expect(active().dataset.value).toBe('u1');
    await key(trigger(), 'ArrowUp');
    await key(trigger(), 'ArrowUp');
    expect(active().dataset.value).toBe('u2');
    await key(trigger(), 'Home');
    expect(active().dataset.value).toBe('');
    await key(trigger(), 'End');
    expect(active().dataset.value).toBe('u2');
    await key(trigger(), 'Enter');
    expect(chosen.value).toBe('u2');
    expect($('[data-test=picker-menu]')).toBe(null);

    await key(trigger(), ' ');
    expect($('[data-test=picker-menu]')).not.toBe(null);
    await key(trigger(), 'Tab');
    expect($('[data-test=picker-menu]')).toBe(null);

    await key(trigger(), 'Enter');
    await click(trigger());
    expect($('[data-test=picker-menu]')).toBe(null);
  });

  it('closes on Escape without closing the dialog around it', async () => {
    const outer = vi.fn();
    window.addEventListener('keydown', outer);
    try {
      await pick(people);
      await click(trigger());
      await key(trigger(), 'Escape');
      expect($('[data-test=picker-menu]')).toBe(null);
      expect(outer).not.toHaveBeenCalled();
    } finally {
      window.removeEventListener('keydown', outer);
    }
  });

  it('closes on a click outside it, and not on one inside', async () => {
    await pick(people);
    await click(trigger());
    $('[data-test=picker-menu]').dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    await flush();
    expect($('[data-test=picker-menu]')).not.toBe(null);
    $('[data-test=outside]').dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    await flush();
    expect($('[data-test=picker-menu]')).toBe(null);
    expect(document.activeElement).not.toBe(trigger());
  });

  it('jumps to an item by its first letters', async () => {
    const now = vi.spyOn(Date, 'now');
    await pick([{ value: 'a', label: 'Bea' }, { value: 'b', label: 'Ben' }, { value: 'c', label: 'Bob' }, { value: 'd', label: 'Cy' }]);
    await click(trigger());
    now.mockReturnValue(1000);
    await key(trigger(), 'b');
    expect(active().dataset.value).toBe('b');
    now.mockReturnValue(1100);
    await key(trigger(), 'o');
    expect(active().dataset.value).toBe('c');
    now.mockReturnValue(5000);
    await key(trigger(), 'b');
    expect(active().dataset.value).toBe('a');
    now.mockReturnValue(9000);
    await key(trigger(), 'z');
    expect(active().dataset.value).toBe('a');
    await key(trigger(), 'c', { ctrlKey: true });
    expect(active().dataset.value).toBe('a');
  });

  it('offers a search past eight items, which narrows the list', async () => {
    expect(SEARCH_THRESHOLD).toBe(8);
    await pick(many, {}, 'golf');
    await click(trigger());
    const search = $('[data-test=picker-search]');
    expect(document.activeElement).toBe(search);
    expect(active().dataset.value).toBe('golf');

    await type(search, 'ha');
    expect(options().map((o) => o.dataset.value)).toEqual(['alpha', 'charlie']);
    expect(active().dataset.value).toBe('alpha');
    // Typing in the box is a search, not a jump; Home and End stay in the box.
    await key(search, 'c');
    await key(search, 'End');
    expect(active().dataset.value).toBe('alpha');
    await key(search, 'ArrowDown');
    await key(search, 'Enter');
    expect(chosen.value).toBe('charlie');

    await click(trigger());
    await type($('[data-test=picker-search]'), 'zulu');
    expect(options()).toEqual([]);
    expect(text($('[data-test=picker-empty]'))).toBe('No match');
    await key($('[data-test=picker-search]'), 'Enter');
    expect(chosen.value).toBe('charlie');
    await key($('[data-test=picker-search]'), 'ArrowDown');
    expect(active()).toBeUndefined();
  });

  it('does not open while disabled', async () => {
    await pick(people, { disabled: true });
    await key(trigger(), 'ArrowDown');
    expect($('[data-test=picker-menu]')).toBe(null);
  });

  it('keeps its list logic in pure helpers', () => {
    expect(filterPickerItems(people, '  BO ')).toEqual([people[2]]);
    expect(filterPickerItems(people)).toBe(people);
    expect(filterPickerItems(undefined, 'x')).toEqual([]);
    expect(filterPickerItems([{ value: 'x' }], 'x')).toEqual([]);
    expect(stepIndex(0, 1, 0)).toBe(-1);
    expect(stepIndex(-1, 1, 3)).toBe(0);
    expect(stepIndex(-1, -1, 3)).toBe(2);
    expect(stepIndex(2, 1, 3)).toBe(0);
    expect(stepIndex(0, -1, 3)).toBe(2);
    expect(typeaheadIndex(people, '')).toBe(-1);
    expect(typeaheadIndex(people, 'b', 2)).toBe(2);
    expect(typeaheadIndex([{ value: 'x' }], 'b')).toBe(-1);
    expect(typeaheadIndex()).toBe(-1);
    expect(isTypeaheadKey({ key: 'a' })).toBe(true);
    expect(isTypeaheadKey({ key: ' ' })).toBe(false);
    expect(isTypeaheadKey({ key: 'ArrowDown' })).toBe(false);
    expect(isTypeaheadKey({ key: 'a', metaKey: true })).toBe(false);
    expect(isTypeaheadKey({ key: 'a', altKey: true })).toBe(false);
  });
});

describe('the team task filters', () => {
  it('pick a member and a team workspace into the URL, and clear them', async () => {
    api.fetchTeamWorkspaces.mockResolvedValue({ workspaces: [{ id: 'w1', name: 'api', owner: alice }] });
    await mount(defineComponent({
      setup() {
        useTeamStore().loadWorkspaces('tm1');
        return () => h(TeamTaskFilters, { align: 'left' });
      },
    }), { path: '/teams/tm1/kanban?member=u1&workspace=api' });
    expect(text($('[data-test=team-filter-member]'))).toContain('Alice');
    expect(text($('[data-test=team-filter-workspace]'))).toContain('api');
    // Both fold on a narrow page.
    for (const t of ['member', 'workspace']) expect($(`[data-test=team-filter-${t}]`).classList.contains('@max-[48rem]:h-8.5')).toBe(true);

    await click($('[data-test=team-filter-member]'));
    expect($('[data-test=team-filter-member-menu]').className).toContain('left-0');
    await click($('[data-test=team-filter-member-option][data-value=""]'));
    expect(here().fullPath).toBe('/teams/tm1/kanban?workspace=api');

    await click($('[data-test=team-filter-workspace]'));
    await click($('[data-test=team-filter-workspace-option][data-value=""]'));
    expect(here().fullPath).toBe('/teams/tm1/kanban');
    expect(text($('[data-test=team-filter-member]'))).toBe('All members');
  });
});

describe('sharing a workspace', () => {
  const core = { id: 'tm1', name: 'Core', version: 3 };
  const docs = { id: 'tm2', name: 'Docs', version: 1 };
  const api1 = { id: 'w1', name: 'api' };
  const fork = { id: 'w2', name: 'api-fork', forkOfId: 'w1' };

  async function inWorkspace(shares, { teams = [core, docs], workspaces = [api1, fork], id = 'w1' } = {}) {
    api.listWorkspaceShares.mockResolvedValue(shares);
    await mount(defineComponent({
      setup() {
        useTeamStore().teams = teams.map((t) => ({ ...t }));
        useWorkspaceStore().workspaces = workspaces;
        return () => h(WorkspaceSharing, { workspaceId: id });
      },
    }));
  }

  const rows = () => $$('[data-test=share-row]');
  const row = (label) => rows().find((r) => text(r).startsWith(label));
  const toggle = (label) => row(label).querySelector('[data-test=share-switch]');

  it('lists your teams with the workspace\'s share in each, and no name to show it under', async () => {
    await inWorkspace({ shares: [{ teamId: 'tm1' }] });
    expect(api.listWorkspaceShares).toHaveBeenCalledWith('w1');
    expect(rows().length).toBe(2);
    expect(toggle('Core').getAttribute('aria-checked')).toBe('true');
    expect(toggle('Docs').getAttribute('aria-checked')).toBe('false');
    expect(toggle('Core').getAttribute('aria-label')).toBe('Share api with Core');
    expect($('[data-test=workspace-sharing] input')).toBe(null);
    expect(text($('[data-test=workspace-sharing]'))).not.toContain('Show in team as');
  });

  it('reads an answer with no shares as none', async () => {
    await inWorkspace({});
    expect(toggle('Core').getAttribute('aria-checked')).toBe('false');
  });

  it('says so when you are in no team', async () => {
    await inWorkspace({ shares: [] }, { teams: [] });
    expect(text($('[data-test=sharing-empty]'))).toBe('You are not in a team yet. Create one or accept an invitation to share this workspace.');
  });

  it('greys a fork out, with no switch: it follows its parent', async () => {
    await inWorkspace({ shares: [] }, { id: 'w2' });
    expect(text(row('Core').querySelector('[data-test=share-fork]'))).toBe('Shared with parent');
    expect(toggle('Core')).toBe(null);
    expect(row('Core').className).toContain('opacity-60');
  });

  it('shares against the team\'s version, and moves to the one the answer names', async () => {
    api.shareWorkspace.mockResolvedValue({ teamId: 'tm1', version: 4 });
    await inWorkspace({ shares: [] });
    await click(toggle('Core'));
    expect(api.shareWorkspace).toHaveBeenCalledWith('w1', 'tm1', 3);
    expect(useTeamStore().getTeam('tm1').version).toBe(4);
    expect(toggle('Core').getAttribute('aria-checked')).toBe('true');
    expect(toasts()).toContain('Shared api with Core');
  });

  it('says why a share failed, and stays off', async () => {
    api.shareWorkspace.mockRejectedValue(failure(500, 'Share failed'));
    await inWorkspace({ shares: [] });
    await click(toggle('Core'));
    expect(toasts()).toEqual(['Share failed']);
    expect(toggle('Core').getAttribute('aria-checked')).toBe('false');
    expect(toggle('Core').disabled).toBe(false);
  });

  it('reads the team and the shares again when someone changed the team first', async () => {
    api.shareWorkspace.mockRejectedValue(conflict());
    api.getTeam.mockResolvedValue({ ...core, version: 9 });
    await inWorkspace({ shares: [] });
    api.listWorkspaceShares.mockResolvedValue({ shares: [{ teamId: 'tm1' }] });
    await click(toggle('Core'));
    expect(toasts()).toEqual(['Someone changed this team, refreshed']);
    expect(useTeamStore().getTeam('tm1').version).toBe(9);
    expect(api.listWorkspaceShares).toHaveBeenCalledTimes(2);
    expect(toggle('Core').getAttribute('aria-checked')).toBe('true');
  });

  it('asks before it stops sharing, and stops at once on yes, against the version', async () => {
    api.unshareWorkspace.mockResolvedValue({ version: 5 });
    await inWorkspace({ shares: [{ teamId: 'tm1' }] });
    await click(toggle('Core'));
    expect(document.body.textContent).toContain('Teammates in Core will stop seeing api and its tasks immediately.');
    await click([...$$('button')].find((b) => text(b) === 'Cancel'));
    expect(api.unshareWorkspace).not.toHaveBeenCalled();

    await click(toggle('Core'));
    await click([...$$('button')].filter((b) => text(b) === 'Stop sharing').pop());
    expect(api.unshareWorkspace).toHaveBeenCalledWith('w1', 'tm1', 3);
    expect(useTeamStore().getTeam('tm1').version).toBe(5);
    expect(toggle('Core').getAttribute('aria-checked')).toBe('false');
    expect(toasts()).toContain('Stopped sharing api with Core');
  });

  it('draws a workspace or team it does not know yet by id alone', async () => {
    api.listWorkspaceShares.mockResolvedValue({ shares: [] });
    api.shareWorkspace.mockResolvedValue(null);
    await mount(defineComponent({
      setup() {
        useTeamStore().teams = [{ id: 'tm1', name: 'Core' }];
        return () => h(WorkspaceSharing, { workspaceId: 'w9' });
      },
    }));
    expect(toggle('Core').getAttribute('aria-label')).toBe('Share  with Core');
    await click(toggle('Core'));
    expect(api.shareWorkspace).toHaveBeenCalledWith('w9', 'tm1', undefined);
    app.unmount();

    api.fetchWorkspaces.mockResolvedValue({ workspaces: [api1] });
    await mount(defineComponent({ render: () => h(WorkspaceSharing, { teamId: 'tm9' }) }));
    expect(toggle('api').getAttribute('aria-label')).toBe('Share api with ');
  });

  const part = (label, key) => row(label).querySelector(`[data-test=share-part-${key}]`);
  const checked = (label) => ['tasks', 'memory'].map((k) => part(label, k).getAttribute('aria-checked'));

  it('shows what each share lets the team read, a part the answer does not name shared', async () => {
    await inWorkspace({ shares: [{ teamId: 'tm1', tasks: false, skills: false }] });
    expect(checked('Core')).toEqual(['false', 'true']);
    expect(text(row('Core').querySelector('[data-test=share-parts]'))).toBe('Tasks Memory');
    expect(part('Core', 'skills')).toBe(null);
    expect(part('Core', 'tasks').getAttribute('aria-label')).toBe('Share the tasks of api with Core');
    expect(row('Docs').querySelector('[data-test=share-parts]')).toBe(null);
  });

  it('holds a part back, or shares it again, against the version, sending every part', async () => {
    api.shareWorkspace.mockResolvedValue({ teamId: 'tm1', version: 4 });
    await inWorkspace({ shares: [{ teamId: 'tm1', tasks: true, memory: true }] });
    await click(part('Core', 'memory'));
    expect(api.shareWorkspace).toHaveBeenCalledWith('w1', 'tm1', 3, { tasks: true, memory: false, autoOptInSkills: true });
    expect(useTeamStore().getTeam('tm1').version).toBe(4);
    expect(checked('Core')).toEqual(['true', 'false']);
    expect(toasts()).toContain('Core no longer reads the memory of api');

    api.shareWorkspace.mockResolvedValue({ teamId: 'tm1', version: 5 });
    await click(part('Core', 'memory'));
    expect(api.shareWorkspace).toHaveBeenLastCalledWith('w1', 'tm1', 4, { tasks: true, memory: true, autoOptInSkills: true });
    expect(checked('Core')).toEqual(['true', 'true']);
    expect(toasts()).toContain('Core now reads the memory of api');
  });

  it('shows whether new team skills are turned on, on unless the share says off', async () => {
    await inWorkspace({ shares: [{ teamId: 'tm1', autoOptInSkills: false }] });
    expect(part('Core', 'autoOptInSkills').getAttribute('aria-checked')).toBe('false');
    expect(text(row('Core').querySelector('[data-test=share-auto-opt-in]')))
      .toBe('Auto opt-in to team skills New team skills are turned on in this workspace. Skills you turn off stay off.');
    app.unmount();

    await inWorkspace({ shares: [{ teamId: 'tm1' }] });
    expect(part('Core', 'autoOptInSkills').getAttribute('aria-checked')).toBe('true');
  });

  it('turns auto opt-in off, or on again, with the other parts as they are', async () => {
    api.shareWorkspace.mockResolvedValue({ teamId: 'tm1', version: 4 });
    await inWorkspace({ shares: [{ teamId: 'tm1', tasks: true, memory: true }] });
    await click(part('Core', 'autoOptInSkills'));
    expect(api.shareWorkspace).toHaveBeenCalledWith('w1', 'tm1', 3, { tasks: true, memory: true, autoOptInSkills: false });
    expect(part('Core', 'autoOptInSkills').getAttribute('aria-checked')).toBe('false');
    expect(toasts()).toContain('New team skills stay off in api');

    api.shareWorkspace.mockResolvedValue({ teamId: 'tm1', version: 5 });
    await click(part('Core', 'autoOptInSkills'));
    expect(api.shareWorkspace).toHaveBeenLastCalledWith('w1', 'tm1', 4, { tasks: true, memory: true, autoOptInSkills: true });
    expect(toasts()).toContain('New team skills are turned on in api');
  });

  it('keeps a part as it was when holding it back fails', async () => {
    api.shareWorkspace.mockRejectedValue(failure(500, 'Share failed'));
    await inWorkspace({ shares: [{ teamId: 'tm1' }] });
    await click(part('Core', 'tasks'));
    expect(toasts()).toEqual(['Share failed']);
    expect(checked('Core')).toEqual(['true', 'true']);
    expect(part('Core', 'tasks').disabled).toBe(false);
  });

  it('shows the parts a new share answers with, and none for a fork or in Manage team', async () => {
    api.shareWorkspace.mockResolvedValue({ teamId: 'tm1', version: 4, tasks: true, memory: true });
    await inWorkspace({ shares: [] });
    expect(row('Core').querySelector('[data-test=share-parts]')).toBe(null);
    await click(toggle('Core'));
    expect(checked('Core')).toEqual(['true', 'true']);
    app.unmount();

    await inWorkspace({ shares: [{ teamId: 'tm1' }] }, { id: 'w2' });
    expect(row('Core').querySelector('[data-test=share-parts]')).toBe(null);
    app.unmount();

    api.fetchTeamWorkspaces.mockResolvedValue({ workspaces: [{ id: 'w1' }] });
    await mount(defineComponent({
      setup() {
        useWorkspaceStore().workspaces = [api1];
        return () => h(WorkspaceSharing, { teamId: 'tm1' });
      },
    }));
    expect(toggle('api').getAttribute('aria-checked')).toBe('true');
    expect($('[data-test=share-parts]')).toBe(null);
  });

  it('says so when the shares cannot be read', async () => {
    api.listWorkspaceShares.mockRejectedValue(failure(500, 'Could not read'));
    await mount(defineComponent({ render: () => h(WorkspaceSharing, { workspaceId: 'w1' }) }));
    expect(toasts()).toEqual(['Could not read']);
  });

  it('lists your workspaces in Manage team, the shared ones on', async () => {
    api.fetchWorkspaces.mockResolvedValue({ workspaces: [api1, fork, { id: 'w3', name: 'old', archivedAt: '2026-01-01' }] });
    api.fetchTeamWorkspaces.mockResolvedValue({ workspaces: [{ id: 'w1', name: 'api', sharedName: 'Backend' }, { id: 'zz', name: 'theirs' }, { id: 'w2', name: 'api-fork' }] });
    await mount(defineComponent({
      setup() {
        useTeamStore().teams = [{ ...core }];
        return () => h(WorkspaceSharing, { teamId: 'tm1' });
      },
    }));
    expect(rows().map((r) => text(r).split(' ')[0])).toEqual(['api', 'api-fork']);
    const first = rows()[0].querySelector('[data-test=share-switch]');
    expect(first.getAttribute('aria-checked')).toBe('true');
    expect(first.getAttribute('aria-label')).toBe('Share api with Core');
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
    expect(toggle('api').getAttribute('aria-checked')).toBe('true');
    app.unmount();

    api.fetchWorkspaces.mockResolvedValue({ workspaces: [] });
    await mount(defineComponent({ render: () => h(WorkspaceSharing, { teamId: 'tm1' }) }));
    expect(text($('[data-test=sharing-empty]'))).toBe('You have no workspaces to share.');
  });
});

describe('cloning a teammate\'s task', () => {
  const task = { id: 't1', title: 'Fix login' };
  const mine = [{ id: 'm1', name: 'scratch' }, { id: 'm2', name: 'API' }, { id: 'm3', name: 'old', archivedAt: 'x' }];

  async function open({ workspaces = mine, workspace = { name: 'web', sharedName: 'api' }, show = true } = {}) {
    await mount(defineComponent({
      setup() {
        useWorkspaceStore().workspaces = workspaces;
        return () => h(CloneTaskModal, { show, teamId: 'tm1', task, workspace, onClose: closed });
      },
    }));
  }
  let closed;
  beforeEach(() => { closed = vi.fn(); });

  it('picks your workspace named like the team workspace, for a human, and clones into the one chosen', async () => {
    api.cloneTeamTask.mockResolvedValue({ task: { id: 'c1', workspaceId: 'm1' } });
    await open();
    expect(text($('[data-test=clone-modal]'))).toContain('A copy of Fix login');
    expect($('[data-test=clone-modal] select')).toBe(null);
    expect($('[data-test=clone-status]')).toBe(null);
    expect(text($('[data-test=clone-workspace]'))).toBe('API');
    expect($('[data-test=clone-assignee-human]').getAttribute('aria-checked')).toBe('true');
    expect($('[data-test=clone-assignee-agent]').getAttribute('aria-checked')).toBe('false');

    await click($('[data-test=clone-workspace]'));
    expect($$('[data-test=clone-workspace-option]').map((o) => o.dataset.value)).toEqual(['m1', 'm2']);
    await click($('[data-test=clone-workspace-option][data-value=m1]'));
    expect(text($('[data-test=clone-workspace]'))).toBe('scratch');
    // Escape in the open menu closes the menu, not the dialog.
    await click($('[data-test=clone-workspace]'));
    await key($('[data-test=clone-workspace]'), 'Escape');
    expect($('[data-test=clone-workspace-menu]')).toBe(null);
    expect(closed).not.toHaveBeenCalled();

    await click($('[data-test=clone-confirm]'));
    expect(api.cloneTeamTask).toHaveBeenCalledWith('tm1', 't1', 'm1', 'human');
    const toast = useToasts().toasts.value.at(-1);
    expect(toast.message).toBe('Task cloned');
    expect(toast.link).toEqual({ taskId: 'c1', workspaceId: 'm1', label: 'Open' });
    expect(closed).toHaveBeenCalled();
  });

  it('assigns the clone to an agent when you choose one, and to a human again each time it opens', async () => {
    api.cloneTeamTask.mockResolvedValue({ task: { id: 'c1' } });
    const show = ref(true);
    await mount(defineComponent({
      setup() {
        useWorkspaceStore().workspaces = mine;
        return () => h(CloneTaskModal, { show: show.value, teamId: 'tm1', task, workspace: { name: 'api' }, onClose: () => { show.value = false; } });
      },
    }));
    await click($('[data-test=clone-assignee-agent]'));
    expect($('[data-test=clone-assignee-agent]').getAttribute('aria-checked')).toBe('true');
    await click($('[data-test=clone-confirm]'));
    expect(api.cloneTeamTask).toHaveBeenCalledWith('tm1', 't1', 'm2', 'agent');
    expect(useToasts().toasts.value.at(-1).link).toEqual({ taskId: 'c1', workspaceId: 'm2', label: 'Open' });

    show.value = true;
    await flush();
    expect($('[data-test=clone-assignee-human]').getAttribute('aria-checked')).toBe('true');
  });

  it('links the clone to the workspace chosen when the answer names none, or not at all', async () => {
    api.cloneTeamTask.mockResolvedValueOnce({ id: 'c2' }).mockResolvedValueOnce(undefined);
    await open({ workspace: null });
    expect(text($('[data-test=clone-workspace]'))).toBe('scratch');
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
    expect($('[data-test=clone-assignee]')).toBe(null);
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
  it('reads the whole team\'s numbers, or one team workspace\'s, and goes back to its tasks', async () => {
    api.fetchTeamStats.mockResolvedValue({ summary: { tasksCompleted: 3 } });
    await mount(TeamAnalyticsView, { path: '/teams/tm1/analytics' });
    expect(api.fetchTeamStats).toHaveBeenCalledWith('tm1', '7d', 0, 0, '');
    expect(text($('[data-test=team-analytics-scope]'))).toBe('All workspaces');
    await click($('[data-test=team-actions-status-active]'));
    expect(here().fullPath).toBe('/teams/tm1/tasks/active');

    await router.push('/teams/tm1/analytics?workspace=Backend');
    await flush();
    expect(api.fetchTeamStats).toHaveBeenLastCalledWith('tm1', '7d', 0, 0, 'Backend');
    expect(text($('h1'))).toBe('Backend / Analytics');
    expect(text($('[data-test=team-analytics-scope]'))).toBe('Backend');
    await click($('[data-test=team-actions-status-active]'));
    expect(here().fullPath).toBe('/teams/tm1/tasks/active?workspace=Backend');

    await router.push('/teams/tm1/analytics?workspace=Backend');
    await flush();
    await click($('[data-test=team-analytics-scope]'));
    expect(here().fullPath).toBe('/teams/tm1/tasks/active?workspace=Backend');
  });

  it('has the buttons a workspace page has: statuses, analytics and skills, the current one pressed', async () => {
    api.fetchTeamStats.mockResolvedValue({ summary: {} });
    await mount(TeamAnalyticsView, { path: '/teams/tm1/analytics?workspace=Backend' });
    const pressed = (sel) => $(sel).classList.contains('border-black');
    expect(pressed('[data-test=team-analytics]')).toBe(true);
    expect(pressed('[data-test=team-skills-link]')).toBe(false);
    expect($$('[data-test=team-actions-statuses] button').map((b) => b.getAttribute('aria-label'))).toEqual(['Active', 'Not Started', 'Ongoing', 'Completed', 'Scheduled']);
    await click($('[data-test=team-skills-link]'));
    expect(here().fullPath).toBe('/teams/tm1/skills?workspace=Backend');
    app.unmount();
    await mount(SkillsView, { path: '/teams/tm1/skills?workspace=Backend' });
    expect(pressed('[data-test=team-skills-link]')).toBe(true);
    expect(pressed('[data-test=team-analytics]')).toBe(false);
    await click($('[data-test=team-analytics]'));
    expect(here().fullPath).toBe('/teams/tm1/analytics?workspace=Backend');
  });

  it('offers no skills for the whole team, and its statuses open the whole team\'s lists', async () => {
    await mount(TeamWorkspaceActions, { path: '/teams/tm1/analytics' });
    expect($('[data-test=team-skills-link]')).toBe(null);
    await click($('[data-test=team-actions-status-completed]'));
    expect(here().fullPath).toBe('/teams/tm1/tasks/completed');
  });

  it('on a phone: the search opens the palette, and the statuses fold behind a toggle that closes on a pick', async () => {
    await mount(TeamWorkspaceActions, { path: '/teams/tm1/analytics?workspace=Backend', props: { current: 'analytics' } });
    const opened = vi.fn();
    window.addEventListener('open-command-palette', opened);
    await click($('[data-test=team-actions-search]'));
    expect(opened).toHaveBeenCalledTimes(1);
    window.removeEventListener('open-command-palette', opened);
    const strip = () => $('[data-test=team-actions-statuses]');
    expect(strip().classList.contains('hidden')).toBe(true);
    await click($('[data-test=team-actions-filters]'));
    expect(strip().classList.contains('absolute')).toBe(true);
    const status = $('[data-test=team-actions-status-ongoing]');
    status.dispatchEvent(new MouseEvent('mouseenter'));
    status.dispatchEvent(new MouseEvent('mouseleave'));
    await click(status);
    expect(here().fullPath).toBe('/teams/tm1/tasks/ongoing?workspace=Backend');
    expect(strip().classList.contains('hidden')).toBe(true);
  });

  it('titles the whole team\'s numbers Analytics alone', async () => {
    api.fetchTeamStats.mockResolvedValue({ summary: {} });
    await mount(TeamAnalyticsView, { path: '/teams/tm1/analytics' });
    expect(text($('h1'))).toBe('Analytics');
  });

  it('loads a custom range once both ends are set', async () => {
    api.fetchTeamStats.mockResolvedValue({ summary: {} });
    await mount(TeamAnalyticsView, { path: '/teams/tm1/analytics' });
    await click($('[data-test=range-custom]'));
    const calls = api.fetchTeamStats.mock.calls.length;
    await click($('[data-test=custom-from]'));
    expect(api.fetchTeamStats.mock.calls.length).toBe(calls);
    await click($('[data-test=custom-to]'));
    expect(api.fetchTeamStats.mock.calls.length).toBe(calls + 1);
  });

  it('goes back to Me without the team, and says other failures', async () => {
    api.fetchTeamStats.mockRejectedValueOnce(failure(500, 'Stats down'));
    await mount(TeamAnalyticsView, { path: '/teams/tm1/analytics' });
    expect(toasts()).toContain('Stats down');
    app.unmount();
    api.fetchTeamStats.mockRejectedValueOnce(failure(404));
    await mount(TeamAnalyticsView, { path: '/teams/tm1/analytics' });
    expect(here().path).toBe('/');
  });
});
