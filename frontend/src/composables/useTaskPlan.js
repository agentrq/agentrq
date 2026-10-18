// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The plan a task's agent is working to.
 *
 * A task has at most one, kept as markdown and replaced whole each time the
 * agent revises it. Its steps are the markdown's task-list items; the step
 * being worked on is the first one not ticked, since the plan says no more.
 *
 * The agent owns the plan and overwrites it on every update, so a person never
 * edits it. They comment on a step instead, and a comment is an ordinary
 * message to the agent that quotes the step it is about. Steps have no ids, so
 * a comment finds its step again by the step's text; one whose step has since
 * been renamed or dropped is shown under the earlier steps rather than lost.
 */
import { ref } from 'vue';
import { getTaskPlan } from '../api';

/** What a plan comment's message starts with, followed by the step. */
export const PLAN_COMMENT_PREFIX = '> Plan step: ';

const STEP = /^\s*[-*+]\s+\[([ xX])\]\s+(.+?)\s*$/;
const FENCE = /^\s*(```|~~~)/;

/**
 * A plan's markdown as the panel draws it: its task-list items as steps, and
 * what lies between them as markdown, in order. Items inside a code fence are
 * code, not steps.
 *
 * @param {string} content
 * @returns {{ blocks: Array<{type: 'markdown', text: string} | {type: 'step', text: string, done: boolean}>,
 *             steps: Array<{text: string, done: boolean}>,
 *             progress: {done: number, total: number} | null,
 *             current: string | null }}
 */
export function parsePlan(content) {
  const blocks = [];
  const steps = [];
  let pending = [];
  let inFence = false;
  const flush = () => {
    const text = pending.join('\n').trim();
    if (text) blocks.push({ type: 'markdown', text });
    pending = [];
  };
  for (const line of String(content ?? '').split('\n')) {
    if (FENCE.test(line)) inFence = !inFence;
    const m = inFence ? null : STEP.exec(line);
    if (!m) {
      pending.push(line);
      continue;
    }
    flush();
    const step = { text: m[2], done: m[1] !== ' ' };
    blocks.push({ type: 'step', ...step });
    steps.push(step);
  }
  flush();
  const done = steps.filter((s) => s.done).length;
  return {
    blocks,
    steps,
    progress: steps.length ? { done, total: steps.length } : null,
    current: steps.find((s) => !s.done)?.text ?? null,
  };
}

/**
 * The one line the collapsed panel shows: the step being worked on, or that
 * every step is done, or the plan's first line when it has no steps.
 *
 * @param {string} content
 * @returns {string}
 */
export function planSummary(content) {
  const { steps, current } = parsePlan(content);
  if (current) return current;
  if (steps.length) return 'All steps done';
  const first = String(content ?? '').split('\n').find((l) => l.trim());
  return first ? first.replace(/^[#>\s*-]+/, '').trim() : '';
}

/**
 * The message a comment on a step becomes.
 *
 * @param {string} step the step's text
 * @param {string} comment what the person wrote
 */
export function planCommentText(step, comment) {
  return `${PLAN_COMMENT_PREFIX}${step}\n\n${String(comment).trim()}`;
}

/**
 * The step and comment a message carries, or null when it is not a comment on
 * a plan step.
 *
 * @param {string} text
 * @returns {{ step: string, comment: string } | null}
 */
export function parsePlanComment(text) {
  if (typeof text !== 'string' || !text.startsWith(PLAN_COMMENT_PREFIX)) return null;
  const end = text.indexOf('\n');
  const step = (end === -1 ? text : text.slice(0, end)).slice(PLAN_COMMENT_PREFIX.length).trim();
  if (!step) return null;
  return { step, comment: end === -1 ? '' : text.slice(end + 1).trim() };
}

/**
 * The people's comments on a plan, filed under the step each quotes. A
 * comment whose step the plan no longer has goes under `earlier`.
 *
 * @param {Array<object>} messages the task's conversation
 * @param {Array<{text: string}>} steps the plan's steps
 * @returns {{ byStep: Map<string, Array<{id: string, comment: string, createdAt: string}>>,
 *             earlier: Array<{id: string, step: string, comment: string, createdAt: string}> }}
 */
export function planComments(messages, steps) {
  const known = new Set(steps.map((s) => s.text));
  const byStep = new Map();
  const earlier = [];
  for (const m of messages ?? []) {
    if (m?.sender === 'agent') continue;
    const parsed = parsePlanComment(m?.text);
    if (!parsed) continue;
    const entry = { id: m.id, comment: parsed.comment, createdAt: m.createdAt };
    if (known.has(parsed.step)) {
      if (!byStep.has(parsed.step)) byStep.set(parsed.step, []);
      byStep.get(parsed.step).push(entry);
    } else {
      earlier.push({ ...entry, step: parsed.step });
    }
  }
  return { byStep, earlier };
}

/**
 * The task's plan, read once per task and then kept current by the stream's
 * `taskPlan.updated` events.
 *
 * @param {(workspaceId: string, taskId: string) => Promise<object|null>} [fetchPlan] test seam
 */
export function useTaskPlan(fetchPlan = (workspaceId, taskId) => getTaskPlan(workspaceId, taskId)) {
  const plan = ref(null);
  let reading = 0;

  /** Read the plan of the task now on screen; a slower, older read is dropped. */
  async function load(workspaceId, taskId) {
    const mine = ++reading;
    plan.value = null;
    try {
      const read = await fetchPlan(workspaceId, taskId);
      if (mine === reading) plan.value = read;
    } catch {
      // No plan shown is the honest answer to a read that failed; the next
      // update the agent makes brings it.
    }
  }

  /**
   * Take a stream event's plan, when it is the task's. An event outranks a
   * read still in flight, and an older plan never replaces a newer one.
   */
  function apply(payload, taskId) {
    if (!payload || String(payload.taskId) !== String(taskId)) return;
    const next = payload.plan ?? null;
    if (next && plan.value && new Date(next.updatedAt) < new Date(plan.value.updatedAt)) return;
    reading++;
    plan.value = next;
  }

  return { plan, load, apply };
}
