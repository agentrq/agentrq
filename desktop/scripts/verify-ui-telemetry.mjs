// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * End-to-end check that the interface reports what people do with it.
 *
 * The unit tests prove each link — the allowlist, the entity-to-row mapping,
 * the workspace resolution — and the Go tests prove that a reported action
 * becomes a stored row. What none of them can prove is the part that only
 * exists in a browser: that pressing a key, copying something or searching
 * actually causes a report, attributed to the right workspace, exactly once.
 *
 * So this drives the real interface against a real backend and watches what the
 * page reports.
 *
 * ## Why it watches the page rather than the database
 *
 * It read the telemetry table at first, and every check failed while the
 * feature worked. Three delays sit between a click and a row: `recordTelemetry`
 * sends with `keepalive`, which this renderer defers until the document
 * unloads; the ingest route allows five reports a second and drops the rest
 * with a 429, which a script trips and a person never does; and the controller
 * batches rows on a five-second timer. Measuring through all three measures the
 * browser's send schedule, not the application.
 *
 * The seam that matters here is what the interface *decides to report*, so that
 * is what this observes — the fetch the page makes, captured in the page. That
 * a report becomes a row is the backend's contract, and is covered by
 * `TestRecordTelemetryAcceptsEachUIAction` and
 * `TestUIActionsPersistWithDistinctIDs`. One row is still read back at the end,
 * as a smoke check that the two halves are really connected.
 *
 * Needs a scratch backend, never your dev instance:
 *   AGENTRQ_SERVER_URL=http://localhost:3997 \
 *   AGENTRQ_QA_DB=/path/to/qa.db \
 *   AGENTRQ_QA_WORKSPACE=<id> npx electron scripts/verify-ui-telemetry.mjs
 */
import { app, BrowserWindow, ipcMain, net, protocol, session } from 'electron'
import { readFile, access } from 'node:fs/promises'
import { constants } from 'node:fs'
import { execFileSync } from 'node:child_process'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

import { createAppProtocolHandler } from '../src/main/protocol.js'

const __dirname = dirname(fileURLToPath(import.meta.url))
const RENDERER_ROOT = join(__dirname, '../dist/renderer')
const PRELOAD = join(__dirname, '../dist/preload/index.cjs')
const serverUrl = process.env.AGENTRQ_SERVER_URL ?? 'http://localhost:3997'
const dbPath = process.env.AGENTRQ_QA_DB ?? ''
const workspaceId = process.env.AGENTRQ_QA_WORKSPACE ?? ''
const rootToken = process.env.AGENTRQ_ROOT_TOKEN ?? 'qa-root-token'

// The stored action ids, from backend/internal/data/model/model.go. Written out
// rather than imported because they are Go constants; the numbers are what the
// database actually holds, so reading them back is the point.
const ACTION = {
  uiShortcutUse: 22,
  uiSearch: 23,
  uiSearchOpen: 24,
  uiCopyLink: 25,
  uiCopyMarkdown: 26,
  uiTrajectoryView: 27,
}

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

/** Counts per action, straight out of the telemetry table. */
function telemetryCounts() {
  const out = execFileSync('sqlite3', [dbPath, 'SELECT action, COUNT(*) FROM telemetries GROUP BY action;'], {
    encoding: 'utf8',
  })
  const counts = {}
  for (const line of out.trim().split('\n').filter(Boolean)) {
    const [action, count] = line.split('|')
    counts[Number(action)] = Number(count)
  }
  return counts
}

/**
 * Long enough for the server to write what it was told.
 *
 * The telemetry controller batches and flushes on a 5s timer, so a report is
 * accepted by the API well before it is a row. Reading the table sooner finds
 * nothing and says the feature is broken when it is only in flight.
 */
/**
 * Wait until the server has stored what the page reported, or give up.
 *
 * Polling rather than sleeping, because three separate delays sit between a
 * click and a row and only the last is predictable: `recordTelemetry` sends
 * with `keepalive`, which the browser may defer; the ingest route is rate
 * limited, so a burst earns a 429 and is dropped; and the telemetry controller
 * batches on a 5s timer. A fixed sleep either flakes or is slower than it needs
 * to be, and a fixed sleep that is too short reports a working feature as
 * broken — which is exactly what it did while this script was being written.
 *
 * @param {(counts: Record<number, number>) => boolean} satisfied
 * @param {number} [timeoutMs]
 */
async function waitFor(satisfied, timeoutMs = 30000) {
  const deadline = Date.now() + timeoutMs
  for (;;) {
    const counts = telemetryCounts()
    if (satisfied(counts) || Date.now() > deadline) return counts
    await new Promise((r) => setTimeout(r, 500))
  }
}

/**
 * A gap between actions.
 *
 * The ingest route allows a burst of five reports a second. A person clicking
 * never approaches that; a script driving six features does, and the reports it
 * loses to a 429 look exactly like a feature that never reported.
 */
const pace = () => new Promise((r) => setTimeout(r, 1500))

setTimeout(() => {
  console.error('✗ verification timed out')
  app.exit(3)
}, 180000)

app.whenReady().then(async () => {
  if (!dbPath || !workspaceId) {
    console.error('✗ AGENTRQ_QA_DB and AGENTRQ_QA_WORKSPACE are required')
    app.exit(2)
    return
  }

  ipcMain.handle('agentrq:connection:get', () => ({ configured: true, serverUrl, locked: true }))
  ipcMain.handle('agentrq:update:get', () => ({ status: 'idle', detail: '', version: '', enabled: false }))
  ipcMain.handle('agentrq:theme:set', () => ({ source: 'system', color: '#fafafa' }))
  ipcMain.handle('agentrq:profiles:get', () => ({ activeProfileId: '', profiles: [] }))
  ipcMain.handle('agentrq:notifications:get', () => ({ supported: false, mutedWorkspaces: [] }))
  ipcMain.handle('agentrq:clipboard:write', () => true)

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

  // Sign in the way the app does, so the cookie lands in this window's jar and
  // every report below is made as a real session.
  await win.webContents.executeJavaScript(`
    fetch('/api/v1/auth/root/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ rootToken: ${JSON.stringify(rootToken)} }),
    }).then((r) => r.status)
  `)

  const results = []
  const record = (name, pass, detail) => results.push({ name, pass, detail })

  const signedIn = await win.webContents.executeJavaScript(
    "fetch('/api/v1/auth/user').then((r) => r.status)"
  )
  record('the QA session is signed in', signedIn === 200, 'GET /auth/user -> ' + signedIn)

  const before = telemetryCounts()
  const gained = (counts, action) => (counts[action] ?? 0) - (before[action] ?? 0)

  // Land on the task, which is where most of these actions live.
  await win.loadURL(`app://agentrq/workspaces/${workspaceId}/board`)
  await new Promise((r) => setTimeout(r, 2500))

  const taskId = await win.webContents.executeJavaScript(`
    fetch('/api/v1/workspaces/${workspaceId}/tasks?limit=1')
      .then((r) => r.json())
      .then((d) => d.tasks?.[0]?.id ?? '')
  `)
  record('the QA task is reachable', Boolean(taskId), taskId || 'none found')

  const landed = await win.webContents.executeJavaScript('location.pathname')
  record('the board rendered rather than bouncing to login', !landed.endsWith('/login'), 'at ' + landed)

  await win.loadURL(`app://agentrq/workspaces/${workspaceId}/tasks/${taskId}`)
  await new Promise((r) => setTimeout(r, 2500))

  // Watch what the page reports, installed here because a full page load
  // replaces the document and every navigation before this one would have
  // taken the capture with it.
  await win.webContents.executeJavaScript(`
    window.__reports = [];
    const real = window.fetch;
    window.fetch = (input, init) => {
      const url = typeof input === 'string' ? input : input?.url ?? '';
      if (url.includes('/telemetry') && init?.body) window.__reports.push(JSON.parse(init.body));
      return real(input, init);
    };
    true
  `)

  // Every action first, then one read.
  //
  // `recordTelemetry` sends with `keepalive`, and Chromium is entitled to hold
  // such a request until the page goes away — which it does here, so a report
  // made on a click may not reach the server until the window next navigates.
  // Reading after each action therefore measures the browser's send schedule
  // rather than whether the feature works. Doing the work, then forcing a
  // navigation, then reading once, measures the thing that matters.

  // 1. A keyboard shortcut. Dispatched as a real keydown, so it travels the
  //    same path a keypress does — and `t` also switches to the trajectory,
  //    which is a second metric from one press.
  await win.webContents.executeJavaScript(`
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 't', bubbles: true }));
    true
  `)
  await pace()

  // The description starts collapsed, and the markdown — with the copy controls
  // beside it — only exists once it is expanded, which is what a person does
  // before copying anything.
  const expanded = await win.webContents.executeJavaScript(`
    (async () => {
      const collapsed = document.querySelector('.truncate.cursor-pointer');
      if (!collapsed) return 'no collapsed description';
      collapsed.click();
      await new Promise((r) => setTimeout(r, 400));
      return document.querySelectorAll('.md-body').length + ' rendered bodies';
    })()
  `)
  record('the description expands to rendered markdown', /[1-9]\d* rendered/.test(expanded), String(expanded))

  // 2. Copying a task body as raw markdown.
  const copiedMarkdown = await win.webContents.executeJavaScript(`
    (async () => {
      const button = document.querySelector('button[title="Copy raw text"]');
      if (!button) return 'no copy control';
      button.click();
      await new Promise((r) => setTimeout(r, 300));
      return 'clicked';
    })()
  `)
  record('the raw-markdown copy control was reachable', copiedMarkdown === 'clicked', String(copiedMarkdown))
  await pace()

  // 3. The copy button beside a link in the rendered body.
  const copiedLink = await win.webContents.executeJavaScript(`
    (() => {
      const button = document.querySelector('.md-copy-link');
      if (!button) return 'no copy-link button';
      button.click();
      return 'clicked';
    })()
  `)
  record('the link copy button was reachable', copiedLink === 'clicked', String(copiedLink))
  await pace()

  // 4. The finder: one search however much is typed, then opening a result.
  //    One modifier only — `matchShortcut` requires the other to be up, so
  //    Cmd+Ctrl+K deliberately matches nothing.
  await win.webContents.executeJavaScript(`
    window.dispatchEvent(new KeyboardEvent('keydown', {
      key: 'k', ${process.platform === 'darwin' ? 'metaKey' : 'ctrlKey'}: true, bubbles: true,
    }));
    true
  `)
  await new Promise((r) => setTimeout(r, 800))

  const finder = await win.webContents.executeJavaScript(`
    (async () => {
      const input = document.querySelector('[role=dialog][aria-label="Find task"] input');
      if (!input) return 'no finder';
      // Six keystrokes, which must be one search.
      for (const ch of 'Teleme') {
        input.value += ch;
        input.dispatchEvent(new Event('input', { bubbles: true }));
        await new Promise((r) => setTimeout(r, 120));
      }
      await new Promise((r) => setTimeout(r, 700));
      const row = document.querySelector('[role=dialog][aria-label="Find task"] ul button');
      if (!row) return 'no result row';
      row.click();
      return 'opened';
    })()
  `)
  record('the finder searched and opened a result', finder === 'opened', String(finder))
  await pace()

  // Read before navigating anywhere: a full page load replaces the document,
  // and with it the record of what this one reported. Opening a search result
  // is a router push, which leaves it intact.
  const reports = await win.webContents.executeJavaScript('window.__reports ?? []')
  const reported = (action) => reports.filter((r) => r.action === action)
  const countOf = (action) => reported(action).length

  record(
    'pressing a shortcut is reported',
    countOf('ui_shortcut_use') >= 1,
    countOf('ui_shortcut_use') + ' reports'
  )
  record(
    'one keypress is one report, not one per listener',
    countOf('ui_shortcut_use') === 2,
    'two presses -> ' + countOf('ui_shortcut_use') + ' reports'
  )
  record(
    'switching to the trajectory is reported separately',
    countOf('ui_trajectory_view') === 1,
    countOf('ui_trajectory_view') + ' reports'
  )
  record(
    'copying raw markdown is reported',
    countOf('ui_copy_markdown') >= 1,
    countOf('ui_copy_markdown') + ' reports'
  )
  record(
    'copying a link is reported',
    countOf('ui_copy_link') >= 1,
    countOf('ui_copy_link') + ' reports'
  )
  record(
    'a search is reported once, not once per keystroke',
    countOf('ui_search') === 1,
    countOf('ui_search') + ' reports for 6 keystrokes'
  )
  record(
    'opening a search result is reported',
    countOf('ui_search_open') === 1,
    countOf('ui_search_open') + ' reports'
  )

  record(
    'every report names the workspace it happened in',
    reports.length > 0 && reports.every((r) => r.workspaceId === workspaceId),
    reports.length + ' reports, all for ' + workspaceId
  )

  // 5. The attribution rule: no workspace in context, no report at all.
  await win.loadURL('app://agentrq/tasks/all')
  await new Promise((r) => setTimeout(r, 2000))
  // The document was replaced, so the capture goes back on and starts empty —
  // which is the point: nothing pressed here should be reported.
  await win.webContents.executeJavaScript(`
    window.__reports = [];
    const real = window.fetch;
    window.fetch = (input, init) => {
      const url = typeof input === 'string' ? input : input?.url ?? '';
      if (url.includes('/telemetry') && init?.body) window.__reports.push(JSON.parse(init.body));
      return real(input, init);
    };
    true
  `)
  const helpOpened = await win.webContents.executeJavaScript(`
    (async () => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: '?', bubbles: true }));
      await new Promise((r) => setTimeout(r, 600));
      return Boolean(document.querySelector('[role=dialog][aria-label="Keyboard shortcuts"]'));
    })()
  `)
  record('the shortcut still works off-workspace', helpOpened === true, 'help sheet opened: ' + helpOpened)

  const offWorkspaceReports = await win.webContents.executeJavaScript('(window.__reports ?? []).length')
  record(
    'a shortcut with no workspace in context is dropped, not misattributed',
    offWorkspaceReports === 0,
    offWorkspaceReports + ' reports made from a page with no workspace'
  )

  // The two halves, joined: reports made in the page become rows in the table.
  //
  // Closing the window is what forces the issue. The reports were sent with
  // `keepalive`, whose whole purpose is to outlive the document, and this
  // renderer holds them until there is no document left to outlive.
  win.destroy()
  const stored = await waitFor((c) => Object.keys(c).some((a) => Number(a) >= ACTION.uiShortcutUse))
  const uiRows = Object.entries(stored)
    .filter(([action]) => Number(action) >= ACTION.uiShortcutUse)
    .map(([action, count]) => action + '×' + count)
  record('reports reach the telemetry table', uiRows.length > 0, uiRows.join(', ') || 'no rows')

  const foreign = execFileSync(
    'sqlite3',
    [dbPath, `SELECT COUNT(*) FROM telemetries WHERE action >= 22 AND workspace_id NOT IN (SELECT id FROM workspaces);`],
    { encoding: 'utf8' }
  ).trim()
  record('every stored row is attributed to a real workspace', foreign === '0', foreign + ' orphaned rows')

  console.log('\n─── interface telemetry, end to end ───')
  for (const r of results) console.log(`${r.pass ? '✓' : '✗'} ${r.name} — ${r.detail}`)
  const failed = results.filter((r) => !r.pass)
  console.log(failed.length === 0 ? '\n✓ all checks passed' : `\n✗ ${failed.length} check(s) failed`)
  app.exit(failed.length === 0 ? 0 : 1)
})
