// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The workspaces page: each workspace a card with its first few Active tasks,
 * and its forks under it rather than beside it.
 *
 * Kept out of the view so the decisions are plain functions with tests: which
 * card a fork sits under, what a search leaves, which column a card goes in,
 * and what a card's counts and "+N more" say.
 */

/** How many tasks a card shows: the server's own cap on a preview. */
export const OVERVIEW_TASKS = 4;

/** On a phone a card shows one fewer, to keep the list short to scroll. */
export const OVERVIEW_TASKS_NARROW = 3;

const FINISHED = new Set(['completed', 'rejected']);

/**
 * Workspaces as cards: every workspace that is not a fork, each with its forks.
 *
 * A fork whose parent is not in the list (archived, or not loaded yet) stands
 * as a card of its own rather than disappearing.
 *
 * @param {Array<{ id: string, forkOfId?: string }>} workspaces
 * @returns {Array<{ workspace: object, forks: object[] }>}
 */
export function groupWorkspaces(workspaces) {
  const ids = new Set(workspaces.map((w) => w.id));
  const forksOf = new Map();
  for (const w of workspaces) {
    if (w.forkOfId && ids.has(w.forkOfId)) {
      if (!forksOf.has(w.forkOfId)) forksOf.set(w.forkOfId, []);
      forksOf.get(w.forkOfId).push(w);
    }
  }
  return workspaces
    .filter((w) => !(w.forkOfId && ids.has(w.forkOfId)))
    .map((workspace) => ({ workspace, forks: forksOf.get(workspace.id) ?? [] }));
}

/** Whether a workspace's name or description contains the query, ignoring case. */
export function workspaceMatches(workspace, query) {
  const q = query.toLowerCase();
  return workspace.name.toLowerCase().includes(q) || !!workspace.description?.toLowerCase().includes(q);
}

/**
 * The cards a search leaves. A parent that matches keeps all its forks; one
 * that does not stays, dimmed, only to hold the forks that do.
 *
 * @returns {Array<{ workspace: object, forks: object[], dimmed: boolean }>}
 */
export function filterWorkspaceGroups(groups, query) {
  if (!query) return groups.map((g) => ({ ...g, dimmed: false }));
  const result = [];
  for (const g of groups) {
    if (workspaceMatches(g.workspace, query)) {
      result.push({ ...g, dimmed: false });
      continue;
    }
    const forks = g.forks.filter((f) => workspaceMatches(f, query));
    if (forks.length > 0) result.push({ workspace: g.workspace, forks, dimmed: true });
  }
  return result;
}

/**
 * Cards into columns, each into whichever is shortest so far, counting a card
 * with its forks as that many cards. Reading order stays roughly row by row,
 * and a workspace with forks does not leave a gap beside it.
 */
export function splitIntoColumns(groups, columns) {
  const cols = Array.from({ length: columns }, () => []);
  const heights = new Array(columns).fill(0);
  for (const g of groups) {
    const i = heights.indexOf(Math.min(...heights));
    cols[i].push(g);
    heights[i] += 1 + g.forks.length;
  }
  return cols;
}

/**
 * The overview response as a lookup by workspace id. A workspace with no tasks
 * has no entry in the response, and none here.
 *
 * @param {{ workspaces?: Array<{ workspaceId: string, counts: object, tasks: object[] }> }} res
 * @returns {Map<string, { counts: object, tasks: object[] }>}
 */
export function indexOverview(res) {
  return new Map((res?.workspaces ?? []).map((w) => [w.workspaceId, { counts: w.counts ?? {}, tasks: w.tasks ?? [] }]));
}

// Each count's dot is the status dot of the tasks it counts, without the glow
// and the pulse: a row of them would be all motion.
const CHIPS = [
  { key: 'pending', label: 'Action required', short: 'Action', text: 'text-yellow-600 dark:text-yellow-400', dot: 'bg-yellow-400' },
  { key: 'ongoing', label: 'Ongoing', short: 'Ongoing', text: 'text-green-600 dark:text-green-500', dot: 'bg-green-500' },
  { key: 'notstarted', label: 'Not started', short: 'To do', text: 'text-gray-500 dark:text-zinc-400', dot: 'bg-gray-400 dark:bg-zinc-500' },
  { key: 'scheduled', label: 'Scheduled', short: 'Scheduled', text: 'text-cyan-600 dark:text-cyan-400', dot: 'bg-cyan-400' },
];

/**
 * A card's counts, the ones that are not zero, in the Active tab's order with
 * what needs the person first.
 */
export function countChips(counts = {}) {
  return CHIPS.filter((c) => counts[c.key] > 0).map((c) => ({ ...c, count: counts[c.key] }));
}

/**
 * How many of a workspace's active tasks the card leaves out. Finished tasks
 * are only shown to fill the space, so they never count as missing.
 */
export function moreTasks(counts = {}, tasks = []) {
  const active = (counts.ongoing ?? 0) + (counts.notstarted ?? 0) + (counts.scheduled ?? 0);
  const shown = tasks.filter((t) => !FINISHED.has(t.status)).length;
  return Math.max(0, active - shown);
}

const STATUS_LABELS = {
  ongoing: 'Ongoing',
  blocked: 'Blocked',
  notstarted: 'Not started',
  cron: 'Scheduled',
  completed: 'Completed',
  rejected: 'Rejected',
};

/** The word a preview row puts beside its time. */
export function previewStatusLabel(status) {
  return STATUS_LABELS[status] ?? status;
}

/** Whether a preview row is a finished task, shown greyed. */
export function isFinished(task) {
  return FINISHED.has(task.status);
}

/**
 * Runs `reload` once for any number of calls made while it is running, and
 * once more afterwards if any were: a burst of task events costs two fetches,
 * not one each, and the last one always sees the latest state.
 */
export function coalesce(reload) {
  let running = null;
  let again = false;
  return function run() {
    if (running) {
      again = true;
      return running;
    }
    running = (async () => {
      do {
        again = false;
        try {
          await reload();
        } catch (err) {
          console.error('Refreshing the workspaces overview failed:', err);
        }
      } while (again);
      running = null;
    })();
    return running;
  };
}
