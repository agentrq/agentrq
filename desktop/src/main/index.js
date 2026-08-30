import { app, BrowserWindow, net, protocol, shell } from 'electron'
import { readFile } from 'node:fs/promises'
import { access } from 'node:fs/promises'
import { constants } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

import { createAppProtocolHandler } from './protocol.js'
import { resolveServerUrl } from './server-config.js'

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

let serverUrl = resolveServerUrl({ env: process.env })

async function fileExists(pathname) {
  try {
    await access(join(RENDERER_ROOT, pathname), constants.R_OK)
    return true
  } catch {
    return false
  }
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

  // Anything that is not the app itself belongs in the user's real browser —
  // OAuth included, which phase 3 (0hua8EL8awL) replaces with a proper flow.
  win.webContents.setWindowOpenHandler(({ url }) => {
    shell.openExternal(url)
    return { action: 'deny' }
  })
  win.webContents.on('will-navigate', (event, url) => {
    if (!url.startsWith(APP_ORIGIN)) {
      event.preventDefault()
      shell.openExternal(url)
    }
  })

  win.loadURL(`${APP_ORIGIN}/`)
  return win
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

  app.whenReady().then(() => {
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

    createWindow()

    app.on('activate', () => {
      if (BrowserWindow.getAllWindows().length === 0) createWindow()
    })
  })

  app.on('window-all-closed', () => {
    if (process.platform !== 'darwin') app.quit()
  })
}
