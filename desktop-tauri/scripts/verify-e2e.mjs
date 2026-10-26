// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * End-to-end check of the app's proxy against a real AgentRQ backend, on the
 * Tauri build: the Tauri twin of desktop/scripts/verify-e2e.mjs, with the same
 * checks in the same order.
 *
 * The bundled renderer loads, an unauthenticated request is rejected,
 * root-token login through the proxy stores the `at` cookie in the profile's
 * jar, the following authenticated call replays it, and the SSE stream connects.
 *
 * The app is the real debug build, pinned to a scratch backend with
 * AGENTRQ_SERVER_URL as a user would pin it, and driven by `tauri-driver`.
 * Where the Electron script stands in for the shell with its own IPC handlers,
 * the real shell answers here.
 *
 *   npm run verify:e2e
 *
 * To use a server of your own: AGENTRQ_SERVER_URL=http://localhost:3999
 * AGENTRQ_ROOT_TOKEN=... npm run verify:e2e
 */
import { launchApp } from './lib/app.mjs'
import { startBackend } from './lib/backend.mjs'
import { printResults, runVerify, skip, summarize } from './lib/report.mjs'

let backend
let app

const checksFor = (rootToken) => `(async () => {
  const out = [];
  const record = (name, pass, detail) => out.push({ name, pass, detail });

  // The SPA itself must have booted from the bundled assets.
  record('renderer mounted', !!document.querySelector('#app')?.children.length,
    document.querySelector('#app')?.children.length + ' child nodes');

  // The shared auth guard ran and redirected: proof that the frontend's own
  // route table and navigation guard are driving this window, over
  // createWebHistory on the app:// origin.
  record('shared auth guard routed to /login', location.pathname === '/login', 'at ' + location.pathname);

  // Tailwind's source detection is rooted at the build, and a wrong root
  // silently drops every utility — the DOM looks correct and the app renders
  // unstyled. Assert a computed style, not just presence.
  const styled = getComputedStyle(document.body).fontFamily.includes('Inter')
    || document.styleSheets.length > 0;
  const probe = document.createElement('div');
  probe.className = 'rounded-3xl';
  document.body.appendChild(probe);
  const radius = getComputedStyle(probe).borderRadius;
  probe.remove();
  record('stylesheet is applied', parseFloat(radius) > 0, 'rounded-3xl -> ' + radius);

  // The desktop bridge is visible to the renderer, with every surface the
  // shell needs to reach it.
  const bridge = window.agentrq ?? {};
  const expected = ['connection', 'notifications', 'navigation', 'theme', 'updates'];
  const surfaces = expected.filter((k) => bridge[k]);
  record('desktop bridge exposed', bridge.isDesktop === true && surfaces.length === expected.length,
    'platform ' + bridge.platform + ', surfaces: ' + surfaces.join(', '));

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

  record('no update banner before there is an update',
    !document.body.textContent.includes('A new version of AgentRQ is available'),
    'banner absent');

  return out;
})()`

// Electron drives the update banner by sending the status event itself. Here
// the shell owns that event, and the page cannot emit one (the capability
// grants listen only), so the banner can be raised only by a real update.
const UPDATE_SKIPPED = [
  skip(
    'a ready update raises the shared update banner',
    'needs an update feed the shell can verify; the page cannot emit the status event'
  ),
]

await runVerify(
  async () => {
    backend = await startBackend()
    app = await launchApp({ serverUrl: backend.url })
    const { session } = app

    // The SPA boots, then its router guard sends a signed-out visitor to /login.
    await session.waitForPage(
      'the shared auth guard to route to /login',
      "location.pathname === '/login' && !!document.querySelector('#app')?.children.length"
    )

    const results = await session.evaluate(checksFor(backend.rootToken))
    printResults(`app proxy against ${backend.url}`, [...results, ...UPDATE_SKIPPED])
    return summarize(results, UPDATE_SKIPPED)
  },
  async () => {
    await app?.close()
    await backend?.stop()
  }
)
