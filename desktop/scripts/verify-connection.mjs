/**
 * End-to-end check of the first-run connection screen.
 *
 * Complements verify-e2e.mjs, which covers the already-configured case. This
 * one starts with nothing stored and drives the screen the way a new user
 * would: a bad URL must be refused with a readable reason, and a good one must
 * be probed, stored and then honoured by the proxy.
 *
 * It then reaches the same screen the other way — by adding a profile, where
 * the user chose to come here and must be able to leave again.
 *
 * The IPC handlers below mirror the ones in src/main/index.js. They are
 * repeated rather than imported because index.js takes over the Electron app
 * lifecycle at import time; the logic they exercise — validateServerUrl and the
 * config store — is the real thing.
 *
 *   AGENTRQ_TEST_SERVER=http://localhost:3999 npx electron scripts/verify-connection.mjs
 */
import { app, BrowserWindow, ipcMain, net, protocol, session } from 'electron'
import { readFile, writeFile, access, mkdtemp } from 'node:fs/promises'
import { constants } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

import { createAppProtocolHandler } from '../src/main/protocol.js'
import { CONFIG_FILENAME, createServerConfigStore, validateServerUrl } from '../src/main/server-config.js'

const __dirname = dirname(fileURLToPath(import.meta.url))
const RENDERER_ROOT = join(__dirname, '../dist/renderer')
const PRELOAD = join(__dirname, '../dist/preload/index.cjs')
const testServer = process.env.AGENTRQ_TEST_SERVER ?? 'http://localhost:3999'

protocol.registerSchemesAsPrivileged([
  {
    scheme: 'app',
    privileges: { standard: true, secure: true, supportFetchAPI: true, stream: true, corsEnabled: true },
  },
])

let serverUrl = ''
let configStore
/** Whether the screen is being shown for a profile that can be abandoned. */
let canCancel = false
/** How many times the screen has asked the shell to discard that profile. */
let cancelCalls = 0

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
  const saved = await window.agentrq.connection.save(${JSON.stringify(testServer)});
  record('good server is accepted and stored', saved.ok === true, JSON.stringify(saved));

  const after = await window.agentrq.connection.get();
  record('connection is now configured', after.configured === true && after.serverUrl === ${JSON.stringify(testServer)},
    JSON.stringify(after));

  // And the proxy now has somewhere to forward to.
  const res = await fetch('/api/v1/auth/config');
  record('proxy reaches the newly configured server', res.status === 200, 'status ' + res.status);

  return out;
})()`

/**
 * The same screen reached the other way: by adding a profile.
 *
 * Here the user chose to come here and must be able to leave. Everything the
 * unit tests cover stops at the shell's answer — this is the part they cannot
 * reach, that the answer arrives through the preload bridge and turns into a
 * button that calls back.
 */
const CANCEL_CHECKS = `(async () => {
  const out = [];
  const record = (name, pass, detail) => out.push({ name, pass, detail });

  const cancel = [...document.querySelectorAll('button[type=button]')]
    .find((b) => b.textContent.trim() === 'Cancel');
  record('a way back is offered when a profile can be discarded', !!cancel,
    cancel ? cancel.textContent.trim() : 'no cancel button');

  cancel?.click();
  await new Promise((r) => setTimeout(r, 300));

  // The shell is replacing the window, so the screen stays inert rather than
  // offering an action that no longer means anything.
  record('the screen goes inert once it is on its way out',
    cancel?.disabled === true && cancel?.textContent.trim() === 'Going back...',
    'disabled ' + cancel?.disabled + ', label ' + JSON.stringify(cancel?.textContent.trim()));

  return out;
})()`

app.whenReady().then(async () => {
  await session.defaultSession.clearStorageData({ storages: ['cookies'] })

  // A throwaway userData directory, so the run always starts unconfigured and
  // never disturbs a real installation's settings.
  const configPath = join(await mkdtemp(join(tmpdir(), 'agentrq-conn-')), CONFIG_FILENAME)
  configStore = createServerConfigStore({
    readFile: () => readFile(configPath, 'utf-8'),
    writeFile: (contents) => writeFile(configPath, contents, 'utf-8'),
  })
  serverUrl = (await configStore.load()).serverUrl

  ipcMain.handle('agentrq:connection:get', () => ({
    configured: Boolean(serverUrl),
    serverUrl,
    locked: false,
    canCancel,
  }))
  // The real one replaces the window; counting the request is what this run
  // needs to see, and destroying the window mid-check would take the results
  // with it.
  ipcMain.handle('agentrq:connection:cancel', async () => {
    cancelCalls += 1
    return true
  })
  ipcMain.handle('agentrq:connection:validate', async (_e, url) => {
    const result = await validateServerUrl(url, net.fetch)
    return result.ok ? { ok: true, url: result.url } : result
  })
  ipcMain.handle('agentrq:connection:save', async (_e, url) => {
    const validated = await validateServerUrl(url, net.fetch)
    if (!validated.ok) return validated
    const saved = await configStore.save(validated.url)
    if (!saved.ok) return saved
    serverUrl = saved.url
    return { ok: true, url: saved.url }
  })

  protocol.handle(
    'app',
    createAppProtocolHandler({
      serverUrl: () => serverUrl,
      netFetch: net.fetch,
      fileExists,
      readFile: (pathname) => readFile(join(RENDERER_ROOT, pathname)),
    })
  )

  const win = new BrowserWindow({
    show: false,
    webPreferences: { preload: PRELOAD, contextIsolation: true, nodeIntegration: false, sandbox: true },
  })
  await win.loadURL('app://agentrq/')
  await new Promise((r) => setTimeout(r, 1200))

  let results
  try {
    results = await win.webContents.executeJavaScript(CHECKS)
  } catch (err) {
    console.error('✗ checks threw:', err)
    app.exit(1)
    return
  }

  console.log('\n─── first-run connection screen ───')
  for (const r of results) console.log(`${r.pass ? '✓' : '✗'} ${r.name} — ${r.detail}`)

  // The same screen, reached by adding a profile: unconfigured again, but with
  // somewhere to go back to.
  serverUrl = ''
  canCancel = true
  const second = new BrowserWindow({
    show: false,
    webPreferences: { preload: PRELOAD, contextIsolation: true, nodeIntegration: false, sandbox: true },
  })
  await second.loadURL('app://agentrq/')
  await new Promise((r) => setTimeout(r, 1200))

  let cancelResults
  try {
    cancelResults = await second.webContents.executeJavaScript(CANCEL_CHECKS)
  } catch (err) {
    console.error('✗ cancel checks threw:', err)
    app.exit(1)
    return
  }
  cancelResults.push({
    name: 'the shell was asked exactly once',
    pass: cancelCalls === 1,
    detail: `calls ${cancelCalls}`,
  })

  console.log('\n─── added-profile connection screen ───')
  for (const r of cancelResults) console.log(`${r.pass ? '✓' : '✗'} ${r.name} — ${r.detail}`)

  const failed = [...results, ...cancelResults].filter((r) => !r.pass)
  console.log(failed.length === 0 ? '\n✓ all checks passed' : `\n✗ ${failed.length} check(s) failed`)
  app.exit(failed.length === 0 ? 0 : 1)
})
