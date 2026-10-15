// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Folding a task pushed over SSE into the task already on screen.
 *
 * An event payload is meant to be the whole task, and the view used to take it
 * at its word and replace what it had. But the payloads are built in several
 * places in the backend, and one built without a relation is indistinguishable
 * from one whose relation is genuinely empty — so a single publisher that
 * forgot to load the tool calls emptied the trajectory's tool lane the moment
 * any new message arrived.
 *
 * Merging instead of replacing makes that unrepresentable: a relation the
 * payload does not carry keeps the value the client already had. Nothing in
 * the product removes a message or a tool call from a task, so the only way an
 * arriving list can be shorter than the one on screen is that it was not
 * loaded.
 *
 * Tool calls go further: a task is read with only its latest (#921), so the
 * merge keeps every call either side holds — older pages included.
 */

/** The task's relations an arriving copy replaces whole, when it has them. */
const TASK_RELATIONS = ['messages'];

function hasEntries(value) {
  return Array.isArray(value) && value.length > 0;
}

function byCreatedAt(a, b) {
  return new Date(a.createdAt) - new Date(b.createdAt);
}

/** Every call either list holds, the arriving copy of each winning, oldest first. */
function unionToolCalls(held, arriving) {
  if (!hasEntries(held)) return arriving;
  if (!hasEntries(arriving)) return held;
  const byId = new Map(held.map((c) => [c.id, c]));
  for (const c of arriving) byId.set(c.id, c);
  return [...byId.values()].sort(byCreatedAt);
}

/**
 * The task to render, given the one on screen and the one an event carried.
 *
 * Scalar fields always come from the event — status, title and the rest are
 * exactly what it is reporting. Messages come from the event too, unless it
 * carries none and the current task does; tool calls are the union of both,
 * and toolCallCount the larger count.
 *
 * @param {object|null} current  the task the view is holding, if any
 * @param {object|null} incoming the task payload the event carried
 * @returns {object|null}
 */
export function mergeTaskUpdate(current, incoming) {
  if (!incoming) return current ?? null;
  // A payload for a different task replaces nothing: it describes something
  // else, and merging the two would splice one task's history onto another.
  if (!current || current.id !== incoming.id) return incoming;

  const merged = { ...incoming };
  for (const relation of TASK_RELATIONS) {
    if (!hasEntries(merged[relation]) && hasEntries(current[relation])) {
      merged[relation] = current[relation];
    }
  }
  const toolCalls = unionToolCalls(current.toolCalls, incoming.toolCalls);
  if (toolCalls !== undefined) merged.toolCalls = toolCalls;
  const count = Math.max(current.toolCallCount || 0, incoming.toolCallCount || 0, toolCalls?.length || 0);
  if (count) merged.toolCallCount = count;
  return merged;
}

/**
 * The task with one tool call put in its place: replaced when the task already
 * lists it (a permission answered), appended and counted when it is new.
 *
 * A `toolCall.updated` event carries that one call and nothing else, so a run
 * of thousands never resends the calls the client already has (#921).
 *
 * @param {object|null} task the task the view is holding, if any
 * @param {object|null} call the tool call the event carried
 * @returns {object|null} a new task, or `task` itself when the call is not its
 */
export function upsertToolCall(task, call) {
  if (!task || !call?.id || String(call.taskId) !== String(task.id)) return task ?? null;
  const calls = Array.isArray(task.toolCalls) ? task.toolCalls : [];
  const at = calls.findIndex((c) => c.id === call.id);
  if (at !== -1) return { ...task, toolCalls: calls.map((c, i) => (i === at ? call : c)) };
  // A call older than every loaded one is on a page not loaded yet; it comes with it.
  const unloaded = (task.toolCallCount ?? 0) > calls.length;
  if (unloaded && calls.length && new Date(call.createdAt) < new Date(calls[0].createdAt)) return task;
  return { ...task, toolCalls: [...calls, call], toolCallCount: (task.toolCallCount ?? calls.length) + 1 };
}
