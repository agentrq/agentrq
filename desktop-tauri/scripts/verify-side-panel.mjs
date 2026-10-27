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
 * it, that an Extension's page really reaches its Extension and nothing else,
 * and that the Extension really reaches its page and the panel.
 *
 * The app is the real debug build on a scratch backend, signed in as the root
 * user, on a task whose message links to a stand-in site (lib/site.mjs).
 * WebDriver sees only the main webview, so a guest is read through the site's
 * log of what it requested and what its page told it, and through the panel's
 * own controls in the main page. A guest's death is a silence: its page beats
 * every 100 ms, so "closed" is read as "will never beat again", measured
 * against a page that is known to have started later, never as a wait.
 *
 * The Extension is a real one, installed through the Extensions bridge and
 * run by the extension host on the Node.js the app downloads from nodejs.org.
 *
 * What only a real OS can do is not here: SKIPPED says what, and why.
 *
 *   npm run verify:side-panel
 */
import { spawnSync } from 'node:child_process'
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

/** The Extension: it echoes what its page sends, and posts to it, or opens the panel, when asked. */
const extensionFiles = {
  'package.json': JSON.stringify({ name: EXTENSION, version: '1.0.0', type: 'module', main: 'index.js', private: true }),
  'agentrq-extension.json': JSON.stringify({
    name: EXTENSION,
    displayName: 'Verify notes',
    version: '1.0.0',
    description: 'The side panel verify script\'s Extension.',
    license: 'MIT',
    engines: { agentrq: '>=0.9' },
    provides: { panels: [{ id: 'notes', label: 'Notes', entry: 'panel/index.html' }] },
    artifact: { release: 'v1.0.0', asset: `${EXTENSION}-1.0.0.tgz`, sha256: '0'.repeat(64) },
  }),
  'index.js': `export const name = ${JSON.stringify(EXTENSION)}
export const inject = ['panel']
export function apply(ctx) {
  ctx.panel.onMessage((message) => {
    if (message?.type === 'post') setTimeout(() => ctx.panel.post({ type: 'posted', n: message.n }), 50)
    if (message?.type === 'show') return ctx.panel.open({ url: message.url })
    return { echo: message }
  })
}
`,
}

/** The page an Extension shows in the panel: it asks its Extension, and tells the site. */
const extensionPage = (origin) => `<!doctype html><html><body><script>
  const gid = 'ext-' + Math.random().toString(36).slice(2);
  const tell = (what) => { new Image().src = ${JSON.stringify(origin)} + '/beacon?gid=' + gid + '&' + what; };
  const panel = window.agentrq && window.agentrq.panel;
  tell('hello=1&page=' + location.pathname + '&keys=' + Object.keys(window.agentrq || {}).join() +
    '&send=' + typeof (panel && panel.send) + '&node=' + typeof require);
  // What the Extension posts, told to the site.
  panel.onMessage((message) => tell('posted=' + encodeURIComponent(JSON.stringify(message))));
  // Asked to post, once the page listens; and, given an address, to open the panel there.
  panel.send({ type: 'post', n: 3 });
  const show = new URLSearchParams(location.search).get('show');
  if (show) panel.send({ type: 'show', url: show });
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
let blocker
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
]

await runVerify(
  async () => {
    dir = await mkdtemp(join(tmpdir(), 'agentrq-verify-panel-'))
    backend = await startBackend()
    site = await startSite()
    blocker = await startSite()
    shell = process.platform === 'linux' ? await stubShell() : null
    await mkdir(join(dir, 'panel'))
    await writeFile(join(dir, 'panel/index.html'), extensionPage(site.origin))
    for (const [file, text] of Object.entries(extensionFiles)) await writeFile(join(dir, file), text)
    app = await launchApp({ serverUrl: backend.url, env: shell?.env })
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

    // The Extension, installed as the Extensions page does: Node first. The
    // download can outlast a script's run, so the page keeps the outcome.
    await page(`(window.__verifyInstalled = null, (async () => {
      const ext = window.agentrq.extensions
      if (!(await ext.state()).node?.installed) {
        const node = await ext.downloadNode()
        if (!node?.ok) return node
      }
      return ext.installLocal(${JSON.stringify(dir)}, { grant: null })
    })().then((answer) => (window.__verifyInstalled = answer ?? {}), (err) => (window.__verifyInstalled = { reason: String(err) })), true)`)
    const installed = await inPage('the Extension to be installed', 'window.__verifyInstalled', { timeout: 300000 })
    record('the Extension installs, Node.js and all', installed.ok === true, JSON.stringify(installed))
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

    // Tauri gives every webview its IPC object, a guest's too: the ACL is what
    // refuses it, so no capability may name a guest (parity.test.js).
    site.page('/ipc', `<!doctype html><html><body><script>
      const tell = (what) => { new Image().src = '/beacon?' + what + '&_=' + Math.random(); };
      // Not \`ipc\`: wry's own \`window.ipc\` cannot be shadowed.
      const internals = window.__TAURI_INTERNALS__;
      (internals ? internals.invoke('connection_get') : Promise.reject(new Error('no IPC object')))
        .then(() => tell('ipc=allowed'), (e) => tell('ipc=refused&ipcObject=' + typeof internals + '&why=' + encodeURIComponent(String(e).slice(0, 120))));
    </script></body></html>`)
    const probing = mark()
    await type(`${site.origin}/ipc`)
    const ipc = await site.hit('the page to call an app command', (h) => h.query.has('ipc'), { since: probing })
    record('a web page in the panel can call no app command', ipc.query.get('ipc') === 'refused',
      `Tauri's IPC object: ${ipc.query.get('ipcObject')}; ${ipc.query.get('why')}`)

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

    // ── find in page ────────────────────────────────────────────────────────
    // Opened by the panel's own Cmd/Ctrl+F, in the main page: the same key
    // typed into the guest needs a native key press (SKIPPED).
    const findCount = () => page("document.querySelector('[data-side-panel-find-count]')?.textContent.trim()")
    /**
     * Run `script` in the main page, then whether the find bar's count comes
     * to read `count` and the page's selection to be in paragraph `paragraph`,
     * and what they read. The selection is WebKit's own step: the count's
     * index is the app's bookkeeping of which step was asked.
     */
    const finds = async (script, count, paragraph) => {
      const since = mark()
      await page(script)
      const selection = () => site.beacons(finder.gid).filter((h) => h.at > since && h.query.has('sel')).at(-1)?.query.get('sel')
      await waitFor(`the find count to read ${count}, in paragraph ${paragraph}`,
        async () => (await findCount()) === count && selection() === String(paragraph)).catch(() => {})
      const [read, sel] = [await findCount(), selection()]
      return [read === count && sel === String(paragraph), `${read}, selection in paragraph ${sel}`]
    }
    const findButton = (button) => `document.querySelector('[data-side-panel-find-${button}]').click()`
    const finding = mark()
    await type(`${site.origin}/find`)
    const finder = await hello('the page to search to load', '/find', finding)
    await addressIs(`${site.origin}/find`)
    await page("document.querySelector('[data-side-panel-address]').dispatchEvent(new KeyboardEvent('keydown', { key: 'f', ctrlKey: true, bubbles: true }))")
    await inPage('the find bar to take the focus', "document.activeElement?.matches('[data-side-panel-find-input]') === true")
    record('the panel\'s Cmd/Ctrl+F opens its find bar, focused', true)
    record('typing searches the page, counts its matches and selects the first', ...(await finds(`(() => {
      const input = document.querySelector('[data-side-panel-find-input]');
      input.value = 'needle';
      input.dispatchEvent(new Event('input'));
    })()`, '1/3', 0)))
    record('next steps to the following match', ...(await finds(findButton('next'), '2/3', 1)))
    record('previous steps back', ...(await finds(findButton('previous'), '1/3', 0)))
    await page("document.querySelector('[data-side-panel-find-close]').click()")
    await inPage('the find bar to close', "!document.querySelector('[data-side-panel-find]')")
    record('the find bar closes', true)

    // ── a page asking for a permission ──────────────────────────────────────
    // The location: WebKitGTK asks for it with no device behind it, where a
    // camera under Xvfb is refused before anybody is asked, and notifications
    // are asked for only from a person's click (below).
    const asking = mark()
    await type(`${site.origin}/ask`)
    const asker = await hello('the page that asks for the location to load', '/ask', asking)
    const bar = await inPage('the permission bar', "document.querySelector('[data-side-panel-permission]')?.textContent.trim()")
    record('a page asking for the location shows the bar in the window, naming its site', bar.includes(`${new URL(site.origin).host} wants to know your location`), bar)
    await page("document.querySelector('[data-side-panel-permission-allow]').click()")
    /**
     * What the page with `gid` was answered, and the error it got. Granted is
     * "not PERMISSION_DENIED": with no location service an allowed page gets
     * POSITION_UNAVAILABLE (2).
     */
    const answered = (gid) => site.hit('the page to be answered', (h) => h.query.get('gid') === gid && h.query.has('permission'))
      .then((h) => [h.query.get('permission'), `error ${h.query.get('error') ?? 'none'}`])
    const [allowed, error] = await answered(asker.gid)
    record('Allow gives the page its permission', allowed === 'granted', `${allowed}, ${error}`)
    await inPage('the bar to go', "!document.querySelector('[data-side-panel-permission]')")
    // The page asks again on every load: answered with no bar, it was remembered.
    await inPage('the page to finish loading', "document.querySelector('[data-side-panel-reload]')?.title === 'Reload'")
    const reloading = mark()
    await page("document.querySelector('[data-side-panel-reload]').click()")
    const reloaded = await hello('the page to load again', '/ask', reloading)
    const [remembered] = await answered(reloaded.gid)
    const barAgain = await page("!!document.querySelector('[data-side-panel-permission]')")
    record('after a reload it asks again and is answered with no bar', remembered === 'granted' && !barAgain, `${remembered}, bar ${barAgain ? 'shown' : 'not shown'}`)
    // The answer is the app's, not only WebKit's: the shield lists it.
    await page("document.querySelector('[data-side-panel-permissions-toggle]').click()")
    const listed = await inPage('the site permissions', "document.querySelector('[data-site-permissions] [data-site]')?.textContent.replace(/\\s+/g, ' ').trim()")
    record('Site permissions lists the site\'s location as allowed', listed.includes(new URL(site.origin).host) && listed.includes('Location') && listed.includes('Allowed'), listed)
    await page("document.querySelector('[data-site-permissions-close]').click()")
    await inPage('the site permissions to close', "!document.querySelector('[data-site-permissions]')")

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
    // The task has no messages yet, so its description opens on its own; the
    // link is in its markdown.
    await page("document.querySelector('.truncate.cursor-pointer')?.click()")
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

    // ── a site's notifications ──────────────────────────────────────────────
    // The page asks on a person's click when its address says so (WebKit asks
    // for nobody else), and shows one once allowed; its other addresses show
    // what the shell must hold back.
    const NOTIFY = `<!doctype html><html><head><title>Notifies</title></head><body><script>
      const gid = Math.random().toString(36).slice(2);
      const tell = (what) => { new Image().src = '/beacon?gid=' + gid + '&' + what + '&_=' + Math.random(); };
      const N = window.Notification;
      const mode = location.search.slice(1);
      tell('hello=1&page=/notify&mode=' + mode + '&notification=' + (/\\[native code\\]/.test(String(N)) ? 'native' : typeof N) + '/' + N?.permission);
      const show = (title, options) => {
        const shown = new N(title, options);
        shown.onclick = () => tell('clicked=' + encodeURIComponent(title));
        shown.onclose = () => tell('closed=' + encodeURIComponent(title));
        return shown;
      };
      if (mode === 'ask') document.addEventListener('click', () => N.requestPermission().then((answer) => {
        tell('asked=' + answer);
        if (answer === 'granted') show('Build passed', { body: 'Checked in the <b>side panel</b> & <a href="https://example.com">here</a>' });
        // A blocked page that tries anyway.
        else { show('Blocked anyway'); tell('tried=1'); }
      }), { once: true });
      if (mode === 'tag') {
        show('Tagged 1', { tag: 'build' });
        setTimeout(() => { show('Tagged 2', { tag: 'build' }); tell('tagged=2'); }, 300);
      }
      if (mode === 'burst') {
        try { for (let i = 1; i <= 8; i++) show('Burst ' + i); tell('burst=8'); }
        catch (e) { tell('burst=' + encodeURIComponent(String(e))); }
      }
      if (mode === 'show') { if (N.permission === 'granted') show('After removal'); tell('shown=' + N.permission); }
      setInterval(() => tell('beat=1'), 100);
    </script></body></html>`
    site.page('/notify', NOTIFY)
    blocker.page('/notify', NOTIFY)
    /**
     * A person's click in the middle of the guest, through the X server: no
     * script can make one. The app's page starts below the window's menu
     * bar, which is the window's height less the page's.
     */
    const clickGuest = async () => {
      const box = await page(`(() => {
        const r = document.querySelector('[data-side-panel-host]').getBoundingClientRect();
        return { x: Math.round(r.x + r.width / 2), y: Math.round(r.y + r.height / 2), height: innerHeight };
      })()`)
      const xdotool = (...args) => spawnSync('xdotool', args, { encoding: 'utf-8' }).stdout.trim()
      const window = xdotool('search', '--onlyvisible', '--name', '^AgentRQ Tauri$').split('\n')[0]
      const height = Number(/Geometry: \d+x(\d+)/.exec(xdotool('getwindowgeometry', window))?.[1] ?? box.height)
      xdotool('mousemove', '--window', window, String(box.x), String(box.y + height - box.height), 'click', '1')
    }
    /**
     * A new guest, at `url`: its page's hello. The panel opens again at the
     * last address, so the guest leaves for a page that shows nothing first.
     */
    const newGuest = async (url, server = site) => {
      const leaving = mark()
      await type(link)
      await hello('the guest to leave for a quiet page', '/page', leaving)
      await closePanel()
      const since = server.hits.length - 1
      await page("document.querySelector('[data-side-panel-toggle]').click()")
      await inPage('the panel', "!!document.querySelector('[data-side-panel]')")
      await type(url)
      const h = await server.hit(`${url} to load in a new guest`, (h) => h.path === '/beacon' && h.query.get('hello') === '1' && h.query.get('page') === '/notify' &&
        h.query.get('mode') === new URL(url).search.slice(1), { since })
      return { gid: h.query.get('gid'), said: Object.fromEntries(h.query) }
    }
    /** What the notifier was given whose title ends in `title`. */
    const shownAs = async (title) => (await shell.notifications()).filter((n) => n.summary.endsWith(` — ${title}`))
    /** Long enough for a notification the page made to have reached the notifier. */
    const settle = () => new Promise((resolve) => setTimeout(resolve, 1500))
    const host = new URL(site.origin).host
    const xdotool = spawnSync('xdotool', ['version']).status === 0
    const notificationChecks = 'a site\'s notification in the panel is asked for, shown, limited and refused'
    if (shell?.notifications && xdotool) {
      const notifying = mark()
      await type(`${site.origin}/notify?ask`)
      const notifier = await hello('the page that asks to notify to load', '/notify', notifying)
      record('a site in the panel has the webview\'s own Notification, not yet answered', notifier.said.notification === 'native/default',
        notifier.said.notification)
      await clickGuest()
      const bar = await inPage('the permission bar', "document.querySelector('[data-side-panel-permission]')?.textContent.trim()")
      record('asking to notify shows the bar in the window, naming its site', bar.includes(host), bar)
      await page("document.querySelector('[data-side-panel-permission-allow]').click()")
      const asked = await site.hit('the page to be answered', (h) => h.query.get('gid') === notifier.gid && h.query.has('asked'))
      record('Allow gives the page its permission', asked.query.get('asked') === 'granted', asked.query.get('asked'))
      const notification = await waitFor('the notification to be shown', async () => (await shownAs('Build passed'))[0])
      record('the page\'s notification is shown through the app\'s notifier, its title after the site\'s host',
        notification.summary === `${host} — Build passed`, JSON.stringify(notification))
      record('its markup is escaped', notification.body === 'Checked in the &lt;b&gt;side panel&lt;/b&gt; &amp; &lt;a href="https://example.com"&gt;here&lt;/a&gt;',
        notification.body)
      await shell.click(notification.id)
      await site.hit('the page to hear the click', (h) => h.query.get('gid') === notifier.gid && h.query.get('clicked') === 'Build passed')
      record('clicking it reaches the page', true)

      // A new guest's page of the site knows it may notify before it asks.
      const tagged = await newGuest(`${site.origin}/notify?tag`)
      record('a new page of an allowed site reads "granted" before asking', tagged.said.notification === 'native/granted',
        tagged.said.notification)
      await site.hit('the page to show its second tagged one', (h) => h.query.get('gid') === tagged.gid && h.query.has('tagged'))
      const [first, second] = await waitFor('both tagged ones to be shown', async () => {
        const both = [...(await shownAs('Tagged 1')), ...(await shownAs('Tagged 2'))]
        return both.length === 2 && both
      })
      await waitFor('the first tagged one to be closed', async () => (await shell.closes()).includes(first.id))
      record('a notification with a tag replaces the last with that tag', !(await shell.closes()).includes(second.id),
        `closed ${JSON.stringify(await shell.closes())} of ${first.id}, ${second.id}`)
      await shell.dismiss(second.id)
      await site.hit('the page to hear its notification closed', (h) => h.query.get('gid') === tagged.gid && h.query.get('closed') === 'Tagged 2')
      record('dismissing one closes it for the page', true)

      const burst = await newGuest(`${site.origin}/notify?burst`)
      const burstDone = await site.hit('the page to show its burst', (h) => h.query.get('gid') === burst.gid && h.query.has('burst'))
      await settle()
      const bursts = (await shell.notifications()).filter((n) => / — Burst \d$/.test(n.summary)).map((n) => n.summary.split(' ').pop())
      record('a page that floods shows five, then nothing', bursts.join() === '1,2,3,4,5', `${bursts.join()} (the page: ${burstDone.query.get('burst')}; ${burst.said.notification})`)

      // With the site's Allow removed, a page WebKit already told "granted"
      // still shows nothing: the shell asks again at each one.
      const removing = await newGuest(`${site.origin}/notify`)
      await page(`window.agentrq.sidePanel.removePermission(${JSON.stringify(site.origin)}, 'notifications')`)
      const showing = mark()
      await type(`${site.origin}/notify?show`)
      const removed = await site.hit('the page to try after the removal', (h) => h.query.has('shown'), { since: showing })
      await settle()
      const afterRemoval = await shownAs('After removal')
      record('after its Allow is removed, a page still told "granted" shows nothing',
        removing.said.notification === 'native/granted' && removed.query.get('shown') === 'granted' && afterRemoval.length === 0,
        `told ${removed.query.get('shown')}; notifier given ${JSON.stringify(afterRemoval)}`)

      // A site the person blocks.
      const blocking = await newGuest(`${blocker.origin}/notify?ask`, blocker)
      await clickGuest()
      await inPage('the permission bar', "document.querySelector('[data-side-panel-permission]')?.textContent.includes(" + JSON.stringify(new URL(blocker.origin).host) + ')')
      await page("document.querySelector('[data-side-panel-permission-block]').click()")
      const denied = await blocker.hit('the blocked page to try', (h) => h.query.get('gid') === blocking.gid && h.query.has('tried'))
      await settle()
      const blockedShown = await shownAs('Blocked anyway')
      record('Block tells the page "denied", and it shows nothing', blockedShown.length === 0 &&
        blocker.beacons(blocking.gid).some((h) => h.query.get('asked') === 'denied'), `${denied.query.get('tried')}; ${JSON.stringify(blockedShown)}`)
      // WebKitGTK reads a blocked site as "default" in a new page, seeded or
      // not; asked again, the stored Block answers it with no bar.
      const returning = await newGuest(`${blocker.origin}/notify?ask`, blocker)
      await clickGuest()
      const again = await blocker.hit('the blocked page to be answered again', (h) => h.query.get('gid') === returning.gid && h.query.has('tried'))
      const barAgain = await page("!!document.querySelector('[data-side-panel-permission]')")
      await settle()
      const stillNothing = await shownAs('Blocked anyway')
      record('a new page of a blocked site is answered "denied" with no bar, and shows nothing',
        blocker.beacons(returning.gid).some((h) => h.query.get('asked') === 'denied') && !barAgain && stillNothing.length === 0,
        `${returning.said.notification}, then ${again.query.get('tried')}; bar ${barAgain ? 'shown' : 'not shown'}; ${JSON.stringify(stillNothing)}`)
    } else if (process.env.CI) {
      // A lost package must fail the run, never pass it unchecked.
      record(notificationChecks, false, `CI must have Linux's stand-in notification server (python3-gi) and xdotool: ${shell?.notifications ? '' : 'no notification server; '}${xdotool ? '' : 'no xdotool'}`)
    } else SKIPPED.push(skip(notificationChecks, 'needs Linux\'s stand-in notification server and xdotool'))

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
    const posted = await site.hit('the Extension to post to its page', (h) => h.query.get('gid') === ext.gid && h.query.has('posted'))
    record('what the Extension posts reaches its page', JSON.parse(posted.query.get('posted')).n === 3, posted.query.get('posted'))
    // The Extension opens the panel at an address of its choosing.
    since = mark()
    const shown = `${site.origin}/second`
    await type(`${EXTENSION_PAGE}?show=${encodeURIComponent(shown)}`)
    await hello('the page the Extension opened to load', '/second', since)
    await addressIs(shown)
    record('the Extension can open the panel at an address', true, shown)

    // ── a reply while a task is open ────────────────────────────────────────
    // Leaving a task with messages for one with none opens its description,
    // which once squeezed the conversation until replies arrived out of sight.
    await closePanel()
    const live = await page(`(async () => {
      const call = async (path, body) => (await fetch('/api/v1/workspaces/${ids.workspace}' + path, {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })).json()
      await call('/tasks/${ids.task}/reply', { reply: { text: 'An earlier message.', attachments: [] } })
      const { task } = await call('/tasks', { task: { title: 'Live replies',
        body: 'A long brief.\\n\\n' + '- one more point to read through\\n'.repeat(60), createdBy: 'human', assignee: 'agent' } })
      return task.id
    })()`)
    const router = "document.querySelector('#app').__vue_app__.config.globalProperties.$router"
    await page(`${router}.push(${JSON.stringify(taskPath)}).then(() => true)`)
    await inPage('the task with a message', "document.body.textContent.includes('An earlier message.')")
    await page(`${router}.push('/workspaces/${ids.workspace}/tasks/${live}').then(() => true)`)
    await inPage('the task with none', "document.body.textContent.includes('A long brief.')")
    await page(`fetch('/api/v1/workspaces/${ids.workspace}/tasks/${live}/reply', { method: 'POST',
      headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ reply: { text: 'A live reply.', attachments: [] } }) }).then(() => true)`)
    // Seen, not only in the DOM: what is drawn at the reply's own middle is the reply.
    const atReply = `(() => {
      const bubble = [...document.querySelectorAll('.md-body')].find((el) => el.textContent.includes('A live reply.'));
      if (!bubble) return 'no reply in the page';
      const r = bubble.getBoundingClientRect();
      const hit = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2);
      if (bubble.contains(hit)) return 'the reply';
      return hit ? 'a <' + hit.tagName.toLowerCase() + '> reading ' + JSON.stringify(hit.textContent.trim().slice(0, 40)) : 'nothing, outside the window';
    })()`
    const seen = await inPage('the reply to be drawn', `${atReply} === 'the reply'`).catch(() => false)
    record('a reply posted while a task is open appears in it', seen === true, `at its middle: ${await page(atReply)}`)

    printResults('the side panel, in a real window', [...results, ...SKIPPED])
    return summarize(results, SKIPPED)
  },
  async () => {
    await app?.close()
    await shell?.stop()
    await site?.stop()
    await blocker?.stop()
    await backend?.stop()
    if (dir) await rm(dir, { recursive: true, force: true })
  }
)
