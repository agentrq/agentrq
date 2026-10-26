// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * End-to-end check of the side panel, on the Tauri build: the Tauri twin of
 * desktop/scripts/verify-side-panel.mjs.
 *
 * The unit tests cover each rule: what a guest is created with, where its
 * links go, how wide the panel may be. They cannot say whether a real webview
 * honours them: that a guest really is made, that a page's own redirect really
 * stays in the panel, that a reload of the app really takes every guest with
 * it, that an Extension's page really reaches its Extension and nothing else.
 *
 * The app is the real debug build on a scratch backend, signed in as the root
 * user, on a task whose message links to a stand-in site (lib/site.mjs).
 * WebDriver sees only the main webview, so a guest is read through the site's
 * log of what it requested and what its page told it, and through the panel's
 * own controls in the main page. A guest's death is a silence: its page beats
 * every 100 ms, so "closed" is read as "will never beat again", measured
 * against a page that is known to have started later, never as a wait.
 *
 * What only a real OS can do is not here: SKIPPED says what, and why.
 *
 *   npm run verify:side-panel
 */
import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import { launchApp } from './lib/app.mjs'
import { startBackend } from './lib/backend.mjs'
import { printResults, runVerify, skip, summarize } from './lib/report.mjs'
import { stubShell } from './lib/shell-stub.mjs'
import { startSite } from './lib/site.mjs'
import { waitFor } from './lib/webdriver.mjs'

const EXTENSION = 'verify-notes'
const EXTENSION_PAGE = `agentrq-ext://${EXTENSION}/panel/index.html`

/** The page an Extension shows in the panel: it asks its Extension, and tells the site. */
const extensionPage = (origin) => `<!doctype html><html><body><script>
  const gid = 'ext-' + Math.random().toString(36).slice(2);
  const tell = (what) => { new Image().src = ${JSON.stringify(origin)} + '/beacon?gid=' + gid + '&' + what; };
  const panel = window.agentrq && window.agentrq.panel;
  tell('hello=1&page=' + location.pathname + '&keys=' + Object.keys(window.agentrq || {}).join() +
    '&send=' + typeof (panel && panel.send) + '&node=' + typeof require);
  // Something to hand back to the Extension, and to be handed back.
  panel.send({ type: 'ping', n: 7 }).then(
    (answer) => tell('answer=' + encodeURIComponent(JSON.stringify(answer))),
    (err) => tell('answer-failed=' + encodeURIComponent(String(err))));
  // Another Extension's pages, and the Extension's own code, are not served.
  fetch('agentrq-ext://' + ${JSON.stringify(EXTENSION)} + '/index.js').then((r) => tell('code=' + r.status), () => tell('code=error'));
  // Asking to speak for somebody else changes nothing: the shell reads the sender.
  panel.send({ type: 'ping', to: 'other' }).then((a) => tell('forged=' + typeof a), () => tell('forged=failed'));
  setInterval(() => tell('beat=1'), 100);
</script></body></html>`

let backend
let app
let site
let shell
let dir

const results = []
const record = (name, pass, detail = '') => results.push({ name, pass, detail })

/** What a run on a real OS has to check by hand: printed, never counted. */
const SKIPPED = [
  skip('a person\'s click on a target=_blank link loads it in the panel itself', 'needs a native click: WebKit makes no window of a script\'s click, so nothing reaches the shell'),
  skip('Cmd/Ctrl-click on a link inside a page opens the system browser', 'needs a native click with a key held; WebDriver cannot reach a guest'),
  skip('Cmd/Ctrl+F typed into a page opens the panel\'s find bar', 'needs a native key press in the guest; WebDriver cannot reach it'),
  skip('a snapshot of the page in the panel', 'needs the OS to capture the guest\'s own surface'),
  skip('a page asking for a permission shows the bar, and the answer is remembered', 'TODO(WP7c): permissions are not merged yet'),
  skip('find in page counts and steps through the matches', 'TODO(WP7c): find is not merged yet'),
]

await runVerify(
  async () => {
    dir = await mkdtemp(join(tmpdir(), 'agentrq-verify-panel-'))
    backend = await startBackend()
    site = await startSite()
    shell = process.platform === 'linux' ? await stubShell() : null
    await mkdir(join(dir, 'panel'))
    await writeFile(join(dir, 'panel/index.html'), extensionPage(site.origin))
    // The debug build installs this one Extension, answered by an echo, until
    // WP9's host does it for real.
    app = await launchApp({
      serverUrl: backend.url,
      env: { ...shell?.env, AGENTRQ_E2E_EXTENSION: `${EXTENSION}:${dir}:panel/index.html` },
    })
    const { session } = app
    const page = (code) => session.evaluate(code)
    const inPage = (what, code, options) => session.waitForPage(what, code, options)

    /** Beacons of one page, after hit number `since`. */
    const beaten = (gid, since) => site.beacons(gid).filter((h) => h.at > since && h.query.get('beat') === '1').length
    /** The hello a page said once it started, after hit number `since`. */
    const hello = (what, path, since = -1, scheme = '') =>
      site.hit(what, (h) => h.path === '/beacon' && h.query.get('hello') === '1' && h.query.get('page') === path, { since }).then(
        (h) => ({ ...h, gid: h.query.get('gid'), said: Object.fromEntries(h.query), scheme })
      )
    /** Wait for `count` more beats of a page: it is alive, and was not replaced. */
    const stillAlive = (gid, count = 3) => {
      const since = site.hits.length - 1
      return waitFor(`${count} beats from ${gid}`, () => beaten(gid, since) >= count)
    }
    const mark = () => site.hits.length - 1
    const address = () => page("document.querySelector('[data-side-panel-address]')?.value")
    const backEnabled = () => page("!document.querySelector('[data-side-panel-back]').disabled")
    const type = (url) =>
      page(`(() => {
        const input = document.querySelector('[data-side-panel-address]');
        input.value = ${JSON.stringify(url)};
        input.dispatchEvent(new Event('input'));
        input.closest('form').requestSubmit();
        // A person's next click is in the page, which takes the keyboard: the
        // field follows a page only while it is not the one being typed in.
        input.blur();
      })()`)
    const addressIs = (url) => inPage(`the address to read ${url}`, `document.querySelector('[data-side-panel-address]')?.value === ${JSON.stringify(url)}`)

    // Sign in, and make a task whose message links to the site.
    await inPage('the login screen', "location.pathname === '/login'")
    const link = `${site.origin}/page`
    const ids = await page(`(async () => {
      const call = async (path, body) => (await fetch('/api/v1' + path, {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })).json()
      await call('/auth/root/login', { rootToken: ${JSON.stringify(backend.rootToken)} })
      const { workspace } = await call('/workspaces', { workspace: { name: 'panel' } })
      const { task } = await call('/workspaces/' + workspace.id + '/tasks', { task: {
        title: 'Release notes', body: 'Check them on the [release page](${link}).', createdBy: 'human', assignee: 'agent' } })
      return { workspace: workspace.id, task: task.id }
    })()`)
    const taskPath = `/workspaces/${ids.workspace}/tasks/${ids.task}`
    /** Load the task and wait until it has drawn, with the panel as the page remembered it. */
    const openTask = async () => {
      // The old document is told apart from the new by a marker only it has.
      await page(`(window.__verifyOld = true, location.assign(${JSON.stringify(taskPath)}))`)
      await inPage(
        'the task',
        "!window.__verifyOld && document.readyState === 'complete' && !!document.querySelector('[data-side-panel], [data-side-panel-toggle]')"
      )
    }
    await openTask()

    // ── opening a guest, and the address bar ────────────────────────────────
    record('the panel starts closed', (await page("!document.querySelector('[data-side-panel]')")) === true)
    await page("document.querySelector('[data-side-panel-toggle]').click()")
    await inPage('the panel', "!!document.querySelector('[data-side-panel]')")
    record('the task view\'s button opens it', true)

    await type(link)
    const first = await hello('the guest to load the page it was asked for', '/page')
    record('typing an address loads it in a guest', true, link)
    record('a web page in the panel has no bridge', first.said.bridge === 'undefined', first.said.bridge)
    record('and no Node', first.said.node === 'undefined/undefined', first.said.node)
    await addressIs(link)
    record('the address field follows the page', true, link)

    // A page's own script moves it, and that is a navigation like any other.
    await type(`${site.origin}/redirect`)
    const redirected = await hello('the redirecting page to load', '/redirect')
    const landed = await hello('the redirect to land', '/second', redirected.at)
    await addressIs(`${site.origin}/second`)
    record('a redirect by script stays in the panel and the address follows it', true, `${landed.said.page}`)
    await inPage('back to be enabled', "!document.querySelector('[data-side-panel-back]').disabled")
    record('back is enabled after navigating', (await backEnabled()) === true)

    // The panel never shows a file: a page that goes to one stays where it is.
    await type(`${site.origin}/file`)
    const filer = await hello('the page that goes to a file to load', '/file')
    await site.hit('the page to have tried', (h) => h.query.get('gid') === filer.gid && h.query.get('before') === '1')
    await stillAlive(filer.gid)
    record('a page cannot navigate the panel to a file', true, 'its page kept beating')
    record('and the address does not change', (await address()) === `${site.origin}/file`, await address())

    // A page asking for a window, by script: never one. Without a person's
    // click WebKit may not even ask; either way the page stays, nothing is
    // loaded for the window and none appears.
    await type(`${site.origin}/popup`)
    const popper = await hello('the page that asks for a window to load', '/popup')
    await stillAlive(popper.gid)
    const loaded = site.hits.filter((h) => h.at > popper.at && h.path === '/second')
    record('a script\'s popup opens no window and loads nothing', (await session.windowHandles()).length === 1 && loaded.length === 0,
      `${(await session.windowHandles()).length} window, ${loaded.length} loads of the popup's page`)
    record('and does not take the panel from the page', (await address()) === `${site.origin}/popup`, await address())

    // Back, through the panel's own button.
    // Marked before typing: the page can say hello before the typing returns.
    const typed = mark()
    await type(`${site.origin}/page`)
    await hello('the page again', '/page', typed)
    await addressIs(`${site.origin}/page`)
    await page("document.querySelector('[data-side-panel-back]').click()")
    await addressIs(`${site.origin}/popup`)
    record('back goes to the page before', true, `${site.origin}/popup`)

    // Open in browser: a web page goes to the system, as the shell hands it.
    if (shell) {
      const calls = (await shell.calls()).length
      await page("document.querySelector('[data-side-panel-external]').click()")
      const call = await waitFor('the opener to be called', async () => (await shell.calls())[calls], { timeout: 5000 }).catch(() => undefined)
      record('Open in browser hands the page to the system', call === `${site.origin}/popup`, JSON.stringify(call))
    } else SKIPPED.push(skip('Open in browser hands the page to the system', 'only Linux has a stand-in opener'))

    // ── a link in a message ─────────────────────────────────────────────────
    const messageLink = `document.querySelector('.md-body a[href="${link}"]')`
    /** Close the panel and wait for it to go. */
    const closePanel = async () => {
      await page("document.querySelector('[data-side-panel-close]').click()")
      await inPage('the panel to close', "!document.querySelector('[data-side-panel]')")
    }
    /** Who has beaten since hit number `since`. */
    const beating = (since) => new Set(site.beacons().filter((h) => h.at > since && h.query.get('beat') === '1').map((h) => h.query.get('gid')))
    /** The guest that loads next must be the only one alive: `old` is dead, and stays so. */
    const onlyThe = async (gid, since) => {
      await stillAlive(gid)
      return [...beating(since)]
    }

    await closePanel()
    // The description starts collapsed, and its markdown is drawn once expanded.
    await page("document.querySelector('.truncate.cursor-pointer').click()")
    await inPage('the message link', `!!${messageLink}`)
    let since = mark()
    await page(`${messageLink}.click()`)
    await inPage('the panel to open', "!!document.querySelector('[data-side-panel]')")
    const opened = await hello('the guest to load the linked page', '/page', since)
    record('a plain click on a link in a message opens it in the panel', true, link)
    const alive = await onlyThe(opened.gid, opened.at)
    record('and closing the panel before took its guest with it', alive.length === 1 && alive[0] === opened.gid, JSON.stringify(alive))

    // Cmd/Ctrl-click: the system browser instead, and the panel is left alone.
    if (shell) {
      const calls = (await shell.calls()).length
      await page(`${messageLink}.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, button: 0, ctrlKey: true, metaKey: true }))`)
      const call = await waitFor('the opener to be called', async () => (await shell.calls())[calls], { timeout: 5000 }).catch(() => undefined)
      record('a Cmd/Ctrl-click on it goes to the system browser instead', call === link, JSON.stringify(call))
      await stillAlive(opened.gid)
      record('and the panel keeps its page', true, link)
    } else SKIPPED.push(skip('a Cmd/Ctrl-click on a message link goes to the system browser', 'only Linux has a stand-in opener'))

    // ── guests are closed when the app's page reloads ───────────────────────
    since = mark()
    await openTask()
    await inPage('the panel to come back', "!!document.querySelector('[data-side-panel]')")
    const again = await hello('the panel to open a guest of its own', '/page', since)
    const after = await onlyThe(again.gid, again.at)
    record('a reload of the app closes its guests, and the new page knows none', again.gid !== opened.gid && after.length === 1 && after[0] === again.gid,
      JSON.stringify(after))

    // ── an Extension's page ─────────────────────────────────────────────────
    since = mark()
    await type(EXTENSION_PAGE)
    const ext = await hello('the Extension\'s page to load', '/panel/index.html', since)
    record('an Extension\'s page opens in the panel', true, EXTENSION_PAGE)
    record('its page has the panel bridge and nothing else', ext.said.keys === 'panel' && ext.said.send === 'function' && ext.said.node === 'undefined',
      JSON.stringify({ keys: ext.said.keys, send: ext.said.send, node: ext.said.node }))
    const answer = await site.hit('the Extension to answer', (h) => h.query.get('gid') === ext.gid && (h.query.has('answer') || h.query.has('answer-failed')))
    const said = answer.query.get('answer')
    record('what the page sends reaches its Extension, and the answer comes back', said !== null && JSON.parse(said).echo?.n === 7, said ?? answer.query.get('answer-failed'))
    const code = await site.hit('the Extension\'s own code to be asked for', (h) => h.query.get('gid') === ext.gid && h.query.has('code'))
    record('the Extension\'s own code is never served', code.query.get('code') === '404', code.query.get('code'))
    const forged = await site.hit('the forged message to be answered', (h) => h.query.get('gid') === ext.gid && h.query.has('forged'))
    record('a message cannot choose another recipient', forged.query.get('forged') === 'object', forged.query.get('forged'))
    await stillAlive(ext.gid)
    const chosen = (await address()) === EXTENSION_PAGE
    record('the address shows the Extension\'s page', chosen, await address())

    printResults('the side panel, in a real window', [...results, ...SKIPPED])
    return summarize(results, SKIPPED)
  },
  async () => {
    await app?.close()
    await shell?.stop()
    await site?.stop()
    await backend?.stop()
    if (dir) await rm(dir, { recursive: true, force: true })
  }
)
