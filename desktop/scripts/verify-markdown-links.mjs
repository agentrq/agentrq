// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * End-to-end check of the two things a link in a message body can be clicked
 * for: copying where it points, and — for a local file — opening it.
 *
 * Unit tests cover the two decisions — what the markdown renderer emits, and
 * which files may be handed to their default application — but not the path
 * between them, which is where this bug lived: a sanitised anchor, a delegated
 * click in the real App.vue, the preload bridge, and the shell's answer coming
 * back as a toast. That path only exists inside a running Electron window.
 *
 * Needs no backend: the app boots from app://, fails its first API call and
 * lands on the login screen, which is enough — the click handler is bound at
 * the application root, above any route.
 *
 * The one thing left stubbed is `shell.openPath` / `shell.showItemInFolder`
 * themselves, recorded here rather than called: a passing run would otherwise
 * launch an editor and a Finder window on whoever ran it. Everything on either
 * side of them is the real thing.
 *
 * The clipboard check uses the real system clipboard, so it puts back whatever
 * was on it before.
 *
 *   npm run build && npx electron scripts/verify-markdown-links.mjs
 */
import { app, BrowserWindow, clipboard, ipcMain, net, protocol } from 'electron'
import { readFile, access, mkdtemp, mkdir, stat, writeFile } from 'node:fs/promises'
import { constants } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

import { createAppProtocolHandler } from '../src/main/protocol.js'
import { FileOpenAction, fileOpenAction, localPathFromFileUrl } from '../src/main/files.js'

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

/** What the shell was asked to do, in place of actually doing it. */
const shellCalls = []

/**
 * Mirrors the handler in src/main/index.js. Repeated rather than imported
 * because index.js takes over the Electron app lifecycle at import time; the
 * logic it leans on — files.js — is the real thing, and so is the IPC channel
 * the renderer reaches it through.
 */
function registerFileHandler() {
  ipcMain.handle('agentrq:files:open', async (_event, rawUrl) => {
    const path = localPathFromFileUrl(rawUrl)
    if (!path) return { ok: false, error: 'That link does not point at a file on this computer.' }

    let stats
    try {
      stats = await stat(path)
    } catch {
      return { ok: false, error: `No such file: ${path}` }
    }

    if (fileOpenAction(path, { isDirectory: stats.isDirectory() }) === FileOpenAction.Reveal) {
      shellCalls.push({ call: 'showItemInFolder', path })
      return { ok: true, revealed: true }
    }
    shellCalls.push({ call: 'openPath', path })
    return { ok: true, revealed: false }
  })
}

/**
 * Drives one click, in the window, against the markup `renderMarkdown`
 * produces for a link — pinned to that renderer by its own unit tests.
 *
 * The click is dispatched inside the injected markup and left to bubble:
 * reaching the handler bound at the application root is precisely what is
 * being checked.
 */
const clickScript = (fileUrl, { copyText = '' } = {}) => `(async () => {
  // index.html's mount point and App.vue's own root element both carry
  // id="app". The inner one is the Vue tree; appending to the outer would put
  // the link outside the delegated handler, which is not where message bodies
  // are rendered and would prove nothing.
  const root = document.querySelector('#app #app') ?? document.querySelector('#app');
  document.querySelectorAll('#verify-md').forEach((n) => n.remove());

  const fileUrl = ${JSON.stringify(fileUrl)};
  const copyText = ${JSON.stringify(copyText)};

  const holder = document.createElement('div');
  holder.id = 'verify-md';
  const anchor = document.createElement('a');
  anchor.className = 'md-file-link';
  anchor.setAttribute('data-file-url', fileUrl);
  anchor.setAttribute('role', 'link');
  anchor.tabIndex = 0;
  anchor.textContent = 'plan';
  holder.append(anchor);

  // The copy button carries an icon, so the click lands on a child of it —
  // which is the case the handler has to walk up from.
  let icon = null;
  if (copyText) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'md-copy-link';
    button.setAttribute('data-copy-text', copyText);
    icon = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    button.append(icon);
    holder.append(button);
  }
  root.appendChild(holder);

  const before = document.body.innerText;
  (icon ?? anchor).dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));

  // The handler is async: the bridge round-trip and any toast land after it.
  await new Promise((r) => setTimeout(r, 400));

  const added = document.body.innerText.replace(before, '');
  holder.remove();
  return { hasHref: anchor.hasAttribute('href'), added: added.trim() };
})()`

setTimeout(() => {
  console.error('✗ verification timed out')
  app.exit(3)
}, 60000)

app.whenReady().then(async () => {
  const dir = await mkdtemp(join(tmpdir(), 'agentrq-file-links-'))
  const plan = join(dir, 'skills support plan.md')
  const script = join(dir, 'install.sh')
  const folder = join(dir, 'brain')
  await writeFile(plan, '# plan\n')
  await writeFile(script, '#!/bin/sh\necho hi\n')
  await mkdir(folder)

  registerFileHandler()
  // The connection screen would otherwise take the window; this run is about
  // what happens inside the app, not how it got pointed at a server.
  ipcMain.handle('agentrq:connection:get', () => ({ configured: true, serverUrl: 'http://localhost:3999', locked: true }))
  ipcMain.handle('agentrq:update:get', () => ({ status: 'idle', detail: '', version: '', enabled: false }))
  ipcMain.handle('agentrq:theme:set', () => ({ source: 'system', color: '#fafafa' }))
  ipcMain.handle('agentrq:profiles:get', () => ({ activeProfileId: '', profiles: [] }))
  ipcMain.handle('agentrq:notifications:get', () => ({ supported: false, mutedWorkspaces: [] }))
  // The real one, mirroring src/main/index.js: this check is about the
  // clipboard actually being written, so nothing here is stubbed.
  ipcMain.handle('agentrq:clipboard:write', (_e, text) => {
    clipboard.writeText(String(text ?? ''))
    return true
  })

  protocol.handle(
    'app',
    createAppProtocolHandler({
      serverUrl: () => 'http://localhost:3999',
      netFetch: net.fetch,
      fileExists,
      readFile: (pathname) => readFile(join(RENDERER_ROOT, pathname)),
    })
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

  const bridge = await win.webContents.executeJavaScript(
    "typeof window.agentrq?.files?.open === 'function'"
  )
  record('the bridge exposes files.open', bridge === true, String(bridge))

  const asUrl = (path) => 'file://' + encodeURI(path)

  const document = await win.webContents.executeJavaScript(clickScript(asUrl(plan)))
  record('the anchor carries no href to navigate to', document.hasHref === false, 'href present: ' + document.hasHref)
  record(
    'clicking a document asks the shell to open it',
    shellCalls.at(-1)?.call === 'openPath' && shellCalls.at(-1)?.path === plan,
    JSON.stringify(shellCalls.at(-1))
  )
  record('opening quietly says nothing', document.added === '', JSON.stringify(document.added))

  const executable = await win.webContents.executeJavaScript(clickScript(asUrl(script)))
  record(
    'clicking a script reveals it instead of running it',
    shellCalls.at(-1)?.call === 'showItemInFolder' && shellCalls.at(-1)?.path === script,
    JSON.stringify(shellCalls.at(-1))
  )
  record(
    'and the app says so',
    executable.added.includes('file manager') && executable.added.includes(script),
    JSON.stringify(executable.added)
  )

  await win.webContents.executeJavaScript(clickScript(asUrl(folder)))
  record(
    'clicking a folder opens it',
    shellCalls.at(-1)?.call === 'openPath' && shellCalls.at(-1)?.path === folder,
    JSON.stringify(shellCalls.at(-1))
  )

  const callsBefore = shellCalls.length
  const missing = await win.webContents.executeJavaScript(clickScript(asUrl(join(dir, 'gone.md'))))
  record('a file that is not there reaches the shell and no further', shellCalls.length === callsBefore, 'no shell call')
  record(
    'and the person is told which file',
    missing.added.includes('gone.md'),
    JSON.stringify(missing.added)
  )

  // Copying a link's target. The real clipboard, so it is put back afterwards.
  //
  // This is the check that found the reason copies go through the shell at all:
  // `navigator.clipboard.writeText` throws "Document is not focused" in a
  // window that is not frontmost, gesture or no gesture, and this window never
  // is. A copy button that works only when nothing has stolen focus is the same
  // silent nothing as the bug being fixed.
  const clipboardBefore = clipboard.readText()
  const copied = await win.webContents.executeJavaScript(
    clickScript(asUrl(plan), { copyText: plan })
  )
  record('clicking the copy button copies the path', clipboard.readText() === plan, clipboard.readText())
  record('and says what it copied', copied.added.includes('Copied'), JSON.stringify(copied.added))
  clipboard.writeText(clipboardBefore)

  console.log('\n─── links in a message body, in a real window ───')
  for (const r of results) console.log(`${r.pass ? '✓' : '✗'} ${r.name} — ${r.detail}`)
  const failed = results.filter((r) => !r.pass)
  console.log(failed.length === 0 ? '\n✓ all checks passed' : `\n✗ ${failed.length} check(s) failed`)
  app.exit(failed.length === 0 ? 0 : 1)
})
