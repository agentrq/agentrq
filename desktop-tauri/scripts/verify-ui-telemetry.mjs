// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * End-to-end check that the interface reports what people do with it, on the
 * Tauri build: the Tauri twin of desktop/scripts/verify-ui-telemetry.mjs.
 *
 * The unit tests prove each link: the allowlist, the entity-to-row mapping,
 * the workspace resolution. The Go tests prove that a reported action becomes
 * a stored row. What none of them can prove is the part that only exists in a
 * webview: that pressing a key, copying something or searching actually causes
 * a report, attributed to the right workspace, exactly once.
 *
 * So this drives the real interface against a scratch backend and watches what
 * the page reports: the fetch it makes, captured in the page. Why not the
 * database: three delays sit between a click and a row (a `keepalive` send the
 * webview may hold until the document unloads, the ingest route's rate limit,
 * and the controller's five-second batch), and measuring through them measures
 * the send schedule, not the application. That a report becomes a row is the
 * backend's contract, covered by its own tests.
 *
 * Differences from the Electron script, all of them in how it waits: the
 * scratch backend has the rate limit off, so no action is paced by a sleep;
 * every action is followed by a wait for what it must cause, and a check that
 * something happened only once is read after the action's own visible result
 * (the finder's row, the help sheet), which comes after the report.
 *
 *   npm run verify:ui-telemetry
 */
import { launchApp } from './lib/app.mjs'
import { startBackend } from './lib/backend.mjs'
import { printResults, runVerify, skip, summarize } from './lib/report.mjs'

let backend
let app

const results = []
const record = (name, pass, detail) => results.push({ name, pass, detail })

/** Capture what the page reports. A load replaces the document, so this goes on after each. */
const CAPTURE = `(() => {
  window.__reports = [];
  const real = window.fetch;
  window.fetch = (input, init) => {
    const url = typeof input === 'string' ? input : input?.url ?? '';
    if (url.includes('/telemetry') && init?.body) window.__reports.push(JSON.parse(init.body));
    return real(input, init);
  };
  return true;
})()`

/** What the table holds is the backend's, and this script has no way into its database. */
const STORED = [
  skip('reports reach the telemetry table', 'the Electron script reads SQLite, which the scratch backend keeps to itself; the backend\'s own tests cover it'),
  skip('every stored row is attributed to a real workspace', 'read from the same table'),
]

await runVerify(
  async () => {
    backend = await startBackend()
    app = await launchApp({ serverUrl: backend.url })
    const { session } = app
    const page = (code) => session.evaluate(code)
    const inPage = (what, code) => session.waitForPage(what, code)
    const reported = (action) => `window.__reports.filter((r) => r.action === ${JSON.stringify(action)}).length`
    const countOf = (action) => page(reported(action))
    /** Wait for `count` reports of an action; whatever there is if it never comes. */
    const reportOf = async (action, count = 1) => {
      await inPage(`${count} report(s) of ${action}`, `${reported(action)} >= ${count}`).catch(() => {})
      return countOf(action)
    }
    /** Load `path` and put the capture on the new document. */
    const load = async (path, ready) => {
      await page(`(window.__verifyOld = true, location.assign(${JSON.stringify(path)}))`)
      await inPage(`${path} to load`, `!window.__verifyOld && document.readyState === 'complete' && (${ready})`)
      await page(CAPTURE)
    }
    const key = (init) => page(`(window.dispatchEvent(new KeyboardEvent('keydown', Object.assign({ bubbles: true }, ${JSON.stringify(init)}))), true)`)

    await inPage('the login screen', "location.pathname === '/login'")
    const { workspaceId, taskId } = await page(`(async () => {
      const call = async (path, body) => {
        const res = await fetch('/api/v1' + path, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body),
        });
        if (!res.ok) {
          const text = await res.text().catch(() => '');
          throw new Error('POST ' + path + ' failed with ' + res.status + ': ' + text);
        }
        return res.json();
      };
      const login = await fetch('/api/v1/auth/root/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ rootToken: ${JSON.stringify(backend.rootToken)} }),
      });
      if (!login.ok) {
        throw new Error('login failed with status ' + login.status);
      }
      const wsData = await call('/workspaces', { workspace: { name: 'Telemetry' } });
      if (!wsData?.workspace?.id) {
        throw new Error('create workspace failed: ' + JSON.stringify(wsData));
      }
      const taskData = await call('/workspaces/' + wsData.workspace.id + '/tasks', { task: {
        title: 'Telemetry check', body: 'Some **markdown** with [a link](https://example.com/page).', createdBy: 'human', assignee: 'human' } });
      if (!taskData?.task?.id) {
        throw new Error('create task failed: ' + JSON.stringify(taskData));
      }
      return { workspaceId: wsData.workspace.id, taskId: taskData.task.id, signedIn: login.status };
    })()`)
    record('the QA session is signed in', (await page("fetch('/api/v1/auth/user').then((r) => r.status)")) === 200, 'GET /auth/user')

    await load(`/workspaces/${workspaceId}/tasks/${taskId}`, "document.body.innerText.includes('Telemetry check')")
    record('the QA task rendered rather than bouncing to login', !(await page('location.pathname')).endsWith('/login'), await page('location.pathname'))

    // Every action first, then one read of each. A report is made when the page
    // decides to make it, so each is waited for, never slept on.

    // 1. A keyboard shortcut, as a real keydown. `t` also switches to the
    //    trajectory, which is a second metric from one press.
    await key({ key: 't' })
    await reportOf('ui_trajectory_view')

    // The markdown, with the copy controls beside it, only exists once the
    // description is expanded. A task with no messages opens it on its own.
    const collapsed = await page("!!document.querySelector('.truncate.cursor-pointer')")
    if (collapsed) await page("document.querySelector('.truncate.cursor-pointer').click()")
    await inPage('the description to render', "document.querySelectorAll('.md-body').length > 0").catch(() => {})
    record('the description expands to rendered markdown', (await page("document.querySelectorAll('.md-body').length")) > 0, 'rendered bodies')

    // 2. Copying a task body as raw markdown.
    const rawButton = await page("!!document.querySelector('button[title=\"Copy raw text\"]')")
    if (rawButton) await page("document.querySelector('button[title=\"Copy raw text\"]').click()")
    record('the raw-markdown copy control was reachable', rawButton, String(rawButton))
    await reportOf('ui_copy_markdown')

    // 3. The copy button beside a link in the rendered body.
    const linkButton = await page("!!document.querySelector('.md-copy-link')")
    if (linkButton) await page("document.querySelector('.md-copy-link').click()")
    record('the link copy button was reachable', linkButton, String(linkButton))
    await reportOf('ui_copy_link')

    // 4. The finder: one search however much is typed, then opening a result.
    //    One modifier only: Cmd+Ctrl+K deliberately matches nothing.
    await key({ key: 'k', [process.platform === 'darwin' ? 'metaKey' : 'ctrlKey']: true })
    await inPage('the finder', "!!document.querySelector('[role=dialog][aria-label=\"Find task\"] input')")
    await page(`(async () => {
      const input = document.querySelector('[role=dialog][aria-label="Find task"] input');
      // Six keystrokes, which must be one search.
      for (const ch of 'Teleme') {
        input.value += ch;
        input.dispatchEvent(new Event('input', { bubbles: true }));
      }
      return true;
    })()`)
    // The row is the search's own answer, so the search has been reported by
    // now. The finder loads the recent tasks as it opens, and a search typed
    // before they arrive finds nothing: re-trigger by changing the value (Vue
    // deduplicates same-value input events) until the search result matching
    // 'Telemetry check' arrives, and open it.
    await session.waitForPage(
      'a result row to open',
      `(() => {
        const row = Array.from(document.querySelectorAll('[role=dialog][aria-label="Find task"] ul button')).find(
          (b) => b.innerText.includes('Telemetry check')
        );
        if (row) {
          row.click();
          return true;
        }
        const input = document.querySelector('[role=dialog][aria-label="Find task"] input');
        if (input) {
          input.value = 'Telem';
          input.dispatchEvent(new Event('input', { bubbles: true }));
          input.value = 'Teleme';
          input.dispatchEvent(new Event('input', { bubbles: true }));
        }
        return false;
      })()`
    )
    const opened = true
    record('the finder searched and opened a result', opened, String(opened))
    await reportOf('ui_search_open')

    // Read before navigating anywhere: a full load replaces the document, and
    // with it the record of what this one reported.
    const reports = await page('window.__reports')
    const of = (action) => reports.filter((r) => r.action === action).length
    record('pressing a shortcut is reported', of('ui_shortcut_use') >= 1, of('ui_shortcut_use') + ' reports')
    record('one keypress is one report, not one per listener', of('ui_shortcut_use') === 2, 'two presses -> ' + of('ui_shortcut_use') + ' reports')
    record('switching to the trajectory is reported separately', of('ui_trajectory_view') === 1, of('ui_trajectory_view') + ' reports')
    record('copying raw markdown is reported', of('ui_copy_markdown') >= 1, of('ui_copy_markdown') + ' reports')
    record('copying a link is reported', of('ui_copy_link') >= 1, of('ui_copy_link') + ' reports')
    record('a search is reported once, not once per keystroke', of('ui_search') === 1, of('ui_search') + ' reports for 6 keystrokes')
    record('opening a search result is reported', of('ui_search_open') === 1, of('ui_search_open') + ' reports')
    record(
      'every report names the workspace it happened in',
      reports.length > 0 && reports.every((r) => r.workspaceId === workspaceId),
      reports.length + ' reports, all for ' + workspaceId
    )

    // 5. The attribution rule: no workspace in context, no report at all.
    await load('/tasks/all', "!!document.querySelector('#app')?.children.length")
    await key({ key: '?' })
    await inPage('the help sheet', "!!document.querySelector('[role=dialog][aria-label=\"Keyboard shortcuts\"]')")
    record('the shortcut still works off-workspace', true, 'help sheet opened')
    const off = await page('window.__reports.length')
    record('a shortcut with no workspace in context is dropped, not misattributed', off === 0, off + ' reports made from a page with no workspace')

    printResults('interface telemetry, in a real webview', [...results, ...STORED])
    return summarize(results, STORED)
  },
  async () => {
    await app?.close()
    await backend?.stop()
  }
)
