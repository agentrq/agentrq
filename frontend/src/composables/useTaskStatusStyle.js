// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * The one place a task's status becomes a colour.
 *
 * Every list of tasks colour-codes its rows the same way, and until this file
 * existed each list carried its own copy of the switch — so a status added in
 * one place quietly rendered grey in the others. The Active feed, the global
 * task list and the kanban board now all read from here.
 *
 * Two shapes come out of the same decision:
 *
 * - `taskDotClass` fills the small round status dot.
 * - `taskAccentClass` paints the bar down the left edge of a card. It is the
 *   *same* colour, deliberately: the dot and the bar are one signal seen at two
 *   sizes, and letting them disagree would make the bar a second thing to learn.
 *
 * The glows (`shadow-[...]`) mark the statuses that are live — something is
 * happening, or is waiting on you — and are what separates `ongoing` from a
 * finished `completed` at a glance, both being green.
 *
 * Extracted rather than inlined because the project has no component-test
 * harness; a plain function is testable, a template expression is not.
 */

/**
 * Whether the task is waiting on the person rather than the agent.
 *
 * Kept local to this module: it is the same rule `pendingOnHuman` applies in
 * `useTaskGroups`, but that one filters a list while this one asks about a
 * single task, and importing across composables to share four lines would only
 * couple the two.
 *
 * @param {object} t
 */
function isPendingOnHuman(t) {
  if (!t || typeof t !== 'object') return false;
  if (t.status === 'completed' || t.status === 'rejected') return false;
  if (t.status === 'notstarted' && t.assignee === 'human') return true;
  return !!(
    t.messages &&
    t.messages.some(
      (m) => m.metadata?.type === 'permission_request' && m.metadata?.status === 'pending'
    )
  );
}

/**
 * Resolve a task (or a bare status string) to one of the colour keys below.
 *
 * @param {object|string} task
 * @returns {'pending'|'ongoing'|'notstarted'|'completed'|'rejected'|'blocked'|'cron'|'unknown'}
 */
export function taskStatusTone(task) {
  if (isPendingOnHuman(task)) return 'pending';

  const status = typeof task === 'string' ? task : task?.status;
  switch (status) {
    case 'ongoing':
    case 'notstarted':
    case 'completed':
    case 'rejected':
    case 'blocked':
    case 'cron':
      return status;
    default:
      return 'unknown';
  }
}

const DOT_CLASSES = {
  pending: 'bg-yellow-400 shadow-[0_0_8px_rgba(250,204,21,0.4)]',
  ongoing: 'bg-green-500 shadow-[0_0_8px_rgba(34,197,94,0.4)] animate-pulse',
  notstarted: 'bg-gray-400 dark:bg-zinc-500',
  completed: 'bg-green-500',
  rejected: 'bg-red-500',
  blocked: 'bg-red-500 shadow-[0_0_8px_rgba(239,68,68,0.4)]',
  cron: 'bg-cyan-300 shadow-[0_0_8px_rgba(103,232,249,0.4)]',
  unknown: 'bg-gray-300 dark:bg-zinc-600',
};

// The accent bar is the dot's colour with the glow and the pulse dropped: a
// 4px strip the height of a card is already loud, and animating it would pull
// the eye off the card's text.
//
// Every tone is at full strength, including the finished ones. Fading those was
// tried first, on the reasoning that a done card should not shout as loudly as a
// blocked one — but on a borderless card the bar is the only edge, and a faded
// bar left those cards with no edge at all. What separates done from running is
// the greyed title, and on the board, the column it sits in.
const ACCENT_CLASSES = {
  pending: 'bg-yellow-400',
  ongoing: 'bg-green-500',
  notstarted: 'bg-gray-300 dark:bg-zinc-600',
  completed: 'bg-green-500',
  rejected: 'bg-red-500',
  blocked: 'bg-red-500',
  cron: 'bg-cyan-400',
  unknown: 'bg-gray-200 dark:bg-zinc-700',
};

/**
 * Tailwind classes for a task's status dot.
 *
 * @param {object|string} task  a task, or a bare status string
 * @returns {string}
 */
export function taskDotClass(task) {
  return DOT_CLASSES[taskStatusTone(task)];
}

/**
 * Tailwind classes for the bar down a card's left edge.
 *
 * @param {object|string} task  a task, or a bare status string
 * @returns {string}
 */
export function taskAccentClass(task) {
  return ACCENT_CLASSES[taskStatusTone(task)];
}

export function useTaskStatusStyle() {
  return { taskStatusTone, taskDotClass, taskAccentClass };
}
