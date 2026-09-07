/**
 * Switching workspaces from the keyboard.
 *
 * Unlike the task finder, this needs no backend at all: the workspace list is
 * already in the store, because the shell's sidebar and every agent-connection
 * indicator read from it. So the switcher is pure filtering over data the app
 * has in hand, and works offline for the same reason the sidebar does.
 *
 * The matching lives here rather than in the component so it can be tested as
 * a function — the same split `useTaskFinder` uses for the Cmd+K palette.
 */

/**
 * Workspace IDs are monoflake base62: exactly 11 characters, zero-padded to a
 * fixed width by `monoflake.ID.String()`, so the length is a property of the
 * encoding rather than of how many workspaces exist.
 *
 * Matching on ID matters less here than in the task finder — nobody remembers a
 * workspace by ID — but an ID pasted from a URL or an MCP config should still
 * land, and it costs one comparison.
 */
const ID_LENGTH = 11;

/**
 * Workspaces matching `query`, best match first.
 *
 * The ranking is what makes typing two letters land on the right workspace:
 *
 * 1. the name exactly, so a workspace whose name is a prefix of another's
 *    ("blog" alongside "blog-drafts") is still reachable by typing it in full;
 * 2. a name prefix, which is what a few typed letters usually means;
 * 3. a name substring, for the middle of a hyphenated name — `api` should find
 *    `payments-api`, which is most of how these get named;
 * 4. the ID, for a link or a config pasted in.
 *
 * **Archived workspaces are ranked after active ones of the same quality, not
 * excluded.** The sidebar lists them, so hiding them here would make the
 * switcher disagree with the navigation beside it; but they are not what
 * someone switching workspaces to get work done is reaching for, so on an
 * empty query — where every row ties — they sink to the bottom. An exact name
 * match still wins outright, archived or not: if you typed the whole name, you
 * meant that one.
 *
 * Ties keep the incoming order, which is the store's: name-sorted.
 *
 * @param {Array<{ id: string, name?: string, archivedAt?: string | null }>} workspaces
 * @param {string} query
 * @param {number} [limit]
 */
export function matchWorkspaces(workspaces, query, limit = 8) {
  const list = Array.isArray(workspaces) ? workspaces : [];
  const q = (query ?? '').trim().toLowerCase();

  const rank = (ws) => {
    const name = String(ws?.name ?? '').toLowerCase();
    if (!q) return 0;
    if (name === q) return 0;
    if (name.startsWith(q)) return 1;
    if (name.includes(q)) return 2;

    const id = String(ws?.id ?? '').toLowerCase();
    // A partial ID is not worth matching: it is never typed from memory, so a
    // prefix would only ever be a paste that got truncated.
    if (q.length === ID_LENGTH && id === q) return 3;
    return -1;
  };

  return list
    .map((ws, order) => ({ ws, order, rank: rank(ws), archived: ws?.archivedAt ? 1 : 0 }))
    .filter((row) => row.rank !== -1)
    .sort((a, b) => a.rank - b.rank || a.archived - b.archived || a.order - b.order)
    .slice(0, limit)
    .map((row) => row.ws);
}

/**
 * A workspace's URL — the one place in the app that builds one.
 *
 * With no section it is the workspace's own page, which is what its sidebar
 * entry and its card on the overview both open: switching workspaces should
 * land where clicking the workspace lands, not on some switcher-only
 * destination.
 *
 * `section` reaches a tab within it (`'analytics'`, `'board'`, `'settings'`) and
 * is what lets the account dashboard's per-workspace rows link straight to that
 * workspace's own analytics. It lives here rather than being interpolated at
 * each call site so there is one answer to "what is a workspace's URL" —
 * these paths are also declared in the router's single route table, and a
 * second hand-built copy is how the two drift.
 *
 * @param {{ id?: string } | string | null | undefined} workspace
 * @param {string} [section] a tab under the workspace, e.g. 'analytics'
 * @returns {string} a route, or the overview when there is nothing to open
 */
export function workspaceRoute(workspace, section = '') {
  const id = typeof workspace === 'string' ? workspace : workspace?.id;
  if (!id) return '/';
  return section ? `/workspaces/${id}/${section}` : `/workspaces/${id}`;
}

/**
 * Whether `workspace` is the one already on screen.
 *
 * Compared as strings because the two sides arrive differently: a route
 * parameter is always a string, while the store's copy is whatever the backend
 * serialised. The switcher marks this row rather than hiding it — a switcher
 * that silently omits where you are makes you wonder whether it is listing
 * everything.
 *
 * @param {{ id?: string } | null | undefined} workspace
 * @param {string | number | null | undefined} currentId
 */
export function isCurrentWorkspace(workspace, currentId) {
  if (workspace?.id == null || currentId == null || currentId === '') return false;
  return String(workspace.id) === String(currentId);
}
