// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The button that turns a doc the person wrote into a task, mounted: it opens
 * a list of the workspaces, makes the task in the one picked and says so, and
 * closes on a click anywhere else. The rule it follows is tested in
 * `usePano.test.js`.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createApp, h, nextTick } from 'vue';
import { createPinia } from 'pinia';

const api = vi.hoisted(() => ({
  fetchWorkspaces: vi.fn(),
  getPanoDoc: vi.fn(),
  createTask: vi.fn(),
}));
vi.mock('../src/api', () => api);

const { default: PanoTaskButton } = await import('../src/components/PanoTaskButton.vue');
const { useWorkspaceStore } = await import('../src/stores/workspaceStore');
const { toasts } = (await import('../src/composables/useToasts')).useToasts();

// The app's `v-click-outside`, as `src/app.js` registers it.
const clickOutside = {
  mounted(el, binding) {
    el._clickOutside = (event) => {
      if (!(el === event.target || el.contains(event.target))) binding.value(event);
    };
    document.body.addEventListener('click', el._clickOutside);
  },
  unmounted(el) {
    document.body.removeEventListener('click', el._clickOutside);
  },
};

const settle = () => new Promise((r) => setTimeout(r, 0));
const apps = [];

async function mount(props, workspaces = [{ id: 'ws1', name: 'Home' }, { id: 'ws2', name: 'Ops' }]) {
  const el = document.createElement('div');
  document.body.append(el);
  const app = createApp({ render: () => h(PanoTaskButton, props) });
  app.directive('click-outside', clickOutside);
  const pinia = createPinia();
  app.use(pinia);
  useWorkspaceStore(pinia).workspaces = workspaces;
  app.mount(el);
  apps.push(app);
  await nextTick();
  return el;
}

const click = async (node) => {
  node.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
  await settle();
};
const button = (el) => el.querySelector('[data-test="pano-to-task"]');
const menu = (el) => el.querySelector('[data-test="pano-to-task-menu"]');
const items = (el) => [...el.querySelectorAll('[data-test="pano-to-task-workspace"]')];

beforeEach(() => {
  while (apps.length) apps.pop().unmount();
  document.body.innerHTML = '';
  toasts.value = [];
  vi.clearAllMocks();
});

describe('making a doc a task', () => {
  it('opens the list of workspaces, and closes it again', async () => {
    const el = await mount({ doc: { id: 'd1', title: 'Plan', markdown: 'x' }, buttonClass: 'extra' });
    expect(button(el).classList.contains('extra')).toBe(true);
    expect(menu(el)).toBeNull();
    await click(button(el));
    expect(button(el).getAttribute('aria-expanded')).toBe('true');
    expect(items(el).map((b) => b.textContent)).toEqual(['Home', 'Ops']);
    await click(button(el));
    expect(menu(el)).toBeNull();
  });

  it('closes the list on a click anywhere else', async () => {
    const el = await mount({ doc: { id: 'd1', title: 'Plan', markdown: 'x' } });
    await click(button(el));
    expect(menu(el)).not.toBeNull();
    await click(document.body);
    expect(menu(el)).toBeNull();
  });

  it('says there is nowhere to make it for an account with no workspaces', async () => {
    const el = await mount({ doc: { id: 'd1', title: 'Plan', markdown: 'x' } }, []);
    await click(button(el));
    expect(menu(el).textContent).toContain('No workspaces yet.');
  });

  it('makes the task in the workspace picked, and links the toast to it', async () => {
    let resolve;
    api.createTask.mockImplementation(() => new Promise((r) => { resolve = r; }));
    const el = await mount({ doc: { id: 'd1', title: 'Plan', markdown: 'x' } });
    await click(button(el));
    await click(items(el)[1]);
    expect(menu(el)).toBeNull();
    expect(button(el).disabled).toBe(true);
    expect(api.createTask).toHaveBeenCalledWith('ws2', 'Plan', expect.stringContaining('From the Pano doc `d1`'));
    resolve({ task: { id: 't9' } });
    await settle();
    expect(button(el).disabled).toBe(false);
    expect(toasts.value).toEqual([
      expect.objectContaining({ type: 'success', message: 'Task created in Ops', link: { taskId: 't9', workspaceId: 'ws2' } }),
    ]);
  });

  it('says why the task was not made', async () => {
    api.createTask.mockRejectedValueOnce(new Error('workspace archived'));
    const el = await mount({ doc: { id: 'd1', title: 'Plan', markdown: 'x' } });
    await click(button(el));
    await click(items(el)[0]);
    expect(toasts.value.at(-1)).toMatchObject({ type: 'error', message: 'workspace archived' });
    expect(button(el).disabled).toBe(false);

    api.createTask.mockRejectedValueOnce({});
    await click(button(el));
    await click(items(el)[0]);
    expect(toasts.value.at(-1)).toMatchObject({ type: 'error', message: 'Failed to create the task' });
  });
});
