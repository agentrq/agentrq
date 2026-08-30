/**
 * Tray / menu-bar item.
 *
 * The point of a tray item is to answer "is anything waiting for me?" without
 * switching to the app, and to get somewhere in one click when the answer is
 * yes. So it carries the unread count and the workspaces the user has heard
 * from most recently.
 *
 * Built as plain data, so the contents are testable without a running Electron.
 */

/** Most recent workspaces to offer. Enough to be useful, short enough to scan. */
export const MAX_RECENT_WORKSPACES = 5

/**
 * Track which workspaces have been active most recently.
 *
 * Ordered by last activity rather than alphabetically: the tray is a shortcut
 * to what is happening now, and a fixed list would make it a worse version of
 * the sidebar.
 */
export function createRecentWorkspaces({ limit = MAX_RECENT_WORKSPACES } = {}) {
  let recent = []

  return {
    /** Record activity, moving the workspace to the front. */
    touch(id, name) {
      if (!id) return recent
      recent = [{ id, name: name ?? '' }, ...recent.filter((entry) => entry.id !== id)].slice(0, limit)
      return recent
    },

    /** Fill in names that were unknown when the activity was recorded. */
    rename(id, name) {
      recent = recent.map((entry) => (entry.id === id ? { ...entry, name } : entry))
      return recent
    },

    get list() {
      return recent
    },
  }
}

/**
 * Tray menu contents.
 *
 * @param {object} options
 * @param {Array<{id: string, name: string}>} [options.workspaces]
 * @param {number} [options.unreadCount]
 * @param {object} [options.actions]
 */
export function buildTrayMenuTemplate({ workspaces = [], unreadCount = 0, actions = {} }) {
  const template = [
    {
      id: 'status',
      // The count is the whole reason to glance at the tray, so it leads.
      label: unreadCount > 0 ? `${unreadCount} unread` : 'No unread notifications',
      enabled: false,
    },
    { type: 'separator' },
    { id: 'open', label: 'Open AgentRQ', click: actions.open },
    { id: 'new-task', label: 'New Task…', click: actions.newTask },
  ]

  if (workspaces.length > 0) {
    template.push({ type: 'separator' })
    template.push({ id: 'recent-heading', label: 'Recent Workspaces', enabled: false })
    for (const workspace of workspaces) {
      template.push({
        id: `workspace-${workspace.id}`,
        // A workspace whose name has not been resolved yet still deserves a
        // usable entry rather than a blank line.
        label: workspace.name || 'Untitled workspace',
        click: () => actions.openWorkspace?.(workspace.id),
      })
    }
  }

  template.push({ type: 'separator' })
  template.push({ id: 'quit', label: 'Quit AgentRQ', click: actions.quit })

  return template
}

/** Tooltip shown on hover. */
export function trayTooltip(unreadCount) {
  if (unreadCount <= 0) return 'AgentRQ'
  return `AgentRQ — ${unreadCount} unread notification${unreadCount === 1 ? '' : 's'}`
}
