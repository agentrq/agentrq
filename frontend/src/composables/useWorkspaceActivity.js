// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * What each workspace's agent is doing, read from the `taskSummary` the
 * workspace list carries, so the sidebar and a parent's forks bar can say it
 * without opening the workspace.
 *
 * The summary is counted on the server. The stream's task events only say
 * that something changed, so the list is read again — at most once per
 * `delay`, because an agent at work sends an event for every message.
 */

/** The stream events after which a workspace's tasks may count differently. */
export const TASK_ACTIVITY_EVENTS = Object.freeze([
  'task.created',
  'task.updated',
  'task.deleted',
  'status.updated',
  'reply.received',
]);

/** One workspace: whether its agent is at work, and on what. */
export function activityOf(ws) {
  const s = ws?.taskSummary ?? {};
  const ongoing = s.ongoing ?? 0;
  return {
    working: !!ws?.agentConnected && ongoing > 0,
    ongoing,
    needsYou: s.needsYou ?? 0,
    done: s.done ?? 0,
    total: s.total ?? 0,
    current: s.current ?? null,
  };
}

/** "1 needs you" / "2 need you". */
export function needsYouLabel(n) {
  return n === 1 ? '1 needs you' : `${n} need you`;
}

/** What the agent works on, with how many more tasks are ongoing. */
export function workingLabel(a) {
  if (!a.current) return 'Working';
  const more = a.ongoing - 1;
  return more > 0 ? `Working on ${a.current.title} (+${more} more)` : `Working on ${a.current.title}`;
}

/** How many folded forks a parent's tooltip names before it counts the rest. */
export const TOOLTIP_FORKS = 6;

/**
 * A sidebar row's tooltip: the name, then what is going on. A parent whose
 * forks are folded away names the busy ones, busiest first, and counts the
 * rest, so ten forks still fit.
 */
export function activityTooltip(ws, foldedForks = []) {
  const a = activityOf(ws);
  const lines = [ws?.name ?? ''];
  if (a.working) lines.push(workingLabel(a));
  if (a.needsYou > 0) lines.push(needsYouLabel(a.needsYou));
  if (foldedForks.length > 0) {
    const busy = sortForks(foldedForks).filter((f) => rankOf(f) < 2);
    for (const f of busy.slice(0, TOOLTIP_FORKS)) lines.push(`${f.name} — ${forkStatusLabel(f)}`);
    const tail = [];
    if (busy.length > TOOLTIP_FORKS) tail.push(`+${busy.length - TOOLTIP_FORKS} more`);
    const ready = foldedForks.filter((f) => rankOf(f) === 2).length;
    if (ready > 0) tail.push(`${ready} ready to merge`);
    const quiet = foldedForks.length - busy.length - ready;
    if (quiet > 0) tail.push(busy.length + ready > 0 ? `${quiet} idle` : `${quiet} ${quiet === 1 ? 'fork' : 'forks'} idle`);
    if (tail.length > 0) lines.push(tail.join(' · '));
  }
  return lines.join('\n');
}

// Needs you, then working, then ready to merge, then the rest.
function rankOf(fork) {
  const a = activityOf(fork);
  if (a.needsYou > 0) return 0;
  if (a.working) return 1;
  if (a.total > 0 && !(fork?.unfinishedTasks > 0)) return 2;
  return 3;
}

/** Forks busiest first; the list's order otherwise. */
export function sortForks(forks = []) {
  return forks
    .map((fork, i) => ({ fork, i, rank: rankOf(fork) }))
    .sort((x, y) => x.rank - y.rank || x.i - y.i)
    .map((x) => x.fork);
}

/** A fork chip's tooltip: its name, its state, and its progress. */
export function forkTooltip(fork) {
  return [fork?.name ?? '', forkStatusLabel(fork), forkProgressLabel(fork)].filter(Boolean).join('\n');
}

/** The count on a sidebar row: its own, plus its folded forks'. */
export function needsYouCount(ws, foldedForks = []) {
  return [ws, ...foldedForks].reduce((n, w) => n + activityOf(w).needsYou, 0);
}

/** A fork's chip on its parent's page: its state, in a few words. */
export function forkStatusLabel(fork) {
  const a = activityOf(fork);
  if (a.needsYou > 0) return needsYouLabel(a.needsYou);
  if (a.working) return workingLabel(a);
  return fork?.agentConnected ? 'Idle' : 'Agent offline';
}

/** "3/5 done", or '' when the fork has no tasks yet. */
export function forkProgressLabel(fork) {
  const a = activityOf(fork);
  return a.total > 0 ? `${a.done}/${a.total} done` : '';
}

/**
 * Reads the workspace list again after task events: the first one schedules a
 * read `delay` ms later, and the events until then ride on it.
 */
export function createActivityRefresher({ refresh, delay = 2000, timers = globalThis } = {}) {
  let timer = null;
  return {
    onEvent(event) {
      if (timer !== null || !TASK_ACTIVITY_EVENTS.includes(event?.type)) return;
      timer = timers.setTimeout(() => {
        timer = null;
        refresh();
      }, delay);
    },
    stop() {
      if (timer !== null) timers.clearTimeout(timer);
      timer = null;
    },
  };
}
