/**
 * End-to-end check that an extension's contribution actually reaches the screen.
 *
 * This exists because the feature shipped without it. Every piece had unit
 * tests, an integration test drove the host and the composables together, and
 * the menu item still never appeared — because `menuItemsFor(task)` was called
 * with one argument in the two views that show tasks, and nothing in the
 * renderer asked the main process what extensions had registered. Both halves
 * were correct and the path between them did not exist.
 *
 * So the path is what this runs: a real Electron window, the real preload, the
 * real `app://` protocol, the real host loading the real `task-stats` example
 * off disk, and the real IPC handlers. The only stubs are the connection screens
 * that would otherwise take the window, and there is no backend.
 *
 *   npm run build && npx electron scripts/verify-extensions.mjs
 */
import { app, BrowserWindow, ipcMain, net, protocol } from 'electron'
import { readFile, access } from 'node:fs/promises'
import { constants } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

import { createAppProtocolHandler } from '../src/main/protocol.js'
import { createHost } from '../src/main/extensions/host.js'
import { entriesFor, invokeEntry } from '../src/main/extensions/surfaces.js'
// The renderer's own modules, imported here rather than from inside the page: a
// production bundle exposes no source paths, and these are plain functions with
// no Vue in them. What the page is for is the bridge hop, which is the part no
// unit test can reach.
import { menuItemsFor, parseSelection } from '../../frontend/src/composables/useTaskContextMenu.js'
import { normaliseView } from '../../frontend/src/composables/useExtensionView.js'

const __dirname = dirname(fileURLToPath(import.meta.url))
const RENDERER_ROOT = join(__dirname, '../dist/renderer')
const PRELOAD = join(__dirname, '../dist/preload/index.cjs')
const EXAMPLE = join(__dirname, '../../examples/extensions/task-stats')

protocol.registerSchemesAsPrivileged([
  {
    scheme: 'app',
    privileges: { standard: true, secure: true, supportFetchAPI: true, stream: true, corsEnabled: true },
  },
])

async function fileExists(pathname) {
  try {
    await access(join(RENDERER_ROOT, pathname), constants.R_OK)
    return true
  } catch {
    return false
  }
}

/**
 * The extension IPC, mirroring src/main/index.js.
 *
 * Repeated rather than imported because index.js takes over the Electron app
 * lifecycle at import time. What it leans on — the host, and surfaces.js — is
 * the real thing, and so are the channel names the preload reaches them on.
 */
function registerExtensionHandlers(host) {
  ipcMain.handle('agentrq:extensions:entries', (_event, { surface, context } = {}) =>
    entriesFor(host.resolve('ui', context ?? {}), surface ?? '', context ?? {}),
  )
  ipcMain.handle('agentrq:extensions:invoke', async (_event, { target, context } = {}) =>
    invokeEntry(host.resolve('ui', context ?? {}), target ?? {}, context ?? {}),
  )
  ipcMain.handle('agentrq:extensions:state', async () => ({ index: { entries: [] }, installed: [] }))
}

/**
 * Drives the composables the two task views use, in the window.
 *
 * Deliberately not a click on a rendered board: reaching one needs a backend, a
 * session and a workspace. What is being checked is the seam that was missing —
 * the renderer asking the bridge, and what comes back becoming menu rows and a
 * validated view — and these are the exact functions both views call.
 */
const script = `(async () => {
  const bridge = window.agentrq?.extensions;
  if (!bridge) return { error: 'no extensions bridge on window.agentrq' };
  if (typeof bridge.entries !== 'function') return { error: 'the bridge has no entries()' };

  const task = {
    title: 'Ship it',
    body: 'three words here',
    status: 'ongoing',
    createdAt: new Date(Date.now() - 2 * 3600 * 1000).toISOString(),
    messages: [{ text: 'one two' }],
  };

  const rows = await bridge.entries('task-menu', task);
  return { task, rows };
})()`

/** The second half of the round trip, once the page has told us what to run. */
const invokeScript = (target, task) => `window.agentrq.extensions.invoke(${JSON.stringify(target)}, ${JSON.stringify(task)})`

setTimeout(() => {
  console.error('✗ verification timed out')
  app.exit(3)
}, 60000)

app.whenReady().then(async () => {
  // The real host, loading the real example off disk the way an install does.
  const host = createHost({
    load: () => import(pathToFileURL(join(EXAMPLE, 'index.js')).href),
    readConfig: async () => ({}),
  })
  const started = await host.start({ name: 'task-stats', version: '1.0.0', enabled: true })

  registerExtensionHandlers(host)
  ipcMain.handle('agentrq:connection:get', () => ({ configured: true, serverUrl: 'http://localhost:3999', locked: true }))
  ipcMain.handle('agentrq:update:get', () => ({ status: 'idle', detail: '', version: '', enabled: false }))
  ipcMain.handle('agentrq:theme:set', () => ({ source: 'system', color: '#fafafa' }))
  ipcMain.handle('agentrq:profiles:get', () => ({ activeProfileId: '', profiles: [] }))
  ipcMain.handle('agentrq:notifications:get', () => ({ supported: false, mutedWorkspaces: [] }))

  protocol.handle(
    'app',
    createAppProtocolHandler({
      serverUrl: () => 'http://localhost:3999',
      netFetch: net.fetch,
      fileExists,
      readFile: (pathname) => readFile(join(RENDERER_ROOT, pathname)),
    }),
  )

  // The webPreferences the real window uses, preload included — otherwise this
  // would be checking a window the app never creates.
  const win = new BrowserWindow({
    show: false,
    webPreferences: { preload: PRELOAD, contextIsolation: true, nodeIntegration: false, sandbox: true },
  })
  await win.loadURL('app://agentrq/')
  await new Promise((r) => setTimeout(r, 1500))

  const results = []
  const record = (name, pass, detail) => results.push({ name, pass, detail })

  record('the example loads', started.ok === true, JSON.stringify(started))

  const bridge = await win.webContents.executeJavaScript(
    "typeof window.agentrq?.extensions?.entries === 'function' && typeof window.agentrq?.extensions?.invoke === 'function'",
  )
  record('the bridge exposes entries and invoke', bridge === true, String(bridge))

  // The hop nothing else can check: the page asks, the main process answers.
  const { task, rows, error } = await win.webContents.executeJavaScript(script)
  record('the page can ask what extensions offer', !error, String(error ?? ''))
  record('entries cross the bridge', Array.isArray(rows) && rows.length === 1, JSON.stringify(rows))
  record(
    'nothing callable crosses with them',
    Array.isArray(rows) && rows.every((row) => Object.values(row).every((v) => typeof v !== 'function')),
    JSON.stringify(rows),
  )

  // What both task views do with that answer.
  const menu = menuItemsFor(task, rows ?? [])
  record(
    'the menu reads Move Task, a divider, then Task Stats',
    JSON.stringify(menu.map((i) => i.label ?? '(divider)')) === JSON.stringify(['Move Task', '(divider)', 'Task Stats']),
    JSON.stringify(menu.map((i) => i.label ?? '(divider)')),
  )

  const chosen = menu.find((item) => item.label === 'Task Stats')
  const parsed = chosen ? parseSelection(chosen.key) : {}
  record('the selection parses back to the extension', parsed.owner === 'task-stats', JSON.stringify(parsed))

  // Back over the bridge, from the page, exactly as a click does.
  const invoked = chosen
    ? await win.webContents.executeJavaScript(invokeScript({ ...parsed, surface: 'task-menu' }, task))
    : { ok: false, reason: 'nothing to click' }
  record('invoking it from the page answers with a view', invoked.ok === true, String(invoked.reason ?? ''))

  const drawn = normaliseView(invoked.view ?? null)
  record('the renderer will draw it', drawn.ok === true, String(drawn.reason ?? ''))
  const values = (drawn.view?.nodes?.[0]?.items ?? []).map((r) => `${r.label}=${r.value}`)
  record(
    'and it says what it worked out',
    JSON.stringify(values) === JSON.stringify(['Age=2 hours', 'Messages=1', 'Words=5', 'Status=ongoing']),
    JSON.stringify(values),
  )

  for (const { name, pass, detail } of results) {
    console.log(`${pass ? '✓' : '✗'} ${name}${pass ? '' : ` — ${detail}`}`)
  }

  const failed = results.filter((r) => !r.pass).length
  console.log(failed === 0 ? '\nAll checks passed.' : `\n${failed} check(s) failed.`)
  app.exit(failed === 0 ? 0 : 1)
})
