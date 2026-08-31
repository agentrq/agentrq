import {
  app,
  BrowserWindow,
  Menu,
  Notification,
  Tray,
  dialog,
  globalShortcut,
  ipcMain,
  nativeImage,
  nativeTheme,
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
import { activeProfile, partitionFor } from './profiles.js'
import { fetchProfileIdentity } from './identity.js'
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
import { LinkTarget, classifyLink, linkWindowBounds } from './links.js'
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
/**
 * The signed-in profile in use, and the Electron session that carries its
 * cookies. Everything that talks to the server goes through this session rather
 * than the default one — that is what makes two profiles two accounts.
 */
let profileState = null
let profileSession = null

/** Sessions that already have the app:// handler; registering twice throws. */
const handledSessions = new WeakSet()

/**
 * The session for a partition, with the app:// protocol handler attached.
 *
 * The handler has to be registered per session, not once globally: a partition
 * gets its own protocol registry, so a window opened on one without this would
 * fail to load the app at all.
 */
function sessionFor(partition) {
  const ses = session.fromPartition(partition)
  if (!handledSessions.has(ses)) {
    ses.protocol.handle(
      'app',
      createAppProtocolHandler({
        serverUrl: () => serverUrl,
        // The profile's own session, so the `at` cookie sent upstream is that
        // profile's. Using net.fetch here would send the default session's.
        netFetch: (input, init) => ses.fetch(input, init),
        fileExists,
        readFile: (pathname) => readFile(join(RENDERER_ROOT, pathname)),
        devServerUrl: process.env.AGENTRQ_RENDERER_DEV_URL ?? '',
      })
    )
    handledSessions.add(ses)
  }
  return ses
}

/**
 * The partition a window should be created on.
 *
 * Null-safe on purpose: every current caller runs after the profiles are
 * loaded, but a window created before that would otherwise throw here rather
 * than fall back to something sane.
 */
function currentPartition() {
  return profileState ? activeProfile(profileState).partition : partitionFor('default')
}

/** Fetch through the active profile's session, so its cookies go with it. */
const profileFetch = (input, init) => (profileSession ?? session.defaultSession).fetch(input, init)

/** Last update state, so a renderer that loads later is not left in the dark. */
let updateState = { status: UpdateStatus.Idle, detail: '', remedy: '', version: '', enabled: false }

async function refreshWorkspaceNames() {
  if (!serverUrl) return
  try {
    const res = await profileFetch(`${serverUrl}/api/v1/workspaces`)
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

/**
 * Show a link in a window belonging to the app.
 *
 * Hardened exactly like the OAuth window below, and for the same reason: this
 * is rendering a page the app does not control, so it gets no preload, no node
 * and a sandbox. It also gets its own session partition — the default session
 * is where the `at` cookie lives, and an arbitrary website has no business
 * sharing that jar.
 *
 * No `parent`, deliberately: a child window is pinned above its parent, which
 * is the wrong behaviour for something the user may want to read beside the
 * app rather than on top of it.
 */
function openLinkWindow(url, parentWin) {
  const alive = parentWin && !parentWin.isDestroyed()
  const { width, height } = linkWindowBounds(alive ? parentWin.getBounds() : null)

  const child = new BrowserWindow({
    width,
    height,
    autoHideMenuBar: true,
    backgroundColor: backgroundColorFor(currentTheme),
    webPreferences: {
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      partition: 'persist:agentrq-links',
    },
  })

  // A page opened this way must not be able to spawn further app windows. Its
  // own popups go to the real browser, where an unfamiliar site belongs.
  child.webContents.setWindowOpenHandler(({ url: next }) => {
    const target = classifyLink(next, { appOrigin: APP_ORIGIN })
    if (target === LinkTarget.Window || target === LinkTarget.System) shell.openExternal(next)
    return { action: 'deny' }
  })

  child.loadURL(url)
  return child
}

/**
 * Send a link where it belongs. Blocked targets fall through to nothing on
 * purpose: a javascript: or file: URL in a message body is not a link the user
 * meant to follow, and reporting it would only teach them to click through.
 */
function routeLink(url, parentWin) {
  switch (classifyLink(url, { appOrigin: APP_ORIGIN })) {
    case LinkTarget.Window:
      openLinkWindow(url, parentWin)
      break
    case LinkTarget.System:
      shell.openExternal(url)
      break
    default:
      break
  }
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
          // No preload: this window shows a third-party sign-in page, so it
          // gets no bridge. It runs on the *active profile's* partition, which
          // is the whole mechanism — the `at` cookie the callback sets lands in
          // that profile's jar, so it signs in that profile and no other.
          partition: currentPartition(),
          contextIsolation: true,
          nodeIntegration: false,
          sandbox: true,
        },
      }),
    hasAuthCookie: async () => {
      const cookies = await profileSession.cookies.get({ name: AUTH_COOKIE })
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
    netFetch: profileFetch,
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
      // Fixed when the window is created and not changeable afterwards, which
      // is why switching profiles recreates the window rather than reloading.
      partition: currentPartition(),
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

  // A link out of the app opens in a window of the app's own, which the user
  // closes to get straight back to what they were doing. Handing these to the
  // system browser instead put the docs, the terms and any URL on a message
  // behind a context switch with nothing to come back to.
  win.webContents.setWindowOpenHandler(({ url }) => {
    routeLink(url, win)
    return { action: 'deny' }
  })

  win.webContents.on('will-navigate', (event, url) => {
    if (!url.startsWith(APP_ORIGIN)) {
      event.preventDefault()
      routeLink(url, win)
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
        await profileSession.clearStorageData({ storages: ['cookies'] })
        eventStream?.stop()
        unread?.clear()
        reloadToRoot(getWindow())
      },
    },
  })

  Menu.setApplicationMenu(Menu.buildFromTemplate(template))
}

/**
 * Who each profile is signed in as, kept between menu openings.
 *
 * Cached because the answer changes rarely — signing in or out — while the
 * menu can be opened repeatedly, and each lookup is a request to a possibly
 * distant server.
 */
const profileIdentities = new Map()

/**
 * Ask every profile who it is signed in as, each through its own session.
 *
 * In parallel, and every lookup already swallows its own failures, so one
 * unreachable server delays the others by nothing and fails nothing.
 */
async function refreshProfileIdentities() {
  if (!profileState) return
  await Promise.all(
    profileState.profiles.map(async (profile) => {
      const ses = session.fromPartition(profile.partition)
      const identity = await fetchProfileIdentity({
        fetchImpl: (input, init) => ses.fetch(input, init),
        serverUrl: profile.serverUrl,
        timeout: (ms) => AbortSignal.timeout(ms),
      })
      profileIdentities.set(profile.id, identity)
    })
  )
}

/** What the renderer needs to draw the switcher: names, not sessions. */
function profilesPayload() {
  const state = profileState ?? { profiles: [], activeProfileId: '' }
  return {
    activeProfileId: state.activeProfileId,
    profiles: state.profiles.map(({ id, label, serverUrl: url }) => ({
      id,
      label,
      serverUrl: url,
      active: id === state.activeProfileId,
      // null when that profile is signed out or its server cannot answer.
      identity: profileIdentities.get(id) ?? null,
    })),
  }
}

/**
 * Switch to another profile.
 *
 * A window's partition is fixed when it is created, so this replaces the
 * window rather than reloading it — reloading would keep the old session and
 * quietly show the previous account's data under the new profile's name.
 *
 * The new window is created *before* the old one is destroyed: on Windows and
 * Linux, closing the last window quits the app, and briefly having none would
 * do exactly that mid-switch.
 */
async function switchProfileToActive() {
  const profile = activeProfile(profileState)

  eventStream?.stop()
  eventStream = null
  unread?.clear()
  workspaceNames.clear()

  serverUrl = isConnectionLocked ? envServerUrl.url : profile.serverUrl
  mutedWorkspaces = profile.mutedWorkspaces
  profileSession = sessionFor(profile.partition)

  const previous = BrowserWindow.getAllWindows().find((w) => !w.isDestroyed())
  createWindow()
  previous?.destroy()

  startEventStream()
  installMenu(() => BrowserWindow.getAllWindows().find((w) => !w.isDestroyed()))
  return profilesPayload()
}

async function switchProfile(id) {
  if (!profileState || id === profileState.activeProfileId) return profilesPayload()
  if (!profileState.profiles.some((p) => p.id === id)) return profilesPayload()

  profileState = await configStore.activateProfile(id)
  return switchProfileToActive()
}

function registerIpc(getWindow) {
  ipcMain.handle('agentrq:connection:get', () => connectionState())

  ipcMain.handle('agentrq:connection:validate', async (_event, url) => {
    const result = await validateServerUrl(url, profileFetch)
    // The probe response carries auth flags that are of no use to the
    // connection screen and would be a needless leak across the bridge.
    return result.ok ? { ok: true, url: result.url } : result
  })

  // The renderer is the source of truth for appearance — the theme store the
  // web app already has — so the shell follows it rather than reading the OS.
  // Pick a working directory with the platform's own folder chooser, because
  // typing an absolute path from memory is exactly the sort of thing people get
  // subtly wrong. Only the chosen path crosses the bridge; the renderer never
  // gets a handle to the dialog or the window it is attached to.
  //
  // Returns '' when the dialog is dismissed, so the caller has one thing to
  // check rather than distinguishing cancellation from failure.
  ipcMain.handle('agentrq:dialog:chooseDirectory', async (event, currentPath) => {
    const parent = BrowserWindow.fromWebContents(event.sender)
    const options = {
      title: 'Choose a working directory',
      properties: ['openDirectory', 'createDirectory'],
    }
    // Open where they already pointed it, so changing a path is not a fresh
    // hunt through the filesystem every time.
    if (typeof currentPath === 'string' && currentPath.startsWith('/')) {
      options.defaultPath = currentPath
    }

    const result = parent
      ? await dialog.showOpenDialog(parent, options)
      : await dialog.showOpenDialog(options)

    if (result.canceled) return ''
    return result.filePaths?.[0] ?? ''
  })

  // Profiles. Only names and servers cross the bridge — never a session, a
  // partition or a cookie.
  ipcMain.handle('agentrq:profiles:get', async () => {
    await refreshProfileIdentities()
    return profilesPayload()
  })
  ipcMain.handle('agentrq:profiles:switch', (_event, id) => switchProfile(id))
  ipcMain.handle('agentrq:profiles:add', async (_event, label) => {
    profileState = await configStore.addProfile(label)
    // addProfile makes the new one active, so this is a switch into a session
    // that has never signed in: the window lands on the connection screen.
    return switchProfileToActive()
  })
  ipcMain.handle('agentrq:profiles:rename', async (_event, id, label) => {
    profileState = await configStore.renameProfile(id, label)
    installMenu(getWindow)
    return profilesPayload()
  })
  ipcMain.handle('agentrq:profiles:remove', async (_event, id) => {
    const wasActive = id === profileState?.activeProfileId
    profileState = await configStore.removeProfile(id)
    // Its cookies would otherwise outlive it on disk, still signed in.
    await session.fromPartition(partitionFor(id)).clearStorageData()
    profileIdentities.delete(id)
    return wasActive ? switchProfileToActive() : profilesPayload()
  })

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

    const validated = await validateServerUrl(url, profileFetch)
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

    profileState = await configStore.load()
    const profile = activeProfile(profileState)
    mutedWorkspaces = profile.mutedWorkspaces
    if (!isConnectionLocked) {
      serverUrl = profile.serverUrl
    }
    // Attaches the app:// handler to this profile's session before any window
    // is created on it.
    profileSession = sessionFor(profile.partition)

    unread = createUnreadCounter({ setBadge: applyBadge })

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
