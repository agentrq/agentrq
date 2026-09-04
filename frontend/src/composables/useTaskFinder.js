/**
 * Finding a task from the keyboard.
 *
 * Two different lookups sit behind one text box, because the backend offers no
 * single endpoint that answers "where is task X":
 *
 * 1. **Filtering what is already loaded.** The global task list is capped at 50
 *    rows by the API, so this is fast and covers the recent work — which is
 *    what almost every search is for. It matches titles as well as IDs, so the
 *    box is useful even when you cannot remember an ID.
 * 2. **Resolving an exact ID.** A task older than those 50 rows is invisible to
 *    the filter, and an ID pasted from a commit message or a chat is exactly the
 *    case where the task is old. So a query that looks like an ID and matches
 *    nothing is asked for by ID, against each of the user's workspaces.
 *
 * The second lookup is deliberately *not* run while typing: it is one request
 * per workspace, so it waits for the user to commit to the query.
 *
 * Each of those requests is `GET /workspaces/:id/tasks/:taskID`, which the
 * backend answers with `WHERE id = ? AND workspace_id = ? AND user_id = ?`. All
 * three columns are in the query, so a task is only ever returned to the user it
 * belongs to — there is no lookup by task ID alone anywhere in this path.
 */

/**
 * Task IDs are monoflake base62: **always exactly 11 characters**, because
 * `monoflake.ID.String()` zero-pads to a fixed width rather than trimming
 * leading zeros. The length is a property of the encoding, not of how large the
 * IDs happen to have grown, so it does not drift.
 */
const TASK_ID = /^[0-9A-Za-z]{11}$/;

/**
 * Whether `query` could be a task ID.
 *
 * This is the gate on every backend lookup: a query that cannot be an ID never
 * becomes a request. Without it, typing an ordinary word would fan a round trip
 * out to every workspace for something that could not possibly be found.
 *
 * A shape test is all it can be — an eleven-letter word ("performance") still
 * passes — but that only costs one lookup that comes back empty, and it is
 * checked *after* the filter has already failed to match anything.
 *
 * @param {string} query
 */
export function looksLikeTaskId(query) {
  return TASK_ID.test((query ?? '').trim());
}

/**
 * Tasks matching `query`, best match first.
 *
 * The ordering is what makes the box feel right: an exact ID is what you get
 * when you paste one, an ID prefix comes next, and title matches fill the rest.
 * Within a group the incoming order is kept, which is the API's — newest first.
 *
 * An empty query is not "match everything filtered": it is the resting state of
 * the box, and showing the most recent tasks there is more useful than a blank
 * panel.
 *
 * @param {Array<{ id: string, title?: string }>} tasks
 * @param {string} query
 * @param {number} [limit]
 */
export function matchTasks(tasks, query, limit = 8) {
  const list = Array.isArray(tasks) ? tasks : [];
  const q = (query ?? '').trim().toLowerCase();
  if (!q) return list.slice(0, limit);

  const rank = (task) => {
    const id = String(task?.id ?? '').toLowerCase();
    if (id === q) return 0;
    if (id.startsWith(q)) return 1;
    if (String(task?.title ?? '').toLowerCase().includes(q)) return 2;
    return -1;
  };

  return list
    .map((task, order) => ({ task, order, rank: rank(task) }))
    .filter((row) => row.rank !== -1)
    .sort((a, b) => a.rank - b.rank || a.order - b.order)
    .slice(0, limit)
    .map((row) => row.task);
}

/**
 * Look an ID up in every workspace and return the first one that has it.
 *
 * The requests go out together rather than in sequence: a user with a dozen
 * workspaces should not wait a dozen round trips, and all but one of them are
 * expected to 404. A rejection is therefore an answer, not a failure — only the
 * case where *every* workspace says no is reported, as null.
 *
 * @param {string} taskId
 * @param {string[]} workspaceIds
 * @param {(workspaceId: string, taskId: string) => Promise<any>} getTask
 * @returns {Promise<{ workspaceId: string, task: any } | null>}
 */
export async function resolveTaskById(taskId, workspaceIds, getTask) {
  const ids = Array.isArray(workspaceIds) ? workspaceIds : [];
  if (!taskId || ids.length === 0) return null;

  const settled = await Promise.allSettled(ids.map((workspaceId) => getTask(workspaceId, taskId)));

  for (let i = 0; i < settled.length; i += 1) {
    const outcome = settled[i];
    if (outcome.status !== 'fulfilled') continue;
    const task = outcome.value?.task ?? outcome.value;
    if (task?.id) return { workspaceId: ids[i], task };
  }
  return null;
}

/**
 * Where a task lives.
 *
 * The task list route and the workspace route both render the task detail, but
 * only the workspace one can be built from a task alone — the other needs a
 * filter the finder has no opinion about.
 *
 * @param {{ id: string, workspaceId?: string }} task
 * @param {string} [workspaceId] when the caller knows it and the task does not
 */
export function taskRoute(task, workspaceId) {
  const ws = task?.workspaceId ?? workspaceId;
  return `/workspaces/${ws}/tasks/${task.id}`;
}
