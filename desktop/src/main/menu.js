/**
 * Application menu.
 *
 * Two kinds of entry live here: the standard platform roles a desktop app is
 * expected to have, and the AgentRQ actions the web app has no equivalent for —
 * creating a task from anywhere, switching server, signing out, and checking
 * for updates.
 *
 * The template is built as plain data so it can be asserted on without
 * launching Electron.
 */

/**
 * Global shortcut for quick task creation.
 *
 * Cmd/Ctrl+Shift+N: `Shift` keeps it clear of the browser-standard "new window"
 * on Cmd/Ctrl+N, and this is registered *globally*, so it must not collide with
 * a common system binding.
 */
export const QUICK_CREATE_ACCELERATOR = 'CommandOrControl+Shift+N'

/**
 * @param {object} options
 * @param {string} options.platform  `process.platform`
 * @param {string} options.appName
 * @param {object} options.actions   handlers, keyed by menu item id
 */
export function buildMenuTemplate({ platform, appName = 'AgentRQ', actions = {} }) {
  const isMac = platform === 'darwin'

  const accountItems = [
    { id: 'switch-server', label: 'Switch Server…', click: actions.switchServer },
    { id: 'log-out', label: 'Log Out', click: actions.logOut },
  ]

  const newTaskItem = {
    id: 'new-task',
    label: 'New Task',
    accelerator: QUICK_CREATE_ACCELERATOR,
    click: actions.newTask,
  }

  // Present from the start so the menu does not shift under users later, but
  // disabled until phase 6 (task 0huaAvwzIuH) supplies a handler — a menu item
  // that silently does nothing is worse than one that is visibly not ready.
  const updateItem = {
    id: 'check-for-updates',
    label: 'Check for Updates…',
    enabled: Boolean(actions.checkForUpdates),
    click: actions.checkForUpdates,
  }

  const template = []

  if (isMac) {
    // macOS convention: about and update checks at the top of the application
    // menu, account actions above Quit.
    template.push({
      label: appName,
      submenu: [
        { role: 'about' },
        { type: 'separator' },
        updateItem,
        { type: 'separator' },
        ...accountItems,
        { type: 'separator' },
        { role: 'services' },
        { type: 'separator' },
        { role: 'hide' },
        { role: 'hideOthers' },
        { role: 'unhide' },
        { type: 'separator' },
        { role: 'quit' },
      ],
    })
  }

  template.push({
    label: 'File',
    submenu: isMac
      ? [newTaskItem, { type: 'separator' }, { role: 'close' }]
      : // Elsewhere there is no application menu, so everything lives under
        // File alongside Quit.
        [
          newTaskItem,
          { type: 'separator' },
          ...accountItems,
          { type: 'separator' },
          updateItem,
          { type: 'separator' },
          { role: 'quit' },
        ],
  })

  template.push({ label: 'Edit', role: 'editMenu' })
  template.push({
    label: 'View',
    submenu: [
      { role: 'reload' },
      { role: 'forceReload' },
      { role: 'toggleDevTools' },
      { type: 'separator' },
      { role: 'resetZoom' },
      { role: 'zoomIn' },
      { role: 'zoomOut' },
      { type: 'separator' },
      { role: 'togglefullscreen' },
    ],
  })
  template.push({ label: 'Window', role: 'windowMenu' })

  if (!isMac) {
    template.push({ label: 'Help', submenu: [{ role: 'about' }] })
  }

  return template
}

/**
 * Every item carrying the given id, at any depth. Used by the tests, and by any
 * later code that needs to toggle an item's state.
 */
export function findMenuItems(template, id) {
  return template.flatMap((item) => [
    ...(item.id === id ? [item] : []),
    ...findMenuItems(item.submenu ?? [], id),
  ])
}
