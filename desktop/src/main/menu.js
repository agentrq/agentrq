/**
 * Application menu.
 *
 * Only the entries phase 3 needs live here — switching server and signing out,
 * both of which are shell concerns the web app has no equivalent for. The full
 * menu, tray and shortcuts arrive with phase 5 (task 0huaA6sKHoX).
 *
 * The template is built as plain data so it can be asserted on without
 * launching Electron.
 */

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

  const template = []

  if (isMac) {
    // macOS convention puts account-level actions in the application menu,
    // above Quit.
    template.push({
      label: appName,
      submenu: [
        { role: 'about' },
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
      ? [{ role: 'close' }]
      : // Elsewhere there is no application menu, so the same actions live
        // under File alongside Quit.
        [...accountItems, { type: 'separator' }, { role: 'quit' }],
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
