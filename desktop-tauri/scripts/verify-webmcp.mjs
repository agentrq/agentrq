// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * End-to-end check that the WebMCP catalogue reaches a real browser, on the
 * Tauri build: the Tauri twin of desktop/scripts/verify-webmcp.mjs.
 *
 * The unit tests prove the catalogue is right and that registration is driven
 * correctly. What they cannot prove is the part that only exists in a webview:
 * that the app finds `document.modelContext` at all, registers against it once
 * the user is signed in, that a tool invoked through that object really calls
 * the interface's own API, and that signing out withdraws the tools rather
 * than leaving an agent holding the last person's session.
 *
 * No WebMCP-capable browser is required: a stand-in `modelContext` is installed
 * before the app boots, which is what the page sees from a real one. It goes in
 * through the debug build's AGENTRQ_E2E_INIT_SCRIPT, the script the main
 * webview runs first on every load; no hook of this script's own is needed.
 *
 * Where the Electron script answers the API itself and reads the requests a
 * tool made, this one runs against a real backend and reads what a tool did
 * there: a task that exists afterwards says the tool drove the real API.
 *
 *   npm run verify:webmcp
 */
import { launchApp } from './lib/app.mjs'
import { startBackend } from './lib/backend.mjs'
import { printResults, runVerify, summarize } from './lib/report.mjs'

/** The stub the page will find, mirroring what a WebMCP browser exposes. */
const STUB = `(() => {
  // Idempotent on purpose: a script that runs again must not hand the page a
  // fresh empty registry while the app holds tools registered against the old one.
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
})();`

let backend
let app

const results = []
const record = (name, pass, detail) => results.push({ name, pass, detail })

await runVerify(
  async () => {
    backend = await startBackend()
    app = await launchApp({ serverUrl: backend.url, env: { AGENTRQ_E2E_INIT_SCRIPT: STUB } })
    const { session } = app
    const page = (code) => session.evaluate(code)
    const toolNames = () => page('(async () => [...(await document.modelContext.getTools())].map((t) => t.name))()')

    await session.waitForPage('the login screen', "location.pathname === '/login' && !!document.querySelector('#app')?.children.length")
    record('the stand-in is there before the app boots', (await page("typeof document.modelContext?.registerTool")) === 'function', 'registerTool')
    record('signed out, the app offers no tools', (await toolNames()).length === 0, (await toolNames()).length + ' tools')

    // Sign in as a person would be, then let the app find out on its next load.
    const { workspaceId, user } = await page(`(async () => {
      const call = async (path, body) => (await fetch('/api/v1' + path, {
        method: body ? 'POST' : 'GET', headers: { 'Content-Type': 'application/json' }, body: body && JSON.stringify(body) })).json()
      await call('/auth/root/login', { rootToken: ${JSON.stringify(backend.rootToken)} })
      const { workspace } = await call('/workspaces', { workspace: { name: 'Ops', description: 'Run things' } })
      return { workspaceId: workspace.id, user: await call('/auth/user') }
    })()`)
    await session.replaceWindow('location.assign("/")', '!!document.modelContext')
    await session.waitForPage('the tools to be registered', "document.modelContext.getTools().then((t) => t.length > 40)")

    const registered = await toolNames()
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
    const created = await page(`window.__stubTools.get('createTask').execute(
      { workspaceId: ${JSON.stringify(workspaceId)}, title: 'From an agent', body: 'Body' }, { signal: undefined })`)
    record('the tool returns what the API said', Boolean(created?.task?.id), JSON.stringify(created).slice(0, 120))
    const stored = await page(`fetch('/api/v1/workspaces/${workspaceId}/tasks').then((r) => r.json())`)
    record(
      'invoking a tool drives the interface\'s own API',
      Boolean(stored.tasks?.some((t) => t.id === created?.task?.id && t.title === 'From an agent')),
      'the backend holds the task: ' + Boolean(stored.tasks?.length)
    )

    const current = await page("window.__stubTools.get('getCurrentPage').execute({}, {})")
    record('getCurrentPage answers with the live route', typeof current?.path === 'string', JSON.stringify(current))

    await page("window.__stubTools.get('navigate').execute({ path: '/events' }, {})")
    await session.waitForPage('the navigate tool to move the user', "location.pathname === '/events'")
    record('the navigate tool moves the user', true, 'now at ' + (await page('location.pathname')))

    const annotations = await page(`(async () => {
      const byName = Object.fromEntries((await document.modelContext.getTools()).map((t) => [t.name, t.annotations]));
      return { read: byName.listWorkspaces, remove: byName.deleteWorkspace };
    })()`)
    record(
      'reads and deletions are annotated for the agent',
      annotations.read?.readOnlyHint === true && annotations.remove?.destructiveHint === true,
      JSON.stringify(annotations)
    )

    // Signing out must withdraw the tools: the page is not reloaded, so anything
    // left registered would still be acting as the person who just left.
    // The sidebar is collapsed to the avatar, which shows the name's first letter.
    const name = (user.name || user.email).charAt(0)
    await session.waitForPage(
      'the user menu',
      `[...document.querySelectorAll('button')].some((b) => b.textContent.trim() === ${JSON.stringify(name)})`
    )
    // Log Out lives inside the user menu, so it is opened first: driving this
    // as a person does, not calling the handler.
    await page(`[...document.querySelectorAll('button')].find((b) => b.textContent.trim() === ${JSON.stringify(name)}).click()`)
    await session.waitForPage('Logout in the open menu', "[...document.querySelectorAll('button')].some((b) => b.textContent.trim() === 'Logout')")
    await page("[...document.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Logout').click()")
    record('the Logout control was reachable', true, 'clicked')
    await session.waitForPage('every tool to be withdrawn', "document.modelContext.getTools().then((t) => t.length === 0)")
    record('signing out withdraws every tool', true, '0 tools left registered')

    printResults('WebMCP, in a real webview', results)
    return summarize(results)
  },
  async () => {
    await app?.close()
    await backend?.stop()
  }
)
