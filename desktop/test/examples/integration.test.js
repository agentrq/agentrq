import { describe, it, expect, vi } from 'vitest'
import { readFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

import { createBroker, permitsWorkspace } from '../../src/main/extensions/broker.js'
import { createHost } from '../../src/main/extensions/host.js'
import { createInstaller } from '../../src/main/extensions/install.js'
import { createSchedules } from '../../src/main/extensions/schedules.js'
import { entriesFor, invokeEntry } from '../../src/main/extensions/surfaces.js'
import { checkCompatibility, parseManifest } from '../../src/main/extensions/manifest.js'
import { describeAsk, toGrant, validateGrant, SCOPE } from '../../../frontend/src/composables/useExtensionGrant.js'
import { menuItemsFor, parseSelection } from '../../../frontend/src/composables/useTaskContextMenu.js'
import { normaliseView } from '../../../frontend/src/composables/useExtensionView.js'
import { useExtensionShortcuts } from '../../../frontend/src/composables/useExtensionShortcuts.js'

/**
 * The three examples, driven end to end: install, grant, load, invoke, uninstall.
 *
 * Unit tests prove each piece behaves. This file exists for the thing they
 * cannot show — that the pieces meet. The manifest, the grant ladder, the
 * broker's allowlist, the registries, the renderer's vocabulary and the
 * schedule reconciler were built in separate tasks against separate tests, and
 * every seam between them is a place where two correct halves disagree.
 *
 * The extensions are the real files from `examples/extensions/`, loaded the way
 * the host loads one. Only the two MCP servers are stubbed, because those are
 * the only things here that would otherwise need a network.
 */

const EXAMPLES = join(dirname(fileURLToPath(import.meta.url)), '../../../examples/extensions')

const read = (name) => readFile(join(EXAMPLES, name, 'agentrq-extension.json'), 'utf8')

/** The tool lists a current backend offers, for the compatibility check. */
const SERVERS = {
  appVersion: '0.5.21',
  workspaceTools: ['createTask', 'updateTaskStatus', 'reply', 'getWorkspace', 'getTask', 'listTasks', 'publishEvent'],
  supervisorTools: ['listWorkspaces', 'listAllTasks', 'createTask', 'createEventTrigger', 'listEvents'],
}

/**
 * Everything an install touches, with the disk and the servers stubbed.
 *
 * The installer, broker, host and reconciler are all the real ones — the point
 * of this harness is that nothing between the manifest and the rendered page is
 * a stand-in.
 */
function harness({ workspaceAnswers = {}, supervisorAnswers = {} } = {}) {
  const wrap = (payload) => ({ content: [{ text: JSON.stringify(payload) }] })
  const calls = []

  const callWorkspace = vi.fn(async ({ workspaceId, tool, args }) => {
    calls.push({ surface: 'workspace', tool, workspaceId, args })
    const answer = workspaceAnswers[tool]
    if (!answer) throw new Error(`the stub workspace server has no ${tool}`)
    return wrap(answer(args))
  })

  const callSupervisor = vi.fn(async ({ tool, args }) => {
    calls.push({ surface: 'supervisor', tool, args })
    const answer = supervisorAnswers[tool]
    if (!answer) throw new Error(`the stub supervisor has no ${tool}`)
    return wrap(answer(args))
  })

  const audit = []
  const broker = createBroker({ callWorkspace, callSupervisor, record: (entry) => audit.push(entry) })

  let installState = { installations: [] }
  const installer = createInstaller({
    fetchSource: async () => ({ ok: false, reason: 'not used: the examples install from a linked folder' }),
    readManifest: (path) => read(path),
    move: async () => {},
    remove: async () => {},
    makeTempDir: async () => '/tmp/unused',
    dirFor: (name) => `/tmp/extensions/${name}`,
    store: {
      read: async () => installState,
      write: async (next) => {
        installState = next
      },
    },
  })

  const logger = { info: vi.fn(), warn: vi.fn() }
  const host = createHost({
    // The real module, imported the way the host imports one.
    load: (installation) => import(join(EXAMPLES, installation.name, 'index.js')),
    readConfig: async (name) => configs[name] ?? {},
    clientFor: (name) => broker.clientFor(name),
    logger,
  })

  const configs = {}

  let scheduleState = {}
  const schedules = createSchedules({
    // As the host, with its own credential — an extension declaring a nightly
    // task should not have to ask for the ability to delete any task on the
    // account, which is what going through the broker here would require.
    call: async (tool, args) => callSupervisor({ tool, args }),
    allows: (name, workspaceId) => permitsWorkspace(broker.grantFor(name), workspaceId),
    store: {
      read: async () => scheduleState,
      write: async (next) => {
        scheduleState = next
      },
    },
    logger,
  })

  /**
   * The whole path a user walks: the catalogue's compatibility check, the grant
   * screen's answer, the installer's record, and the host's load.
   */
  async function install(name, { scope, workspaceId = 'ws1', config = {} } = {}) {
    const parsed = parseManifest(await read(name))
    expect(parsed.ok, parsed.reason).toBe(true)

    const compatibility = checkCompatibility(parsed.manifest, SERVERS)
    expect(compatibility.compatible, compatibility.reasons.join(' ')).toBe(true)

    const ask = describeAsk(parsed.manifest)
    if (ask.level !== 'none') {
      const choice = { scope, workspaces: [workspaceId], workspaceId }
      const valid = validateGrant(ask, choice)
      expect(valid.ok, valid.reason).toBe(true)
      broker.setGrant(name, toGrant(ask, choice))
    }

    configs[name] = config
    // Linked, which is how an example in this repository is installed: the
    // folder is recorded rather than copied.
    const installed = await installer.install({ kind: 'local', path: name }, { linked: true })
    expect(installed.ok, installed.reason).toBe(true)

    const started = await host.start(installed.installation)
    expect(started.ok, started.reason).toBe(true)
    return { manifest: parsed.manifest, registered: started.registered }
  }

  return { install, installer, host, broker, schedules, calls, audit, logger, listInstalled: () => installState.installations }
}

describe('task-stats — the extension that asks for nothing', () => {
  it('installs with no grant at all and still contributes a menu item', async () => {
    const rig = harness()

    const { registered } = await rig.install('task-stats')

    expect(registered).toBe(1)
    // Nothing was granted, because nothing was asked for.
    expect(rig.broker.grantFor('task-stats')).toBeNull()
  })

  it('appears on a task menu after the built-ins, behind a divider', async () => {
    const rig = harness()
    await rig.install('task-stats')

    const entries = rig.host.resolve('ui', {}).filter((entry) => entry.surface === 'task-menu')
    const items = menuItemsFor({ title: 'Ship it' }, entries)

    expect(items[0]).toMatchObject({ key: 'move' })
    expect(items[1]).toMatchObject({ divider: true })
    expect(items[2].label).toBe('Task Stats')
    // Namespaced on the way in, so two extensions may both call theirs "stats".
    expect(parseSelection(items[2].key)).toEqual({ kind: 'extension', owner: 'task-stats', id: 'stats' })
  })

  // The whole path, with only the IPC hop as a function call: the host loads the
  // real module, the main process serialises what it registered, the renderer's
  // own composables build the menu and validate what comes back. This is the
  // check that was missing when the feature shipped with its menu item
  // registered in the main process and read by nobody.
  it('reaches a task menu and back again, end to end', async () => {
    const rig = harness()
    await rig.install('task-stats')
    const task = {
      title: 'Ship it',
      body: 'three words here',
      status: 'ongoing',
      createdAt: new Date(Date.now() - 2 * 3600_000).toISOString(),
      messages: [{ text: 'one two' }],
    }

    // Main process: what crosses the bridge, with every function stripped.
    const rows = entriesFor(rig.host.resolve('ui', task), 'task-menu', task)
    expect(rows).toEqual([
      { owner: 'task-stats', id: 'stats', surface: 'task-menu', label: 'Task Stats', order: 10 },
    ])

    // Renderer: the menu a person sees, and the key they click.
    const items = menuItemsFor(task, rows)
    expect(items.map((item) => item.label ?? '(divider)')).toEqual(['Move Task', '(divider)', 'Task Stats'])
    const parsed = parseSelection(items[2].key)

    // Main process again: run it. Renderer: validate and draw.
    const result = await invokeEntry(rig.host.resolve('ui', task), { ...parsed, surface: 'task-menu' }, task)
    const drawn = normaliseView(result.view)

    expect(drawn.ok, drawn.reason).toBe(true)
    expect(drawn.view.title).toContain('Ship it')
    expect(drawn.view.nodes[0].items.map((row) => `${row.label}=${row.value}`)).toEqual([
      'Age=2 hours',
      'Messages=1',
      'Words=5',
      'Status=ongoing',
    ])
  })

  it('draws a panel the renderer accepts, without touching a server', async () => {
    const rig = harness()
    await rig.install('task-stats')
    const entry = rig.host.resolve('ui', {}).find((e) => e.surface === 'task-menu')

    const view = normaliseView(entry.run({ title: 'Ship it', body: 'two words', status: 'ongoing', messages: [] }))

    expect(view.ok, view.reason).toBe(true)
    expect(rig.calls).toEqual([])
  })
})

describe('standup — the workspace case', () => {
  const answers = {
    listTasks: () => ({
      tasks: [
        { id: 't1', title: 'Ship it', status: 'completed', updatedAt: new Date().toISOString() },
        { id: 't2', title: 'Stuck', status: 'blocked', updatedAt: new Date().toISOString() },
      ],
    }),
    getTask: () => ({ task: { messages: [{ text: 'Asked on Tuesday' }] } }),
  }

  it('reaches the workspace it was granted, and draws what it found', async () => {
    const rig = harness({ workspaceAnswers: answers })
    await rig.install('standup', { scope: SCOPE.workspace, workspaceId: 'ws1' })

    const page = rig.host.resolve('ui', {}).find((entry) => entry.surface === 'page')
    const view = normaliseView(await page.view({ workspaceId: 'ws1', workspaceName: 'Backend' }))

    expect(view.ok, view.reason).toBe(true)
    expect(rig.calls.map((call) => call.tool)).toEqual(['listTasks', 'getTask'])
    // The credential never reaches the extension: the host attached it.
    expect(rig.calls.every((call) => call.workspaceId === 'ws1')).toBe(true)
  })

  // The seam this exists for: the manifest allowlist and the grant are checked
  // per call, and the extension only ever sees a refusal.
  it('is refused a workspace it was not granted, and says so on the page', async () => {
    const rig = harness({ workspaceAnswers: answers })
    await rig.install('standup', { scope: SCOPE.workspace, workspaceId: 'ws1' })

    const page = rig.host.resolve('ui', {}).find((entry) => entry.surface === 'page')
    const view = await page.view({ workspaceId: 'ws2', workspaceName: 'Frontend' })

    expect(view.nodes[0].value).toContain('not granted access to that workspace')
    expect(rig.calls).toEqual([])
    // Refused *and* recorded: a refusal is the interesting half of an audit.
    expect(rig.audit.at(-1)).toMatchObject({ name: 'standup', allowed: false })
  })

  it('binds x-then-s, and the application keeps its own bare letters', async () => {
    const rig = harness({ workspaceAnswers: answers })
    await rig.install('standup', { scope: SCOPE.workspace })

    const invoked = []
    const keyboard = useExtensionShortcuts({
      entries: () => rig.host.resolve('shortcuts', {}),
      onInvoke: (binding) => invoked.push(binding.id),
    })

    // A bare `s` is not ours to take.
    expect(keyboard.handle({ key: 's', target: {} })).toBe(false)
    expect(keyboard.handle({ key: 'x', target: {} })).toBe(true)
    expect(keyboard.handle({ key: 's', target: {} })).toBe(true)
    expect(invoked).toEqual(['open'])
  })
})

describe('digest — the full case', () => {
  const answers = {
    listWorkspaces: () => ({ workspaces: [{ id: 'ws1', name: 'Backend' }] }),
    listAllTasks: () => ({ tasks: [{ workspaceId: 'ws1', title: 'Stuck', status: 'blocked' }] }),
    createTask: (args) => ({ task: { id: 'tk1', ...args } }),
    listEvents: () => ({ events: [] }),
  }

  const install = (rig) =>
    rig.install('digest', { scope: SCOPE.supervisor, config: { workspaceId: 'ws1', cron: '0 6 * * *' } })

  it('contributes three surfaces and one schedule', async () => {
    const rig = harness({ supervisorAnswers: answers })

    await install(rig)

    // Sorted, because the three share an `order` and nothing about their
    // relative sequence across different surfaces is meaningful.
    expect(rig.host.resolve('ui', {}).map((entry) => entry.surface).sort()).toEqual([
      'page',
      'task-menu',
      'workspace-action',
    ])
    expect(rig.host.resolve('schedules', {})).toHaveLength(1)
  })

  it('reads the whole account, because that is what it was granted', async () => {
    const rig = harness({ supervisorAnswers: answers })
    await install(rig)

    const page = rig.host.resolve('ui', {}).find((entry) => entry.surface === 'page')
    const view = normaliseView(await page.view())

    expect(view.ok, view.reason).toBe(true)
    expect(rig.calls.map((call) => call.tool)).toEqual(['listWorkspaces', 'listAllTasks'])
  })

  // The end the whole feature was built for: standing work that runs when the
  // desktop app does not.
  it('creates its scheduled task, leaves it alone, and takes it away', async () => {
    const rig = harness({ supervisorAnswers: answers })
    await install(rig)
    const declared = rig.host.resolve('schedules', {})

    const first = await rig.schedules.reconcile('digest', declared)
    const second = await rig.schedules.reconcile('digest', declared)

    expect(first.created).toEqual(['daily'])
    expect(second).toMatchObject({ created: [], unchanged: ['daily'] })
    expect(rig.calls.filter((call) => call.tool === 'createTask')).toHaveLength(1)
    expect(rig.calls.at(-1).args.cronSchedule).toBe('0 6 * * *')
  })

  it('leaves nothing behind when it is uninstalled', async () => {
    const rig = harness({
      supervisorAnswers: { ...answers, deleteTask: () => ({}) },
    })
    await install(rig)
    await rig.schedules.reconcile('digest', rig.host.resolve('schedules', {}))

    // The order the app takes an extension away in: stop it, remove what it set
    // up, forget the grant, remove the installation.
    rig.host.stop('digest')
    const removed = await rig.schedules.removeOwner('digest')
    rig.broker.revoke('digest')
    await rig.installer.uninstall('digest')

    expect(removed).toMatchObject({ ok: true, removed: ['daily'] })
    expect(rig.calls.at(-1)).toMatchObject({ tool: 'deleteTask', args: { taskId: 'tk1' } })
    expect(rig.host.list()).toEqual([])
    expect(rig.listInstalled()).toEqual([])
    // Every registry it held is empty, whether or not it tidied up itself.
    expect(rig.host.resolve('ui', {})).toEqual([])
    expect(rig.host.resolve('schedules', {})).toEqual([])
  })

  // The bound that replaces a tool allowlist for the reconciler: it acts as the
  // host, so what stops it is the grant on the workspace a schedule names.
  it('cannot put standing work in a workspace it was not granted', async () => {
    const rig = harness({ supervisorAnswers: answers })
    const parsed = parseManifest(await read('digest'))
    rig.broker.setGrant('digest', toGrant(describeAsk(parsed.manifest), { scope: SCOPE.workspace, workspaceId: 'ws1' }))
    const installed = await rig.installer.install({ kind: 'local', path: 'digest' }, { linked: true })
    await rig.host.start({ ...installed.installation })

    const result = await rig.schedules.reconcile('digest', [
      { id: 'daily', value: { kind: 'task', workspaceId: 'ws-elsewhere', title: 'Daily digest', cron: '0 6 * * *' } },
    ])

    expect(result.problems[0].reason).toContain('not granted access to that workspace')
    expect(rig.calls.filter((call) => call.tool === 'createTask')).toEqual([])
  })

  // The rung below the one it asked for is a coherent choice a user can make,
  // and the extension has to survive it rather than crash.
  it('is refused the account when granted only a workspace, and says so', async () => {
    const rig = harness({ supervisorAnswers: answers })
    const parsed = parseManifest(await read('digest'))
    rig.broker.setGrant('digest', toGrant(describeAsk(parsed.manifest), { scope: SCOPE.workspace, workspaceId: 'ws1' }))
    const installed = await rig.installer.install({ kind: 'local', path: 'digest' }, { linked: true })
    await rig.host.start(installed.installation)

    const page = rig.host.resolve('ui', {}).find((entry) => entry.surface === 'page')
    const view = await page.view()

    expect(view.nodes[0].value).toContain('may not call "listWorkspaces"')
    expect(normaliseView(view).ok).toBe(true)
    expect(rig.calls).toEqual([])
  })
})

describe('all three at once', () => {
  it('coexist without either colliding or reordering each other', async () => {
    const rig = harness({
      workspaceAnswers: { listTasks: () => ({ tasks: [] }) },
      supervisorAnswers: { listWorkspaces: () => ({ workspaces: [] }), listAllTasks: () => ({ tasks: [] }) },
    })

    await rig.install('digest', { scope: SCOPE.supervisor, config: { workspaceId: 'ws1' } })
    await rig.install('task-stats')
    await rig.install('standup', { scope: SCOPE.workspace })

    // Ordered by their declared `order`, not by which loaded first.
    expect(rig.host.resolve('ui', {}).map((entry) => `${entry.owner}:${entry.id}`)).toEqual([
      'task-stats:stats',
      'standup:today',
      'digest:context',
      'digest:digest',
      'digest:now',
    ])

    const menu = menuItemsFor(
      { title: 'Ship it', status: 'ongoing' },
      rig.host.resolve('ui', {}).filter((entry) => entry.surface === 'task-menu'),
    )
    // Digest's item is conditional and this task is not completed, so only one
    // extension row appears — which is the point of `when`.
    expect(menu.map((item) => item.label)).toEqual(['Move Task', undefined, 'Task Stats'])
  })

  it('refuses a second extension asking for a key that is taken', async () => {
    const rig = harness({ workspaceAnswers: { listTasks: () => ({ tasks: [] }) } })
    await rig.install('standup', { scope: SCOPE.workspace })

    // The registry names the extension already holding it, because "duplicate
    // id" tells an author nothing they can act on.
    const clash = rig.host.registries.shortcuts.add('other', { id: 'open', key: 's' })

    expect(clash.ok).toBe(false)
    expect(clash.reason).toContain('standup')
  })
})
