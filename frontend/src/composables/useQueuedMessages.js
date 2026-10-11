// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { ref, watch } from 'vue';

/**
 * Messages written while the agent is mid-turn, held in the browser.
 *
 * The ACP gateway gives a session one turn at a time and chains a second
 * delivery behind the first, so a message posted mid-turn does not reach the
 * agent — it waits in the gateway, unseen and unrecallable. The composer used
 * to answer that by refusing input altogether. This is the other answer: take
 * the message, keep it here, and send nothing until the turn ends.
 *
 * Which is what makes it editable. Nothing has left the browser, so changing
 * your mind is a local edit rather than a retraction — the thing the old
 * locked composer could not offer.
 *
 * Two rules the tests exist to hold:
 *
 * - **A queued message belongs to the task it was written in.** The task view
 *   is reused across tasks, so the queue is keyed by task and a send is
 *   addressed to the task the queue was loaded for, never to whatever is on
 *   screen when it fires. `usePendingSend` learned this the hard way.
 * - **They leave one at a time, one per turn.** The head goes when a turn ends
 *   and starts a turn of its own; the rest wait for that one to end. Which is
 *   what keeps them editable — a message only stops being yours to change at
 *   the moment it is actually its turn to go.
 */

const KEY_PREFIX = 'agentrq:queued';

/** Where one task's queue is kept, or null when no task is open. */
export function queueStorageKey(workspaceId, taskId) {
  if (!workspaceId || !taskId) return null;
  return `${KEY_PREFIX}:${workspaceId}:${taskId}`;
}

/**
 * The queue for whichever task `target` names, kept across reloads.
 *
 * @param {object} deps
 * @param {() => {workspaceId?: string, taskId?: string}} deps.target
 * @param {Storage} [deps.storage]
 */
export function useQueuedMessages({ target, storage = globalThis.localStorage }) {
  const queued = ref([]);
  let loadedTarget = {};
  let loadedKey = null;
  let nextId = 1;

  function read(key) {
    // Every read is defended rather than trusted: a private window can refuse
    // storage outright, and anything on disk may predate the current shape.
    if (!key || !storage) return [];
    try {
      const parsed = JSON.parse(storage.getItem(key));
      return Array.isArray(parsed) ? parsed : [];
    } catch {
      return [];
    }
  }

  function persist() {
    if (!loadedKey || !storage) return;
    try {
      if (queued.value.length === 0) storage.removeItem(loadedKey);
      else storage.setItem(loadedKey, JSON.stringify(queued.value));
    } catch {
      // Attachments are carried as base64, so a queue can be larger than the
      // quota allows. Failing to write costs the reload, not the queue.
    }
  }

  function load() {
    loadedTarget = target();
    loadedKey = queueStorageKey(loadedTarget.workspaceId, loadedTarget.taskId);
    queued.value = read(loadedKey);
    // Continuing past the highest id on disk, so a message queued after a
    // reload cannot collide with one queued before it.
    nextId = queued.value.reduce((high, m) => Math.max(high, m.id), 0) + 1;
  }

  load();
  watch(
    () => {
      const t = target();
      return queueStorageKey(t.workspaceId, t.taskId);
    },
    load
  );

  /** Hold a message rather than sending it. */
  function enqueue({ text, atts = [] }) {
    queued.value = [...queued.value, { id: nextId, text, atts }];
    nextId += 1;
    persist();
  }

  /**
   * Rewrite one queued message.
   *
   * Emptying it removes it — an empty message is a deletion expressed the
   * obvious way, and sending one would be worse than honouring it. A message
   * whose text is gone but whose attachment is not is still a message.
   */
  function edit(id, text) {
    queued.value = queued.value.flatMap((m) => {
      if (m.id !== id) return [m];
      if (!text.trim() && m.atts.length === 0) return [];
      return [{ ...m, text }];
    });
    persist();
  }

  /** Drop one queued message unsent. */
  function remove(id) {
    queued.value = queued.value.filter((m) => m.id !== id);
    persist();
  }

  /**
   * Move one queued message to the head of the queue, so it is the next to go.
   *
   * What "send now" does with it: the turn is stopped, and the turn's end
   * sends the head as it always does. Sent any earlier, the gateway would chain
   * it behind the turn being stopped, and the end of that turn would send the
   * next queued message into this one.
   */
  function promote(id) {
    const chosen = queued.value.find((m) => m.id === id)
    if (!chosen) return
    queued.value = [chosen, ...queued.value.filter((m) => m.id !== id)]
    persist()
  }

  /**
   * Take the message at the head of the queue, addressed to its own task.
   *
   * One, not all of them: each starts a turn of its own, so the next is taken
   * when that turn ends. Everything still queued therefore stays editable
   * until its own moment comes.
   *
   * It leaves the queue here rather than when the caller's send succeeds: a
   * delivery that fails puts its text back in the composer, and leaving a copy
   * queued as well would send it twice.
   *
   * @returns {{text: string, atts: Array, target: object} | null}
   */
  function dequeue() {
    const [head, ...rest] = queued.value;
    if (!head) return null;
    const to = loadedTarget;
    queued.value = rest;
    persist();
    return { text: head.text, atts: head.atts, target: to };
  }

  return { queued, enqueue, edit, remove, promote, dequeue };
}
