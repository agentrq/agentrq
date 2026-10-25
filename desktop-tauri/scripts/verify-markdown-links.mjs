// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * End-to-end check of the two things a link in a message body can be clicked
 * for, on the Tauri build: the Tauri twin of
 * desktop/scripts/verify-markdown-links.mjs.
 *
 * Copying where it points, and, for a local file, opening it. The unit tests
 * cover the two decisions but not the path between them: a sanitised anchor, a
 * delegated click in the real App.vue, the bridge, and the shell's answer
 * coming back as a toast. That path only exists inside a running window.
 *
 * Needs no backend: the app is pinned to a server that is not there, fails its
 * first API call and lands on the login screen, which is enough, because the
 * click handler is bound at the application root, above any route.
 *
 * What the shell hands to the system's opener, and to the file manager over
 * D-Bus, is recorded by stand-ins rather than run (see lib/shell-stub.mjs), so
 * a passing run does not launch an editor or a file manager on whoever ran it.
 * That seam exists on Linux only; elsewhere those assertions are skipped. The
 * clipboard is the real one.
 *
 *   npm run verify:markdown-links
 */
import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { pathToFileURL } from 'node:url'

import { launchApp } from './lib/app.mjs'
import { printResults, runVerify, skip, summarize } from './lib/report.mjs'
import { readClipboard, stubShell, writeClipboard } from './lib/shell-stub.mjs'
import { waitFor } from './lib/webdriver.mjs'

// Nothing listens on port 1.
const NO_SERVER = 'http://127.0.0.1:1'

let app

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

  window.__verifyMd = { holder, before: document.body.innerText };
  (icon ?? anchor).dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
  return { hasHref: anchor.hasAttribute('href') };
})()`

/** Whatever text has appeared since the click (a toast), line by line. */
const ADDED = `(() => {
  const before = new Set(window.__verifyMd.before.split('\\n'));
  return document.body.innerText.split('\\n').filter((line) => !before.has(line)).join('\\n').trim();
})()`

const results = []
const record = (name, pass, detail) => results.push({ name, pass, detail })

let dir
let shell

await runVerify(
  async () => {
    dir = await mkdtemp(join(tmpdir(), 'agentrq-file-links-'))
    const plan = join(dir, 'skills support plan.md')
    const script = join(dir, 'install.sh')
    const folder = join(dir, 'brain')
    await writeFile(plan, '# plan\n')
    await writeFile(script, '#!/bin/sh\necho hi\n')
    await mkdir(folder)

    shell = process.platform === 'linux' ? await stubShell() : null
    app = await launchApp({ serverUrl: NO_SERVER, env: shell?.env })
    const { session } = app
    await session.waitForPage('the app to mount', "!!window.agentrq && !!document.querySelector('#app')?.children.length")
    // The server is not there: the first API call fails and the router lands on
    // /login. A click made before that would see the login screen arrive as if
    // it were its own toast, so wait for the screen, and for it to stop changing.
    await session.waitForPage('the login screen', "location.pathname === '/login'")
    let screen = ''
    let steady = 0
    await waitFor('the login screen to stop changing', async () => {
      const now = await session.evaluate('document.body.innerText')
      steady = now === screen ? steady + 1 : 0
      screen = now
      return steady >= 3
    })

    const bridge = await session.evaluate("typeof window.agentrq?.files?.open === 'function'")
    record('the bridge exposes files.open', bridge === true, String(bridge))

    const asUrl = (path) => 'file://' + encodeURI(path)
    /** Click; `added()` is then the toast text so far, if any. */
    const click = async (path, options) => {
      const { hasHref } = await session.evaluate(clickScript(asUrl(path), options))
      return { hasHref, added: () => session.evaluate(ADDED) }
    }
    /**
     * For a check that something did *not* appear: call it once the shell has
     * shown it answered (a record on the stand-ins). The page cannot be told
     * when the handler has finished, and its bridge is frozen, so this is one
     * more round trip through the same channel and a yield for what follows.
     */
    const answered = () =>
      session.evaluate('window.agentrq.connection.get().then(() => new Promise((r) => setTimeout(r, 0)))')
    /** The toast, once it says `text`; whatever is there when the wait runs out. */
    const toast = async (clicked, text) => {
      let last = ''
      await waitFor(
        `a toast saying ${text}`,
        async () => (last = await clicked.added()).includes(text),
        { timeout: 5000 }
      ).catch(() => {})
      return last
    }
    /** The opener's calls after the first `count`, once there is one. */
    const nextCall = (count) =>
      waitFor('the shell to reach the opener', async () => (await shell.calls())[count], { timeout: 5000 }).catch(() => undefined)
    const noOpener = (name) => skip(name, 'only Linux has a stand-in opener')
    const shellChecks = []
    let seen = 0

    const doc = await click(plan)
    record('the anchor carries no href to navigate to', doc.hasHref === false, 'href present: ' + doc.hasHref)
    if (shell) {
      const call = await nextCall(seen)
      record('clicking a document asks the shell to open it', call === plan, JSON.stringify(call))
      seen += 1
    } else shellChecks.push(noOpener('clicking a document asks the shell to open it'))
    await answered()
    const quiet = await doc.added()
    record('opening quietly says nothing', quiet === '', JSON.stringify(quiet))

    const executable = await click(script)
    const told = await toast(executable, 'file manager')
    record('and the app says so', told.includes('file manager') && told.includes(script), JSON.stringify(told))
    if (shell) seen = (await shell.calls()).length

    await click(folder)
    if (shell) {
      const call = await nextCall(seen)
      record('clicking a folder opens it', call === folder, JSON.stringify(call))
      seen += 1
    } else shellChecks.push(noOpener('clicking a folder opens it'))

    if (shell) {
      // On Linux the reveal is a D-Bus call to the file manager, never the
      // opener: so the stand-in file manager is what proves it was asked.
      if (shell.reveals) {
        const shown = pathToFileURL(script).href
        const got = await waitFor('the file manager to be asked', async () => (await shell.reveals()).includes(shown), {
          timeout: 5000,
        }).catch(() => false)
        record('clicking a script reveals it in the file manager', got === true, JSON.stringify(await shell.reveals()))
      } else shellChecks.push(skip('clicking a script reveals it in the file manager', shell.whyNoReveals))
      record(
        'and never hands it to the opener to run',
        !(await shell.calls()).includes(script),
        'the script was never handed to the opener'
      )
    } else shellChecks.push(noOpener('clicking a script reveals it in the file manager'))

    const revealed = shell?.reveals ? (await shell.reveals()).length : 0
    const missing = await click(join(dir, 'gone.md'))
    // The toast is the shell's answer: once it is up, the shell has finished
    // with the click, and anything it was going to hand on is on the record.
    const lost = await toast(missing, 'gone.md')
    record('and the person is told which file', lost.includes('gone.md'), JSON.stringify(lost))
    if (shell) {
      record(
        'a file that is not there reaches the shell and no further',
        (await shell.calls()).length === seen && (!shell.reveals || (await shell.reveals()).length === revealed),
        'no shell call'
      )
    } else shellChecks.push(noOpener('a file that is not there reaches the shell and no further'))

    // Copying a link's target, on the real clipboard, put back afterwards.
    //
    // This is the check that found the reason copies go through the shell at
    // all: `navigator.clipboard.writeText` throws "Document is not focused" in
    // a window that is not frontmost, gesture or no gesture, and this window
    // never is. A copy button that works only when nothing has stolen focus is
    // the same silent nothing as the bug being fixed.
    const before = readClipboard()
    const copy = await click(plan, { copyText: plan })
    if (before === null) {
      shellChecks.push(skip('clicking the copy button copies the path', 'no tool to read the clipboard (xclip on Linux)'))
    } else {
      const got = await waitFor('the path on the clipboard', () => readClipboard() === plan, { timeout: 5000 }).catch(() => false)
      record('clicking the copy button copies the path', got === true, String(readClipboard()))
    }
    const copied = await toast(copy, 'Copied')
    record('and says what it copied', copied.includes('Copied'), JSON.stringify(copied))
    if (before !== null) writeClipboard(before)

    printResults('links in a message body, in a real window', [...results, ...shellChecks])
    return summarize(results, shellChecks)
  },
  async () => {
    await app?.close()
    await shell?.stop()
    if (dir) await rm(dir, { recursive: true, force: true })
  }
)
