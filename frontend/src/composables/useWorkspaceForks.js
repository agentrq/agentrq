// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Workspace forks, as the interface sees them.
 *
 * A fork is an ordinary workspace with `forkOfId` set: its own queue and agent,
 * the parent's settings, memory and skills. It is left one way only — merged
 * back, which moves its tasks to the parent — so a fork offers no Delete and no
 * Archive anywhere, and a fork cannot itself be forked. The server refuses all
 * of those too; deciding them here is what keeps the buttons from being offered
 * only to be refused.
 */

import { reactive } from 'vue';
import * as api from '../api';
import { useFormat } from './useFormat';

const { toKebabCase } = useFormat();

/** The account-wide workspace, which is never forked. */
export const SUPERVISOR_WORKSPACE = 'supervisor';

/** The width of a workspace's name on the server, in characters. */
export const MAX_WORKSPACE_NAME = 128;

export function isFork(ws) {
  return !!ws?.forkOfId;
}

/** Whether this workspace may be forked: not a fork, not the supervisor, not archived. */
export function canFork(ws) {
  return !!ws && !isFork(ws) && ws.name !== SUPERVISOR_WORKSPACE && !ws.archivedAt;
}

/**
 * A name cut to what the server accepts, counted in characters rather than
 * UTF-16 units so an emoji is not split in half. Trimmed, as the server trims.
 */
export function fitName(name, max = MAX_WORKSPACE_NAME) {
  const chars = Array.from(String(name ?? '').trim());
  return chars.slice(0, max).join('').trim();
}

/** A name as the workspace form makes one: kebab-case, cut to fit. */
export function kebabName(name, max = MAX_WORKSPACE_NAME) {
  return toKebabCase(fitName(toKebabCase(String(name ?? '')), max));
}

/** What the name prompt starts on: the server's own default, "<parent>-fork". */
export function defaultForkName(parentName) {
  const suffix = '-fork';
  const base = kebabName(parentName, MAX_WORKSPACE_NAME - suffix.length);
  return base ? base + suffix : 'fork';
}

/** The parent's name for a fork: the list carries it, the store is the fallback. */
export function parentName(fork, workspaces = []) {
  if (fork?.forkOf?.name) return fork.forkOf.name;
  const parent = workspaces.find((w) => String(w.id) === String(fork?.forkOfId));
  return parent?.name ?? 'its parent';
}

/**
 * The workspaces as the sidebar draws them: each parent, then its forks.
 *
 * A fork whose parent is not in the list — archived, or not loaded yet — is
 * shown at the top level rather than hidden, because a fork nobody can see is
 * a fork nobody can merge.
 */
export function workspaceTree(workspaces = []) {
  const ids = new Set(workspaces.map((w) => String(w.id)));
  const forks = new Map();
  for (const w of workspaces) {
    if (isFork(w) && ids.has(String(w.forkOfId))) {
      const key = String(w.forkOfId);
      if (!forks.has(key)) forks.set(key, []);
      forks.get(key).push(w);
    }
  }
  return workspaces
    .filter((w) => !isFork(w) || !ids.has(String(w.forkOfId)))
    .map((w) => ({ workspace: w, forks: forks.get(String(w.id)) ?? [] }));
}

/** The same order as a flat list: each workspace, then its forks. */
export function forksAfterParents(workspaces = []) {
  return workspaceTree(workspaces).flatMap((g) => [g.workspace, ...g.forks]);
}

/** "1 task is not finished" / "2 tasks are not finished". */
export function unfinishedLabel(n) {
  return n === 1 ? '1 task is not finished' : `${n} tasks are not finished`;
}

/**
 * Why this fork cannot be merged right now, or '' when it can.
 *
 * The count comes from the workspace list and can be a moment old; the server
 * counts again inside the merge and refuses, and that refusal is shown as-is.
 */
export function mergeBlockedReason(fork) {
  const n = Number(fork?.unfinishedTasks) || 0;
  return n > 0 ? unfinishedLabel(n) : '';
}

/** What the merge confirmation says will happen. */
export function mergeConfirmMessage(fork, parent, deleteFolder = false) {
  const n = fork?.taskCount;
  const tasks = typeof n === 'number' ? `${n} ${n === 1 ? 'task moves' : 'tasks move'}` : 'its tasks move';
  let folder = 'It has no folder on a machine yet.';
  if (fork?.workingDirectory) {
    folder = deleteFolder
      ? `Its folder on the machine is deleted, and its git branch is kept: ${fork.workingDirectory}`
      : `Its folder on the machine is left as it is: ${fork.workingDirectory}`;
  }
  return `The fork's agent is stopped, ${tasks} back to ${parent}, and the fork is removed. ${folder}`;
}

/** The toast after a merge. */
export function mergedMessage(movedTasks, parent) {
  const n = Number(movedTasks) || 0;
  return `Merged into ${parent}: ${n} ${n === 1 ? 'task' : 'tasks'} moved back`;
}

/**
 * Forking and merging, with the state their two dialogs need.
 *
 * Shared by every place that offers them — the sidebar's menu, the settings
 * page — so both say the same thing and land in the same place.
 *
 * @param {object} deps
 * @param {{ push: Function }} deps.router
 * @param {{ fetchWorkspaces: Function, workspaces: Array }} deps.store
 * @param {{ notifySuccess: Function, notifyError: Function }} deps.toasts
 */
export function useForkActions({
  router,
  store,
  toasts,
  forkWorkspace = api.forkWorkspace,
  mergeFork = api.mergeFork,
  fetchTaskCounts = api.fetchTaskCounts,
}) {
  const state = reactive({
    forking: null,
    forkName: '',
    merging: null,
    busy: false,
  });

  function startFork(ws) {
    if (!canFork(ws)) return;
    state.forking = ws;
    state.forkName = defaultForkName(ws.name);
  }

  function cancelFork() {
    state.forking = null;
  }

  /** Makes the fork, and goes to it. Resolves to the fork, or null. */
  async function confirmFork(name = state.forkName) {
    const parent = state.forking;
    if (!parent || state.busy) return null;
    state.busy = true;
    try {
      const res = await forkWorkspace(parent.id, { name: kebabName(name) });
      const fork = res?.workspace;
      state.forking = null;
      await store.fetchWorkspaces();
      toasts.notifySuccess(`Forked ${parent.name}`);
      if (fork?.id) router.push(`/workspaces/${fork.id}`);
      return fork ?? null;
    } catch (err) {
      toasts.notifyError(err.message);
      return null;
    } finally {
      state.busy = false;
    }
  }

  /**
   * Opens the confirmation, unless the fork is known to be blocked.
   *
   * The counts are read now rather than taken from the list, so the dialog
   * says how many tasks move as of the moment somebody is deciding. Without
   * them it still opens, saying "its tasks" instead of a number.
   */
  async function startMerge(fork) {
    if (!isFork(fork) || mergeBlockedReason(fork)) return;
    let taskCount;
    try {
      const c = await fetchTaskCounts(fork.id);
      taskCount = (c.ongoing || 0) + (c.notstarted || 0) + (c.scheduled || 0) + (c.completed || 0);
    } catch {
      taskCount = undefined;
    }
    state.merging = { ...fork, taskCount };
  }

  function cancelMerge() {
    state.merging = null;
  }

  /** Merges, then goes to the parent. A refusal is shown as the server wrote it. */
  async function confirmMerge(deleteFolder = false) {
    const fork = state.merging;
    if (!fork || state.busy) return null;
    state.busy = true;
    const parent = parentName(fork, store.workspaces);
    try {
      const res = await mergeFork(fork.id, { deleteFolder });
      state.merging = null;
      await store.fetchWorkspaces();
      toasts.notifySuccess(mergedMessage(res?.movedTasks, parent));
      router.push(`/workspaces/${res?.parentId || fork.forkOfId}`);
      return res;
    } catch (err) {
      state.merging = null;
      toasts.notifyError(err.message);
      return null;
    } finally {
      state.busy = false;
    }
  }

  return { state, startFork, cancelFork, confirmFork, startMerge, cancelMerge, confirmMerge };
}
