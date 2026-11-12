// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * End-to-end check of site tools: a page's WebMCP tools, shared from the
 * extension, called by an agent through the workspace MCP server.
 *
 * The unit tests cover each piece against fakes. What only a real browser can
 * show is the chain: the observer finding the page's modelContext before the
 * page uses it, the worker's socket reaching a real backend with the cookie,
 * the approval gate holding a destructive call, and a closed tab reopening in
 * the background.
 *
 * It builds and runs its own backend on a free port with a throwaway database,
 * so it never touches a running dev server. It needs Go, and playwright or
 * playwright-core with its Chromium (the full build: headless_shell cannot
 * load extensions).
 *
 *   npm run verify:site-tools
 *
 * PLAYWRIGHT_DIR points at a directory whose node_modules holds playwright(-core),
 * CHROMIUM at a Chrome binary, and AGENTRQ_SERVER_BIN at a prebuilt server.
 */
import { execFile, spawn } from 'node:child_process'
import { cp, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { createServer } from 'node:http'
import { createRequire } from 'node:module'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const HERE = dirname(fileURLToPath(import.meta.url))
const EXTENSION = join(HERE, '..')
const REPO = join(EXTENSION, '../..')
const CLI = join(REPO, 'cli/agentrq-ws/bin/agentrq-ws.js')
const ALL_SITES = ['https://*/*', 'http://*/*']

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

async function loadChromium() {
  const require = createRequire(process.env.PLAYWRIGHT_DIR ? join(process.env.PLAYWRIGHT_DIR, 'x.js') : import.meta.url)
  for (const name of ['playwright', 'playwright-core']) {
    try {
      return require(name).chromium
    } catch {}
  }
  throw new Error('playwright is not installed: npm i --no-save playwright && npx playwright install chromium, or set PLAYWRIGHT_DIR')
}

const freePort = () =>
  new Promise((resolve) => {
    const s = createServer().listen(0, '127.0.0.1', () => {
      const { port } = s.address()
      s.close(() => resolve(port))
    })
  })

async function until(what, check, ms = 20000) {
  const end = Date.now() + ms
  for (;;) {
    const value = await check()
    if (value) return value
    if (Date.now() > end) throw new Error(`timed out waiting for ${what}`)
    await sleep(250)
  }
}

/**
 * The stub the page finds. Installed by an init script, which runs before any
 * content script, so to the observer it is the browser's own.
 */
const NATIVE_STUB = `(() => {
  const tools = new Map()
  Object.defineProperty(document, 'modelContext', {
    configurable: true,
    value: {
      registerTool(tool, options = {}) {
        if (tools.has(tool.name)) return Promise.reject(new DOMException('duplicate', 'InvalidStateError'))
        tools.set(tool.name, tool)
        options.signal?.addEventListener('abort', () => tools.delete(tool.name))
        return Promise.resolve()
      },
    },
  })
})()`

/**
 * A site with one read-only and one destructive tool, one that navigates the
 * page and one that never answers. The last two are marked read-only so no
 * approval stands between the call and the navigation.
 */
const PAGE = `<!doctype html><title>Thing shop</title><h1>Thing shop</h1><script>
  window.deleted = []
  document.modelContext.registerTool({
    name: 'getGreeting',
    description: 'Say hello',
    inputSchema: { type: 'object', properties: {} },
    annotations: { readOnlyHint: true },
    execute: async () => 'hello from ' + location.host,
  })
  document.modelContext.registerTool({
    name: 'deleteThing',
    description: 'Delete a thing',
    inputSchema: { type: 'object', properties: { id: { type: 'string' } }, required: ['id'] },
    annotations: { destructiveHint: true },
    execute: async ({ id }) => { window.deleted.push(id); return { content: [{ type: 'text', text: 'deleted ' + id }] } },
  })
  document.modelContext.registerTool({
    name: 'navigate',
    description: 'Go to a page of the shop',
    inputSchema: { type: 'object', properties: { url: { type: 'string' } }, required: ['url'] },
    annotations: { readOnlyHint: true },
    execute: async ({ url }) => { location.href = url; return { navigatedTo: url } },
  })
  document.modelContext.registerTool({
    name: 'waitForever',
    description: 'Never answers',
    inputSchema: { type: 'object', properties: {} },
    annotations: { readOnlyHint: true },
    execute: () => new Promise(() => {}),
  })
</script>`

async function buildServer(dir) {
  if (process.env.AGENTRQ_SERVER_BIN) return process.env.AGENTRQ_SERVER_BIN
  const out = join(dir, 'server')
  await new Promise((resolve, reject) =>
    execFile('go', ['build', '-o', out, './cmd/server'], { cwd: join(REPO, 'backend') }, (err, _o, stderr) =>
      err ? reject(new Error(`go build failed: ${stderr}`)) : resolve(),
    ),
  )
  return out
}

function startServer(bin, port, dir) {
  const log = []
  const child = spawn(bin, [], {
    // Its config path is relative to the working directory.
    cwd: join(REPO, 'backend/cmd/server'),
    env: {
      ...process.env,
      PORT: String(port),
      AGENTRQ_AUTH_ROOT_LOGIN_ENABLED: 'true',
      AGENTRQ_AUTH_ROOT_ACCESS_TOKEN: 'agentrq',
      AGENTRQ_SQLITE_DSN: join(dir, 'agentrq.db'),
      AGENTRQ_RATELIMIT_ENABLED: 'false',
      AGENTRQ_DDOS_ENABLED: 'false',
    },
  })
  child.stdout.on('data', (d) => log.push(String(d)))
  child.stderr.on('data', (d) => log.push(String(d)))
  return { child, log }
}

/** Run agentrq-ws as an agent would; resolves with its exit code and output. */
function agentrqWs(url, args) {
  return new Promise((resolve) => {
    execFile(process.execPath, [CLI, ...args], { env: { ...process.env, AGENTRQ_WS_URL: url } }, (err, stdout, stderr) =>
      resolve({ code: err ? (err.code ?? 1) : 0, out: (stdout + stderr).trim() }),
    )
  })
}

const results = []
const record = (name, pass, detail) => {
  results.push({ name, pass, detail })
  console.log(`${pass ? '✓' : '✗'} ${name} — ${detail}`)
}

// Undone in reverse, on the way out and on the deadline.
const cleanup = []
const undoAll = async () => {
  for (const undo of cleanup.splice(0).reverse()) await Promise.resolve().then(undo).catch(() => {})
}

async function main() {
  const dir = await mkdtemp(join(tmpdir(), 'agentrq-site-tools-'))
  cleanup.push(() => rm(dir, { recursive: true, force: true }))
  try {
    const chromium = await loadChromium()
    const [serverPort, pagePort] = [await freePort(), await freePort()]
    // Two origins, so the site's tools are told apart from the server's own app.
    const server = `http://127.0.0.1:${serverPort}`
    const site = `http://localhost:${pagePort}`

    const backend = startServer(await buildServer(dir), serverPort, dir)
    cleanup.push(() => backend.child.kill())
    await until('the backend', () => fetch(`${server}/api/v1/auth/user`).then(() => true, () => false), 30000).catch((err) => {
      throw new Error(`${err.message}\n${backend.log.join('')}`)
    })

    const pages = createServer((_, res) => res.setHeader('content-type', 'text/html').end(PAGE)).listen(pagePort)
    cleanup.push(() => pages.close())

    // A copy of the extension holding the all-sites access up front: granting
    // an optional permission needs a click, which a headless run cannot make.
    const ext = join(dir, 'extension')
    await cp(join(EXTENSION, 'manifest.json'), join(ext, 'manifest.json'))
    await cp(join(EXTENSION, 'src'), join(ext, 'src'), { recursive: true })
    await cp(join(EXTENSION, 'icons'), join(ext, 'icons'), { recursive: true })
    const manifest = JSON.parse(await readFile(join(ext, 'manifest.json'), 'utf8'))
    manifest.host_permissions.push(...ALL_SITES)
    await writeFile(join(ext, 'manifest.json'), JSON.stringify(manifest))

    const context = await chromium.launchPersistentContext(join(dir, 'profile'), {
      executablePath: process.env.CHROMIUM || chromium.executablePath(),
      headless: true,
      viewport: null,
      args: [`--disable-extensions-except=${ext}`, `--load-extension=${ext}`],
    })
    cleanup.push(() => context.close())
    await context.addInitScript(NATIVE_STUB)
    const sw = context.serviceWorkers()[0] ?? (await context.waitForEvent('serviceworker'))

    // Signed in, as the human: the worker's ticket request carries this cookie.
    const api = async (method, path, data) => {
      const res = await context.request.fetch(`${server}/api/v1${path}`, { method, data })
      if (!res.ok()) throw new Error(`${method} ${path}: ${res.status()} ${await res.text()}`)
      return res.headers()['content-type']?.includes('json') ? res.json() : null
    }
    await api('POST', '/auth/root/login', { rootToken: 'agentrq' })
    const { workspace } = await api('POST', '/workspaces', { workspace: { name: 'Site tools', description: 'e2e' } })
    const { task } = await api('POST', `/workspaces/${workspace.id}/tasks`, {
      task: { title: 'Tidy the shop', body: 'e2e', createdBy: 'human', assignee: 'agent', status: 'ongoing' },
    })
    const { token } = await api('GET', `/workspaces/${workspace.id}/token`)
    const mcp = `${server}/mcp/${workspace.id}?token=${token}`

    await sw.evaluate((url) => chrome.storage.sync.set({ serverUrl: url }), server)
    const scripts = await until('the content scripts', () =>
      sw.evaluate(() =>
        chrome.scripting.getRegisteredContentScripts().then((s) => (s.length ? s : null)),
      ),
    )
    record('detection is on for every site', scripts.length === 2, scripts.map((s) => `${s.id}:${s.world}`).join(', '))

    const page = await context.newPage()
    await page.goto(`${site}/things`)
    const badge = await until('the badge', () =>
      sw.evaluate(async (origin) => {
        const [tab] = await chrome.tabs.query({ url: `${origin}/*` })
        return chrome.action.getBadgeText({ tabId: tab.id })
      }, site),
    )
    record('the observer sees the page register its tools', badge === '4', `badge "${badge}"`)
    record('and the page cannot read the nonce', (await page.evaluate(() => document.documentElement.dataset.agentrqNonce)) === undefined, 'data-agentrq-nonce is gone')

    // What the popup's Share button stores.
    await sw.evaluate(
      ({ origin, workspaceId, lastUrl }) =>
        chrome.storage.local.set({ shares: { [origin]: { workspaceId, lastUrl, tools: [], alwaysAllow: [], pending: true } } }),
      { origin: site, workspaceId: workspace.id, lastUrl: `${site}/things` },
    )
    const listed = await until('the share to reach the server', async () => {
      const { out } = await agentrqWs(mcp, ['site-tools'])
      const shares = JSON.parse(out)
      return shares[0]?.online && shares[0].tools.length === 4 ? shares : null
    })
    record('listSiteTools shows the site online', listed[0].site === site, `${listed[0].site}: ${listed[0].tools.map((t) => t.name).join(', ')}`)

    const call = (tool, args = {}) => agentrqWs(mcp, ['call-site-tool', site, tool, '--task', task.id, '--args', JSON.stringify(args)])
    // The approval the call posted to the task, as the web app shows it.
    const pendingApproval = async () => {
      const { messages = [] } = (await api('GET', `/workspaces/${workspace.id}/tasks/${task.id}`)).task
      return messages.map((m) => m.metadata).find((m) => m?.type === 'elicitation_request' && m.status === 'pending')
    }

    let started = Date.now()
    const greeting = await call('getGreeting')
    record('a read-only tool runs without asking', greeting.code === 0 && greeting.out === `hello from localhost:${pagePort}`, `${JSON.stringify(greeting.out)} in ${Date.now() - started} ms`)
    record('and posts no approval', !(await pendingApproval()), 'no pending request in the task')

    started = Date.now()
    const deleting = call('deleteThing', { id: '42' })
    const asked = await until('the approval request', pendingApproval)
    const early = await page.evaluate(() => window.deleted.length)
    record('a destructive tool waits for the human', early === 0, `approval posted after ${Date.now() - started} ms; nothing deleted yet`)
    await api('POST', `/workspaces/${workspace.id}/tasks/${task.id}/elicitation`, {
      requestId: asked.requestId,
      action: 'accept',
      content: { decision: 'allow' },
    })
    const deleted = await deleting
    const onPage = await page.evaluate(() => window.deleted)
    record('and runs once allowed', deleted.code === 0 && deleted.out === 'deleted 42' && onPage.join() === '42', `${JSON.stringify(deleted.out)}; the page deleted [${onPage}]`)

    started = Date.now()
    const navigating = await call('navigate', { url: '/elsewhere' })
    await page.waitForURL(`${site}/elsewhere`)
    record(
      'a tool that navigates its own page still returns its result',
      navigating.code === 0 && navigating.out === '{"navigatedTo":"/elsewhere"}',
      `${JSON.stringify(navigating.out)} in ${Date.now() - started} ms`,
    )

    // The new page announces again before the next call can find the tool.
    await until('the new page to announce', () =>
      sw.evaluate(async (origin) => {
        const [tab] = await chrome.tabs.query({ url: `${origin}/*` })
        return (await chrome.action.getBadgeText({ tabId: tab.id })) === '4'
      }, site),
    )
    started = Date.now()
    const hanging = call('waitForever')
    await sleep(1000)
    await page.goto(`${site}/away`)
    const abandoned = await hanging
    const waited = Date.now() - started
    record(
      'a call whose page navigates away fails at once',
      abandoned.code !== 0 && abandoned.out.includes(`the ${site} page navigated away during the call`) && waited < 5000,
      `${JSON.stringify(abandoned.out)} after ${waited} ms`,
    )

    await page.close()
    await until('the tab to close', () => sw.evaluate((origin) => chrome.tabs.query({ url: `${origin}/*` }).then((t) => t.length === 0), site))
    started = Date.now()
    const again = await call('getGreeting')
    const reopened = await sw.evaluate((origin) => chrome.tabs.query({ url: `${origin}/*` }), site)
    record(
      'with no tab open, the call reopens the site in the background',
      again.code === 0 && again.out === `hello from localhost:${pagePort}` && reopened.length === 1 && !reopened[0].active,
      `${JSON.stringify(again.out)} in ${Date.now() - started} ms; reopened ${reopened[0]?.url} active=${reopened[0]?.active}`,
    )
  } finally {
    await undoAll()
  }
}

const deadline = setTimeout(async () => {
  console.error('✗ verification timed out')
  await undoAll()
  process.exit(3)
}, 180000)

console.log('─── Site tools, in a real browser ───')
main()
  .then(() => {
    const failed = results.filter((r) => !r.pass)
    console.log(failed.length === 0 ? '\n✓ all checks passed' : `\n✗ ${failed.length} check(s) failed`)
    process.exitCode = failed.length === 0 ? 0 : 1
  })
  .catch((err) => {
    console.error(`✗ ${err.stack || err}`)
    process.exitCode = 2
  })
  .finally(() => clearTimeout(deadline))
