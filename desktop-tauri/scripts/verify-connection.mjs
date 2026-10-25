// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * End-to-end check of the first-run connection screen, on the Tauri build: the
 * Tauri twin of desktop/scripts/verify-connection.mjs.
 *
 * Complements verify-e2e.mjs, which covers the already-configured case. This
 * one starts with nothing stored and drives the screen the way a new user
 * would: a bad URL must be refused with a readable reason, and a good one must
 * be probed, stored and then honoured by the proxy.
 *
 * Electron's script stands in for the shell with its own IPC handlers. Here
 * the real shell answers, and it answers a save by rebuilding the window.
 *
 *   npm run verify:connection
 */
import { launchApp } from './lib/app.mjs'
import { startBackend } from './lib/backend.mjs'
import { printResults, runVerify, skip, summarize } from './lib/report.mjs'

let backend
let app

const checksFor = (testServer) => `(async () => {
  const out = [];
  const record = (name, pass, detail) => out.push({ name, pass, detail });

  const heading = document.querySelector('h2')?.textContent?.trim();
  const subtitle = document.querySelector('p')?.textContent?.trim();
  record('connection screen is showing', subtitle === 'Connect to your workspace server',
    heading + ' / ' + subtitle);

  const input = document.querySelector('input');
  const button = document.querySelector('button[type=submit]');
  record('URL field is focused and prefilled', document.activeElement === input && !!input?.value,
    'value ' + JSON.stringify(input?.value));

  // A first run has nowhere to go back to, and the app cannot start without a
  // server — so the way out must not be offered here.
  record('no way out is offered on a first run',
    document.querySelector('button[type=button]') === null,
    (document.querySelector('button[type=button]')?.textContent ?? 'none').trim());

  // macOS hides the title bar, so this screen has to declare the window's drag
  // handle itself — without one the window cannot be moved at all. Elsewhere
  // there is a real title bar and the page must not claim page content as a
  // handle.
  const drag = document.querySelector('.app-drag');
  // Tauri's handle is an attribute, where Electron's is a CSS app-region.
  const region = drag ? drag.getAttribute('data-tauri-drag-region') : 'none';
  record('the window can be dragged where it has no title bar',
    ${process.platform === 'darwin'} ? region === 'deep' : drag === null,
    'region ' + region);

  // The screen is styled, not just present. Tailwind's source detection is
  // rooted at the build, and a misconfigured root silently drops every
  // utility — which looks fine in the DOM and broken on screen.
  const card = document.querySelector('[role=dialog] > div');
  const radius = getComputedStyle(card).borderRadius;
  record('styles are applied', parseFloat(radius) > 0, 'card border-radius ' + radius);

  const setValue = (el, v) => {
    el.value = v;
    el.dispatchEvent(new Event('input', { bubbles: true }));
  };
  const settled = () => new Promise((r) => setTimeout(r, 400));

  // A URL that parses but is not a server: the screen must say so rather than
  // storing it and failing mysteriously later.
  setValue(input, 'http://localhost:1');
  await settled();
  button.click();
  await new Promise((r) => setTimeout(r, 1500));
  const error = document.querySelector('[role=dialog] p.text-red-600, [role=dialog] .text-red-600');
  record('bad server is refused with a reason', !!error?.textContent?.trim(),
    (error?.textContent ?? 'no error shown').trim());

  record('nothing was stored for the bad server', (await window.agentrq.connection.get()).configured === false,
    'configured ' + (await window.agentrq.connection.get()).configured);

  // The real one.
  setValue(input, ${JSON.stringify(testServer)});
  await settled();

  return out;
})()`

/**
 * After the save. The shell answers a save by rebuilding the window on the new
 * profile, so what follows runs in the page that replaced the one that saved.
 * Electron's stand-in shell leaves the page alone and checks in the same one.
 */
const checksAfterSave = (testServer) => `(async () => {
  const out = [];
  const record = (name, pass, detail) => out.push({ name, pass, detail });

  const after = await window.agentrq.connection.get();
  record('connection is now configured', after.configured === true && after.serverUrl === ${JSON.stringify(testServer)},
    JSON.stringify(after));

  // And the proxy now has somewhere to forward to.
  const res = await fetch('/api/v1/auth/config');
  record('proxy reaches the newly configured server', res.status === 200, 'status ' + res.status);

  return out;
})()`

// TODO(driver): the added-profile screen (a Cancel button that leads back) cannot
// be driven. A new profile's window has a data store of its own, and
// WebKitWebDriver only automates the web context it launched with: after
// `profiles.add()` the session lists no window at all. Electron's script
// reaches it through a stand-in shell and a second window. Run it by hand,
// and see profiles.rs for the shell's side of it, which has unit tests.
const CANCEL_SKIPPED = [
  skip('a way back is offered when a profile can be discarded', 'WebKitWebDriver cannot see a second profile\'s window'),
  skip('the screen goes inert once it is on its way out', 'WebKitWebDriver cannot see a second profile\'s window'),
  skip('the shell was asked exactly once', 'WebKitWebDriver cannot see a second profile\'s window'),
]

await runVerify(
  async () => {
    backend = await startBackend()
    // No AGENTRQ_SERVER_URL: the app starts with nothing stored.
    app = await launchApp()
    const { session } = app

    await session.waitForPage(
      'the connection screen',
      "document.querySelector('p')?.textContent?.trim() === 'Connect to your workspace server'"
    )
    const results = await session.evaluate(checksFor(backend.url))
    const saved = await session.replaceWindow(
      `window.agentrq.connection.save(${JSON.stringify(backend.url)})`,
      // The server is set, so the router sends the signed-out visitor to /login.
      "location.pathname === '/login'"
    )
    results.push({ name: 'good server is accepted and stored', pass: saved?.ok === true, detail: JSON.stringify(saved) })
    results.push(...(await session.evaluate(checksAfterSave(backend.url))))
    printResults('first-run connection screen', [...results, ...CANCEL_SKIPPED])

    return summarize(results, CANCEL_SKIPPED)
  },
  async () => {
    await app?.close()
    await backend?.stop()
  }
)
