/**
 * End-to-end check that the WebMCP catalogue reaches a real browser.
 *
 * The unit tests prove the catalogue is right and that registration is driven
 * correctly. What they cannot prove is the part that only exists in a browser:
 * that the app finds `document.modelContext` at all, registers against it once
 * the user is signed in, that a tool invoked through that object really calls
 * the interface's own API, and that signing out withdraws the tools rather than
 * leaving an agent holding the last person's session.
 *
 * No WebMCP-capable browser is required: this installs a stub `modelContext`
 * before the app boots, which is exactly what the page sees from a real one.
 * The agent side of the protocol is the browser's job, not AgentRQ's — what is
 * under test here is what AgentRQ hands it.
 *
 *   npm run build && npx electron scripts/verify-webmcp.mjs
 */
import { app, BrowserWindow, ipcMain, net, protocol } from 'electron'
import { readFile, access } from 'node:fs/promises'
import { constants } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

import { createAppProtocolHandler } from '../src/main/protocol.js'

const __dirname = dirname(fileURLToPath(import.meta.url))
const RENDERER_ROOT = join(__dirname, '../dist/renderer')
const PRELOAD = join(__dirname, '../dist/preload/index.cjs')

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
 * A stand-in for the API the app is pointed at.
 *
 * Every request the page makes is answered here, so a tool invocation can be
 * observed as the HTTP call it becomes — which is the claim worth checking:
 * that a WebMCP tool drives the same interface a click does.
 */
const requests = []
function apiResponse(url, method) {
  const { pathname, search } = new URL(url)
  requests.push({ method, pathname, search })

  if (pathname.endsWith('/auth/user')) return { id: 'u1', name: 'QA', email: 'qa@example.com' }
  if (pathname.endsWith('/workspaces')) return [{ id: 'ws1', name: 'Ops', description: 'Run things' }]
  if (pathname.endsWith('/tasks')) return { id: 't9', title: 'From an agent' }
  if (pathname.endsWith('/auth/logout')) return {}
  return []
}

/** The stub the page will find, mirroring what a WebMCP browser exposes. */
const STUB = `
  (() => {
    // Idempotent on purpose: 'did-start-loading' fires more than once per
    // document, and reinstalling would hand the page a fresh empty registry
    // while the app held tools registered against the old one.
    if (document.modelContext) return;
    const tools = new Map();
    Object.defineProperty(document, 'modelContext', {
      configurable: true,
      value: {
        registerTool(tool, options = {}) {
          if (tools.has(tool.name)) return Promise.reject(new Error('InvalidStateError'));
          tools.set(tool.name, tool);
          options.signal?.addEventListener('abort', () => tools.delete(tool.name));
          return Promise.resolve();
        },
        getTools: () => Promise.resolve([...tools.values()]),
      },
    });
    window.__stubTools = tools;
  })();
`

setTimeout(() => {
  console.error('✗ verification timed out')
  app.exit(3)
}, 60000)

app.whenReady().then(async () => {
  ipcMain.handle('agentrq:connection:get', () => ({ configured: true, serverUrl: 'http://stub', locked: true }))
  ipcMain.handle('agentrq:update:get', () => ({ status: 'idle', detail: '', version: '', enabled: false }))
  ipcMain.handle('agentrq:theme:set', () => ({ source: 'system', color: '#fafafa' }))
  ipcMain.handle('agentrq:profiles:get', () => ({ activeProfileId: '', profiles: [] }))
  ipcMain.handle('agentrq:notifications:get', () => ({ supported: false, mutedWorkspaces: [] }))

  const staticHandler = createAppProtocolHandler({
    serverUrl: () => 'http://stub',
    netFetch: net.fetch,
    fileExists,
    readFile: (pathname) => readFile(join(RENDERER_ROOT, pathname)),
  })

  // The API is answered here rather than proxied, so this run needs no backend
  // and every call a tool makes is observable.
  protocol.handle('app', (request) => {
    const { pathname } = new URL(request.url)
    if (pathname.startsWith('/api/')) {
      return new Response(JSON.stringify(apiResponse(request.url, request.method)), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      })
    }
    return staticHandler(request)
  })

  const win = new BrowserWindow({
    show: false,
    webPreferences: { preload: PRELOAD, contextIsolation: true, nodeIntegration: false, sandbox: true },
  })

  // Before any page script runs: the app looks for the API while it boots.
  win.webContents.on('did-start-loading', () => {
    win.webContents.executeJavaScript(STUB).catch(() => {})
  })

  await win.loadURL('app://agentrq/')
  await new Promise((r) => setTimeout(r, 2500))

  const results = []
  const record = (name, pass, detail) => results.push({ name, pass, detail })

  const registered = await win.webContents.executeJavaScript(
    '(async () => [...(await document.modelContext.getTools())].map((t) => t.name))()'
  )
  record('the app registers its catalogue with the browser', registered.length > 40, registered.length + ' tools')
  record(
    'including the tools only a page can offer',
    registered.includes('getCurrentPage') && registered.includes('navigate'),
    'getCurrentPage/navigate present: ' + registered.includes('getCurrentPage')
  )
  record(
    'and the ones that mirror the interface',
    ['listWorkspaces', 'createTask', 'replyToTask', 'updateTaskStatus'].every((n) => registered.includes(n)),
    registered.slice(0, 4).join(', ') + ', …'
  )

  // A tool, invoked the way an agent would: through the object the browser owns.
  const before = requests.length
  const created = await win.webContents.executeJavaScript(`
    (async () => {
      const tool = window.__stubTools.get('createTask');
      return tool.execute({ workspaceId: 'ws1', title: 'From an agent', body: 'Body' }, { signal: undefined });
    })()
  `)
  const call = requests.slice(before).find((r) => r.method === 'POST')
  record(
    'invoking a tool drives the interface\'s own API',
    call?.pathname === '/api/v1/workspaces/ws1/tasks',
    call ? call.method + ' ' + call.pathname : 'no request was made'
  )
  record('and the tool returns what the API said', created?.id === 't9', JSON.stringify(created))

  const page = await win.webContents.executeJavaScript(
    "window.__stubTools.get('getCurrentPage').execute({}, {})"
  )
  record('getCurrentPage answers with the live route', typeof page?.path === 'string', JSON.stringify(page))

  await win.webContents.executeJavaScript(
    "window.__stubTools.get('navigate').execute({ path: '/events' }, {})"
  )
  await new Promise((r) => setTimeout(r, 400))
  const movedTo = await win.webContents.executeJavaScript('location.pathname')
  record('the navigate tool moves the user', movedTo === '/events', 'now at ' + movedTo)

  const annotations = await win.webContents.executeJavaScript(`
    (async () => {
      const tools = await document.modelContext.getTools();
      const byName = Object.fromEntries(tools.map((t) => [t.name, t.annotations]));
      return { read: byName.listWorkspaces, remove: byName.deleteWorkspace };
    })()
  `)
  record(
    'reads and deletions are annotated for the agent',
    annotations.read?.readOnlyHint === true && annotations.remove?.destructiveHint === true,
    JSON.stringify(annotations)
  )

  // Signing out must withdraw the tools: the page is not reloaded, so anything
  // left registered would still be acting as the person who just left.
  const clicked = await win.webContents.executeJavaScript(`
    (async () => {
      const find = (label) =>
        [...document.querySelectorAll('button')].find((b) => b.textContent.trim() === label);
      // Log Out lives inside the user menu, so it has to be opened first —
      // driving this the way a person does rather than calling the handler,
      // which would prove only that the function exists.
      const menu = find('Q');
      if (!menu) return 'no user menu';
      menu.click();
      await new Promise((r) => setTimeout(r, 300));
      const logout = find('Logout');
      if (!logout) return 'no Logout in the open menu';
      logout.click();
      return 'clicked';
    })()
  `)
  record('the Logout control was reachable', clicked === 'clicked', JSON.stringify(clicked))
  await new Promise((r) => setTimeout(r, 1500))
  const afterLogout = await win.webContents.executeJavaScript(
    '(async () => (await document.modelContext.getTools()).length)()'
  )
  record('signing out withdraws every tool', afterLogout === 0, afterLogout + ' tools left registered')

  console.log('\n─── WebMCP, in a real browser ───')
  for (const r of results) console.log(`${r.pass ? '✓' : '✗'} ${r.name} — ${r.detail}`)
  const failed = results.filter((r) => !r.pass)
  console.log(failed.length === 0 ? '\n✓ all checks passed' : `\n✗ ${failed.length} check(s) failed`)
  app.exit(failed.length === 0 ? 0 : 1)
})
