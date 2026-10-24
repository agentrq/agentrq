// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi } from 'vitest';

import {
  MAX_WORKSPACE_NAME,
  canFork,
  defaultForkName,
  fitName,
  isFork,
  mergeBlockedReason,
  mergeConfirmMessage,
  mergedMessage,
  parentName,
  forksAfterParents,
  unfinishedLabel,
  useForkActions,
  workspaceTree,
} from '../src/composables/useWorkspaceForks';

const parent = { id: 'p1', name: 'ops', workingDirectory: '/srv/ops' };
const fork = { id: 'f1', name: 'ops fork', forkOfId: 'p1', forkOf: { id: 'p1', name: 'ops' } };

describe('what a workspace can do', () => {
  it('knows a fork by its parent', () => {
    expect(isFork(fork)).toBe(true);
    expect(isFork(parent)).toBe(false);
    expect(isFork(null)).toBe(false);
  });

  it('forks only an active workspace that is not a fork and not the supervisor', () => {
    expect(canFork(parent)).toBe(true);
    expect(canFork(fork)).toBe(false);
    expect(canFork({ id: 's', name: 'supervisor' })).toBe(false);
    expect(canFork({ ...parent, archivedAt: '2026-01-01' })).toBe(false);
    expect(canFork(undefined)).toBe(false);
  });
});

describe('names', () => {
  it('cuts to the server width in characters, not UTF-16 units', () => {
    const emoji = '🚀'.repeat(MAX_WORKSPACE_NAME + 5);
    expect(Array.from(fitName(emoji))).toHaveLength(MAX_WORKSPACE_NAME);
    expect(fitName('  spaced  ')).toBe('spaced');
    expect(fitName(undefined)).toBe('');
    expect(fitName('abcdef', 3)).toBe('abc');
  });

  it('starts the prompt on the server default, still fitting', () => {
    expect(defaultForkName('ops')).toBe('ops-fork');
    expect(defaultForkName('Billing API')).toBe('billing-api-fork');
    expect(defaultForkName('проект')).toBe('fork');
    const long = defaultForkName('x'.repeat(200));
    expect(long).toHaveLength(MAX_WORKSPACE_NAME);
    expect(long.endsWith('x-fork')).toBe(true);
  });

  it('names the parent from the list, then the store, then generically', () => {
    expect(parentName(fork)).toBe('ops');
    expect(parentName({ forkOfId: 'p1' }, [parent])).toBe('ops');
    expect(parentName({ forkOfId: 'zz' }, [parent])).toBe('its parent');
    expect(parentName(null)).toBe('its parent');
  });
});

describe('workspaceTree', () => {
  it('puts forks under their parent, in list order', () => {
    const other = { id: 'o1', name: 'web' };
    const fork2 = { id: 'f2', name: 'ops try', forkOfId: 'p1' };
    const tree = workspaceTree([fork, parent, other, fork2]);
    expect(tree.map((g) => g.workspace.id)).toEqual(['p1', 'o1']);
    expect(tree[0].forks.map((f) => f.id)).toEqual(['f1', 'f2']);
    expect(tree[1].forks).toEqual([]);
  });

  it('keeps a fork whose parent is not listed at the top, so it can still be merged', () => {
    expect(workspaceTree([fork]).map((g) => g.workspace.id)).toEqual(['f1']);
    expect(workspaceTree()).toEqual([]);
  });

  it('flattens to each workspace followed by its forks', () => {
    const other = { id: 'o1', name: 'web' };
    expect(forksAfterParents([fork, other, parent]).map((w) => w.id)).toEqual(['o1', 'p1', 'f1']);
    expect(forksAfterParents()).toEqual([]);
  });
});

describe('merge wording', () => {
  it('counts what blocks a merge', () => {
    expect(unfinishedLabel(1)).toBe('1 task is not finished');
    expect(unfinishedLabel(2)).toBe('2 tasks are not finished');
    expect(mergeBlockedReason({ ...fork, unfinishedTasks: 2 })).toBe('2 tasks are not finished');
    expect(mergeBlockedReason(fork)).toBe('');
    expect(mergeBlockedReason(null)).toBe('');
  });

  it('says what stops, what moves, what goes, and the folder left behind', () => {
    expect(mergeConfirmMessage({ ...fork, taskCount: 3, workingDirectory: '/home/me/.agentrq/forks/f1' }, 'ops')).toBe(
      "The fork's agent is stopped, 3 tasks move back to ops, and the fork is removed. Its folder on the machine is left as it is: /home/me/.agentrq/forks/f1"
    );
    expect(mergeConfirmMessage({ ...fork, taskCount: 1 }, 'ops')).toBe(
      "The fork's agent is stopped, 1 task moves back to ops, and the fork is removed. It has no folder on a machine yet."
    );
    expect(mergeConfirmMessage(fork, 'ops')).toMatch(/, its tasks move back to ops,/);
  });

  it('says the folder is deleted, and its branch kept, when asked to delete it', () => {
    const f = { ...fork, taskCount: 3, workingDirectory: '/home/me/.agentrq/forks/f1' };
    expect(mergeConfirmMessage(f, 'ops', true)).toBe(
      "The fork's agent is stopped, 3 tasks move back to ops, and the fork is removed. Its folder on the machine is deleted, and its git branch is kept: /home/me/.agentrq/forks/f1"
    );
    // Nothing to delete on a fork that never ran.
    expect(mergeConfirmMessage({ ...fork, taskCount: 1 }, 'ops', true)).toMatch(/It has no folder on a machine yet\.$/);
  });

  it('reports the moved count', () => {
    expect(mergedMessage(2, 'ops')).toBe('Merged into ops: 2 tasks moved back');
    expect(mergedMessage(1, 'ops')).toBe('Merged into ops: 1 task moved back');
    expect(mergedMessage(undefined, 'ops')).toBe('Merged into ops: 0 tasks moved back');
  });
});

function setup(over = {}) {
  const router = { push: vi.fn() };
  const store = { workspaces: [parent, fork], fetchWorkspaces: vi.fn(() => Promise.resolve()) };
  const toasts = { notifySuccess: vi.fn(), notifyError: vi.fn() };
  const deps = {
    forkWorkspace: vi.fn(() => Promise.resolve({ workspace: { id: 'f9', name: 'ops fork' } })),
    mergeFork: vi.fn(() => Promise.resolve({ parentId: 'p1', movedTasks: 2 })),
    fetchTaskCounts: vi.fn(() => Promise.resolve({ ongoing: 0, notstarted: 0, scheduled: 1, completed: 2 })),
    ...over,
  };
  const actions = useForkActions({ router, store, toasts, ...deps });
  return { actions, router, store, toasts, deps };
}

describe('useForkActions: forking', () => {
  it('prompts on the default name, forks, refreshes the list and goes to the fork', async () => {
    const { actions, router, store, toasts, deps } = setup();
    actions.startFork(parent);
    expect(actions.state.forking).toEqual(parent);
    expect(actions.state.forkName).toBe('ops-fork');

    const made = await actions.confirmFork('  Ops try  ');
    expect(deps.forkWorkspace).toHaveBeenCalledWith('p1', { name: 'ops-try' });
    expect(store.fetchWorkspaces).toHaveBeenCalled();
    expect(toasts.notifySuccess).toHaveBeenCalledWith('Forked ops');
    expect(router.push).toHaveBeenCalledWith('/workspaces/f9');
    expect(made.id).toBe('f9');
    expect(actions.state.forking).toBe(null);
  });

  it('uses the prompt as typed when no name is passed', async () => {
    const { actions, deps } = setup();
    actions.startFork(parent);
    actions.state.forkName = 'mine';
    await actions.confirmFork();
    expect(deps.forkWorkspace).toHaveBeenCalledWith('p1', { name: 'mine' });
  });

  it('does not offer to fork a fork, and cancels', () => {
    const { actions } = setup();
    actions.startFork(fork);
    expect(actions.state.forking).toBe(null);
    actions.startFork(parent);
    actions.cancelFork();
    expect(actions.state.forking).toBe(null);
  });

  it('does nothing without a prompt open, or while busy', async () => {
    const { actions, deps } = setup();
    expect(await actions.confirmFork('x')).toBe(null);
    actions.startFork(parent);
    actions.state.busy = true;
    expect(await actions.confirmFork('x')).toBe(null);
    expect(deps.forkWorkspace).not.toHaveBeenCalled();
  });

  it('shows a refusal as the server wrote it, and keeps the prompt', async () => {
    const { actions, toasts, router } = setup({ forkWorkspace: vi.fn(() => Promise.reject(new Error('a fork cannot be forked'))) });
    actions.startFork(parent);
    expect(await actions.confirmFork('x')).toBe(null);
    expect(toasts.notifyError).toHaveBeenCalledWith('a fork cannot be forked');
    expect(router.push).not.toHaveBeenCalled();
    expect(actions.state.forking).toEqual(parent);
    expect(actions.state.busy).toBe(false);
  });

  it('stays put when the answer names no fork', async () => {
    const { actions, router } = setup({ forkWorkspace: vi.fn(() => Promise.resolve({})) });
    actions.startFork(parent);
    expect(await actions.confirmFork('x')).toBe(null);
    expect(router.push).not.toHaveBeenCalled();
  });
});

describe('useForkActions: merging', () => {
  it('counts the tasks for the confirmation, merges, and goes to the parent', async () => {
    const { actions, router, toasts, deps, store } = setup();
    await actions.startMerge(fork);
    expect(deps.fetchTaskCounts).toHaveBeenCalledWith('f1');
    expect(actions.state.merging).toEqual({ ...fork, taskCount: 3 });

    const res = await actions.confirmMerge();
    expect(deps.mergeFork).toHaveBeenCalledWith('f1', { deleteFolder: false });
    expect(store.fetchWorkspaces).toHaveBeenCalled();
    expect(toasts.notifySuccess).toHaveBeenCalledWith('Merged into ops: 2 tasks moved back');
    expect(router.push).toHaveBeenCalledWith('/workspaces/p1');
    expect(res.movedTasks).toBe(2);
    expect(actions.state.merging).toBe(null);
  });

  it('asks for the folder to be deleted when told to', async () => {
    const { actions, deps } = setup();
    await actions.startMerge(fork);
    await actions.confirmMerge(true);
    expect(deps.mergeFork).toHaveBeenCalledWith('f1', { deleteFolder: true });
  });

  it('counts only the statuses it is given', async () => {
    const { actions } = setup({ fetchTaskCounts: vi.fn(() => Promise.resolve({})) });
    await actions.startMerge(fork);
    expect(actions.state.merging.taskCount).toBe(0);
  });

  it('still opens when the counts cannot be read, without a number', async () => {
    const { actions } = setup({ fetchTaskCounts: vi.fn(() => Promise.reject(new Error('network unreachable'))) });
    await actions.startMerge(fork);
    expect(actions.state.merging.taskCount).toBe(undefined);
  });

  it('does not open for a blocked fork, or for a workspace that is not a fork', async () => {
    const { actions } = setup();
    await actions.startMerge({ ...fork, unfinishedTasks: 1 });
    await actions.startMerge(parent);
    expect(actions.state.merging).toBe(null);
  });

  it('falls back to the fork\'s parent id when the answer has none', async () => {
    const { actions, router } = setup({ mergeFork: vi.fn(() => Promise.resolve({ movedTasks: 1 })) });
    await actions.startMerge(fork);
    await actions.confirmMerge();
    expect(router.push).toHaveBeenCalledWith('/workspaces/p1');
  });

  it('shows a 409 as the server wrote it and closes the dialog', async () => {
    const { actions, toasts, router } = setup({
      mergeFork: vi.fn(() => Promise.reject(new Error('1 task in this fork is not finished'))),
    });
    await actions.startMerge(fork);
    expect(await actions.confirmMerge()).toBe(null);
    expect(toasts.notifyError).toHaveBeenCalledWith('1 task in this fork is not finished');
    expect(router.push).not.toHaveBeenCalled();
    expect(actions.state.merging).toBe(null);
    expect(actions.state.busy).toBe(false);
  });

  it('does nothing without a dialog open, or while busy, and cancels', async () => {
    const { actions, deps } = setup();
    expect(await actions.confirmMerge()).toBe(null);
    await actions.startMerge(fork);
    actions.state.busy = true;
    expect(await actions.confirmMerge()).toBe(null);
    expect(deps.mergeFork).not.toHaveBeenCalled();
    actions.cancelMerge();
    expect(actions.state.merging).toBe(null);
  });
});
