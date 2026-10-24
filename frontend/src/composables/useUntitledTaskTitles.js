// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import TitleWorker from '../workers/titleWorker.js?worker';
import { getTask, updateTaskTitle, recordTelemetry, TELEMETRY_LOCAL_AI_TITLE_GENERATE } from '../api';
import { localTitleModelSupported } from './useAutoTitle';

/**
 * Naming a task created without a title.
 *
 * The server calls such a task "Untitled". The browser that created it then
 * asks the local title model for a name, in the background, and renames the
 * task — unless somebody renamed it first. The work outlives the form: it is
 * queued here, not in a component, so leaving the page does not cancel it.
 *
 * Whether to do it at all is a per-workspace choice kept on this device only,
 * on by default. A phone never does it: the model does not run there (see
 * localTitleModelSupported).
 */

/** What the server calls a task created without a title. */
export const UNTITLED_TASK_TITLE = 'Untitled';

export const AUTO_TITLE_PREFIX = 'auto_title_';

export function autoTitleKey(workspaceId) {
  return `${AUTO_TITLE_PREFIX}${workspaceId}`;
}

/**
 * Whether untitled tasks in this workspace are named automatically, here.
 *
 * Unset, or unreadable, means on.
 */
export function isAutoTitleOn(workspaceId, { storage = globalThis.localStorage } = {}) {
  let saved = null;
  try {
    saved = storage?.getItem(autoTitleKey(workspaceId)) ?? null;
  } catch {
    saved = null;
  }
  return saved !== 'off';
}

/** Record the choice. */
export function setAutoTitleOn(workspaceId, on, storage = globalThis.localStorage) {
  try {
    storage.setItem(autoTitleKey(workspaceId), on ? 'on' : 'off');
    return true;
  } catch {
    return false;
  }
}

/**
 * A queue that names untitled tasks one at a time, with one model loaded.
 *
 * The worker is let go once the queue is empty: the model is large, and
 * holding it for a task that may never come costs every page its memory.
 */
export function createUntitledTaskTitler({
  createWorker = () => new TitleWorker(),
  api = { getTask, updateTaskTitle, recordTelemetry },
  supported = localTitleModelSupported,
} = {}) {
  const queue = [];
  let worker = null;
  let draining = null;
  let nextId = 0;

  const generate = (text) => new Promise((resolve, reject) => {
    const id = ++nextId;
    const onMessage = (e) => {
      const { id: answerId, type, data, error } = e.data;
      if (answerId !== id) return;
      if (type === 'SUCCESS') {
        worker.removeEventListener('message', onMessage);
        resolve((data?.title || '').trim());
      } else if (type === 'ERROR') {
        worker.removeEventListener('message', onMessage);
        reject(new Error(error));
      }
    };
    worker.addEventListener('message', onMessage);
    worker.postMessage({ id, type: 'GENERATE_TITLE', data: { text } });
  });

  const nameOne = async ({ workspaceId, taskId, body }) => {
    worker ??= createWorker();
    api.recordTelemetry(TELEMETRY_LOCAL_AI_TITLE_GENERATE, workspaceId);
    const title = await generate(body);
    if (!title) return;
    // Read again just before writing: a rename by hand while the model ran
    // is the person's answer, and wins.
    const { task } = await api.getTask(workspaceId, taskId);
    if (task?.title !== UNTITLED_TASK_TITLE) return;
    await api.updateTaskTitle(workspaceId, taskId, title);
  };

  const drain = async () => {
    while (queue.length) {
      const job = queue.shift();
      try {
        await nameOne(job);
      } catch (err) {
        console.warn(`Could not name untitled task ${job.taskId}; it keeps its title:`, err);
      }
    }
    worker?.terminate();
    worker = null;
    draining = null;
  };

  /**
   * Queue a task for naming. Returns whether it was queued: not when the
   * browser cannot run the model, or there is too little text to name.
   */
  const enqueue = (workspaceId, taskId, body) => {
    const text = (body || '').trim();
    if (!supported() || !taskId || text.length < 5) return false;
    queue.push({ workspaceId, taskId, body: text });
    draining ??= drain();
    return true;
  };

  /** Resolves once everything queued so far has been tried. */
  const idle = () => draining ?? Promise.resolve();

  return { enqueue, idle };
}

const titler = createUntitledTaskTitler();

/** Name a task created without a title, if this workspace wants that here. */
export function nameUntitledTask(workspaceId, taskId, body) {
  if (!isAutoTitleOn(workspaceId)) return false;
  return titler.enqueue(workspaceId, taskId, body);
}
