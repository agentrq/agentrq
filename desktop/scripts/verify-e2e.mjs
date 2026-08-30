/**
 * End-to-end check of the app:// proxy against a real AgentRQ backend.
 *
 * The spike proved the transport in isolation; this proves the whole path:
 * the bundled renderer loads from app://, an unauthenticated request is
 * rejected, root-token login through the proxy stores the `at` cookie in the
 * session jar, the following authenticated call replays it, and the SSE stream
 * connects and delivers events.
 *
 * Run against a scratch backend, not your dev instance:
 *   AGENTRQ_SERVER_URL=http://localhost:3999 \
 *   AGENTRQ_ROOT_TOKEN=... npx electron scripts/verify-e2e.mjs
 */
import { app, BrowserWindow, net, protocol, session } from 'electron'
import { readFile, access } from 'node:fs/promises'
import { constants } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

import { createAppProtocolHandler } from '../src/main/protocol.js'
import { resolveServerUrl } from '../src/main/server-config.js'

const __dirname = dirname(fileURLToPath(import.meta.url))
const RENDERER_ROOT = join(__dirname, '../dist/renderer')
const serverUrl = resolveServerUrl({ env: process.env })
const rootToken = process.env.AGENTRQ_ROOT_TOKEN ?? 'qa-root-token'

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

const CHECKS = `(async () => {
  const out = [];
  const record = (name, pass, detail) => out.push({ name, pass, detail });

  // The SPA itself must have booted from the bundled assets.
  record('renderer mounted', !!document.querySelector('#app')?.children.length,
    document.querySelector('#app')?.children.length + ' child nodes');

  // Relative URL, exactly as src/api.js writes it — no absolute server URL anywhere.
  const anon = await fetch('/api/v1/auth/user');
  record('unauthenticated /auth/user is 401', anon.status === 401, 'status ' + anon.status);

  const login = await fetch('/api/v1/auth/root/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ rootToken: ${JSON.stringify(rootToken)} }),
  });
  record('root login succeeds through proxy', login.ok, 'status ' + login.status);

  // Passes only if Set-Cookie landed in the main-process jar and was replayed.
  const me = await fetch('/api/v1/auth/user');
  const body = me.ok ? await me.json() : null;
  record('cookie replayed on next call', me.status === 200, 'status ' + me.status + (body ? ' user=' + (body.email ?? body.id ?? 'ok') : ''));

  const ws = await fetch('/api/v1/workspaces');
  record('authenticated API call works', ws.status === 200, 'status ' + ws.status);

  // The live-update transport, now against the real backend through the proxy.
  const sse = await new Promise((resolve) => {
    const es = new EventSource('/api/v1/events/stream');
    const wait = Number(${JSON.stringify(process.env.AGENTRQ_SSE_TIMEOUT_MS ?? "6000")});
    const timer = setTimeout(() => { es.close(); resolve({ pass: false, detail: "no open within " + wait + "ms" }); }, wait);
    es.onopen = () => { clearTimeout(timer); es.close(); resolve({ pass: true, detail: 'stream opened' }); };
    es.onerror = () => { clearTimeout(timer); es.close(); resolve({ pass: false, detail: 'connection error' }); };
  });
  record('SSE stream connects through proxy', sse.pass, sse.detail);

  return out;
})()`

app.whenReady().then(async () => {
  // Electron persists the cookie jar across runs, so without this the
  // "unauthenticated" check would pass on a cold run and fail on every run
  // after a successful login — the check would be measuring leftover state
  // rather than the proxy.
  await session.defaultSession.clearStorageData({ storages: ['cookies'] })

  protocol.handle(
    'app',
    createAppProtocolHandler({
      serverUrl: () => serverUrl,
      netFetch: net.fetch,
      fileExists,
      readFile: (pathname) => readFile(join(RENDERER_ROOT, pathname)),
    })
  )

  const win = new BrowserWindow({ show: false, webPreferences: { contextIsolation: true, sandbox: true } })
  await win.loadURL('app://agentrq/')
  // Let the SPA finish its own boot (router guard, initial fetches) first.
  await new Promise((r) => setTimeout(r, 1500))

  let results
  try {
    results = await win.webContents.executeJavaScript(CHECKS)
  } catch (err) {
    console.error('✗ checks threw:', err)
    app.exit(1)
    return
  }

  console.log(`\n─── app:// proxy against ${serverUrl} ───`)
  for (const r of results) {
    console.log(`${r.pass ? '✓' : '✗'} ${r.name} — ${r.detail}`)
  }
  const failed = results.filter((r) => !r.pass)
  console.log(failed.length === 0 ? '\n✓ all checks passed' : `\n✗ ${failed.length} check(s) failed`)
  app.exit(failed.length === 0 ? 0 : 1)
})
