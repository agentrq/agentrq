// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * End-to-end check that an extension's contribution actually reaches the screen.
 *
 * This exists because the feature shipped without it. Every piece had unit
 * tests, an integration test drove the host and the composables together, and
 * the menu item still never appeared — because `menuItemsFor(task)` was called
 * with one argument in the two views that show tasks, and nothing in the
 * renderer asked the main process what extensions had registered. Both halves
 * were correct and the path between them did not exist.
 *
 * So the path is what this runs: a real Electron window, the real preload, the
 * real `app://` protocol, the real host loading the real `task-stats` example
 * off disk, and the real IPC handlers. The only stubs are the connection screens
 * that would otherwise take the window, and there is no backend.
 *
 * It then found a second bug that unit tests could not: a task on the board is a
 * **Vue reactive proxy**, and `contextBridge` converts arguments as they enter
 * the preload's world, refusing a Proxy — `An object could not be cloned.` The
 * call rejected, the rejection was caught and turned into an empty list, and an
 * installed, running extension contributed a row that never appeared. Every fake
 * in every unit test passed a plain object, so nothing else could have caught
 * it. The last two checks below are that bug, and they must stay.
 *
 *   npm run build && npx electron scripts/verify-extensions.mjs
 */
import { app, BrowserWindow, ipcMain, net, protocol } from 'electron'
import { readFile, access, readdir } from 'node:fs/promises'
import { constants } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

import { createAppProtocolHandler } from '../src/main/protocol.js'
import { DRAWER_FRAME_URL, FRAME_SANDBOX } from '../../frontend/src/composables/useDrawerFrame.js'
import { drawerCodeUrl } from '../src/main/extensions/drawer-frame.js'
import { createHost } from '../src/main/extensions/host.js'
import { createInstaller } from '../src/main/extensions/install.js'
import { createBroker } from '../src/main/extensions/broker.js'
import { createSchedules } from '../src/main/extensions/schedules.js'
import { createConfigStore } from '../src/main/extensions/config.js'
import { createRuntime } from '../src/main/extensions/runtime.js'
import { readDrawer, readManifest } from '../src/main/extensions/fetch-source.js'
import { serverTools } from '../src/main/extensions/servers.js'
import { entriesFor, invokeEntry } from '../src/main/extensions/surfaces.js'
// The renderer's own modules, imported here rather than from inside the page: a
// production bundle exposes no source paths, and these are plain functions with
// no Vue in them. What the page is for is the bridge hop, which is the part no
// unit test can reach.
//
// Only modules with no relative imports of their own. The frontend writes those
// without a file extension, which Vite resolves and Node does not — so
// `useExtensionPages` cannot be pulled in here, and what it does is covered by
// its own test instead. What this script uniquely proves is the main process
// half: that the surfaces are offered at all.
import { menuItemsFor, parseSelection } from '../../frontend/src/composables/useTaskContextMenu.js'
import { normaliseView } from '../../frontend/src/composables/useExtensionView.js'

const __dirname = dirname(fileURLToPath(import.meta.url))
const RENDERER_ROOT = join(__dirname, '../dist/renderer')
const PRELOAD = join(__dirname, '../dist/preload/index.cjs')
const EXAMPLES = join(__dirname, '../../examples/extensions')
const EXAMPLE = join(EXAMPLES, 'task-stats')

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

/**
 * The extension IPC, mirroring src/main/index.js.
 *
 * Repeated rather than imported because index.js takes over the Electron app
 * lifecycle at import time. What it leans on — the host, and surfaces.js — is
 * the real thing, and so are the channel names the preload reaches them on.
 */
function registerExtensionHandlers(host) {
  ipcMain.handle('agentrq:extensions:entries', (_event, { surface, context } = {}) =>
    entriesFor(host.resolve('ui', context ?? {}), surface ?? '', context ?? {}),
  )
  ipcMain.handle('agentrq:extensions:invoke', async (_event, { target, context } = {}) =>
    invokeEntry(host.resolve('ui', context ?? {}), target ?? {}, context ?? {}),
  )
  ipcMain.handle('agentrq:extensions:state', async () => ({ index: { entries: [] }, installed: [] }))
}

/**
 * Drives the composables the two task views use, in the window.
 *
 * Deliberately not a click on a rendered board: reaching one needs a backend, a
 * session and a workspace. What is being checked is the seam that was missing —
 * the renderer asking the bridge, and what comes back becoming menu rows and a
 * validated view — and these are the exact functions both views call.
 */
const script = `(async () => {
  const bridge = window.agentrq?.extensions;
  if (!bridge) return { error: 'no extensions bridge on window.agentrq' };
  if (typeof bridge.entries !== 'function') return { error: 'the bridge has no entries()' };

  const task = {
    title: 'Ship it',
    body: 'three words here',
    status: 'ongoing',
    createdAt: new Date(Date.now() - 2 * 3600 * 1000).toISOString(),
    messages: [{ text: 'one two' }],
  };

  const rows = await bridge.entries('task-menu', task);
  return { task, rows };
})()`

/**
 * The same request, made with a Vue reactive proxy instead of a plain object.
 *
 * This is what the board actually hands over, and it is the exact shape that
 * used to fail. Kept as a check rather than a comment because the failure was
 * silent: an empty list, no error anywhere, and an extension that looked
 * installed and did nothing.
 */
const proxyScript = `(async () => {
  // A bare Proxy rather than Vue's \`reactive\`, because a production bundle
  // exposes no importable 'vue' — and it is the Proxy itself that is refused,
  // which is what \`reactive\` returns.
  const task = new Proxy({ id: 't1', title: 'Ship it', status: 'ongoing', messages: [] }, {});
  const ask = async (value) => {
    try {
      return { ok: true, count: (await window.agentrq.extensions.entries('task-menu', value)).length };
    } catch (error) {
      return { ok: false, error: String(error) };
    }
  };

  return {
    // Raw, the way the board used to hand it over.
    raw: await ask(task),
    // Flattened, the way useExtensionSurfaces does before it calls the bridge.
    flattened: await ask(JSON.parse(JSON.stringify(task))),
  };
})()`

/** The second half of the round trip, once the page has told us what to run. */
const invokeScript = (target, task) => `window.agentrq.extensions.invoke(${JSON.stringify(target)}, ${JSON.stringify(task)})`

setTimeout(() => {
  console.error('✗ verification timed out')
  app.exit(3)
}, 60000)

/**
 * The real runtime, with its three JSON files held in memory.
 *
 * Nothing is written to the app's own userData: a verification run must not be
 * able to install something into whoever ran it.
 */
function buildRuntime() {
  const memory = () => {
    let value = null
    return { read: async () => value, write: async (next) => { value = next } }
  }

  const installer = createInstaller({
    fetchSource: async () => ({ ok: false, reason: 'this run only installs from a folder' }),
    readManifest,
    move: async () => {},
    remove: async () => {},
    makeTempDir: async () => '/tmp/unused',
    dirFor: (name) => join(EXAMPLE, '..', name),
    store: memory(),
  })

  const broker = createBroker({
    callWorkspace: async () => { throw new Error('no server in this run') },
    callSupervisor: async () => { throw new Error('no server in this run') },
  })

  const configStore = createConfigStore({
    store: memory(),
    vault: { available: () => false, encrypt: (t) => t, decrypt: (t) => t },
  })

  const host = createHost({
    // Byte for byte what src/main/index.js does.
    load: (installation) => import(pathToFileURL(join(installation.dir, 'index.js')).href),
    readConfig: (name) => configStore.resolve(name),
    clientFor: (name) => broker.clientFor(name),
  })

  const schedules = createSchedules({
    call: async () => { throw new Error('no server in this run') },
    store: memory(),
  })

  const runtime = createRuntime({
    installer,
    host,
    broker,
    schedules,
    configStore,
    readManifest,
    readDrawer,
    // The real lists, as index.js passes them. Passing only the version is what
    // refused every extension that wanted any tool at all.
    servers: () => serverTools(app.getVersion()),
  })

  return { runtime, host }
}

app.whenReady().then(async () => {
  // The whole install, exactly as the button does it: read the folder, judge
  // it, write the record, load the module. This is the half the unit tests
  // reach and the earlier version of this script skipped.
  const { runtime, host } = buildRuntime()
  const inspected = await runtime.inspect(EXAMPLE)
  const installed = await runtime.installLocal(EXAMPLE)
  const started = { ok: installed.ok && !installed.loadFailure, reason: installed.reason ?? installed.loadFailure }

  registerExtensionHandlers(host)
  ipcMain.handle('agentrq:connection:get', () => ({ configured: true, serverUrl: 'http://localhost:3999', locked: true }))
  ipcMain.handle('agentrq:update:get', () => ({ status: 'idle', detail: '', version: '', enabled: false }))
  ipcMain.handle('agentrq:theme:set', () => ({ source: 'system', color: '#fafafa' }))
  ipcMain.handle('agentrq:profiles:get', () => ({ activeProfileId: '', profiles: [] }))
  ipcMain.handle('agentrq:notifications:get', () => ({ supported: false, mutedWorkspaces: [] }))

  protocol.handle(
    'app',
    createAppProtocolHandler({
      serverUrl: () => 'http://localhost:3999',
      netFetch: net.fetch,
      fileExists,
      readFile: (pathname) => readFile(join(RENDERER_ROOT, pathname)),
      // The real lookup, against whatever this run installed — so the frame
      // below imports an actual extension's drawer rather than a stand-in.
      drawerFor: async (format) =>
        format === 'verify-escape'
          ? { ok: true, code: ESCAPE_DRAWER }
          : runtime.drawerFor(format),
    }),
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
  const skipped = []
  const record = (name, pass, detail) => results.push({ name, pass, detail })

  // The bug this missed the first time: an example asking for MCP tools was
  // judged against an empty surface and refused every one of them. task-stats
  // asks for nothing, so it installed and this script said everything was fine.
  const { runtime: second } = buildRuntime()
  const wantsTools = await second.inspect(join(EXAMPLES, 'standup'))
  record(
    'an extension that wants workspace tools is judged against the real ones',
    wantsTools.ok === true && wantsTools.compatible === true,
    JSON.stringify(wantsTools.reasons ?? wantsTools.reason ?? ''),
  )
  const wantsAccount = await second.inspect(join(EXAMPLES, 'digest'))
  record(
    'and so is one that wants the account',
    wantsAccount.ok === true && wantsAccount.compatible === true,
    JSON.stringify(wantsAccount.reasons ?? wantsAccount.reason ?? ''),
  )

  record('the folder reads as an extension', inspected.ok === true, String(inspected.reason ?? ''))
  record('and it can run on this version', inspected.compatible === true, JSON.stringify(inspected.reasons ?? []))
  record('installing it from the folder works', installed.ok === true, String(installed.reason ?? ''))
  record('and the module loads', started.ok === true, String(started.reason ?? ''))
  record(
    'the row would say what it contributed',
    JSON.stringify((await runtime.state())[0]?.contributes) ===
      JSON.stringify({ ui: 1, shortcuts: 0, schedules: 0, renderers: 0 }),
    JSON.stringify((await runtime.state())[0] ?? null),
  )

  /**
   * Every method the renderer calls, checked by name.
   *
   * Two were missing at different times — `onChanged` and the three
   * authorisation methods — because an edit to the preload silently did not
   * apply and nothing looked. The renderer then called `undefined()` and the
   * button did nothing at all, which is the quietest failure this bridge has.
   *
   * So the list is written out and compared, rather than two of them being
   * spot-checked.
   */
  const EXPECTED_BRIDGE = [
    'state', 'refresh', 'chooseFolder', 'installLocal', 'uninstall', 'setEnabled',
    'configure', 'supervisor', 'authorize', 'deauthorize', 'onChanged', 'entries', 'invoke',
  ]

  const exposed = await win.webContents.executeJavaScript(
    `Object.entries(window.agentrq?.extensions ?? {}).filter(([, v]) => typeof v === 'function').map(([k]) => k)`,
  )
  const missing = EXPECTED_BRIDGE.filter((name) => !exposed.includes(name))
  record('the bridge exposes every method the renderer calls', missing.length === 0, `missing: ${missing.join(', ')}`)

  // The hop nothing else can check: the page asks, the main process answers.
  const { task, rows, error } = await win.webContents.executeJavaScript(script)
  record('the page can ask what extensions offer', !error, String(error ?? ''))
  record('entries cross the bridge', Array.isArray(rows) && rows.length === 1, JSON.stringify(rows))
  record(
    'nothing callable crosses with them',
    Array.isArray(rows) && rows.every((row) => Object.values(row).every((v) => typeof v !== 'function')),
    JSON.stringify(rows),
  )

  // What both task views do with that answer.
  const menu = menuItemsFor(task, rows ?? [])
  record(
    'the menu reads Move Task, a divider, then Task Stats',
    JSON.stringify(menu.map((i) => i.label ?? '(divider)')) === JSON.stringify(['Move Task', '(divider)', 'Task Stats']),
    JSON.stringify(menu.map((i) => i.label ?? '(divider)')),
  )

  const chosen = menu.find((item) => item.label === 'Task Stats')
  const parsed = chosen ? parseSelection(chosen.key) : {}
  record('the selection parses back to the extension', parsed.owner === 'task-stats', JSON.stringify(parsed))

  // Back over the bridge, from the page, exactly as a click does.
  const invoked = chosen
    ? await win.webContents.executeJavaScript(invokeScript({ ...parsed, surface: 'task-menu' }, task))
    : { ok: false, reason: 'nothing to click' }
  record('invoking it from the page answers with a view', invoked.ok === true, String(invoked.reason ?? ''))

  const drawn = normaliseView(invoked.view ?? null)
  record('the renderer will draw it', drawn.ok === true, String(drawn.reason ?? ''))
  const values = (drawn.view?.nodes?.[0]?.items ?? []).map((r) => `${r.label}=${r.value}`)
  record(
    'and it says what it worked out',
    JSON.stringify(values) === JSON.stringify(['Age=2 hours', 'Words in the description=3', 'Status=ongoing']),
    JSON.stringify(values),
  )

  // The other two surfaces, which shipped registered and unreachable: the host
  // held a page and a header action and nothing in the renderer ever asked.
  const standup = join(EXAMPLES, 'standup')
  const withPages = buildRuntime()
  const installedStandup = await withPages.runtime.installLocal(standup)
  record('standup installs', installedStandup.ok === true, String(installedStandup.reason ?? ''))

  const pages = withPages.runtime.entries('page', {})
  record('it contributes a page', pages.length === 1 && pages[0].owner === 'standup', JSON.stringify(pages))
  record(
    'addressed by owner and id, which is what the route carries',
    pages[0]?.owner === 'standup' && pages[0]?.id === 'today',
    JSON.stringify(pages[0] ?? null),
  )

  const keys = withPages.runtime.entries('shortcut', {})
  record('and a shortcut, read from its own registry', keys.length === 1 && keys[0].key === 's', JSON.stringify(keys))

  const digest = buildRuntime()
  await digest.runtime.installLocal(join(EXAMPLES, 'digest'), { config: { workspaceId: 'ws1' } })
  const headerActions = digest.runtime.entries('workspace-action', { workspaceId: 'ws1' })
  record('a header action is offered for a workspace', headerActions.length === 1, JSON.stringify(headerActions))
  // ── A drawer, in the frame that is supposed to contain it ────────────────
  //
  // The whole security argument is one attribute — `allow-scripts` with no
  // `allow-same-origin` — and an argument is not a test. This runs a drawer in
  // a real frame in a real window and asks it to reach back, which is the only
  // way to know the boundary is where it is claimed to be.
  //
  // The real constants are imported here in Node and injected, rather than
  // imported in the page: `/src/...` resolves only under the dev server, and a
  // check that quietly tests something other than the shipped module is worse
  // than no check. `useDrawerFrame.js` has no dependencies, which is what makes
  // that possible.
  // Drawn twice: the real extension's drawer proves the mechanism carries a
  // five-megabyte bundle, and this one proves the wall is where it is claimed.
  const ESCAPE_DRAWER = [
    'export default function draw(root, source) {',
    '  const p = document.createElement("p");',
    '  p.textContent = "drew: " + source;',
    '  root.appendChild(p);',
    '  try { window.parent.document.title = "ESCAPED"; } catch (error) { root.dataset.blocked = error.name; }',
    '}',
  ].join('\n')

  const sandboxScript = `(async () => {
    try {
      const frame = document.createElement('iframe');
      frame.setAttribute('sandbox', ${JSON.stringify(FRAME_SANDBOX)});
      frame.src = ${JSON.stringify(DRAWER_FRAME_URL)};
      document.body.appendChild(frame);

      const answer = (want) => new Promise((resolve) => {
        const onMessage = (event) => {
          if (event.source !== frame.contentWindow) return;
          const data = event.data;
          if (!data || !want.includes(data.type)) return;
          window.removeEventListener('message', onMessage);
          resolve(data);
        };
        window.addEventListener('message', onMessage);
        setTimeout(() => resolve({ type: 'timeout' }), 5000);
      });

      const ready = await answer(['ready']);
      if (ready.type !== 'ready') return { ok: false, reason: 'the frame never became ready' };

      const before = document.title;
      frame.contentWindow.postMessage(
        { type: 'draw', url: ${JSON.stringify(drawerCodeUrl('verify-escape'))}, source: 'hello', theme: 'light' },
        '*',
      );
      const drawn = await answer(['drawn', 'failed']);

      // The wall seen from this side: an opaque origin cannot be read into.
      let parentCanRead = false;
      try { parentCanRead = Boolean(frame.contentWindow.document.body); } catch { parentCanRead = false; }

      frame.remove();
      return {
        ok: true,
        drawn: drawn.type,
        reason: drawn.reason ?? '',
        height: drawn.height ?? 0,
        titleUnchanged: document.title === before,
        parentCanRead,
      };
    } catch (error) {
      return { ok: false, reason: String(error) };
    }
  })()`

  // The real extension, from the checkout beside this one. Its drawer is
  // mermaid bundled — five megabytes — which is the case the URL exists for.
  const MERMAID_EXT = join(__dirname, '../../../extensions/mermaid-agentrq')
  const mermaidInstalled = await runtime.installLocal(MERMAID_EXT).catch((error) => ({ ok: false, reason: String(error) }))
  record('the mermaid extension installs from its folder', mermaidInstalled.ok === true, String(mermaidInstalled.reason ?? ''))

  const declared = await runtime.hasDrawer('mermaid')
  record('and declares a drawer the host can find', declared.ok === true, JSON.stringify(declared))

  const read = await runtime.drawerFor('mermaid')
  record(
    'whose bundle is on disk and readable',
    read.ok === true && (read.code?.length ?? 0) > 1_000_000,
    read.ok ? `${(read.code.length / 1048576).toFixed(1)} MB` : String(read.reason),
  )

  const sandbox = await win.webContents.executeJavaScript(sandboxScript)

  record('a drawer runs in a sandboxed frame and draws', sandbox.drawn === 'drawn', JSON.stringify(sandbox))
  record('and it reported a height to size the frame by', (sandbox.height ?? 0) > 0, String(sandbox.height ?? 0))
  // The line that must never move.
  record('the sandbox grants scripts and nothing else', FRAME_SANDBOX === 'allow-scripts', FRAME_SANDBOX)
  // What the sandbox is actually for.
  record(
    'a drawer reaching for the page it is drawn in cannot touch it',
    sandbox.titleUnchanged === true,
    `document.title unchanged: ${sandbox.titleUnchanged}`,
  )
  record(
    'and the page cannot read into the frame, because the origin is opaque',
    sandbox.parentCanRead === false,
    `parent could read the frame document: ${sandbox.parentCanRead}`,
  )

  // The real thing: mermaid, bundled by the extension, imported over the URL,
  // drawing an actual diagram. A toy drawer proves the mechanism; this proves
  // it carries what anybody would actually put through it.
  const realDraw = await win.webContents.executeJavaScript(`(async () => {
    try {
      const frame = document.createElement('iframe');
      frame.setAttribute('sandbox', ${JSON.stringify(FRAME_SANDBOX)});
      frame.src = ${JSON.stringify(DRAWER_FRAME_URL)};
      document.body.appendChild(frame);

      const answer = (want) => new Promise((resolve) => {
        const onMessage = (event) => {
          if (event.source !== frame.contentWindow) return;
          if (!event.data || !want.includes(event.data.type)) return;
          window.removeEventListener('message', onMessage);
          resolve(event.data);
        };
        window.addEventListener('message', onMessage);
        setTimeout(() => resolve({ type: 'timeout' }), 20000);
      });

      await answer(['ready']);
      frame.contentWindow.postMessage(
        {
          type: 'draw',
          url: ${JSON.stringify(drawerCodeUrl('mermaid'))},
          source: 'graph TD;\\n  A["<img src=x onerror=alert(1)>"]-->B;',
          theme: 'light',
        },
        '*',
      );
      const drawn = await answer(['drawn', 'failed']);
      frame.remove();
      return { type: drawn.type, reason: drawn.reason ?? '', height: drawn.height ?? 0 };
    } catch (error) {
      return { type: 'threw', reason: String(error) };
    }
  })()`)

  record(
    'the real mermaid drawer imports and draws a diagram',
    realDraw.type === 'drawn',
    JSON.stringify(realDraw),
  )
  record(
    'and the diagram has a size, so it was actually laid out',
    (realDraw.height ?? 0) > 20,
    `height: ${realDraw.height}`,
  )

  // The drawing and the sanitising both happen in the page, because DOMPurify
  // needs a DOM and mermaid needs a browser that can measure text. That is the
  // whole reason this check is here rather than in a unit test.

  /**
   * The renderer path, end to end, in the page.
   *
   * This is the seam that shipped broken: `MarkdownBody` consulted a list of
   * claimed languages that was initialised empty and never filled, so every
   * message said "nothing to do" and no fence reached an extension. Every unit
   * test passed because each supplied the list directly — none asked where it
   * came from, which was the only question that mattered.
   */
  const pipeline = await win.webContents.executeJavaScript(`(async () => {
    try {
      const [{ mayHaveBlocks, splitFences }, { normaliseView }, renderers] = await Promise.all([
        import('/src/utils/markdownBlocks.js'),
        import('/src/composables/useExtensionView.js'),
        import('/src/composables/useExtensionRenderers.js'),
      ]);

      // The bridge the real composable talks to, answering as the main process does.
      window.agentrq = { extensions: {
        entries: async () => [{ owner: 'mermaid', id: 'mermaid', language: 'mermaid', order: 100 }],
        invoke: async (target, context) => ({
          ok: true,
          view: { nodes: [{ type: 'diagram', format: 'mermaid', source: context.source }] },
        }),
      } };

      const body = renderers.useExtensionRenderers({ workspaceId: 'ws1' });
      await body.load();

      const text = '\`\`\`mermaid\\ngraph TD;\\n    A-->B;\\n\`\`\`';
      const claimed = body.languages.value;
      if (!mayHaveBlocks(text, claimed)) return { ok: false, step: 'nothing claimed', claimed };

      const [block] = splitFences(text, claimed);
      if (block?.type !== 'block') return { ok: false, step: 'fence not split', claimed };

      const answer = await body.render('mermaid', block.source, { workspaceId: 'ws1' });
      if (!answer.ok) return { ok: false, step: 'extension refused', reason: answer.reason, claimed };

      const view = normaliseView(answer.view);
      return { ok: view.ok, step: 'drawn', node: view.view?.nodes?.[0], claimed };
    } catch (error) {
      return { ok: false, step: 'threw', reason: String(error), unavailable: true };
    }
  })()`)

  if (pipeline.unavailable) {
    // A production bundle exposes no source paths, so this one check only runs
    // against a dev build. Skipped rather than failed: a check that is red for
    // everybody every time is one people learn to ignore, which is worse than
    // one that says plainly when it did not run.
    //
    //   npm run dev   # in another terminal, then re-run this
    skipped.push('the renderer path (needs a dev build: npm run dev)')
  } else {
    record(
      'a message asks which languages are claimed',
      JSON.stringify(pipeline.claimed) === '["mermaid"]',
      JSON.stringify(pipeline.claimed),
    )
    record('and the fence reaches the extension and comes back drawable', pipeline.ok === true, `${pipeline.step}: ${pipeline.reason ?? ''}`)
    record(
      'as a diagram carrying the source that was written',
      pipeline.node?.type === 'diagram' && pipeline.node?.source === 'graph TD;\n    A-->B;',
      JSON.stringify(pipeline.node),
    )
  }

  // The regression check, in two halves. The first states the constraint that
  // caused the bug; the second is the fix, and it is only meaningful because
  // the first fails.
  const proxy = await win.webContents.executeJavaScript(proxyScript)
  record(
    'a raw reactive task is still refused by the bridge',
    proxy.raw.ok === false && String(proxy.raw.error).includes('could not be cloned'),
    JSON.stringify(proxy.raw),
  )
  record(
    'and flattening it the way the renderer does gets it across',
    proxy.flattened.ok === true && proxy.flattened.count === 1,
    JSON.stringify(proxy.flattened),
  )

  for (const { name, pass, detail } of results) {
    console.log(`${pass ? '✓' : '✗'} ${name}${pass ? '' : ` — ${detail}`}`)
  }

  for (const name of skipped) console.log(`- ${name} — skipped`)

  const failed = results.filter((r) => !r.pass).length
  console.log(failed === 0 ? '\nAll checks passed.' : `\n${failed} check(s) failed.`)
  app.exit(failed === 0 ? 0 : 1)
})
