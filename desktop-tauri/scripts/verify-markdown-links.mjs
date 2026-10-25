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
 * What the shell hands to the system's opener is recorded by a stand-in
 * `xdg-open` rather than run (see lib/shell-stub.mjs), so a passing run does
 * not launch an editor on whoever ran it. That seam exists on Linux only;
 * elsewhere those assertions are skipped. The clipboard is the real one.
 *
 *   npm run verify:markdown-links
 */
import { mkdir, mkdtemp, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'

import { launchApp } from './lib/app.mjs'
import { printResults, runVerify, skip, summarize } from './lib/report.mjs'
import { readClipboard, stubOpener, writeClipboard } from './lib/shell-stub.mjs'
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

  const before = document.body.innerText;
  (icon ?? anchor).dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));

  // The handler is async: the bridge round-trip and any toast land after it.
  await new Promise((r) => setTimeout(r, 400));

  const added = document.body.innerText.replace(before, '');
  holder.remove();
  return { hasHref: anchor.hasAttribute('href'), added: added.trim() };
})()`

const results = []
const record = (name, pass, detail) => results.push({ name, pass, detail })

await runVerify(
  async () => {
    const dir = await mkdtemp(join(tmpdir(), 'agentrq-file-links-'))
    const plan = join(dir, 'skills support plan.md')
    const script = join(dir, 'install.sh')
    const folder = join(dir, 'brain')
    await writeFile(plan, '# plan\n')
    await writeFile(script, '#!/bin/sh\necho hi\n')
    await mkdir(folder)

    const opener = process.platform === 'linux' ? await stubOpener() : null
    app = await launchApp({ serverUrl: NO_SERVER, env: opener?.env })
    const { session } = app
    await session.waitForPage('the app to mount', "!!window.agentrq && !!document.querySelector('#app')?.children.length")

    const bridge = await session.evaluate("typeof window.agentrq?.files?.open === 'function'")
    record('the bridge exposes files.open', bridge === true, String(bridge))

    const asUrl = (path) => 'file://' + encodeURI(path)
    const click = (path, options) => session.evaluate(clickScript(asUrl(path), options))
    /** The opener's calls after the first `count`, once there is one. */
    const nextCall = (count) =>
      waitFor('the shell to reach the opener', async () => (await opener.calls())[count], { timeout: 5000 }).catch(() => undefined)
    const noOpener = (name) => skip(name, 'only Linux has a stand-in opener')
    const shellChecks = []
    let seen = 0

    const doc = await click(plan)
    record('the anchor carries no href to navigate to', doc.hasHref === false, 'href present: ' + doc.hasHref)
    record('opening quietly says nothing', doc.added === '', JSON.stringify(doc.added))
    if (opener) {
      const call = await nextCall(seen)
      record('clicking a document asks the shell to open it', call === plan, JSON.stringify(call))
      seen += 1
    } else shellChecks.push(noOpener('clicking a document asks the shell to open it'))

    const executable = await click(script)
    record(
      'and the app says so',
      executable.added.includes('file manager') && executable.added.includes(script),
      JSON.stringify(executable.added)
    )
    // The file manager is the system's to run; on Linux with none on the bus
    // there is nothing to record. That it was never opened as a file is
    // checked below, once every click has been made.
    if (opener) seen = (await opener.calls()).length
    else shellChecks.push(noOpener('clicking a script reveals it instead of running it'))

    await click(folder)
    if (opener) {
      const call = await nextCall(seen)
      record('clicking a folder opens it', call === folder, JSON.stringify(call))
      seen += 1
    } else shellChecks.push(noOpener('clicking a folder opens it'))

    if (opener) {
      record(
        'clicking a script reveals it instead of running it',
        !(await opener.calls()).includes(script),
        'the script was never handed to the opener'
      )
    }

    const missing = await click(join(dir, 'gone.md'))
    if (opener) {
      record('a file that is not there reaches the shell and no further', (await opener.calls()).length === seen, 'no shell call')
    } else shellChecks.push(noOpener('a file that is not there reaches the shell and no further'))
    record('and the person is told which file', missing.added.includes('gone.md'), JSON.stringify(missing.added))

    // Copying a link's target, on the real clipboard, put back afterwards.
    //
    // This is the check that found the reason copies go through the shell at
    // all: `navigator.clipboard.writeText` throws "Document is not focused" in
    // a window that is not frontmost, gesture or no gesture, and this window
    // never is. A copy button that works only when nothing has stolen focus is
    // the same silent nothing as the bug being fixed.
    const before = readClipboard()
    if (before === null) {
      shellChecks.push(skip('clicking the copy button copies the path', 'no tool to read the clipboard (xclip on Linux)'))
      const copied = await click(plan, { copyText: plan })
      record('and says what it copied', copied.added.includes('Copied'), JSON.stringify(copied.added))
    } else {
      const copied = await click(plan, { copyText: plan })
      const got = await waitFor('the path on the clipboard', () => readClipboard() === plan, { timeout: 5000 }).catch(() => false)
      record('clicking the copy button copies the path', got === true, String(readClipboard()))
      record('and says what it copied', copied.added.includes('Copied'), JSON.stringify(copied.added))
      writeClipboard(before)
    }

    printResults('links in a message body, in a real window', [...results, ...shellChecks])
    return summarize(results, shellChecks)
  },
  async () => {
    await app?.close()
  }
)
