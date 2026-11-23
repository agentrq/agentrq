// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// Workspace settings › Sharing: whose memory the workspace's agent reads.
// The section, its switches, and the settings page that holds it, mounted
// for real against a mocked API.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createApp, defineComponent, h, nextTick, ref } from 'vue';
import { createPinia } from 'pinia';

const route = { params: { id: 'w1' }, query: { tab: 'sharing' } };
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ push: vi.fn() }) }));

vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  listTeammateMemories: vi.fn(),
  setTeammateMemory: vi.fn(),
  listWorkspaceShares: vi.fn(),
  shareWorkspace: vi.fn(),
  listTeams: vi.fn(),
  getWorkspace: vi.fn(),
  getWorkspaceToken: () => Promise.resolve({ token: 't' }),
  fetchWorkspaces: vi.fn(),
  fetchMachines: () => Promise.resolve({ machines: [] }),
  fetchTaskCounts: () => Promise.resolve({ completed: 0 }),
}));

const api = await import('../src/api');
const { useToasts } = await import('../src/composables/useToasts');
const { notifyWebMCPChange } = await import('../src/composables/useWebMCPChanges');
const { useTeamStore } = await import('../src/stores/teamStore');
const { useWorkspaceStore } = await import('../src/stores/workspaceStore');
const { default: TeammateMemories } = await import('../src/components/TeammateMemories.vue');
const { default: WorkspaceSettingsView } = await import('../src/views/WorkspaceSettingsView.vue');

const HOUR = 3600 * 1000;
const ALICE = { userId: 'u1', name: 'Alice', picture: 'https://x/a.png', workspaceId: 'wa', updatedAt: new Date(Date.now() - 2 * HOUR).toISOString(), included: true };
const BOB = { userId: 'u2', name: 'Bob', picture: '', workspaceId: 'wb', included: false };
const NONAME = { userId: 'u3', name: '', picture: '', workspaceId: 'wc', included: true };

const flush = async () => {
  for (let i = 0; i < 4; i += 1) {
    await new Promise((resolve) => setTimeout(resolve, 0));
    await nextTick();
  }
};
const text = (n) => (n?.textContent ?? '').replace(/\s+/g, ' ').trim();
const toasts = () => useToasts().toasts.value.map((t) => t.message);
const rows = () => [...document.body.querySelectorAll('[data-test=teammate-memory-row]')];
const row = (name) => rows().find((r) => text(r.querySelector('p')) === name);
const toggle = (name) => row(name).querySelector('[data-test=teammate-memory-switch]');

let app;

async function mount(render, setup = () => {}) {
  const el = document.createElement('div');
  document.body.appendChild(el);
  const pinia = createPinia();
  app = createApp(defineComponent({ setup() { setup(); return render; } }));
  app.use(pinia);
  app.component('RouterLink', {
    props: ['to'],
    setup: (p, { slots }) => () => h('a', { href: String(p.to) }, slots.default?.()),
  });
  app.directive('click-outside', {});
  app.mount(el);
  await flush();
  return el;
}

const section = (workspaceId = 'w1') => mount(() => h(TeammateMemories, { workspaceId }));

beforeEach(() => {
  document.body.innerHTML = '';
  localStorage.clear();
  vi.clearAllMocks();
  useToasts().toasts.value = [];
  api.listTeammateMemories.mockResolvedValue({ teammates: [ALICE, BOB, NONAME].map((t) => ({ ...t })) });
});

afterEach(() => {
  app?.unmount();
  app = null;
});

describe('teammates\' memory your agent reads', () => {
  it('lists each teammate with their avatar, when they last wrote, and whether the agent reads them', async () => {
    await section();
    expect(api.listTeammateMemories).toHaveBeenCalledWith('w1');
    const el = document.body.querySelector('[data-test=teammate-memories]');
    expect(text(el.querySelector('h3'))).toBe('Teammates\' memory your agent reads');
    expect(text(el)).toContain('Turn one off to leave it out; they are not told.');
    expect(rows().length).toBe(3);
    expect(text(row('Alice').querySelector('[data-test=teammate-memory-updated]'))).toBe('Updated 2 hours ago');
    expect(row('Alice').querySelector('[data-test=teammate-memory-updated]').getAttribute('title')).toBe(ALICE.updatedAt);
    expect(row('Alice').querySelector('img').getAttribute('src')).toBe('https://x/a.png');
    expect(text(row('Bob').querySelector('[data-test=teammate-memory-updated]'))).toBe('No memory yet');
    expect(toggle('Alice').getAttribute('aria-checked')).toBe('true');
    expect(toggle('Bob').getAttribute('aria-checked')).toBe('false');
    expect(toggle('Alice').getAttribute('aria-label')).toBe('Your agent reads Alice\'s memory');
    expect(toggle('Alice').querySelector('span').className).toContain('left-[23px]');
    expect(toggle('Bob').querySelector('span').className).toContain('left-[3px]');
  });

  it('names a teammate with no name as a member', async () => {
    await section();
    expect(toggle('A member').getAttribute('aria-label')).toBe('Your agent reads this member\'s memory');
  });

  it('shows nothing when the workspace has no teammates, or the answer names none', async () => {
    api.listTeammateMemories.mockResolvedValue({ teammates: [] });
    await section();
    expect(document.body.querySelector('[data-test=teammate-memories]')).toBe(null);
    app.unmount();
    api.listTeammateMemories.mockResolvedValue(null);
    await section();
    expect(document.body.querySelector('[data-test=teammate-memories]')).toBe(null);
  });

  it('says why it could not load them, and shows nothing', async () => {
    api.listTeammateMemories.mockRejectedValue(new Error('Failed to load teammates\' memory'));
    await section();
    expect(toasts()).toEqual(['Failed to load teammates\' memory']);
    expect(document.body.querySelector('[data-test=teammate-memories]')).toBe(null);
  });

  it('leaves a teammate out at once, and takes one back in, quietly', async () => {
    let answer;
    api.setTeammateMemory.mockReturnValue(new Promise((r) => { answer = r; }));
    await section();
    toggle('Alice').click();
    await nextTick();
    expect(toggle('Alice').getAttribute('aria-checked')).toBe('false');
    expect(toggle('Alice').disabled).toBe(true);
    answer({ teammate: { ...ALICE, included: false } });
    await flush();
    expect(api.setTeammateMemory).toHaveBeenCalledWith('w1', 'u1', false);
    expect(toggle('Alice').disabled).toBe(false);

    api.setTeammateMemory.mockResolvedValue({ teammate: { ...BOB, included: true } });
    toggle('Bob').click();
    await flush();
    expect(api.setTeammateMemory).toHaveBeenLastCalledWith('w1', 'u2', true);
    expect(toggle('Bob').getAttribute('aria-checked')).toBe('true');
    expect(toasts()).toEqual([]);
  });

  it('flips every workspace of one teammate together, since the choice is about the person', async () => {
    api.listTeammateMemories.mockResolvedValue({ teammates: [{ ...ALICE }, { ...ALICE, workspaceId: 'wa2' }, { ...BOB }] });
    api.setTeammateMemory.mockResolvedValue({ teammate: { ...ALICE, included: false } });
    await section();
    expect(rows().length).toBe(3);
    rows()[0].querySelector('[data-test=teammate-memory-switch]').click();
    await flush();
    const checked = rows().map((r) => r.querySelector('[data-test=teammate-memory-switch]').getAttribute('aria-checked'));
    expect(checked).toEqual(['false', 'false', 'false']);
    expect(api.setTeammateMemory).toHaveBeenCalledTimes(1);
  });

  it('turns the switch back and says why when the change is refused', async () => {
    api.setTeammateMemory.mockRejectedValue(new Error('not found'));
    await section();
    toggle('Alice').click();
    await flush();
    expect(toggle('Alice').getAttribute('aria-checked')).toBe('true');
    expect(toggle('Alice').disabled).toBe(false);
    expect(toasts()).toEqual(['not found']);
  });

  it('reads them again for another workspace, and after a browser agent\'s change', async () => {
    const id = ref('w1');
    await mount(() => h(TeammateMemories, { workspaceId: id.value }));
    id.value = 'w9';
    await flush();
    expect(api.listTeammateMemories).toHaveBeenLastCalledWith('w9');
    notifyWebMCPChange();
    await flush();
    expect(api.listTeammateMemories).toHaveBeenCalledTimes(3);
  });
});

describe('the Sharing tab of a workspace\'s settings', () => {
  const W1 = { id: 'w1', name: 'api', notificationSettings: {} };
  const FORK = { id: 'f1', name: 'api-try', forkOfId: 'w1', notificationSettings: {} };

  async function settings(workspace) {
    route.params = { id: workspace.id };
    route.query = { tab: 'sharing' };
    api.getWorkspace.mockResolvedValue({ workspace: { ...workspace } });
    api.fetchWorkspaces.mockResolvedValue({ workspaces: [W1, FORK] });
    api.listTeams.mockResolvedValue({ teams: [] });
    api.listWorkspaceShares.mockResolvedValue({ shares: [] });
    return mount(() => h(WorkspaceSettingsView), () => {
      useWorkspaceStore().workspaces = [W1, FORK].map((w) => ({ ...w }));
      useTeamStore().teams = [{ id: 'tm1', name: 'Core', version: 3 }];
    });
  }

  it('lists the teammates below the teams, and reads them again after a share', async () => {
    api.shareWorkspace.mockResolvedValue({ teamId: 'tm1', version: 4 });
    await settings(W1);
    expect(rows().length).toBe(3);
    expect(api.listTeammateMemories).toHaveBeenCalledTimes(1);
    document.body.querySelector('[data-test=share-switch]').click();
    await flush();
    expect(api.shareWorkspace).toHaveBeenCalledWith('w1', 'tm1', 3);
    expect(api.listTeammateMemories).toHaveBeenCalledTimes(2);
  });

  it('has no teammates\' memory for a fork, which reads its parent\'s', async () => {
    await settings(FORK);
    expect(document.body.querySelector('[data-test=workspace-sharing]')).not.toBe(null);
    expect(api.listTeammateMemories).not.toHaveBeenCalled();
    expect(document.body.querySelector('[data-test=teammate-memories]')).toBe(null);
  });
});
