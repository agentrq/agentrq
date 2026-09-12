import { describe, it, expect, vi } from 'vitest'

import { clampToManifest, createRuntime, describeCandidate } from '../../src/main/extensions/runtime.js'

/**
 * The sequence, and everything that goes wrong when it is out of order.
 *
 * Each piece underneath this has its own tests proving it behaves. What only
 * these can show is the ordering: a grant applied after the load refuses calls
 * the user allowed, and a grant revoked before the reconciler runs leaves a cron
 * task firing every night with nothing left that remembers making it.
 */

const manifest = (over = {}) => ({
  name: 'standup',
  displayName: 'Standup',
  version: '1.0.0',
  license: 'MIT',
  engines: { agentrq: '>=0.5' },
  mcp: { workspace: ['listTasks'] },
  config: [{ key: 'since', type: 'number', label: 'Hours' }],
  shortcuts: [{ key: 's', action: 'open', title: 'Standup' }],
  artifact: { release: 'v1.0.0', asset: 'standup.tgz', sha256: 'a'.repeat(64) },
  ...over,
})

const SERVERS = { appVersion: '1.0.0', workspaceTools: ['listTasks'], supervisorTools: ['listAllTasks'] }

const installation = (over = {}) => ({
  name: 'standup',
  version: '1.0.0',
  enabled: true,
  failures: 0,
  sourceLabel: 'a folder on this machine',
  source: { kind: 'local', path: '/tmp/standup' },
  manifest: manifest(),
  ...over,
})

/**
 * Doubles for the four pieces the runtime sequences, recording the order they
 * were called in — because the order is what is being tested.
 */
function build(over = {}) {
  const order = []
  const track = (label, value) => (...args) => {
    order.push(label)
    return typeof value === 'function' ? value(...args) : value
  }

  let installations = over.installations ?? []
  const installer = {
    list: async () => installations.map((entry) => ({ ...entry })),
    install: vi.fn(track('install', async () => ({ ok: true, installation: installation() }))),
    uninstall: vi.fn(track('uninstall', async (name) => {
      installations = installations.filter((entry) => entry.name !== name)
      return { ok: true }
    })),
    setEnabled: vi.fn(async (name, enabled) => ({ ok: true, installation: installation({ enabled }) })),
    recordFailure: vi.fn(async () => ({ ok: true })),
    ...over.installer,
  }

  const shortcuts = { list: () => over.takenShortcuts ?? [] }
  const loaded = []
  /** Drawer files, keyed by "<dir>/<entry>", so a test can say what is on disk. */
  const files = over.files ?? {}
  const readDrawer = vi.fn(async (dir, entry) => {
    const code = files[`${dir}/${entry}`]
    return code === undefined ? { ok: false, reason: 'that file is not there' } : { ok: true, code }
  })

  const host = {
    registries: { ui: { listFor: () => [] }, shortcuts: { ...shortcuts, listFor: () => [] }, schedules: { listFor: () => [] } },
    list: () => loaded.map((name) => ({ name, version: '1.0.0', failures: 0 })),
    start: vi.fn(track('start', async (entry) => {
      loaded.push(entry.name)
      return { ok: true, registered: 3 }
    })),
    stop: vi.fn(track('stop', (name) => {
      const at = loaded.indexOf(name)
      if (at >= 0) loaded.splice(at, 1)
      return { ok: true }
    })),
    stopAll: vi.fn(),
    resolve: vi.fn(() => over.declaredSchedules ?? []),
    ...over.host,
  }

  const held = new Map()
  const broker = {
    setGrant: vi.fn(track('setGrant', (name, grant) => held.set(name, grant))),
    revoke: vi.fn(track('revoke', (name) => held.delete(name))),
    grantFor: (name) => held.get(name) ?? null,
    ...over.broker,
  }

  const schedules = {
    reconcile: vi.fn(track('reconcile', async () => ({ ok: true, problems: [] }))),
    removeOwner: vi.fn(track('removeOwner', async () => ({ ok: true, removed: [], problems: [] }))),
    ...over.schedules,
  }

  const configStore = {
    save: vi.fn(track('saveConfig', async () => ({ ok: true }))),
    forget: vi.fn(track('forgetConfig', async () => ({ ok: true }))),
    ...over.configStore,
  }

  const logger = { info: vi.fn(), warn: vi.fn() }
  const runtime = createRuntime({
    installer,
    host,
    broker,
    schedules,
    configStore,
    readManifest: over.readManifest ?? (async () => JSON.stringify(manifest())),
    readDrawer,
    servers: () => SERVERS,
    logger,
  })

  return { runtime, installer, host, broker, schedules, configStore, logger, order, held, readDrawer }
}

/**
 * A grant is written once, at install, and then outlives the manifest it was
 * built from — it is persisted across restarts, and a linked installation is a
 * folder somebody can edit at any moment.
 *
 * `broker.js` says its tool check is "against the manifest's `mcp` allowlist".
 * It is really against the grant, so without this the two drift: an extension
 * keeps calling a tool it no longer declares, and no screen says so.
 */
describe('clampToManifest', () => {
  const grant = (over = {}) => ({
    scope: 'selected',
    workspaces: ['ws1'],
    tools: { workspace: ['listTasks', 'getTask'], supervisor: ['listAllTasks'] },
    ...over,
  })

  it('drops a tool the manifest no longer asks for', () => {
    const narrowed = clampToManifest(grant(), { mcp: { workspace: ['getTask'], supervisor: [] } })

    expect(narrowed.tools).toEqual({ workspace: ['getTask'], supervisor: [] })
  })

  // Only a person can widen a grant, at an install screen. A manifest edited to
  // ask for more must not be able to help itself.
  it('never widens one, however much the manifest asks for', () => {
    const widened = clampToManifest(
      grant({ tools: { workspace: ['getTask'], supervisor: [] } }),
      { mcp: { workspace: ['getTask', 'createTask'], supervisor: ['listAllTasks'] } },
    )

    expect(widened.tools).toEqual({ workspace: ['getTask'], supervisor: [] })
  })

  it('leaves the workspaces alone, because that answer came from a person', () => {
    const clamped = clampToManifest(grant(), { mcp: { workspace: ['getTask'] } })

    expect(clamped.workspaces).toEqual(['ws1'])
    expect(clamped.scope).toBe('selected')
  })

  it('empties the grant of an extension that now asks for nothing', () => {
    expect(clampToManifest(grant(), {}).tools).toEqual({ workspace: [], supervisor: [] })
    expect(clampToManifest(grant(), undefined).tools).toEqual({ workspace: [], supervisor: [] })
  })

  it('has nothing to say about no grant at all', () => {
    expect(clampToManifest(null, { mcp: { workspace: ['getTask'] } })).toBeNull()
  })

  it('copes with a grant that recorded no tools', () => {
    expect(clampToManifest({ scope: 'workspace' }, { mcp: { workspace: ['getTask'] } }).tools).toEqual({
      workspace: [],
      supervisor: [],
    })
  })
})

describe('describeCandidate', () => {
  it('reads a folder into everything the grant screen needs', () => {
    const described = describeCandidate(JSON.stringify(manifest()), { ...SERVERS })

    expect(described.ok).toBe(true)
    expect(described.manifest.name).toBe('standup')
    expect(described.compatible).toBe(true)
    expect(described.keys).toEqual([{ key: 's', action: 'open', title: 'Standup' }])
  })

  it('reports a manifest that is not one', () => {
    expect(describeCandidate('{', SERVERS).reason).toBe('This is not valid JSON.')
  })

  // Answered before anybody agrees to anything, so the grant screen cannot
  // offer a grant for something that was never going to load.
  it('says it cannot run here before offering any permissions', () => {
    const described = describeCandidate(JSON.stringify(manifest({ engines: { agentrq: '>=9' } })), SERVERS)

    expect(described.ok).toBe(true)
    expect(described.compatible).toBe(false)
    expect(described.reasons[0]).toContain('Needs AgentRQ >=9')
  })

  it('names the extension already holding a key it wants', () => {
    const described = describeCandidate(JSON.stringify(manifest()), {
      ...SERVERS,
      taken: [{ key: 's', owner: 'linear' }],
    })

    expect(described.shortcutProblems[0]).toContain('linear')
  })
})

describe('inspect', () => {
  it('reads the folder and judges it', async () => {
    const { runtime } = build()

    const inspected = await runtime.inspect('/tmp/standup')

    expect(inspected).toMatchObject({ ok: true, compatible: true, path: '/tmp/standup' })
  })

  it('says what is missing rather than failing obscurely', async () => {
    const { runtime } = build({ readManifest: async () => null })

    expect((await runtime.inspect('/tmp/nope')).reason).toContain('no agentrq-extension.json')
  })

  it('carries a filesystem failure through as a sentence', async () => {
    const { runtime } = build({
      readManifest: async () => {
        throw new Error('EACCES: permission denied')
      },
    })

    expect((await runtime.inspect('/root/x')).reason).toContain('EACCES')
  })

  it('says something even when what was thrown was not an error', async () => {
    const { runtime } = build({
      readManifest: async () => {
        throw 'ENOENT' // eslint-disable-line no-throw-literal -- exactly the case being covered
      },
    })

    expect((await runtime.inspect('/root/x')).reason).toContain('ENOENT')
  })

  it('carries a malformed manifest through with its own reason', async () => {
    const { runtime } = build({ readManifest: async () => '{' })

    expect(await runtime.inspect('/tmp/x')).toEqual({ ok: false, reason: 'This is not valid JSON.' })
  })

  /**
   * Assembled without a tool list.
   *
   * This is correct as a statement about the function and it is also exactly
   * what the real app did: `index.js` passed only `appVersion`, so every
   * extension wanting any tool at all was refused with "the workspace server
   * does not offer …" for tools the server has had all along. A unit test can
   * only answer the question it was asked, and this one was never asked what
   * the app passes — `servers.test.js` is.
   */
  it('judges nothing compatible when it was told nothing about the servers', async () => {
    const runtime = createRuntime({
      installer: { list: async () => [] },
      host: { registries: { shortcuts: { list: () => [] } } },
      broker: {},
      schedules: {},
      configStore: {},
      readManifest: async () => JSON.stringify(manifest()),
    })

    const inspected = await runtime.inspect('/tmp/standup')

    expect(inspected.ok).toBe(true)
    expect(inspected.compatible).toBe(false)
  })

  it('does not count the extension against itself when it is being replaced', async () => {
    // Reinstalling from the same folder must not report that its own shortcut
    // is already taken — by itself.
    const { runtime } = build({ takenShortcuts: [{ key: 's', owner: 'standup' }] })

    expect((await runtime.inspect('/tmp/standup')).shortcutProblems).toEqual([])
  })
})

describe('installLocal', () => {
  it('installs as linked, grants, loads and reconciles — in that order', async () => {
    const { runtime, installer, order } = build()
    const grant = { scope: 'workspace', workspaces: ['ws1'], tools: { workspace: ['listTasks'], supervisor: [] } }

    const result = await runtime.installLocal('/tmp/standup', { grant })

    expect(result.ok).toBe(true)
    expect(installer.install).toHaveBeenCalledWith({ kind: 'local', path: '/tmp/standup' }, { linked: true })
    // The grant is set before the load: `apply` may call MCP, and a grant
    // applied afterwards would refuse exactly what the user allowed.
    expect(order).toEqual(['install', 'setGrant', 'start', 'reconcile'])
  })

  it('refuses one that cannot run here, before installing anything', async () => {
    const { runtime, installer } = build({
      readManifest: async () => JSON.stringify(manifest({ engines: { agentrq: '>=9' } })),
    })

    const result = await runtime.installLocal('/tmp/standup')

    expect(result.reason).toContain('Needs AgentRQ >=9')
    expect(installer.install).not.toHaveBeenCalled()
  })

  it('refuses one whose shortcut is already taken, naming who has it', async () => {
    const { runtime, installer } = build({ takenShortcuts: [{ key: 's', owner: 'linear' }] })

    expect((await runtime.installLocal('/tmp/standup')).reason).toContain('linear')
    expect(installer.install).not.toHaveBeenCalled()
  })

  it('carries a bad folder through untouched', async () => {
    const { runtime } = build({ readManifest: async () => null })
    expect((await runtime.installLocal('/tmp/x')).ok).toBe(false)
  })

  it('loads one whose reconciler answered with nothing to report', async () => {
    const { runtime } = build({ schedules: { reconcile: async () => ({ ok: true }) } })

    expect((await runtime.installLocal('/tmp/standup')).scheduleProblems).toEqual([])
  })

  // The name is an address, so two of them cannot coexist. Stopping first is
  // what keeps the old one from still holding the names the new one claims.
  it('stops the version already running before replacing it', async () => {
    const { runtime, order } = build()
    await runtime.installLocal('/tmp/standup')
    order.length = 0

    await runtime.installLocal('/tmp/standup')

    expect(order[0]).toBe('stop')
  })

  it('saves configuration before loading, so apply sees it', async () => {
    const { runtime, configStore, order } = build()

    await runtime.installLocal('/tmp/standup', { config: { since: 8 } })

    expect(configStore.save).toHaveBeenCalledWith('standup', manifest().config, { since: 8 })
    expect(order.indexOf('saveConfig')).toBeLessThan(order.indexOf('start'))
  })

  // A machine with no secure storage does not get a plaintext credential, and
  // half-configuring is worse than not installing.
  it('backs the install out when a secret could not be stored', async () => {
    const { runtime, installer } = build({
      configStore: { save: async () => ({ ok: false, reason: 'no secure storage available' }) },
    })

    const result = await runtime.installLocal('/tmp/standup', { config: { token: 'x' } })

    expect(result.reason).toContain('no secure storage')
    expect(installer.uninstall).toHaveBeenCalledWith('standup')
  })

  it('reports a failure to write to disk', async () => {
    const { runtime } = build({ installer: { install: async () => ({ ok: false, reason: 'disk full' }) } })

    expect((await runtime.installLocal('/tmp/standup')).reason).toBe('disk full')
  })

  // Installed and broken is a real state, and it has to be distinguishable from
  // "did not install" — the row a person needs in order to remove it is the one
  // a failed install would otherwise never create.
  it('stays installed when the module throws on load, and says why', async () => {
    const { runtime, installer } = build({
      host: { start: async () => ({ ok: false, reason: 'Cannot find module "linear-sdk"' }) },
    })

    const result = await runtime.installLocal('/tmp/standup')

    expect(result.ok).toBe(true)
    expect(result.loadFailure).toContain('linear-sdk')
    expect(installer.recordFailure).toHaveBeenCalledWith('standup')
  })

  it('reconciles only the schedules this extension declared', async () => {
    const { runtime, schedules } = build({
      declaredSchedules: [
        { owner: 'standup', id: 'daily', value: {} },
        { owner: 'digest', id: 'daily', value: {} },
      ],
    })

    await runtime.installLocal('/tmp/standup')

    expect(schedules.reconcile).toHaveBeenCalledWith('standup', [{ owner: 'standup', id: 'daily', value: {} }])
  })

  it('loads even when its schedule was refused, and says so', async () => {
    const { runtime, logger } = build({
      schedules: { reconcile: async () => ({ ok: false, problems: [{ id: 'daily', reason: 'bad cron' }] }) },
    })

    const result = await runtime.installLocal('/tmp/standup')

    expect(result.ok).toBe(true)
    expect(result.scheduleProblems).toEqual([{ id: 'daily', reason: 'bad cron' }])
    expect(logger.warn).toHaveBeenCalledWith(expect.stringContaining('bad cron'))
  })
})

describe('installFromCatalogue', () => {
  /** A catalogue entry, as the main process hands one over. */
  const entry = (over = {}) => ({
    fullName: 'owner/standup',
    ok: true,
    compatible: true,
    reasons: [],
    manifest: manifest(),
    ...over,
  })

  it('installs the release the manifest names, and not as a linked folder', async () => {
    const { runtime, installer, order } = build()
    const grant = { scope: 'workspace', workspaces: ['ws1'], tools: { workspace: ['listTasks'], supervisor: [] } }

    const result = await runtime.installFromCatalogue(entry(), { grant })

    expect(result.ok).toBe(true)
    // Not linked: a downloaded release is a copy this app owns, so removing the
    // extension removes it. Only a folder is the author's own working copy.
    expect(installer.install).toHaveBeenCalledWith({
      kind: 'release',
      name: 'standup',
      repo: 'owner/standup',
      release: 'v1.0.0',
      asset: 'standup.tgz',
      sha256: 'a'.repeat(64),
    })
    expect(order).toEqual(['install', 'setGrant', 'start', 'reconcile'])
  })

  it('refuses an entry with no release to install', async () => {
    const { runtime, installer } = build()
    const noRelease = entry({ manifest: manifest({ artifact: undefined }) })

    expect((await runtime.installFromCatalogue(noRelease)).reason).toContain('no release to install')
    expect(installer.install).not.toHaveBeenCalled()
  })

  it('refuses a manifest that never parsed, in the words it failed with', async () => {
    const { runtime, installer } = build()

    const result = await runtime.installFromCatalogue(entry({ ok: false, reason: '"license" is required.' }))

    expect(result.reason).toBe('"license" is required.')
    expect(installer.install).not.toHaveBeenCalled()
  })

  it('still says something when a broken entry carries no reason', async () => {
    const { runtime } = build()

    expect((await runtime.installFromCatalogue(entry({ ok: false }))).reason).toBe(
      'That manifest could not be read.',
    )
  })

  it('refuses one that cannot run here, before downloading anything', async () => {
    const { runtime, installer } = build()
    const wrong = entry({ compatible: false, reasons: ['Needs AgentRQ >=9.'] })

    expect((await runtime.installFromCatalogue(wrong)).reason).toBe('Needs AgentRQ >=9.')
    expect(installer.install).not.toHaveBeenCalled()
  })

  it('refuses an entry that names no extension', async () => {
    const { runtime } = build()
    const nameless = entry({ manifest: { ...manifest(), name: '' } })

    expect((await runtime.installFromCatalogue(nameless)).reason).toBe(
      'That catalogue entry names no extension.',
    )
  })

  // The name is an address, so a reinstall replaces what is there rather than
  // installing beside it — and the old one must not still be registered when the
  // new one claims the same names.
  it('stops what is already running under that name first', async () => {
    const { runtime, host, order } = build()
    await runtime.installLocal('/tmp/standup')
    order.length = 0

    await runtime.installFromCatalogue(entry())

    expect(host.stop).toHaveBeenCalledWith('standup')
    expect(order.indexOf('stop')).toBeLessThan(order.indexOf('install'))
  })

  it('carries a refused download through as it was given', async () => {
    const { runtime } = build({
      installer: { install: vi.fn(async () => ({ ok: false, reason: 'That checksum did not match.' })) },
    })

    expect((await runtime.installFromCatalogue(entry())).reason).toBe('That checksum did not match.')
  })

  // The manifest inside the downloaded package decides, not the catalogue's copy
  // of it — the digest proves they are the same bytes, and the one unpacked is
  // the one whose settings are saved.
  it('saves settings against the manifest that was actually unpacked', async () => {
    const { runtime, configStore } = build()

    await runtime.installFromCatalogue(entry(), { config: { since: 8 } })

    expect(configStore.save).toHaveBeenCalledWith('standup', manifest().config, { since: 8 })
  })

  it('falls back to the catalogue manifest when the install reports none', async () => {
    const { runtime, configStore } = build({
      installer: {
        install: vi.fn(async () => ({ ok: true, installation: { name: 'standup', enabled: true, failures: 0 } })),
      },
    })

    const result = await runtime.installFromCatalogue(entry(), { config: { since: 8 } })

    expect(result.ok).toBe(true)
    expect(configStore.save).toHaveBeenCalledWith('standup', manifest().config, { since: 8 })
  })
})

describe('drawerFor', () => {
  const drawing = (over = {}) => ({
    ...manifest(),
    provides: { drawers: [{ format: 'mermaid', entry: 'dist/draw.js' }] },
    ...over,
  })

  it('reads the file the extension declared, from its own directory', async () => {
    const { runtime } = build({
      installations: [installation({ manifest: drawing(), dir: '/ext/standup' })],
      files: { '/ext/standup/dist/draw.js': 'export default () => {}' },
    })

    expect(await runtime.drawerFor('mermaid')).toEqual({
      ok: true,
      code: 'export default () => {}',
      owner: 'standup',
    })
  })

  it('folds the format, so one drawer is not two', async () => {
    const { runtime } = build({
      installations: [installation({ manifest: drawing(), dir: '/ext/standup' })],
      files: { '/ext/standup/dist/draw.js': 'code' },
    })

    expect((await runtime.drawerFor('MERMAID')).ok).toBe(true)
  })

  it('says so when nothing installed draws it', async () => {
    const { runtime } = build({ installations: [installation({ manifest: drawing() })] })

    expect((await runtime.drawerFor('vega-lite')).reason).toContain('Nothing installed draws')
  })

  // A disabled extension is one the user turned off; its drawer goes with it.
  it('skips an extension that is disabled', async () => {
    const { runtime } = build({
      installations: [installation({ manifest: drawing(), dir: '/ext/standup', enabled: false })],
      files: { '/ext/standup/dist/draw.js': 'code' },
    })

    expect((await runtime.drawerFor('mermaid')).ok).toBe(false)
  })

  // Named rather than swallowed: an extension declaring a drawer it does not
  // ship is a broken package, and its author should hear which one.
  it('names the extension when the file it promised is not there', async () => {
    const { runtime } = build({
      installations: [installation({ manifest: drawing(), dir: '/ext/standup' })],
    })

    const result = await runtime.drawerFor('mermaid')

    expect(result.ok).toBe(false)
    expect(result.reason).toContain('standup')
    expect(result.reason).toContain('mermaid')
  })

  /**
   * What the renderer actually asks, and all it needs: the answer decides
   * whether a block shows a frame or a sentence. The code itself is fetched by
   * the frame from a URL — a real drawer is megabytes, and sending that over
   * the bridge to answer yes or no would be absurd.
   */
  describe('hasDrawer', () => {
    it('says yes without reading anything off disk', async () => {
      const { runtime, readDrawer } = build({
        installations: [installation({ manifest: drawing(), dir: '/ext/standup' })],
      })

      expect(await runtime.hasDrawer('mermaid')).toEqual({ ok: true, owner: 'standup' })
      expect(readDrawer).not.toHaveBeenCalled()
    })

    it('folds the format, and says no for one nothing draws', async () => {
      const { runtime } = build({ installations: [installation({ manifest: drawing() })] })

      expect((await runtime.hasDrawer('MERMAID')).ok).toBe(true)
      expect((await runtime.hasDrawer('vega-lite')).reason).toContain('Nothing installed draws')
    })

    it('skips an extension that is disabled', async () => {
      const { runtime } = build({
        installations: [installation({ manifest: drawing(), enabled: false })],
      })

      expect((await runtime.hasDrawer('mermaid')).ok).toBe(false)
    })

    it('has nothing to look up for no format', async () => {
      const { runtime } = build()

      expect((await runtime.hasDrawer('')).reason).toContain('No format')
      expect((await runtime.hasDrawer(undefined)).ok).toBe(false)
    })

    it('passes over an extension that declares none', async () => {
      const { runtime } = build({ installations: [installation()] })

      expect((await runtime.hasDrawer('mermaid')).ok).toBe(false)
    })
  })

  it('has nothing to look up for no format', async () => {
    const { runtime, readDrawer } = build()

    expect((await runtime.drawerFor('')).reason).toContain('No format')
    expect((await runtime.drawerFor(undefined)).ok).toBe(false)
    expect(readDrawer).not.toHaveBeenCalled()
  })

  // An extension declaring none is the ordinary case and must not throw.
  it('passes over an extension that declares no drawers at all', async () => {
    const { runtime } = build({ installations: [installation()] })

    expect((await runtime.drawerFor('mermaid')).ok).toBe(false)
  })
})

describe('remove', () => {
  // The whole reason this function exists in one place.
  it('takes the standing work down while the grant still exists', async () => {
    const { runtime, order } = build()
    await runtime.installLocal('/tmp/standup', { grant: { scope: 'workspace', workspaces: ['ws1'] } })
    order.length = 0

    await runtime.remove('standup')

    expect(order).toEqual(['stop', 'removeOwner', 'revoke', 'forgetConfig', 'uninstall'])
  })

  it('forgets the grant, so reinstalling asks again', async () => {
    const { runtime, broker } = build()
    await runtime.installLocal('/tmp/standup', { grant: { scope: 'workspace', workspaces: ['ws1'] } })

    await runtime.remove('standup')

    expect(broker.grantFor('standup')).toBeNull()
    expect(runtime.grants()).toEqual({})
  })

  // Refusing to uninstall over a cron task nobody can delete would leave
  // somebody stuck with an extension they cannot remove.
  it('finishes the uninstall even when something could not be taken down', async () => {
    const { runtime } = build({
      schedules: {
        removeOwner: async () => ({ ok: false, removed: [], problems: [{ id: 'daily', reason: 'workspace not found' }] }),
      },
    })

    const result = await runtime.remove('standup')

    expect(result.ok).toBe(true)
    expect(result.leftBehind).toEqual(['workspace not found'])
  })

  it('is quiet when the reconciler answered with nothing to report', async () => {
    const { runtime } = build({ schedules: { removeOwner: async () => ({ ok: true, removed: [] }) } })

    expect((await runtime.remove('standup')).leftBehind).toEqual([])
  })

  it('reports a failure to remove the files', async () => {
    const { runtime } = build({ installer: { uninstall: async () => ({ ok: false, reason: 'EBUSY' }) } })

    expect((await runtime.remove('standup')).reason).toBe('EBUSY')
  })
})

describe('setEnabled', () => {
  // "Disabled" has to mean it stopped doing things, including at 3am.
  it('takes standing work down when switched off', async () => {
    const { runtime, schedules, host } = build()

    const result = await runtime.setEnabled('standup', false)

    expect(result.ok).toBe(true)
    expect(host.stop).toHaveBeenCalledWith('standup')
    expect(schedules.removeOwner).toHaveBeenCalledWith('standup')
  })

  it('loads it again, with the grant it already had, when switched on', async () => {
    const { runtime, broker } = build()
    runtime.rememberGrant('standup', {
      scope: 'workspace',
      workspaces: ['ws1'],
      tools: { workspace: ['listTasks'], supervisor: [] },
    })
    broker.setGrant.mockClear()

    const result = await runtime.setEnabled('standup', true)

    expect(result.ok).toBe(true)
    expect(broker.setGrant).toHaveBeenCalledWith('standup', {
      scope: 'workspace',
      workspaces: ['ws1'],
      tools: { workspace: ['listTasks'], supervisor: [] },
    })
  })

  it('reports one that is not installed', async () => {
    const { runtime } = build({ installer: { setEnabled: async () => ({ ok: false, reason: 'linear is not installed.' }) } })

    expect((await runtime.setEnabled('linear', true)).reason).toContain('not installed')
  })

  it('reports a module that will not load when switched back on', async () => {
    const { runtime } = build({ host: { start: async () => ({ ok: false, reason: 'broken' }) } })

    expect((await runtime.setEnabled('standup', true)).reason).toBe('broken')
  })
})

describe('configure', () => {
  it('saves and reloads, so the extension sees the new values', async () => {
    const { runtime, order } = build({ installations: [installation()] })

    const result = await runtime.configure('standup', { since: 8 })

    expect(result.ok).toBe(true)
    // Reloaded, because `apply` reads config once: an extension carrying on
    // with the old values makes the settings screen look like it did nothing.
    expect(order).toEqual(['saveConfig', 'stop', 'start', 'reconcile'])
  })

  it('reports one that is not installed', async () => {
    const { runtime } = build({ installations: [] })
    expect((await runtime.configure('linear', {})).reason).toContain('not installed')
  })

  it('does not reload when the settings were refused', async () => {
    const { runtime, host } = build({
      installations: [installation()],
      configStore: { save: async () => ({ ok: false, reason: 'no secure storage' }) },
    })

    expect((await runtime.configure('standup', {})).ok).toBe(false)
    expect(host.stop).not.toHaveBeenCalled()
  })

  it('copes with an installation whose manifest declares no config', async () => {
    const { runtime, configStore } = build({ installations: [installation({ manifest: {} })] })

    await runtime.configure('standup', {})

    expect(configStore.save).toHaveBeenCalledWith('standup', [], {})
  })
})

describe('startAll', () => {
  it('loads everything enabled and skips what is not', async () => {
    const { runtime, host } = build({
      installations: [installation(), installation({ name: 'digest', enabled: false })],
    })

    const results = await runtime.startAll()

    expect(results.map((entry) => entry.name)).toEqual(['standup'])
    expect(host.start).toHaveBeenCalledTimes(1)
  })

  // One bad install must not take away everything else somebody had.
  it('carries on past one that will not load', async () => {
    let calls = 0
    const { runtime, logger } = build({
      installations: [installation({ name: 'broken' }), installation()],
      host: {
        start: async (entry) => {
          calls += 1
          return calls === 1 ? { ok: false, reason: 'syntax error' } : { ok: true, registered: 1 }
        },
      },
    })

    const results = await runtime.startAll()

    expect(results).toEqual([
      { name: 'broken', ok: false, reason: 'syntax error' },
      { name: 'standup', ok: true, reason: undefined },
    ])
    expect(logger.warn).toHaveBeenCalledWith(expect.stringContaining('syntax error'))
  })

  it('applies the grant it was told to remember', async () => {
    const { runtime, broker } = build({ installations: [installation()] })
    runtime.rememberGrant('standup', { scope: 'supervisor', workspaces: [] })

    await runtime.startAll()

    expect(broker.setGrant).toHaveBeenCalledWith('standup', { scope: 'supervisor', workspaces: [] })
  })

  it('ignores an empty grant rather than storing one', () => {
    const { runtime, broker } = build()

    runtime.rememberGrant('standup', null)

    expect(broker.setGrant).not.toHaveBeenCalled()
    expect(runtime.grants()).toEqual({})
  })
})

describe('state', () => {
  it('describes what is installed, and what each one contributed', async () => {
    const { runtime, host } = build({ installations: [installation()] })
    host.registries.ui.listFor = () => [{ id: 'today' }]
    host.registries.shortcuts.listFor = () => [{ id: 'open' }]
    await runtime.startAll()

    const [row] = await runtime.state()

    expect(row).toMatchObject({
      name: 'standup',
      displayName: 'Standup',
      enabled: true,
      loaded: true,
      linked: true,
      contributes: { ui: 1, shortcuts: 1, schedules: 0 },
    })
  })

  // Enabled and running are not the same thing, and a row that conflated them
  // would have nothing to explain why an enabled extension does nothing.
  it('separates enabled from actually running', async () => {
    const { runtime } = build({
      installations: [installation({ failures: 2 })],
      host: { start: async () => ({ ok: false, reason: 'broken' }) },
    })
    await runtime.startAll()

    const [row] = await runtime.state()

    expect(row).toMatchObject({ enabled: true, loaded: false, failures: 2 })
  })

  it('holds up for an installation with almost no manifest', async () => {
    const { runtime } = build({ installations: [installation({ manifest: undefined, sourceLabel: undefined, source: undefined })] })

    const [row] = await runtime.state()

    expect(row).toMatchObject({ displayName: 'standup', description: '', license: '', source: '', linked: false })
  })

  it('reports the grant it is running under', async () => {
    const { runtime } = build({ installations: [installation()] })
    runtime.rememberGrant('standup', { scope: 'supervisor', workspaces: [] })

    expect((await runtime.state())[0].grant).toEqual({ scope: 'supervisor', workspaces: [] })
  })

  it('treats an installation with no enabled flag as enabled', async () => {
    const { runtime } = build({ installations: [installation({ enabled: undefined, failures: undefined })] })

    expect((await runtime.state())[0]).toMatchObject({ enabled: true, failures: 0 })
  })
})

/**
 * These lived in the wrapper `index.js` builds around the runtime, where
 * nothing could reach them — `index.js` imports Electron at module scope and is
 * excluded from coverage. A verification script asked the runtime for `entries`
 * and found nothing there, which is what moved them here.
 */
describe('entries and invoke', () => {
  const withEntries = (entries) =>
    build({
      host: {
        resolve: vi.fn(() => entries),
        registries: { ui: { listFor: () => [] }, shortcuts: { list: () => [], listFor: () => [] }, schedules: { listFor: () => [] } },
        list: () => [],
        start: async () => ({ ok: true, registered: 1 }),
        stop: () => ({ ok: true }),
        stopAll: () => {},
      },
    })

  it('reads a drawn surface out of the ui registry', () => {
    const { runtime, host } = withEntries([
      { owner: 'standup', id: 'today', surface: 'page', label: 'Standup', order: 20 },
    ])

    const rows = runtime.entries('page', {})

    expect(host.resolve).toHaveBeenCalledWith('ui', {})
    expect(rows).toEqual([{ owner: 'standup', id: 'today', surface: 'page', label: 'Standup', order: 20 }])
  })

  // A key sequence has to be unique across every extension, so it lives in its
  // own registry — and the renderer asks for surfaces, not registries.
  it('reads a shortcut out of the shortcuts registry', () => {
    const { runtime, host } = withEntries([{ owner: 'standup', id: 'open', key: 's', label: 'Standup', order: 20 }])

    const rows = runtime.entries('shortcut', {})

    expect(host.resolve).toHaveBeenCalledWith('shortcuts', {})
    expect(rows[0].key).toBe('s')
  })

  it('runs one and answers with what it drew', async () => {
    const { runtime } = withEntries([
      { owner: 'standup', id: 'today', surface: 'page', run: () => ({ title: 'Standup', nodes: [] }) },
    ])

    const result = await runtime.invoke({ owner: 'standup', id: 'today', surface: 'page' }, {})

    expect(result).toMatchObject({ ok: true, view: { title: 'Standup' } })
  })

  it('says so plainly when the entry has gone', async () => {
    const { runtime } = withEntries([])

    expect(await runtime.invoke()).toEqual({ ok: false, reason: 'That extension is no longer available.' })
  })

  it('reports a predicate that threw against the extension that owns it', () => {
    const { runtime, logger } = withEntries([
      { owner: 'digest', id: 'x', surface: 'task-menu', when: () => { throw new Error('boom') } },
    ])

    expect(runtime.entries('task-menu', {})).toEqual([])
    expect(logger.warn).toHaveBeenCalledWith(expect.stringContaining('digest'), 'boom')
  })

  it('has something to say about a predicate that threw nothing readable', () => {
    const { runtime, logger } = withEntries([
      { owner: 'digest', id: 'x', surface: 'task-menu', when: () => { throw 'boom' } }, // eslint-disable-line no-throw-literal
    ])

    runtime.entries('task-menu', {})

    expect(logger.warn).toHaveBeenCalledWith(expect.any(String), 'boom')
  })
})

describe('stopAll', () => {
  it('unloads everything without touching the server', async () => {
    const { runtime, host, schedules } = build()

    runtime.stopAll()

    expect(host.stopAll).toHaveBeenCalled()
    // Emphatically not: the whole point of standing work is that it outlives
    // the app being open.
    expect(schedules.removeOwner).not.toHaveBeenCalled()
  })
})
