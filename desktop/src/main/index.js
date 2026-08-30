import {
  app,
  BrowserWindow,
  Menu,
  Notification,
  Tray,
  globalShortcut,
  ipcMain,
  nativeImage,
  nativeTheme,
  net,
  protocol,
  screen,
  session,
  shell,
} from 'electron'
import { readFile, writeFile, access } from 'node:fs/promises'
import { constants } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

import { createAppProtocolHandler } from './protocol.js'
import {
  CONFIG_FILENAME,
  createServerConfigStore,
  normalizeServerUrl,
  validateServerUrl,
} from './server-config.js'
import { AUTH_COOKIE, matchOAuthLogin, oauthStartUrl, runOAuthFlow } from './auth.js'
import { QUICK_CREATE_ACCELERATOR, buildMenuTemplate } from './menu.js'
import { badgeFor, createNotificationGate, createUnreadCounter, mapEventToNotification } from './notifications.js'
import { createEventStreamClient } from './sse.js'
import { UpdateStatus, createUpdater } from './updater.js'
// Externalised by the build, so this resolves from node_modules at runtime.
// Importing it is inert; the dev guard is about never *using* it against a
// development checkout.
import electronUpdater from 'electron-updater'
import { DEEP_LINK_SCHEME, deepLinkFromArgv, parseDeepLink } from './deep-link.js'
import { buildTrayMenuTemplate, createRecentWorkspaces, trayTooltip } from './tray.js'
import { applyTheme, backgroundColorFor } from './theme.js'
import {
  WINDOW_STATE_FILENAME,
  captureWindowState,
  clampToDisplays,
  createWindowStateStore,
  debounce,
} from './window-state.js'

const __dirname = dirname(fileURLToPath(import.meta.url))

/** Host portion of the app:// origin. Arbitrary, but it must stay stable. */
const APP_HOST = 'agentrq'
const APP_ORIGIN = `app://${APP_HOST}`

const RENDERER_ROOT = join(__dirname, '../renderer')
const PRELOAD = join(__dirname, '../preload/index.cjs')

/**
 * Must run before app.whenReady(). `standard` is what gives app:// a real
 * origin (so cookies, storage and vue-router's history mode behave); `stream`
 * and `supportFetchAPI` are what let the SSE event stream flow through the
 * protocol handler unbuffered.
 */
protocol.registerSchemesAsPrivileged([
  {
    scheme: 'app',
    privileges: { standard: true, secure: true, supportFetchAPI: true, stream: true, corsEnabled: true },
  },
])

/**
 * The environment override exists so a developer can point at a scratch backend
 * without disturbing stored settings. When it is set the connection is locked:
 * the connection screen is skipped and "Switch Server" is a no-op, because the
 * stored value would not be what the app is using anyway.
 */
const envServerUrl = normalizeServerUrl(process.env.AGENTRQ_SERVER_URL ?? '')
const isConnectionLocked = envServerUrl.ok

let serverUrl = isConnectionLocked ? envServerUrl.url : ''
let configStore

async function fileExists(pathname) {
  try {
    await access(join(RENDERER_ROOT, pathname), constants.R_OK)
    return true
  } catch {
    return false
  }
}

/** Workspace id -> display name, for notification bodies. */
const workspaceNames = new Map()
let mutedWorkspaces = []
let eventStream = null
let unread = null

// One task creation is published twice by the backend — once by the REST
// handler and once by the CRUD-event consumer — so identical notifications
// have to be collapsed before they reach the user.
const notificationGate = createNotificationGate()

const recentWorkspaces = createRecentWorkspaces()
let windowStateStore
let restoredWindowState = null
let tray = null
let currentTheme = 'system'
/** Set when a deep link arrives before the window is ready to receive it. */
let pendingRoute = null
let updater = null
/** Last update state, so a renderer that loads later is not left in the dark. */
let updateState = { status: UpdateStatus.Idle, detail: '', version: '', enabled: false }

async function refreshWorkspaceNames() {
  if (!serverUrl) return
  try {
    const res = await net.fetch(`${serverUrl}/api/v1/workspaces`)
    if (!res.ok) return
    const body = await res.json()
    for (const ws of Array.isArray(body) ? body : (body?.workspaces ?? [])) {
      if (ws?.id) workspaceNames.set(ws.id, ws.name ?? '')
    }
  } catch {
    // A name is a nicety; a notification without one is still useful.
  }
}

function applyBadge(count) {
  refreshTray()
  const { badge, overlay } = badgeFor(count, process.platform)

  if (app.dock) app.dock.setBadge(badge)
  else if (typeof app.setBadgeCount === 'function') app.setBadgeCount(count)

  const win = BrowserWindow.getAllWindows().find((w) => !w.isDestroyed())
  if (win && process.platform === 'win32') {
    win.setOverlayIcon(overlay ? nativeImage.createEmpty() : null, overlay?.description ?? '')
  }
}

function connectionState() {
  return { configured: Boolean(serverUrl), serverUrl, locked: isConnectionLocked }
}

/** Send the window back to the app root, picking up whatever the state now is. */
function reloadToRoot(win) {
  if (win && !win.isDestroyed()) win.loadURL(`${APP_ORIGIN}/`)
}

async function startOAuth(win, pathname, search) {
  const result = await runOAuthFlow({
    serverUrl,
    startUrl: oauthStartUrl(serverUrl, pathname, search),
    createWindow: () =>
      new BrowserWindow({
        parent: win,
        width: 520,
        height: 720,
        title: 'Sign in to AgentRQ',
        autoHideMenuBar: true,
        webPreferences: {
          // No preload and no partition: this window is showing a third-party
          // sign-in page, so it gets no bridge, and it must stay on the default
          // session so the cookie it earns is the one net.fetch will send.
          contextIsolation: true,
          nodeIntegration: false,
          sandbox: true,
        },
      }),
    hasAuthCookie: async () => {
      const cookies = await session.defaultSession.cookies.get({ name: AUTH_COOKIE })
      return cookies.length > 0
    },
  })

  // On success the cookie is already in the jar, so simply reloading lands the
  // user inside the app. On failure the login view is still there to try again.
  if (result.ok) {
    startEventStream()
    reloadToRoot(win)
  }
}

/**
 * Ask the renderer to navigate. The renderer owns the router the shared factory
 * built, so routing is its job; the shell only ever names a destination.
 *
 * A route arriving before the window exists is held rather than dropped — a
 * deep link is often what *launched* the app.
 */
function navigate(route) {
  if (!route) return
  const win = focusMainWindow()
  if (!win || win.webContents.isLoading()) {
    pendingRoute = route
    return
  }
  win.webContents.send('agentrq:navigate', route)
}

function openDeepLink(url) {
  const route = parseDeepLink(url)
  // An unrecognised link still opens the app: the user asked to see AgentRQ,
  // and the default view is a better answer than nothing happening.
  focusMainWindow()
  if (route) navigate(route)
}

/** Where "New Task" goes: the most recent workspace, else the workspace list. */
function newTaskRoute() {
  const [recent] = recentWorkspaces.list
  return recent ? `/workspaces/${recent.id}/tasks/new` : '/'
}

function refreshTray() {
  if (!tray) return
  const count = unread?.value ?? 0
  tray.setToolTip(trayTooltip(count))
  tray.setContextMenu(
    Menu.buildFromTemplate(
      buildTrayMenuTemplate({
        workspaces: recentWorkspaces.list,
        unreadCount: count,
        actions: {
          open: () => focusMainWindow(),
          newTask: () => navigate(newTaskRoute()),
      checkForUpdates: () => updater?.checkNow(),
          openWorkspace: (id) => navigate(`/workspaces/${id}`),
          quit: () => app.quit(),
        },
      })
    )
  )
}

function focusMainWindow() {
  const win = BrowserWindow.getAllWindows().find((w) => !w.isDestroyed())
  if (!win) return null
  if (win.isMinimized()) win.restore()
  win.show()
  win.focus()
  return win
}

function handleStreamEvent(event) {
  const notification = mapEventToNotification(event, {
    mutedWorkspaces,
    workspaceName: (id) => workspaceNames.get(id) ?? '',
  })
  if (!notification) return

  // An unknown workspace means the list is stale — refresh for next time
  // rather than blocking this notification on a round trip.
  if (!workspaceNames.has(event.payload.workspaceId)) refreshWorkspaceNames()

  if (!notificationGate.allow(notification.tag)) return
  if (!Notification.isSupported()) return

  const native = new Notification({ title: notification.title, body: notification.body })
  native.on('click', () => {
    unread?.clear()
    navigate(notification.route)
  })
  native.show()
  unread?.increment()
  // The tray is the at-a-glance version of the same news.
  recentWorkspaces.touch(event.payload.workspaceId, workspaceNames.get(event.payload.workspaceId))
  refreshTray()
}

function startEventStream() {
  eventStream?.stop()
  if (!serverUrl) return

  eventStream = createEventStreamClient({
    streamUrl: () => `${serverUrl}/api/v1/events/stream`,
    netFetch: net.fetch,
    onEvent: handleStreamEvent,
    delay: (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
    onUnauthorized: () => {
      // Signed out. The renderer's own stream sends the user to the login
      // screen; this one waits to be restarted after a successful sign-in.
    },
  })
  eventStream.start()
  refreshWorkspaceNames()
}

function createWindow() {
  // Saved coordinates are only safe if the display they referred to still
  // exists — clamping is what stops a window reopening off-screen after a
  // monitor is unplugged.
  const bounds = clampToDisplays(
    restoredWindowState,
    screen.getAllDisplays().map((display) => display.workArea)
  )

  const win = new BrowserWindow({
    ...bounds,
    minWidth: 940,
    minHeight: 600,
    show: false,
    backgroundColor: backgroundColorFor(currentTheme, nativeTheme.shouldUseDarkColors),
    titleBarStyle: process.platform === 'darwin' ? 'hiddenInset' : 'default',
    webPreferences: {
      preload: PRELOAD,
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
    },
  })

  win.once('ready-to-show', () => {
    if (restoredWindowState?.maximized) win.maximize()
    win.show()
  })

  // A badge answers "is there anything new"; looking at the app answers it.
  win.on('focus', () => unread?.clear())

  // Dragging a window emits these continuously, so the write is debounced —
  // otherwise one gesture would be hundreds of writes.
  const persist = debounce(() => {
    restoredWindowState = captureWindowState(win)
    windowStateStore?.save(restoredWindowState)
  }, 500)
  for (const event of ['resize', 'move', 'maximize', 'unmaximize']) {
    win.on(event, persist)
  }
  win.on('close', () => {
    restoredWindowState = captureWindowState(win)
    windowStateStore?.save(restoredWindowState)
  })

  // A deep link often *is* what launched the app, so a route that arrived
  // before the renderer existed is delivered once it is ready.
  win.webContents.on('did-finish-load', () => {
    if (!pendingRoute) return
    const route = pendingRoute
    pendingRoute = null
    win.webContents.send('agentrq:navigate', route)
  })

  // Anything that is not the app itself belongs in the user's real browser.
  win.webContents.setWindowOpenHandler(({ url }) => {
    shell.openExternal(url)
    return { action: 'deny' }
  })

  win.webContents.on('will-navigate', (event, url) => {
    if (!url.startsWith(APP_ORIGIN)) {
      event.preventDefault()
      shell.openExternal(url)
      return
    }

    // LoginView renders its Google and GitHub buttons as plain links. Following
    // one here would run the provider's redirect chain against the app:// origin
    // and lose the callback, so the shell takes the flow over instead — leaving
    // the login view itself untouched.
    const { pathname, search } = new URL(url)
    if (matchOAuthLogin(pathname)) {
      event.preventDefault()
      startOAuth(win, pathname, search)
    }
  })

  win.loadURL(`${APP_ORIGIN}/`)
  return win
}

function installMenu(getWindow) {
  const template = buildMenuTemplate({
    platform: process.platform,
    appName: app.getName(),
    actions: {
      newTask: () => navigate(newTaskRoute()),
      checkForUpdates: () => updater?.checkNow(),
      switchServer: async () => {
        if (isConnectionLocked) return
        await configStore.clear()
        serverUrl = ''
        eventStream?.stop()
        reloadToRoot(getWindow())
      },
      logOut: async () => {
        await session.defaultSession.clearStorageData({ storages: ['cookies'] })
        eventStream?.stop()
        unread?.clear()
        reloadToRoot(getWindow())
      },
    },
  })

  Menu.setApplicationMenu(Menu.buildFromTemplate(template))
}

function registerIpc(getWindow) {
  ipcMain.handle('agentrq:connection:get', () => connectionState())

  ipcMain.handle('agentrq:connection:validate', async (_event, url) => {
    const result = await validateServerUrl(url, net.fetch)
    // The probe response carries auth flags that are of no use to the
    // connection screen and would be a needless leak across the bridge.
    return result.ok ? { ok: true, url: result.url } : result
  })

  // The renderer is the source of truth for appearance — the theme store the
  // web app already has — so the shell follows it rather than reading the OS.
  ipcMain.handle('agentrq:update:get', () => updateState)
  ipcMain.handle('agentrq:update:check', () => updater?.checkNow() ?? { ok: false, reason: 'Updater unavailable' })
  ipcMain.handle('agentrq:update:install', () => updater?.installNow() ?? false)

  ipcMain.handle('agentrq:theme:set', (_event, theme) => {
    currentTheme = theme
    return applyTheme(theme, {
      nativeTheme,
      windows: () => BrowserWindow.getAllWindows().filter((w) => !w.isDestroyed()),
    })
  })

  ipcMain.handle('agentrq:notifications:get', async () => ({
    supported: Notification.isSupported(),
    mutedWorkspaces,
  }))

  ipcMain.handle('agentrq:notifications:setMuted', async (_event, workspaceId, muted) => {
    mutedWorkspaces = await configStore.setWorkspaceMuted(workspaceId, muted)
    return { mutedWorkspaces }
  })

  ipcMain.handle('agentrq:connection:save', async (_event, url) => {
    if (isConnectionLocked) {
      return { ok: false, reason: 'Server is pinned by AGENTRQ_SERVER_URL' }
    }

    const validated = await validateServerUrl(url, net.fetch)
    if (!validated.ok) return validated

    const saved = await configStore.save(validated.url)
    if (!saved.ok) return saved

    serverUrl = saved.url
    startEventStream()
    reloadToRoot(getWindow())
    return { ok: true, url: saved.url }
  })
}

function installUpdater() {
  updater = createUpdater({
    autoUpdater: electronUpdater.autoUpdater,
    // The real guard: unpackaged means no release to compare against, and no
    // business replacing a development checkout with a downloaded build.
    isPackaged: app.isPackaged,
    onStatus: (state) => {
      updateState = { ...state, enabled: updater?.state.enabled ?? false }
      const win = BrowserWindow.getAllWindows().find((w) => !w.isDestroyed())
      win?.webContents.send('agentrq:update:status', updateState)
    },
  })
  updater.start()
}

function installTray() {
  // An empty image is a deliberate placeholder: a tray icon is an art asset,
  // and shipping a wrong-looking one is worse than the platform's default. The
  // menu, count and tooltip — the parts that carry information — are real.
  tray = new Tray(nativeImage.createEmpty())
  refreshTray()
  tray.on('click', () => focusMainWindow())
}

function installGlobalShortcut() {
  // Registration fails when another application already owns the combination.
  // That is the user's environment, not an error worth interrupting them over;
  // the same action stays available from the menu and the tray.
  globalShortcut.register(QUICK_CREATE_ACCELERATOR, () => navigate(newTaskRoute()))
}

// A second launch should surface the running app, not start a rival instance
// with its own cookie jar.
if (!app.requestSingleInstanceLock()) {
  app.quit()
} else {
  // Windows and Linux deliver a deep link as an argument to a second launch,
  // which the single-instance lock forwards here rather than opening a rival
  // window.
  app.on('second-instance', (_event, argv) => {
    const url = deepLinkFromArgv(argv)
    if (url) openDeepLink(url)
    else focusMainWindow()
  })

  // macOS delivers it as an event instead, and can do so before the app is
  // ready — hence the pending-route handling in navigate().
  app.on('open-url', (event, url) => {
    event.preventDefault()
    openDeepLink(url)
  })

  // Claim the scheme so the OS routes agentrq:// links here. In development the
  // executable is Electron itself, which needs the path spelled out.
  if (process.defaultApp && process.argv.length >= 2) {
    app.setAsDefaultProtocolClient(DEEP_LINK_SCHEME, process.execPath, [process.argv[1]])
  } else {
    app.setAsDefaultProtocolClient(DEEP_LINK_SCHEME)
  }

  app.whenReady().then(async () => {
    const configPath = join(app.getPath('userData'), CONFIG_FILENAME)
    configStore = createServerConfigStore({
      readFile: () => readFile(configPath, 'utf-8'),
      writeFile: (contents) => writeFile(configPath, contents, 'utf-8'),
    })

    const windowStatePath = join(app.getPath('userData'), WINDOW_STATE_FILENAME)
    windowStateStore = createWindowStateStore({
      readFile: () => readFile(windowStatePath, 'utf-8'),
      writeFile: (contents) => writeFile(windowStatePath, contents, 'utf-8'),
    })
    restoredWindowState = await windowStateStore.load()

    const stored = await configStore.load()
    mutedWorkspaces = stored.mutedWorkspaces
    if (!isConnectionLocked) {
      serverUrl = stored.serverUrl
    }

    unread = createUnreadCounter({ setBadge: applyBadge })

    protocol.handle(
      'app',
      createAppProtocolHandler({
        serverUrl: () => serverUrl,
        netFetch: net.fetch,
        fileExists,
        readFile: (pathname) => readFile(join(RENDERER_ROOT, pathname)),
        devServerUrl: process.env.AGENTRQ_RENDERER_DEV_URL ?? '',
      })
    )

    // The main window is looked up lazily: on macOS it can be closed and
    // recreated while the app keeps running, so a captured reference goes stale.
    const getWindow = () => BrowserWindow.getAllWindows().find((w) => !w.isDestroyed())

    registerIpc(getWindow)
    installMenu(getWindow)
    createWindow()
    startEventStream()
    installTray()
    installGlobalShortcut()
    installUpdater()

    const launchLink = deepLinkFromArgv(process.argv)
    if (launchLink) openDeepLink(launchLink)

    app.on('activate', () => {
      if (BrowserWindow.getAllWindows().length === 0) createWindow()
    })
  })

  app.on('window-all-closed', () => {
    if (process.platform !== 'darwin') app.quit()
  })

  app.on('before-quit', () => {
    eventStream?.stop()
    updater?.stop()
    globalShortcut.unregisterAll()
    // A quit that does not close the window first — Cmd+Q, or the tray's Quit —
    // would otherwise lose whatever the debounced save had not yet written.
    const win = BrowserWindow.getAllWindows().find((w) => !w.isDestroyed())
    if (win) windowStateStore?.save(captureWindowState(win))
  })
}
