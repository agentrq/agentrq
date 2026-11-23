// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The Memories tab, mounted: memory.md rendered, its links opened in place
 * with Back, and the memories it does not link to listed under it. The logic
 * is `useMemoryReader` (`memories.test.js`); this checks the wiring, which the
 * coverage gate does not count.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createApp, h } from 'vue';

let MEMORIES = [];
const CONTENT = {
  'memory.md': '# Index\n\n- [how we ship](memory://deploys.md)\n- [later](memory://later.md)',
  'deploys.md': 'Ship **carefully**. Back to [the index](memory://memory.md).',
  'orphan.md': 'Nobody links here.',
};

const api = vi.hoisted(() => ({
  fetchWorkspaceMemories: vi.fn(),
  getWorkspaceMemory: vi.fn(),
  listTeammateMemories: vi.fn(),
  listTeammateMemoryNames: vi.fn(),
  getTeammateMemory: vi.fn(),
  setTeammateMemory: vi.fn(),
}));
vi.mock('../src/api', () => api);

const { default: WorkspaceMemoryPage } = await import('../src/components/WorkspaceMemoryPage.vue');
const { useToasts } = await import('../src/composables/useToasts');
const { notifyWebMCPChange } = await import('../src/composables/useWebMCPChanges');

const settle = () => new Promise((r) => setTimeout(r, 30));
const apps = [];

async function mount(workspaceId = 'ws1') {
  const el = document.createElement('div');
  document.body.append(el);
  const app = createApp({ render: () => h(WorkspaceMemoryPage, { workspaceId }) });
  app.mount(el);
  apps.push(app);
  await settle();
  return el;
}

const q = (el, test) => el.querySelector(`[data-test="${test}"]`);
const text = (node) => node?.textContent.replace(/\s+/g, ' ').trim();
const click = async (node) => {
  node.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  await settle();
};
const unlinked = (el) => [...el.querySelectorAll('[data-test="memory-unlinked-item"]')].map((b) => b.textContent.trim());

beforeEach(() => {
  while (apps.length) apps.pop().unmount();
  document.body.innerHTML = '';
  MEMORIES = ['memory.md', 'deploys.md', 'orphan.md'].map((name) => ({ name }));
  api.fetchWorkspaceMemories.mockReset().mockImplementation(async () => ({ memories: MEMORIES }));
  api.getWorkspaceMemory.mockReset().mockImplementation(async (_ws, name) => ({ memory: { name, content: CONTENT[name] } }));
  api.listTeammateMemories.mockReset().mockResolvedValue({ teammates: [] });
  api.listTeammateMemoryNames.mockReset().mockImplementation(async () => ({ memories: [{ name: 'memory.md' }, { name: 'ada.md' }] }));
  api.getTeammateMemory.mockReset().mockImplementation(async (_ws, _of, name) => ({ memory: { name, content: name === 'memory.md' ? "# Ada's\n\n- [notes](memory://ada.md)" : 'Ada wrote this.' } }));
  api.setTeammateMemory.mockReset().mockResolvedValue({});
  useToasts().toasts.value.splice(0);
});

describe('the Memories tab', () => {
  it('shows memory.md alone, rendered, and lists what it does not link to', async () => {
    const el = await mount();

    expect(api.fetchWorkspaceMemories).toHaveBeenCalledWith('ws1');
    expect(text(q(el, 'memory-name'))).toBe('MEMORY.md');
    expect(q(el, 'memory-body').querySelector('h1').textContent).toBe('Index');
    expect(q(el, 'memory-back')).toBeNull();
    // deploys.md is reached from the index; only the orphan is listed.
    expect(unlinked(el)).toEqual(['orphan.md']);
    expect(text(q(el, 'memory-unlinked'))).toContain('Not linked from MEMORY.md');
  });

  it('follows a memory:// link in place, and Back returns to the index', async () => {
    const el = await mount();

    await click(q(el, 'memory-body').querySelector('[data-memory-link="deploys.md"]'));

    expect(api.getWorkspaceMemory).toHaveBeenLastCalledWith('ws1', 'deploys.md');
    expect(text(q(el, 'memory-name'))).toBe('deploys.md');
    expect(q(el, 'memory-body').querySelector('strong').textContent).toBe('carefully');
    expect(q(el, 'memory-unlinked')).toBeNull();

    await click(q(el, 'memory-back'));
    expect(text(q(el, 'memory-name'))).toBe('MEMORY.md');
    expect(q(el, 'memory-back')).toBeNull();
  });

  it('opens a memory from the list under the index', async () => {
    const el = await mount();

    await click(el.querySelector('[data-test="memory-unlinked-item"]'));

    expect(text(q(el, 'memory-name'))).toBe('orphan.md');
    expect(text(q(el, 'memory-body'))).toBe('Nobody links here.');
  });

  it('says in place when a link names a memory nobody saved', async () => {
    const el = await mount();

    await click(q(el, 'memory-body').querySelector('[data-memory-link="later.md"]'));

    expect(text(q(el, 'memory-missing'))).toBe('Nothing saved under later.md yet.');
    expect(q(el, 'memory-back')).not.toBeNull();
  });

  it('shows the raw markdown on request', async () => {
    const el = await mount();

    await click(q(el, 'memory-raw'));

    expect(q(el, 'memory-raw-body').textContent).toBe(CONTENT['memory.md']);
    expect(q(el, 'memory-raw').getAttribute('aria-pressed')).toBe('true');
  });

  it('keeps its buttons to icons on a phone, with names for a screen reader', async () => {
    const el = await mount();
    await click(q(el, 'memory-body').querySelector('[data-memory-link="deploys.md"]'));

    for (const button of [q(el, 'memory-back'), q(el, 'memory-raw')]) {
      expect(button.getAttribute('aria-label')).toBeTruthy();
      expect(button.querySelector('svg').getAttribute('class')).toContain('sm:hidden');
      expect(button.querySelector('span').getAttribute('class')).toContain('hidden sm:inline');
    }
  });

  it('says there is no index yet and lists every memory instead', async () => {
    MEMORIES = [{ name: 'deploys.md' }, { name: 'orphan.md' }];
    const el = await mount();

    expect(api.getWorkspaceMemory).not.toHaveBeenCalled();
    expect(text(q(el, 'memory-no-index'))).toBe('MEMORY.md is not written yet, so nothing indexes these.');
    expect(unlinked(el)).toEqual(['deploys.md', 'orphan.md']);

    await click(el.querySelector('[data-test="memory-unlinked-item"]'));
    expect(text(q(el, 'memory-name'))).toBe('deploys.md');

    await click(q(el, 'memory-back'));
    expect(q(el, 'memory-no-index')).not.toBeNull();
  });

  it('reads an empty workspace as nothing yet, with no box inside the card on a phone', async () => {
    MEMORIES = [];
    const el = await mount();

    const empty = q(el, 'memories-empty');
    expect(text(empty)).toContain('Nothing remembered yet.');
    expect(empty.className.split(' ').filter((c) => /^(p-|bg-|border|rounded)/.test(c))).toEqual([]);
  });

  it('tells a failed fetch apart from an empty workspace, and tries again', async () => {
    api.fetchWorkspaceMemories.mockRejectedValueOnce(new Error('offline'));
    const el = await mount();

    expect(text(q(el, 'memories-failed'))).toContain("Could not load this workspace's memories.");
    expect(q(el, 'memories-empty')).toBeNull();

    await click(q(el, 'memories-failed').querySelector('button'));
    expect(text(q(el, 'memory-name'))).toBe('MEMORY.md');
  });

  it('reports a memory that would not load', async () => {
    api.getWorkspaceMemory.mockRejectedValue(new Error('offline'));
    const el = await mount();

    expect(text(q(el, 'memory-error'))).toBe('Could not load this memory.');
    expect(q(el, 'memory-unlinked')).toBeNull();
  });
});

describe("a teammate's memory", () => {
  const ADA = { userId: 'u1', name: 'Ada', picture: '', workspaceId: 'wa', included: true };
  const ADA2 = { ...ADA, workspaceId: 'wa2' };
  const NONAME = { userId: 'u3', name: '', picture: '', workspaceId: 'wc', included: false };
  const chips = (el) => [...el.querySelectorAll('[data-test="memory-source"]')];
  const label = (c) => text(c.lastElementChild);
  const chip = (el, name) => chips(el).find((c) => label(c) === name);
  const toggle = (el) => q(el, 'memory-source-switch');

  beforeEach(() => {
    // Fresh rows each call: the page flips `included` on the rows it gets.
    api.listTeammateMemories.mockImplementation(async () => ({ teammates: [ADA, ADA2, NONAME].map((t) => ({ ...t })) }));
  });

  it('shows no choice without teammates, or when they cannot be listed', async () => {
    api.listTeammateMemories.mockResolvedValue({ teammates: [] });
    let el = await mount();
    expect(q(el, 'memory-sources')).toBeNull();

    api.listTeammateMemories.mockResolvedValue({});
    el = await mount();
    expect(q(el, 'memory-sources')).toBeNull();

    api.listTeammateMemories.mockRejectedValue(new Error('offline'));
    el = await mount();
    expect(q(el, 'memory-sources')).toBeNull();
    expect(text(q(el, 'memory-name'))).toBe('MEMORY.md');
  });

  it("reads a teammate's memory, read-only, following its links, and back to yours", async () => {
    const el = await mount();

    expect(api.listTeammateMemories).toHaveBeenCalledWith('ws1');
    expect(chips(el).map(label)).toEqual(['Yours', 'Ada', 'Ada', 'A member']);
    expect(chip(el, 'Yours').getAttribute('aria-selected')).toBe('true');
    expect(q(el, 'memory-source-include')).toBeNull();

    await click(chips(el)[1]);
    expect(api.listTeammateMemoryNames).toHaveBeenCalledWith('ws1', 'wa');
    expect(api.getTeammateMemory).toHaveBeenLastCalledWith('ws1', 'wa', 'memory.md');
    expect(chips(el)[1].getAttribute('aria-selected')).toBe('true');
    expect(q(el, 'memory-body').querySelector('h1').textContent).toBe("Ada's");

    await click(q(el, 'memory-body').querySelector('[data-memory-link="ada.md"]'));
    expect(api.getTeammateMemory).toHaveBeenLastCalledWith('ws1', 'wa', 'ada.md');
    expect(text(q(el, 'memory-body'))).toBe('Ada wrote this.');

    // The chip already open changes nothing.
    await click(chips(el)[1]);
    expect(api.listTeammateMemoryNames).toHaveBeenCalledTimes(1);

    await click(chip(el, 'Yours'));
    expect(api.fetchWorkspaceMemories).toHaveBeenCalledTimes(2);
    expect(q(el, 'memory-body').querySelector('h1').textContent).toBe('Index');
  });

  it('leaves a teammate out of your agent from their page, every row of theirs at once', async () => {
    const el = await mount();
    await click(chips(el)[1]);

    expect(toggle(el).getAttribute('aria-checked')).toBe('true');
    expect(toggle(el).getAttribute('aria-label')).toBe("Your agent reads Ada's memory");
    expect(text(q(el, 'memory-source-include'))).toBe("Your agent reads Ada's memory.");

    await click(toggle(el));
    expect(api.setTeammateMemory).toHaveBeenCalledWith('ws1', 'u1', false);
    expect(text(q(el, 'memory-source-include'))).toBe("Your agent leaves Ada's memory out.");
    await click(chips(el)[2]);
    expect(toggle(el).getAttribute('aria-checked')).toBe('false');
    // Still readable after leaving her out.
    expect(text(q(el, 'memory-name'))).toBe('MEMORY.md');

    await click(chip(el, 'A member'));
    expect(toggle(el).getAttribute('aria-checked')).toBe('false');
    await click(toggle(el));
    expect(api.setTeammateMemory).toHaveBeenLastCalledWith('ws1', 'u3', true);
  });

  it('puts the switch back and says why when the server refuses', async () => {
    api.setTeammateMemory.mockRejectedValue(new Error("Failed to change the teammate's memory"));
    const el = await mount();
    await click(chips(el)[1]);

    await click(toggle(el));

    expect(toggle(el).getAttribute('aria-checked')).toBe('true');
    expect(toggle(el).disabled).toBe(false);
    expect(useToasts().toasts.value.map((t) => t.message)).toContain("Failed to change the teammate's memory");
  });

  it('goes back to yours when the teammate open is no longer listed after an agent change', async () => {
    const el = await mount();
    await click(chips(el)[1]);

    api.listTeammateMemories.mockImplementation(async () => ({ teammates: [{ ...NONAME }] }));
    notifyWebMCPChange();
    await settle();

    expect(chips(el).map(label)).toEqual(['Yours', 'A member']);
    expect(chip(el, 'Yours').getAttribute('aria-selected')).toBe('true');
    expect(q(el, 'memory-body').querySelector('h1').textContent).toBe('Index');
  });

  it('starts at yours again for another workspace', async () => {
    const el = document.createElement('div');
    document.body.append(el);
    const { ref } = await import('vue');
    const ws = ref('ws1');
    const app = createApp({ render: () => h(WorkspaceMemoryPage, { workspaceId: ws.value }) });
    app.mount(el);
    apps.push(app);
    await settle();
    await click(chips(el)[1]);

    ws.value = 'ws2';
    await settle();

    expect(api.listTeammateMemories).toHaveBeenLastCalledWith('ws2');
    expect(api.fetchWorkspaceMemories).toHaveBeenLastCalledWith('ws2');
    expect(chip(el, 'Yours').getAttribute('aria-selected')).toBe('true');
  });
});
