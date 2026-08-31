/**
 * Which sections a task list shows for a given filter, and what goes in each.
 *
 * The distinction that matters here is **primary** versus **supplementary**
 * sections, because it decides what an empty one does:
 *
 * - A *primary* section is the reason you opened the page. It is always
 *   returned, empty or not, so the list can say "No … tasks found" rather than
 *   rendering a blank column. Ongoing, Not Started, Action Required, Completed
 *   and — on its own page — Scheduled.
 * - A *supplementary* section only appears alongside others on the combined
 *   Active view. Blocked, Rejected, and Scheduled when seen from Active. An
 *   empty one is omitted, because five "nothing here" cards stacked up is
 *   noise, not information.
 *
 * Scheduled is the section that is primary on one page and supplementary on
 * another, and treating it as supplementary everywhere is what left the
 * Scheduled page rendering an entirely empty column.
 *
 * Extracted from TaskListView so this can be tested: the view is a large
 * component wired to a router, a store and network calls, and the project has
 * no component-test harness.
 */

/**
 * Tasks awaiting something from the person, rather than from the agent.
 *
 * @param {Array<object>} tasks
 */
export function pendingOnHuman(tasks) {
  return tasks.filter(
    (t) =>
      t.status !== 'completed' &&
      t.status !== 'rejected' &&
      ((t.status === 'notstarted' && t.assignee === 'human') ||
        (t.messages &&
          t.messages.some(
            (m) => m.metadata?.type === 'permission_request' && m.metadata?.status === 'pending'
          )))
  );
}

/**
 * @param {Array<object>} tasks
 * @param {string} filter  'active' | 'ongoing' | 'notstarted' | 'pending' | 'completed' | 'scheduled'
 * @param {{ nextRunAt?: (cronSchedule: string) => Date }} [options]
 *        nextRunAt orders the scheduled section by when each task next fires.
 *        Injected rather than imported so this stays free of the cron composable.
 * @returns {Array<{ title: string, tasks: Array<object> }>}
 */
export function buildTaskGroups(tasks, filter, { nextRunAt } = {}) {
  const all = Array.isArray(tasks) ? tasks : [];
  const f = filter || 'active';
  const withStatus = (status) => all.filter((t) => t.status === status);

  const blocked = withStatus('blocked');
  const rejected = withStatus('rejected');
  const groups = [];

  if (f === 'active' || f === 'ongoing') {
    groups.push({ title: 'Ongoing', tasks: withStatus('ongoing') });
    if (blocked.length > 0) groups.push({ title: 'Blocked', tasks: blocked });
  }

  if (f === 'active' || f === 'notstarted') {
    groups.push({ title: 'Not Started', tasks: withStatus('notstarted') });
  }

  if (f === 'pending') {
    groups.push({ title: 'Action Required', tasks: pendingOnHuman(all) });
  }

  if (f === 'completed') {
    groups.push({ title: 'Completed', tasks: withStatus('completed') });
    if (rejected.length > 0) groups.push({ title: 'Rejected', tasks: rejected });
  }

  if (f === 'active' || f === 'scheduled') {
    const scheduled = sortByNextRun(withStatus('cron'), nextRunAt);
    // Primary on its own page, supplementary on Active. Omitting it when empty
    // on the Scheduled page is what left that page with nothing in the column
    // at all — no heading, no count, no empty state.
    if (f === 'scheduled' || scheduled.length > 0) {
      groups.push({ title: 'Scheduled', tasks: scheduled });
    }
  }

  return groups;
}

/**
 * Soonest first. Without a way to read the next run time the original order is
 * kept, which is better than throwing inside a computed property.
 */
function sortByNextRun(tasks, nextRunAt) {
  if (typeof nextRunAt !== 'function') return [...tasks];
  return [...tasks].sort(
    (a, b) => nextRunAt(a.cronSchedule).getTime() - nextRunAt(b.cronSchedule).getTime()
  );
}
