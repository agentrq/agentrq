// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The create-workspace page, mounted: it opens with the default mission and
 * self-learning loop note filled in, sends what the person left there, and the
 * overview's buttons lead to it rather than opening a form of their own.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createApp, h } from 'vue';
import { createPinia } from 'pinia';

const push = vi.fn();
vi.mock('vue-router', () => ({ useRouter: () => ({ push }), useRoute: () => ({ params: {}, query: {} }) }));

const createWorkspace = vi.fn();
const fetchWorkspaces = vi.fn();
vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  createWorkspace: (...args) => createWorkspace(...args),
  fetchWorkspaces: (...args) => fetchWorkspaces(...args),
  fetchGlobalTaskStats: async () => ({}),
  fetchGlobalTasks: async () => ({ tasks: [] }),
  fetchUserStats: async () => ({}),
}));

import WorkspaceFormView from '../src/views/WorkspaceFormView.vue';
import WorkspaceView from '../src/views/WorkspaceView.vue';
import { usePlatformStore } from '../src/stores/platformStore';
import { useToasts } from '../src/composables/useToasts';
import { DEFAULT_SELF_LEARNING_LOOP_NOTE, DEFAULT_WORKSPACE_MISSION } from '../src/utils/workspaceForm';
import { MISSION_CATEGORIES } from '../src/utils/missionTemplates';

const settle = () => new Promise((r) => setTimeout(r, 30));

let app;
let el;
let pinia;

async function mount(view = WorkspaceFormView, beforeMount = () => {}) {
  el = document.createElement('div');
  document.body.appendChild(el);
  pinia = createPinia();
  app = createApp({ render: () => h(view) });
  app.use(pinia);
  beforeMount();
  app.component('RouterLink', { props: ['to'], setup: (p, { slots }) => () => h('a', {}, slots.default?.()) });
  app.directive('click-outside', {});
  app.mount(el);
  await settle();
}

const $ = (sel) => el.querySelector(sel);
const button = (text) => [...el.querySelectorAll('button')].find((b) => b.textContent.trim() === text);

async function type(sel, value) {
  $(sel).value = value;
  $(sel).dispatchEvent(new Event('input'));
  await settle();
}

beforeEach(() => {
  app?.unmount();
  el?.remove();
  localStorage.clear();
  delete window.agentrq;
  push.mockReset();
  createWorkspace.mockReset();
  createWorkspace.mockResolvedValue({ workspace: { id: 'w1' } });
  fetchWorkspaces.mockReset();
  fetchWorkspaces.mockResolvedValue({ workspaces: [] });
});

afterEach(() => {
  delete window.agentrq;
});

describe('the create-workspace page', () => {
  it('opens with the default mission and self-learning loop note filled in', async () => {
    await mount();
    expect($('#workspaceMission').value).toBe(DEFAULT_WORKSPACE_MISSION);
    expect($('#workspaceSelfLearningNote').value).toBe(DEFAULT_SELF_LEARNING_LOOP_NOTE);
  });

  it('puts the cursor in the name field', async () => {
    await mount();
    expect(document.activeElement).toBe($('#workspaceName'));
  });

  it('cannot be sent without a name, and kebab-cases the name as it is typed', async () => {
    await mount();
    expect($('button[type="submit"]').disabled).toBe(true);

    await type('#workspaceName', 'My Billing_App');
    expect($('#workspaceName').value).toBe('my-billing-app');
    expect($('button[type="submit"]').disabled).toBe(false);

    $('#workspaceName').value = 'trailing-';
    $('#workspaceName').dispatchEvent(new Event('input'));
    $('#workspaceName').dispatchEvent(new Event('blur'));
    await settle();
    expect($('#workspaceName').value).toBe('trailing');
  });

  it('ignores a submit while the name is empty', async () => {
    await mount();
    $('form').dispatchEvent(new Event('submit'));
    await settle();
    expect(createWorkspace).not.toHaveBeenCalled();
  });

  it('sends what the person left, opens the new workspace and refreshes the list', async () => {
    await mount();
    await type('#workspaceName', 'billing');
    await type('#workspaceMission', 'Invoices.');
    await type('#workspaceWorkingDirectory', '/srv/billing');
    await type('#workspaceSelfLearningNote', 'Be concise.');
    fetchWorkspaces.mockClear();

    $('form').dispatchEvent(new Event('submit'));
    await settle();

    expect(createWorkspace).toHaveBeenCalledWith('billing', 'Invoices.', '', 'Be concise.', '/srv/billing');
    expect(push).toHaveBeenCalledWith('/workspaces/w1');
    expect(fetchWorkspaces).toHaveBeenCalled();
  });

  it('goes back to the overview when the server names no new workspace', async () => {
    createWorkspace.mockResolvedValue({});
    fetchWorkspaces.mockRejectedValue(new Error('offline'));
    await mount();
    await type('#workspaceName', 'billing');
    $('form').dispatchEvent(new Event('submit'));
    await settle();
    expect(push).toHaveBeenCalledWith('/');
  });

  it('accepts the id at the top level of the response too', async () => {
    createWorkspace.mockResolvedValue({ id: 'w2' });
    await mount();
    await type('#workspaceName', 'billing');
    $('form').dispatchEvent(new Event('submit'));
    await settle();
    expect(push).toHaveBeenCalledWith('/workspaces/w2');
  });

  it('creates with Cmd or Ctrl + Enter, and not with a bare Enter in the mission', async () => {
    await mount();
    await type('#workspaceName', 'billing');

    $('#workspaceMission').dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    await settle();
    expect(createWorkspace).not.toHaveBeenCalled();

    $('#workspaceMission').dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', ctrlKey: true, bubbles: true }));
    await settle();
    expect(createWorkspace).toHaveBeenCalledOnce();
  });

  it('stays on the page and says why when creating fails', async () => {
    createWorkspace.mockRejectedValue(new Error('Failed to create workspace'));
    await mount();
    await type('#workspaceName', 'billing');
    $('form').dispatchEvent(new Event('submit'));
    await settle();

    expect(push).not.toHaveBeenCalled();
    expect(el.textContent).toContain('Failed to create workspace: Failed to create workspace');
    expect($('button[type="submit"]').disabled).toBe(false);
  });

  it('restores the default mission and note after they were edited', async () => {
    await mount();
    expect(button('Restore default')).toBeUndefined();

    await type('#workspaceMission', 'scratch');
    button('Restore default').click();
    await settle();
    expect($('#workspaceMission').value).toBe(DEFAULT_WORKSPACE_MISSION);

    await type('#workspaceSelfLearningNote', 'scratch');
    button('Restore default').click();
    await settle();
    expect($('#workspaceSelfLearningNote').value).toBe(DEFAULT_SELF_LEARNING_LOOP_NOTE);
  });

  it('pre-fills the mission from a category and speciality, and the text stays editable', async () => {
    await mount();
    const coding = MISSION_CATEGORIES.find((c) => c.id === 'coding');
    for (const c of MISSION_CATEGORIES) expect(button(c.label)).toBeDefined();
    expect(button('General').getAttribute('aria-pressed')).toBe('true');
    expect(button('iOS')).toBeUndefined();

    button('Coding').click();
    await settle();
    expect(button('Sales')).toBeUndefined();
    button('iOS').click();
    await settle();
    expect($('#workspaceMission').value).toBe(coding.subcategories.find((t) => t.id === 'ios').mission);
    expect(button('iOS').getAttribute('aria-pressed')).toBe('true');
    expect(button('Undo')).toBeUndefined();

    await type('#workspaceName', 'app');
    await type('#workspaceMission', 'My own words.');
    expect(button('iOS').getAttribute('aria-pressed')).toBe('false');
    $('button[title="Back to all categories"]').click();
    await settle();
    expect(button('iOS')).toBeUndefined();
    expect(button('General').getAttribute('aria-pressed')).toBe('false');
    button('General').click();
    await settle();
    expect(button('General').getAttribute('aria-pressed')).toBe('true');
    expect($('#workspaceMission').value).toBe(DEFAULT_WORKSPACE_MISSION);
    button('Undo').click();
    await settle();
    expect($('#workspaceMission').value).toBe('My own words.');

    $('form').dispatchEvent(new Event('submit'));
    await settle();
    expect(createWorkspace.mock.calls[0][1]).toBe('My own words.');
  });

  it('returns to the overview on Cancel and on the close button', async () => {
    await mount();
    button('Cancel').click();
    $('button[title="Cancel"]').click();
    expect(push.mock.calls).toEqual([['/'], ['/']]);
  });

  describe('the folder chooser', () => {
    it('is not offered in the browser', async () => {
      await mount();
      expect(button('Browse')).toBeUndefined();
    });

    it('fills in the folder chosen in the desktop app, and keeps it when the dialog is dismissed', async () => {
      const chooseDirectory = vi.fn().mockResolvedValueOnce('/home/me/billing').mockResolvedValueOnce('');
      window.agentrq = { dialog: { chooseDirectory } };
      await mount(WorkspaceFormView, () => usePlatformStore(pinia).setPlatform('desktop'));

      button('Browse').click();
      await settle();
      expect($('#workspaceWorkingDirectory').value).toBe('/home/me/billing');

      button('Browse').click();
      await settle();
      expect(chooseDirectory).toHaveBeenLastCalledWith('/home/me/billing');
      expect($('#workspaceWorkingDirectory').value).toBe('/home/me/billing');
    });

    it('opens one chooser at a time, and reports one that cannot open', async () => {
      let finish;
      const chooseDirectory = vi.fn(() => new Promise((resolve, reject) => { finish = reject; }));
      window.agentrq = { dialog: { chooseDirectory } };
      await mount(WorkspaceFormView, () => usePlatformStore(pinia).setPlatform('desktop'));

      button('Browse').click();
      await settle();
      button('Choosing…').click();
      await settle();
      expect(chooseDirectory).toHaveBeenCalledOnce();

      finish(new Error('dialog broke'));
      await settle();
      expect(useToasts().toasts.value.at(-1).message).toContain('dialog broke');
      expect(button('Browse')).toBeDefined();
    });
  });
});

describe('the overview', () => {
  it('leads to the create page from the first-run empty state', async () => {
    await mount(WorkspaceView);
    expect($('form')).toBeNull();
    button('New Workspace').click();
    expect(push).toHaveBeenCalledWith('/workspaces/new');
  });

  it('leads to the create page from the New button beside the list', async () => {
    fetchWorkspaces.mockResolvedValue({ workspaces: [{ id: 'w1', name: 'billing' }] });
    await mount(WorkspaceView);
    button('New').click();
    expect(push).toHaveBeenCalledWith('/workspaces/new');
  });

  it('lists each workspace followed by its forks', async () => {
    fetchWorkspaces.mockResolvedValue({
      workspaces: [
        { id: 'f1', name: 'zz-try', forkOfId: 'w1' },
        { id: 'w2', name: 'web' },
        { id: 'w1', name: 'billing' },
      ],
    });
    await mount(WorkspaceView);
    const names = [...el.querySelectorAll('h3')].map((h) => h.textContent.trim());
    // By name alone the fork would come last; it follows its parent instead.
    expect(names).toEqual(['billing', 'zz-try', 'web']);
  });
});
