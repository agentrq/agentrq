// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * End-to-end check of the side panel, in a real window.
 *
 * Unit tests cover each rule — what a guest is created with, where its links
 * go, how wide the panel may be — but not whether Electron honours them: that
 * `will-attach-webview` really overrides the partition the element asked for,
 * that a guest really has no bridge, that a `window.open` really stays in the
 * panel. That only exists inside a running Electron.
 *
 * A small local server stands in for AgentRQ (a signed-in user and empty
 * lists, which is enough to get past the login screen) and serves the page the
 * panel opens. The app is the real build, its preloads and the real wiring from
 * `side-panel/wire.js`.
 *
 *   npm run build && npx electron scripts/verify-side-panel.mjs
 *
 * `SHOTS=<dir>` also saves a screenshot, in the theme `THEME` names (light by
 * default). Headless, with no display: `OFFSCREEN=1` and Chromium's
 * `--ozone-platform=headless --ozone-override-screen-size=1600,1000` — without
 * the screen size the window is 1×1 and every width in the page is wrong.
 */
import { app, BrowserWindow, ipcMain, net, protocol, session } from 'electron'
import { access, mkdir, readFile, writeFile } from 'node:fs/promises'
import { constants } from 'node:fs'
import { createServer } from 'node:http'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

import { createAppProtocolHandler } from '../src/main/protocol.js'
import { panelPartitionFor } from '../src/main/side-panel/guest.js'
import { wirePanelPermissions, wireSidePanel } from '../src/main/side-panel/wire.js'
import { createPermissionStore } from '../src/main/side-panel/permission-store.js'
import { createPermissionBroker } from '../src/main/side-panel/permissions.js'
import { mkdtemp } from 'node:fs/promises'
import { tmpdir } from 'node:os'

const __dirname = dirname(fileURLToPath(import.meta.url))
const RENDERER_ROOT = join(__dirname, '../dist/renderer')
const PRELOAD = join(__dirname, '../dist/preload/index.cjs')
const PANEL_PRELOAD = join(__dirname, '../dist/preload/panel.cjs')
const APP_PARTITION = 'persist:verify-side-panel-app'
const PANEL_PARTITION = panelPartitionFor('verify-side-panel')

protocol.registerSchemesAsPrivileged([
  {
    scheme: 'app',
    privileges: { standard: true, secure: true, supportFetchAPI: true, stream: true, corsEnabled: true },
  },
])

const USER = { id: 'u1', name: 'Ada Lovelace', email: 'ada@example.com' }
const WORKSPACE = { id: 'w1', name: 'agentrq-code', agentConnected: true, agentSupportsStop: true }
/** Set once the server is listening, so a message can link to its pages. */
let ORIGIN = ''
const TASK = () => ({
  id: 't1',
  workspaceId: 'w1',
  title: 'Write the release notes for 0.9.8',
  status: 'ongoing',
  assignee: 'agent',
  body: 'Summarise the merged pull requests since 0.9.7.',
  createdAt: '2026-10-02T03:00:00Z',
  messages: [
    { id: 'm1', sender: 'human', text: 'Draft them, and keep the release page open beside this task.', createdAt: '2026-10-02T03:00:00Z' },
    { id: 'm2', sender: 'agent', text: `Drafted. Check them against the [release page](${ORIGIN}/second).`, createdAt: '2026-10-02T03:05:00Z' },
  ],
  toolCalls: [],
})

const PAGE = (title, body = '') => `<!doctype html><html><head><title>${title}</title>
<style>body{font-family:system-ui,sans-serif;margin:0;padding:24px;color:#18181b}
h1{font-size:20px;margin:0 0 8px} p{font-size:13px;color:#52525b;line-height:1.5} a{color:#2563eb}</style></head>
<body><h1>${title}</h1><p>A page served by the verification run's own server, shown in the side panel.</p>${body}</body></html>`

/** The stand-in for AgentRQ, and the website the panel visits. */
function startServer() {
  const server = createServer((req, res) => {
    const url = new URL(req.url, 'http://localhost')
    if (url.pathname.startsWith('/api/v1/auth/user')) return json(res, USER)
    if (url.pathname === '/api/v1/workspaces/w1/tasks/t1') return json(res, { task: TASK(), stateTransitions: [] })
    if (url.pathname === '/api/v1/workspaces/w1/tasks') return json(res, { tasks: [TASK()] })
    if (url.pathname === '/api/v1/workspaces/w1') return json(res, { workspace: WORKSPACE })
    if (url.pathname.startsWith('/api/v1/workspaces')) return json(res, { workspaces: [] })
    if (url.pathname.startsWith('/api/v1/tasks/counts')) return json(res, { counts: {} })
    if (url.pathname.startsWith('/api/v1/events/stream')) {
      res.writeHead(200, { 'content-type': 'text/event-stream' })
      return
    }
    if (url.pathname.startsWith('/api/')) return json(res, {})
    if (url.pathname === '/page') {
      res.writeHead(200, { 'content-type': 'text/html', 'set-cookie': 'site=panel; Path=/' })
      return res.end(PAGE('Release notes', '<p><a id="popup" href="/second" target="_blank">Open the next page</a></p>'))
    }
    if (url.pathname === '/ask') {
      res.writeHead(200, { 'content-type': 'text/html' })
      return res.end(PAGE('Asks for notifications'))
    }
    if (url.pathname === '/second') {
      res.writeHead(200, { 'content-type': 'text/html' })
      return res.end(PAGE('Second page'))
    }
    res.writeHead(404)
    res.end()
  })
  return new Promise((resolve) => server.listen(0, '::', () => resolve(server)))
}

function json(res, body) {
  res.writeHead(200, { 'content-type': 'application/json' })
  res.end(JSON.stringify(body))
}

async function fileExists(pathname) {
  try {
    await access(join(RENDERER_ROOT, pathname), constants.R_OK)
    return true
  } catch {
    return false
  }
}

const wait = (ms) => new Promise((r) => setTimeout(r, ms))

async function until(check, { timeout = 10000, every = 100 } = {}) {
  const end = Date.now() + timeout
  while (Date.now() < end) {
    const value = await check()
    if (value) return value
    await wait(every)
  }
  return null
}

setTimeout(() => {
  console.error('✗ verification timed out')
  app.exit(3)
}, 240000)

app.whenReady().then(async () => {
  const server = await startServer()
  const origin = `http://127.0.0.1:${server.address().port}`
  ORIGIN = origin

  ipcMain.handle('agentrq:connection:get', () => ({ configured: true, serverUrl: origin, locked: true }))
  ipcMain.handle('agentrq:update:get', () => ({ status: 'idle', detail: '', version: '', enabled: false }))
  ipcMain.handle('agentrq:theme:set', () => ({ source: 'system', color: '#fafafa' }))
  ipcMain.handle('agentrq:profiles:get', () => ({ activeProfileId: '', profiles: [] }))
  ipcMain.handle('agentrq:notifications:get', () => ({ supported: false, mutedWorkspaces: [] }))
  ipcMain.handle('agentrq:extensions:state', () => ({ ok: true, installations: [] }))
  const routed = []
  ipcMain.handle('agentrq:side-panel:open-external', (_e, url) => routed.push(url))

  const appSession = session.fromPartition(APP_PARTITION)
  await appSession.clearStorageData()
  await session.fromPartition(PANEL_PARTITION).clearStorageData()
  appSession.protocol.handle(
    'app',
    createAppProtocolHandler({
      serverUrl: () => origin,
      netFetch: (input, init) => appSession.fetch(input, init),
      fileExists,
      readFile: (pathname) => readFile(join(RENDERER_ROOT, pathname)),
    })
  )
  // The profile's credential. A guest must never be able to send it.
  await appSession.cookies.set({ url: origin, name: 'at', value: 'secret-session', path: '/' })

  const win = new BrowserWindow({
    width: 1400,
    height: 860,
    // An offscreen window has to be "shown" to be given its size; a hidden
    // one is 1×1, and every width in the page is measured against that.
    show: process.env.OFFSCREEN === '1',
    // Headless runs paint offscreen; a real window is otherwise identical.
    webPreferences: {
      offscreen: process.env.OFFSCREEN === '1',
      preload: PRELOAD,
      partition: APP_PARTITION,
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      webviewTag: true,
    },
  })
  // Mirrors index.js: a link the app does not keep goes to the shell.
  win.webContents.setWindowOpenHandler(({ url }) => {
    routed.push(url)
    return { action: 'deny' }
  })
  // Mirrors index.js: the panel session asks through the broker, the answer
  // is remembered in a file, and only the app window can give it.
  const userData = await mkdtemp(join(tmpdir(), 'agentrq-verify-permissions-'))
  const permissionStore = createPermissionStore({ dir: userData, profileId: 'verify' })
  const broker = createPermissionBroker({
    store: permissionStore,
    ask: (question) => win.webContents.send('agentrq:side-panel:permission-request', question),
    settled: (id) => win.webContents.send('agentrq:side-panel:permission-settled', id),
  })
  wirePanelPermissions(session.fromPartition(PANEL_PARTITION), broker)
  ipcMain.handle('agentrq:side-panel:permission-answer', (_e, id, decision) => broker.answer(Number(id), decision))
  ipcMain.handle('agentrq:side-panel:permissions', async () => { await permissionStore.load(); return permissionStore.list() })
  ipcMain.handle('agentrq:side-panel:permission-remove', async (_e, o, p) => { await permissionStore.remove(o, p); return permissionStore.list() })

  wireSidePanel(win, {
    partition: () => PANEL_PARTITION,
    preload: PANEL_PRELOAD,
    serverUrl: () => origin,
    routeLink: (url) => routed.push(url),
    permissions: broker,
  })

  // Offscreen windows paint into frames rather than onto a screen, and
  // `capturePage` answers them with a 1×1 image — so the last frame is kept.
  let frame = null
  if (process.env.OFFSCREEN === '1') {
    win.webContents.on('paint', (_e, _dirty, image) => { frame = image })
    win.webContents.setFrameRate(30)
  }

  let guest = null
  win.webContents.on('did-attach-webview', (_e, contents) => { guest = contents })
  let extraWindows = 0
  app.on('browser-window-created', () => { extraWindows += 1 })

  const theme = process.env.THEME === 'dark' ? 'dark' : 'light'
  await win.loadURL('app://agentrq/')
  await win.webContents.executeJavaScript(`localStorage.clear(); localStorage.setItem('theme', '${theme}'); true`)
  await win.loadURL('app://agentrq/')
  await until(() => win.webContents.executeJavaScript("!!document.querySelector('[data-side-panel-toggle]')"))

  const results = []
  const record = (name, pass, detail = '') => {
    results.push({ name, pass, detail })
    if (process.env.VERBOSE) console.log(`… ${pass ? 'ok' : 'FAIL'} ${name} ${detail}`)
  }
  const js = (code) => win.webContents.executeJavaScript(code)

  record('the panel starts closed', (await js("!document.querySelector('[data-side-panel]')")) === true)

  // The View menu's accelerator reaches the renderer this way.
  win.webContents.send('agentrq:side-panel:toggle')
  const opened = await until(() => js("!!document.querySelector('[data-side-panel]')"))
  record('the menu accelerator opens it', Boolean(opened))

  await js(`(() => {
    const input = document.querySelector('[data-side-panel-address]');
    input.value = '${origin}/page';
    input.dispatchEvent(new Event('input'));
    input.closest('form').requestSubmit();
  })()`)
  await until(() => guest && guest.getURL() === `${origin}/page` && !guest.isLoading(), { timeout: 15000 })
  record('typing an address loads it', guest?.getURL() === `${origin}/page`, guest?.getURL())

  record('the guest is on the panel partition, not the profile', guest?.session === session.fromPartition(PANEL_PARTITION))
  const prefs = guest?.getLastWebPreferences?.() ?? {}
  record('the guest is sandboxed with no Node', prefs.sandbox === true && !prefs.nodeIntegration && prefs.contextIsolation === true, JSON.stringify({ sandbox: prefs.sandbox, node: prefs.nodeIntegration, iso: prefs.contextIsolation }))

  const inside = await guest.executeJavaScript('({ bridge: typeof window.agentrq, require: typeof require, process: typeof process, cookie: document.cookie })')
  record('a web page in the panel has no bridge', inside.bridge === 'undefined', inside.bridge)
  record('and no Node', inside.require === 'undefined' && inside.process === 'undefined', `${inside.require}/${inside.process}`)
  record('and never sees the profile credential', !inside.cookie.includes('secret-session') && inside.cookie.includes('site=panel'), inside.cookie)
  const appCookies = await appSession.cookies.get({ name: 'site' })
  record('the site cookie stayed in the panel jar', appCookies.length === 0, `${appCookies.length} in the app jar`)

  await wait(300)
  const address = await js("document.querySelector('[data-side-panel-address]').value")
  record('the address field follows the page', address === `${origin}/page`, address)

  // A target=_blank link stays in the panel and opens no window.
  // As a click: Chromium blocks a popup that no user gesture asked for.
  await guest.executeJavaScript("document.getElementById('popup').click()", true)
  await until(() => guest.getURL() === `${origin}/second`)
  record('a new-window link opens in the panel itself', guest.getURL() === `${origin}/second`, guest.getURL())
  record('and no second window was created', extraWindows === 0, String(extraWindows))

  await until(() => js("!document.querySelector('[data-side-panel-back]').disabled"))
  record('back is enabled after navigating', (await js("!document.querySelector('[data-side-panel-back]').disabled")) === true)

  // A refused scheme from inside the page goes nowhere.
  await guest.executeJavaScript("location.href = 'file:///etc/passwd'")
  await wait(500)
  record('a page cannot navigate the panel to a file', guest.getURL() === `${origin}/second`, guest.getURL())

  // An unresized panel fills what the main column leaves it.
  const filled = await js(`(() => {
    const aside = document.querySelector('[data-side-panel]');
    return { panel: Math.round(aside.getBoundingClientRect().width), row: aside.parentElement.clientWidth,
             main: Math.round(document.querySelector('main').getBoundingClientRect().width) };
  })()`)
  record('an unresized panel leaves the main column a phone\'s width', filled.main >= 470 && filled.main <= 490, JSON.stringify(filled))

  // Dragging the left edge right, narrower.
  const before = await js("document.querySelector('[data-side-panel]').getBoundingClientRect().width")
  await js(`(() => {
    const handle = document.querySelector('[data-side-panel-handle]');
    const r = handle.getBoundingClientRect();
    const x = r.left + r.width / 2, y = r.top + 40;
    handle.dispatchEvent(new PointerEvent('pointerdown', { button: 0, clientX: x, clientY: y, bubbles: true, pointerId: 1 }));
    window.dispatchEvent(new PointerEvent('pointermove', { clientX: x + 120, clientY: y, pointerId: 1 }));
    window.dispatchEvent(new PointerEvent('pointerup', { clientX: x + 120, clientY: y, pointerId: 1 }));
  })()`)
  await wait(200)
  const after = await js("document.querySelector('[data-side-panel]').getBoundingClientRect().width")
  record('dragging the edge right narrows it', Math.round(before - after) === 120, `${before} → ${after}`)
  const saved = await js("JSON.parse(localStorage.getItem('agentrq:side-panel'))")
  record('and the width is remembered', saved?.width === Math.round(after), JSON.stringify(saved))

  await js("document.querySelector('[data-side-panel-external]').click()")
  await wait(200)
  record('Open in browser hands the page to the shell', routed.at(-1) === `${origin}/second`, routed.at(-1))

  // The task view's own button, beside a real task: there while the panel is
  // closed, gone while it is open.
  await js("history.pushState({}, '', '/workspaces/w1/tasks/t1'); dispatchEvent(new PopStateEvent('popstate'))")
  await until(() => js("!!document.querySelector('button[title=\"Go Back\"]')"))
  const taskButton = "document.querySelector('[data-side-panel-toggle].w-7')"
  record('the task view hides its panel button while the panel is open', (await js(`!${taskButton}`)) === true)
  await js("document.querySelector('[data-side-panel-close]').click()")
  const shown = await until(() => js(`!!${taskButton}`))
  record('and shows it once the panel is closed', Boolean(shown))
  await js(`${taskButton}.click()`)
  const reopened = await until(() => js("!!document.querySelector('[data-side-panel]')"))
  record('which opens it again', Boolean(reopened))

  // A link in a message: a plain click opens it in the panel, even closed.
  await js("document.querySelector('[data-side-panel-close]').click()")
  await until(() => js("!document.querySelector('[data-side-panel]')"))
  const messageLink = `document.querySelector('.md-body a[href="${origin}/second"]')`
  await until(() => js(`!!${messageLink}`))
  await js(`${messageLink}.click()`)
  const linked = await until(() => js("!!document.querySelector('[data-side-panel]')"))
  await until(() => guest && guest.getURL() === `${origin}/second`)
  record('a plain click on a link in a message opens it in the panel', Boolean(linked) && guest.getURL() === `${origin}/second`, guest?.getURL())

  // Cmd/Ctrl-click: the shell, which opens the system browser.
  const routedBefore = routed.length
  await js(`${messageLink}.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, button: 0, ctrlKey: true, metaKey: true }))`)
  await until(() => routed.length > routedBefore, { timeout: 3000 })
  record('a Cmd/Ctrl-click on it goes to the system browser instead', routed.at(-1) === `${origin}/second`, JSON.stringify(routed.slice(routedBefore)))
  await until(() => guest && !guest.isDestroyed() && guest.getURL().startsWith(origin))
  record('back on the page it was showing', guest?.getURL() === `${origin}/second`, guest?.getURL())

  // A page asking for a permission: the bar, the answer, and remembering it.
  const askPermission = () => guest.executeJavaScript('Notification.requestPermission().then((r) => { document.title = r; return r })', true)
  await guest.loadURL(`${origin}/ask`)
  await until(() => !guest.isLoading())
  const asking = askPermission()
  const bar = await until(() => js("document.querySelector('[data-side-panel-permission]')?.textContent?.trim()"))
  record('a page asking for notifications shows the question under the toolbar', Boolean(bar) && bar.includes(`127.0.0.1:${server.address().port} wants to show notifications`), bar)
  await js("document.querySelector('[data-side-panel-permission-allow]').click()")
  record('Allow gives the page its permission', (await asking) === 'granted')
  await wait(300)
  record('and the bar goes away', (await js("!document.querySelector('[data-side-panel-permission]')")) === true)

  await guest.reload()
  await until(() => !guest.isLoading())
  record('the next time, the page has it without asking', (await askPermission()) === 'granted' && (await js("!document.querySelector('[data-side-panel-permission]')")))
  const savedFile = JSON.parse(await readFile(join(userData, 'side-panel-permissions', 'verify.json'), 'utf8'))
  record('the answer is saved on this machine', savedFile.origins?.[origin]?.notifications === 'allow', JSON.stringify(savedFile))

  // The shield opens the list, and removing the decision means it asks again.
  await js("document.querySelector('[data-side-panel-permissions-toggle]').click()")
  const listed = await until(() => js("document.querySelector('[data-site-permissions] [data-site]')?.textContent"))
  record('the Site permissions list shows the site', Boolean(listed) && listed.includes('Notifications') && listed.includes('Allowed'), listed?.replace(/\s+/g, ' ').trim())
  if (process.env.SHOTS) {
    await wait(500)
    await writeFile(join(process.env.SHOTS, `side-panel-permissions-${theme}.png`), (frame ?? (await win.webContents.capturePage())).toPNG())
  }
  await js("document.querySelector('[data-site-remove]').click()")
  await until(() => js("!!document.querySelector('[data-site-permissions-empty]')"))
  await js("document.querySelector('[data-site-permissions-close]').click()")

  // Chromium answers a page from its own cache once a permission is granted, so
  // a fresh page on another origin shows the asking again from scratch.
  const other = origin.replace('127.0.0.1', 'localhost')
  await guest.loadURL(`${other}/ask`)
  await until(() => !guest.isLoading())
  const again = askPermission()
  await until(() => js("!!document.querySelector('[data-side-panel-permission]')"))
  if (process.env.SHOTS) {
    await wait(500)
    await writeFile(join(process.env.SHOTS, `side-panel-asking-${theme}.png`), (frame ?? (await win.webContents.capturePage())).toPNG())
  }
  await js("document.querySelector('[data-side-panel-permission-block]').click()")
  record('Block refuses it', (await again) === 'denied')

  // Navigating away while asked answers it, and takes the bar down.
  await permissionStore.remove(other)
  await guest.loadURL(`${origin.replace('127.0.0.1', '[::1]')}/ask`).catch(() => {})
  await until(() => !guest.isLoading())
  const abandoned = guest.executeJavaScript('Notification.requestPermission()', true)
  const barShown = await until(() => js("!!document.querySelector('[data-side-panel-permission]')"))
  await guest.loadURL(`${origin}/page`)
  await until(() => !guest.isLoading())
  const gone = await until(() => js("!document.querySelector('[data-side-panel-permission]')"), { timeout: 3000 })
  record('leaving the page answers what it asked and takes the bar down', Boolean(barShown) && Boolean(gone))
  abandoned.catch(() => {})

  // Beside the panel the task view lays itself out as it does on a phone.
  await js("document.querySelector('[data-side-panel-handle]').dispatchEvent(new MouseEvent('dblclick', { bubbles: true }))")
  await wait(300)
  const phoneLayout = await js("getComputedStyle(document.querySelector('button[title=\"Go Back\"]')).display !== 'none'")
  record('beside the panel the task view uses its phone layout', phoneLayout === true)

  if (process.env.SHOTS) {
    await guest.loadURL(`${origin}/page`)
    await until(() => !guest.isLoading())
    await wait(800)
    await mkdir(process.env.SHOTS, { recursive: true })
    const image = frame ?? (await win.webContents.capturePage())
    await writeFile(join(process.env.SHOTS, `side-panel-${theme}.png`), image.toPNG())
    console.log(`saved ${join(process.env.SHOTS, `side-panel-${theme}.png`)}`)
  }

  // Expanded: everything beside the sidebar, the page hidden but kept.
  await js("document.querySelector('[data-side-panel-full]').click()")
  await wait(300)
  const full = await js(`(() => {
    const aside = document.querySelector('[data-side-panel]');
    const nav = document.querySelector('nav');
    return { panel: Math.round(aside.getBoundingClientRect().width), row: aside.parentElement.clientWidth,
             sidebar: Math.round(nav.getBoundingClientRect().width), mainShown: getComputedStyle(document.querySelector('main')).display !== 'none',
             taskKept: !!document.querySelector('button[title="Go Back"]') };
  })()`)
  record('expanded, the panel takes everything beside the sidebar', full.panel === full.row - full.sidebar && !full.mainShown, JSON.stringify(full))
  record('and the task is kept, not unloaded', full.taskKept === true)
  if (process.env.SHOTS) {
    await wait(800)
    const image = frame ?? (await win.webContents.capturePage())
    await writeFile(join(process.env.SHOTS, `side-panel-full-${theme}.png`), image.toPNG())
  }
  await js("document.querySelector('[data-side-panel-full]').click()")
  await wait(300)
  record('collapsing brings the task back beside it', (await js("getComputedStyle(document.querySelector('main')).display !== 'none'")) === true)

  await js("document.querySelector('[data-side-panel-close]').click()")
  await wait(200)
  record('close closes it', (await js("!document.querySelector('[data-side-panel]')")) === true)
  const desktopLayout = await js("getComputedStyle(document.querySelector('button[title=\"Go Back\"]')).display === 'none'")
  record('and the task view has its full layout back', desktopLayout === true)

  if (process.env.SHOTS) {
    await wait(800)
    const image = frame ?? (await win.webContents.capturePage())
    await writeFile(join(process.env.SHOTS, `side-panel-closed-${theme}.png`), image.toPNG())
  }

  console.log('\n─── the side panel, in a real window ───')
  for (const r of results) console.log(`${r.pass ? '✓' : '✗'} ${r.name}${r.detail ? ` — ${r.detail}` : ''}`)
  const failed = results.filter((r) => !r.pass)
  console.log(failed.length === 0 ? '\n✓ all checks passed' : `\n✗ ${failed.length} check(s) failed`)
  server.close()
  app.exit(failed.length === 0 ? 0 : 1)
})
