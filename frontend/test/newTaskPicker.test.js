// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * A new task from a page that spans every workspace: the New Task button on
 * the Kanban and the task lists asks the shell, as `N` does, and the shell
 * opens the switcher to ask which workspace, then that workspace's task form.
 * The switcher is mounted in its plain switching role too, since both roles
 * share it.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createApp, h, nextTick } from 'vue';
import { createPinia } from 'pinia';

const push = vi.fn();
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }));

const { default: NewTaskButton } = await import('../src/components/NewTaskButton.vue');
const { default: WorkspaceSwitcher } = await import('../src/components/WorkspaceSwitcher.vue');
const { useWorkspaceStore } = await import('../src/stores/workspaceStore');

const WORKSPACES = [
  { id: 'w1', name: 'ops', agentConnected: true },
  { id: 'w2', name: 'old', archivedAt: '2026-01-01T00:00:00Z' },
  { id: 'w3', name: 'billing' },
  { id: 'f1', name: 'ops-fix', forkOfId: 'w1', forkOf: { name: 'ops' } },
];

const settle = () => new Promise((r) => setTimeout(r, 0));
const apps = [];

async function mount(component, props = {}, workspaces = WORKSPACES) {
  const el = document.createElement('div');
  document.body.append(el);
  const app = createApp({ render: () => h(component, props) });
  const pinia = createPinia();
  app.use(pinia);
  useWorkspaceStore(pinia).workspaces = workspaces.map((w) => ({ ...w }));
  app.mount(el);
  apps.push(app);
  await settle();
  return { el, app };
}

const click = async (node) => {
  node.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
  await settle();
};

beforeEach(() => {
  while (apps.length) apps.pop().unmount();
  document.body.innerHTML = '';
  vi.clearAllMocks();
});

describe('the New Task button', () => {
  it('asks the shell for a new task, as N does', async () => {
    const opened = vi.fn();
    window.addEventListener('open-new-task', opened);
    try {
      const { el } = await mount(NewTaskButton);
      await click(el.querySelector('[data-test="new-task"]'));
      expect(opened).toHaveBeenCalledTimes(1);
    } finally {
      window.removeEventListener('open-new-task', opened);
    }
  });
});

describe('the workspace switcher', () => {
  const rows = () => [...document.querySelectorAll('[role="dialog"] li button')];
  const names = () => rows().map((b) => b.querySelector('.grow').textContent.trim());
  const input = () => document.querySelector('[role="dialog"] input');
  const key = async (k) => {
    input().dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true }));
    await nextTick();
  };
  const type = async (text) => {
    input().value = text;
    input().dispatchEvent(new Event('input'));
    await nextTick();
  };

  // Mounted closed and then opened, as the shell does, so the open watcher
  // runs. A closed one stays in jsdom (its leave transition never ends), so
  // closing is read from the `close` it emits.
  const onClose = vi.fn();
  async function open(props, workspaces) {
    let show;
    const { app } = await mount({
      data: () => ({ show: false }),
      created() { show = (v) => { this.show = v; }; },
      render() { return h(WorkspaceSwitcher, { ...props, show: this.show, onClose }); },
    }, {}, workspaces);
    show(true);
    await settle();
    return { app, show };
  }

  it('picking where a new task goes: no archived rows, and a pick opens its task form', async () => {
    await open({ forNewTask: true });
    expect(document.querySelector('[role="dialog"]').getAttribute('aria-label')).toBe('New task in workspace');
    expect(input().getAttribute('placeholder')).toBe('New task in which workspace?');
    expect(document.activeElement).toBe(input());
    expect(names()).toEqual(['ops', 'billing', 'ops-fix']);
    expect(document.body.textContent).toContain('↵ New task');
    expect(document.body.textContent).toContain('3 workspaces');

    await type('bill');
    await key('Enter');
    expect(push).toHaveBeenCalledWith('/workspaces/w3/tasks/new');
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('switching: every workspace, marked current or archived, and a pick opens it', async () => {
    await open({ currentWorkspaceId: 'w1' });
    expect(document.querySelector('[role="dialog"]').getAttribute('aria-label')).toBe('Switch workspace');
    expect(names()).toEqual(['ops', 'billing', 'ops-fix', 'old']);
    expect(rows()[0].textContent).toContain('Current');
    expect(rows()[3].textContent).toContain('Archived');
    expect(rows()[2].querySelector('[data-test="switcher-fork"]').textContent.trim()).toBe('ops');
    expect(document.body.textContent).toContain('↵ Switch');

    await click(rows()[1]);
    expect(push).toHaveBeenCalledWith('/workspaces/w3');
  });

  it('moves the highlight with the arrows, wrapping at both ends', async () => {
    await open({});
    const highlighted = () => rows().findIndex((b) => b.classList.contains('bg-gray-50'));
    await key('ArrowUp');
    expect(highlighted()).toBe(3);
    await key('ArrowDown');
    expect(highlighted()).toBe(0);
    rows()[2].dispatchEvent(new MouseEvent('mouseenter'));
    await nextTick();
    expect(highlighted()).toBe(2);

    // A shorter list moves a highlight past its end back to the top.
    await type('ops');
    expect(highlighted()).toBe(0);
    await key('Enter');
    expect(push).toHaveBeenCalledWith('/workspaces/w1');
  });

  it('says why the list is empty, and Enter or the arrows then do nothing', async () => {
    await open({});
    await type('nothing-here');
    expect(document.body.textContent).toContain('No workspace matches');
    await key('ArrowDown');
    await key('Enter');
    expect(push).not.toHaveBeenCalled();
  });

  it('says there are no workspaces yet, and closes on Esc or the backdrop', async () => {
    // A fork whose parent the list does not name still reads as a fork.
    const { show } = await open({}, [{ id: 'solo', name: 'solo', forkOfId: 'gone' }]);
    expect(document.querySelector('[data-test="switcher-fork"]').textContent.trim()).toBe('fork');
    await type('so');
    await key('Escape');
    expect(onClose).toHaveBeenCalledTimes(1);

    // Opened again, it starts from an empty query.
    show(false);
    await settle();
    show(true);
    await settle();
    expect(input().value).toBe('');

    while (apps.length) apps.pop().unmount();
    await open({}, []);
    expect(document.body.textContent).toContain('No workspaces yet.');
    await click(document.querySelector('[role="dialog"] > div'));
    expect(onClose).toHaveBeenCalledTimes(2);
  });
});
