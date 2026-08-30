import { app, BrowserWindow, Menu, ipcMain, net, protocol, session, shell } from 'electron'
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
import { buildMenuTemplate } from './menu.js'

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
  if (result.ok) reloadToRoot(win)
}

function createWindow() {
  const win = new BrowserWindow({
    width: 1280,
    height: 860,
    minWidth: 940,
    minHeight: 600,
    show: false,
    backgroundColor: '#fafafa',
    titleBarStyle: process.platform === 'darwin' ? 'hiddenInset' : 'default',
    webPreferences: {
      preload: PRELOAD,
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
    },
  })

  win.once('ready-to-show', () => win.show())

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
      switchServer: async () => {
        if (isConnectionLocked) return
        await configStore.clear()
        serverUrl = ''
        reloadToRoot(getWindow())
      },
      logOut: async () => {
        await session.defaultSession.clearStorageData({ storages: ['cookies'] })
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

  ipcMain.handle('agentrq:connection:save', async (_event, url) => {
    if (isConnectionLocked) {
      return { ok: false, reason: 'Server is pinned by AGENTRQ_SERVER_URL' }
    }

    const validated = await validateServerUrl(url, net.fetch)
    if (!validated.ok) return validated

    const saved = await configStore.save(validated.url)
    if (!saved.ok) return saved

    serverUrl = saved.url
    reloadToRoot(getWindow())
    return { ok: true, url: saved.url }
  })
}

// A second launch should surface the running app, not start a rival instance
// with its own cookie jar.
if (!app.requestSingleInstanceLock()) {
  app.quit()
} else {
  app.on('second-instance', () => {
    const [win] = BrowserWindow.getAllWindows()
    if (win) {
      if (win.isMinimized()) win.restore()
      win.focus()
    }
  })

  app.whenReady().then(async () => {
    const configPath = join(app.getPath('userData'), CONFIG_FILENAME)
    configStore = createServerConfigStore({
      readFile: () => readFile(configPath, 'utf-8'),
      writeFile: (contents) => writeFile(configPath, contents, 'utf-8'),
    })

    if (!isConnectionLocked) {
      serverUrl = (await configStore.load()).serverUrl
    }

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

    app.on('activate', () => {
      if (BrowserWindow.getAllWindows().length === 0) createWindow()
    })
  })

  app.on('window-all-closed', () => {
    if (process.platform !== 'darwin') app.quit()
  })
}
