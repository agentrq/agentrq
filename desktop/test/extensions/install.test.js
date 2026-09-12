import { describe, it, expect, vi } from 'vitest'

import { createInstaller, reasonFrom, scopesWidened } from '../../src/main/extensions/install.js'

/**
 * Two things this has to get right, and both are about what happens when
 * something goes wrong: a download that does not match its digest is refused
 * rather than warned about, and any failure leaves nothing behind. A half-
 * written extension directory looks installed, loads partially, and fails
 * somewhere far from the install that caused it.
 */

const manifestFor = (name, mcp) =>
  JSON.stringify({
    name,
    version: '1.0.0',
    license: 'MIT',
    engines: { agentrq: '^1.4' },
    ...(mcp ? { mcp } : {}),
    artifact: { release: 'v1.0.0', asset: `${name}.tgz`, sha256: 'a'.repeat(64) },
  })

const releaseSource = (over = {}) => ({
  kind: 'release',
  name: 'thing',
  repo: 'acme/thing',
  release: 'v1.0.0',
  asset: 'thing.tgz',
  sha256: 'a'.repeat(64),
  ...over,
})

/** An installer with every piece of IO faked, so nothing touches disk or network. */
function build({ manifest = manifestFor('thing'), fetched = {}, ...over } = {}) {
  const removed = []
  const moved = []
  let saved = { installations: [] }
  // Mutable so a test can change what the *next* stage finds — which is how an
  // update that asks for more than the version already installed is set up.
  const current = { manifest }

  const deps = {
    fetchSource: vi.fn(async (source, temp) => ({
      dir: `${temp}/unpacked`,
      sha256: source.kind === 'release' ? source.sha256 : undefined,
      commit: source.kind === 'git' ? 'c0ffee1' : undefined,
      ...fetched,
    })),
    readManifest: vi.fn(async () => current.manifest),
    move: vi.fn(async (from, to) => moved.push([from, to])),
    remove: vi.fn(async (dir) => removed.push(dir)),
    makeTempDir: vi.fn(async () => '/tmp/stage'),
    dirFor: (name) => `/ext/${name}`,
    store: {
      read: vi.fn(async () => saved),
      write: vi.fn(async (state) => {
        saved = JSON.parse(JSON.stringify(state))
      }),
    },
    now: () => 1000,
    ...over,
  }

  return {
    installer: createInstaller(deps),
    deps,
    removed,
    moved,
    saved: () => saved,
    /** What the next fetch will find in the package. */
    setManifest: (next) => {
      current.manifest = next
    },
  }
}

describe('reasonFrom', () => {
  it('uses the message when there is one', () => {
    expect(reasonFrom(new Error('EACCES'))).toBe('EACCES')
  })

  it('copes with what is thrown not being an Error', () => {
    // A reason reading "[object Object]" is worse than useless in a message
    // somebody is expected to act on.
    expect(reasonFrom('just a string')).toBe('just a string')
    expect(reasonFrom({})).toBe('Unknown error')
    expect(reasonFrom(undefined)).toBe('Unknown error')
    expect(reasonFrom(new Error(''))).toBe('Unknown error')
  })
})

describe('scopesWidened', () => {
  it('sees nothing new as nothing to ask about', () => {
    const same = { workspace: ['getTask'], supervisor: [] }
    expect(scopesWidened(same, same).widened).toBe(false)
  })

  it('does not prompt for an update that asks for less', () => {
    // Being asked to re-approve something that now wants *less* teaches people
    // the prompt is noise, and its whole value is being rare enough to read.
    const before = { workspace: ['getTask', 'reply'], supervisor: ['listAllTasks'] }
    const after = { workspace: ['getTask'], supervisor: [] }

    expect(scopesWidened(before, after).widened).toBe(false)
  })

  it('names exactly what was added', () => {
    const before = { workspace: ['getTask'], supervisor: [] }
    const after = { workspace: ['getTask', 'reply'], supervisor: [] }

    const { widened, workspace } = scopesWidened(before, after)

    expect(widened).toBe(true)
    expect(workspace).toEqual(['reply'])
  })

  it('calls out reaching every workspace as its own thing', () => {
    // This is what the check exists to catch: an extension acquiring
    // account-wide access in a patch release nobody read.
    const grew = scopesWidened({ supervisor: [] }, { supervisor: ['listAllTasks'] })

    expect(grew.reachesAllWorkspaces).toBe(true)
  })

  it('does not call an already-supervisor extension a new escalation', () => {
    const grew = scopesWidened(
      { supervisor: ['listAllTasks'] },
      { supervisor: ['listAllTasks', 'createWorkspace'] },
    )

    expect(grew.widened).toBe(true)
    expect(grew.reachesAllWorkspaces).toBe(false)
  })

  it('copes with either side being absent', () => {
    expect(scopesWidened(undefined, undefined).widened).toBe(false)
    expect(scopesWidened(undefined, { workspace: ['x'] }).widened).toBe(true)
  })
})

describe('install', () => {
  it('verifies, stages and records a release', async () => {
    const { installer, moved, saved } = build()

    const { ok, installation } = await installer.install(releaseSource())

    expect(ok).toBe(true)
    expect(moved).toEqual([['/tmp/stage/unpacked', '/ext/thing']])
    expect(installation.pin).toEqual({ kind: 'sha256', value: 'a'.repeat(64) })
    expect(installation.sourceLabel).toBe('acme/thing · v1.0.0')
    expect(saved().installations).toHaveLength(1)
  })

  it('refuses a download that does not match its digest, and leaves nothing behind', async () => {
    // The case the digest exists for: a tag moved to different code after the
    // manifest was published.
    const { installer, deps, moved, saved } = build({ fetched: { sha256: 'b'.repeat(64) } })

    const { ok, reason } = await installer.install(releaseSource())

    expect(ok).toBe(false)
    expect(reason).toContain('does not match the checksum')
    expect(moved).toEqual([])
    expect(deps.remove).toHaveBeenCalledWith('/tmp/stage')
    expect(saved().installations).toEqual([])
  })

  it('refuses a download with no manifest in it', async () => {
    const { installer, moved } = build({ manifest: null })

    const { ok, reason } = await installer.install(releaseSource())

    expect(ok).toBe(false)
    expect(reason).toContain('no agentrq-extension.json')
    expect(moved).toEqual([])
  })

  it('refuses a manifest that does not parse, with the parser’s own reason', async () => {
    const { installer } = build({ manifest: JSON.stringify({ name: 'thing', version: '1.0.0' }) })

    expect((await installer.install(releaseSource())).reason).toContain('"license" is required')
  })

  it('cleans up when the fetch itself throws', async () => {
    const { installer, deps } = build({
      fetchSource: vi.fn(async () => {
        throw new Error('clone failed: repository not found')
      }),
    })

    const { ok, reason } = await installer.install({ kind: 'git', url: 'git@x:y.git' })

    expect(ok).toBe(false)
    expect(reason).toContain('repository not found')
    expect(deps.remove).toHaveBeenCalledWith('/tmp/stage')
  })

  it('reports a swap that fails without leaving a record of it', async () => {
    const { installer, saved } = build({
      move: vi.fn(async () => {
        throw new Error('EXDEV')
      }),
    })

    const { ok, reason } = await installer.install(releaseSource())

    expect(ok).toBe(false)
    expect(reason).toContain('Could not install into /ext/thing')
    expect(saved().installations).toEqual([])
  })

  it('survives a temporary directory that will not delete', async () => {
    // Untidy, not broken — and not worth turning a clear failure into a
    // confusing one.
    const { installer } = build({
      fetched: { sha256: 'b'.repeat(64) },
      remove: vi.fn(async () => {
        throw new Error('EBUSY')
      }),
    })

    expect((await installer.install(releaseSource())).reason).toContain('does not match')
  })

  it('pins a clone by the commit it landed on', async () => {
    const { installer } = build()

    const { installation } = await installer.install({ kind: 'git', url: 'git@x:y.git', ref: '' })

    expect(installation.pin).toEqual({ kind: 'commit', value: 'c0ffee1' })
  })

  it('does not demand a digest from a source that has none', async () => {
    // A clone and a folder are not downloads; requiring a checksum of them
    // would make the two useful private routes impossible.
    const { installer } = build({ fetched: { sha256: undefined } })

    expect((await installer.install({ kind: 'git', url: 'git@x:y.git' })).ok).toBe(true)
  })

  it('links a local folder rather than copying it', async () => {
    // Editing the folder is editing the installed extension, which is the only
    // way developing one is tolerable.
    const { installer, deps, moved } = build()

    const { ok, installation } = await installer.install({ kind: 'local', path: '/work/ext' }, { linked: true })

    expect(ok).toBe(true)
    expect(installation.dir).toBe('/work/ext')
    expect(installation.pin).toEqual({ kind: 'none', value: '' })
    expect(moved).toEqual([])
    expect(deps.fetchSource).not.toHaveBeenCalled()
  })

  it('refuses to link a folder with nothing in it', async () => {
    const { installer } = build({ manifest: null })

    const { ok, reason } = await installer.install({ kind: 'local', path: '/work/x' }, { linked: true })

    expect(ok).toBe(false)
    expect(reason).toContain('no agentrq-extension.json in that folder')
  })

  it('refuses to link a folder whose manifest is broken', async () => {
    const { installer } = build({ manifest: '{ not json' })

    expect((await installer.install({ kind: 'local', path: '/x' }, { linked: true })).ok).toBe(false)
  })

  it('refuses a shortcut another extension already holds, before writing anything', async () => {
    // A conflict found at runtime is a key that silently does nothing, and
    // nothing on screen explains it. Found here, it is a sentence.
    const withShortcut = JSON.stringify({
      ...JSON.parse(manifestFor('thing')),
      shortcuts: [{ key: 'l', action: 'create' }],
    })
    const { installer, moved } = build({
      manifest: withShortcut,
      claimedKeys: () => [{ key: 'l', owner: 'standup' }],
    })

    const { ok, reason } = await installer.install(releaseSource())

    expect(ok).toBe(false)
    expect(reason).toBe('"x l" is already used by standup.')
    expect(moved).toEqual([])
  })

  it('lets an extension keep its own shortcut through an update', async () => {
    // Otherwise every update after the first conflicts with the version it is
    // replacing, which makes updating impossible.
    const withShortcut = JSON.stringify({
      ...JSON.parse(manifestFor('thing')),
      shortcuts: [{ key: 'l', action: 'create' }],
    })
    const { installer } = build({
      manifest: withShortcut,
      claimedKeys: () => [{ key: 'l', owner: 'thing' }],
    })

    expect((await installer.install(releaseSource())).ok).toBe(true)
  })

  it('replaces an earlier installation of the same name rather than doubling it', async () => {
    const { installer, saved } = build()

    await installer.install(releaseSource())
    await installer.install(releaseSource())

    expect(saved().installations).toHaveLength(1)
  })

  it('keeps the extension even when the record cannot be written', async () => {
    // It is installed on disk either way; throwing here would abandon a
    // completed install and leave exactly the half-state this all avoids.
    const { installer } = build({
      store: {
        read: vi.fn(async () => ({ installations: [] })),
        write: vi.fn(async () => {
          throw new Error('EACCES')
        }),
      },
    })

    expect((await installer.install(releaseSource())).ok).toBe(true)
  })

  it('timestamps an install with the real clock when none is injected', async () => {
    // Production injects nothing, so the default has to work.
    const before = Date.now()
    const { installer } = build({ now: undefined })

    const { installation } = await installer.install(releaseSource())

    expect(installation.installedAt).toBeGreaterThanOrEqual(before)
  })

  it('starts empty when there is no record to read', async () => {
    const { installer } = build({
      store: { read: vi.fn(async () => { throw new Error('ENOENT') }), write: vi.fn(async () => {}) },
    })

    expect(await installer.list()).toEqual([])
  })

  // Two callers arriving together must not each read the record and then each
  // write their own idea of it back — the second would overwrite the first.
  // The read is shared, so both wait on one.
  it('reads the record once when two callers arrive together', async () => {
    let release
    const held = new Promise((resolve) => {
      release = resolve
    })
    const read = vi.fn(async () => {
      await held
      return { installations: [] }
    })
    const { installer } = build({ store: { read, write: vi.fn(async () => {}) } })

    const both = Promise.all([installer.list(), installer.list()])
    release()

    expect(await both).toEqual([[], []])
    expect(read).toHaveBeenCalledOnce()
  })

  it('ignores a record of the wrong shape', async () => {
    const { installer } = build({
      store: { read: vi.fn(async () => ({ installations: 'lots' })), write: vi.fn(async () => {}) },
    })

    expect(await installer.list()).toEqual([])
  })
})

describe('update', () => {
  const widerManifest = manifestFor('thing', { workspace: ['getTask'], supervisor: ['listAllTasks'] })

  it('installs quietly when the ask has not changed', async () => {
    const { installer, moved } = build()
    await installer.install(releaseSource())

    const { ok, installation } = await installer.update('thing', releaseSource({ release: 'v1.1.0' }))

    expect(ok).toBe(true)
    expect(installation.sourceLabel).toBe('acme/thing · v1.1.0')
    expect(moved).toHaveLength(2)
  })

  it('stops and asks when the update wants more', async () => {
    // Staged but not installed. Installing first and asking after would make
    // the prompt a formality.
    const { installer, moved, setManifest } = build()
    await installer.install(releaseSource())

    setManifest(widerManifest)
    const { ok, needsGrant, scopes } = await installer.update('thing', releaseSource())

    expect(ok).toBe(false)
    expect(needsGrant).toBe(true)
    expect(scopes.reachesAllWorkspaces).toBe(true)
    expect(scopes.supervisor).toEqual(['listAllTasks'])
    // Nothing moved into place: the second move never happened.
    expect(moved).toHaveLength(1)
  })

  it('installs an update that asks for less without a prompt', async () => {
    const { installer, setManifest } = build({ manifest: widerManifest })
    await installer.install(releaseSource())

    setManifest(manifestFor('thing', { workspace: ['getTask'], supervisor: [] }))
    const { ok, needsGrant } = await installer.update('thing', releaseSource())

    expect(ok).toBe(true)
    expect(needsGrant).toBeUndefined()
  })

  it('refuses an update that renames the extension', async () => {
    // The name is the address: the install directory, the registry keys and the
    // route. Accepting a rename would write the new code into the old
    // directory, leave the old record pointing at it, and add a second record
    // whose directory was never written — after which an uninstall of either
    // removes the wrong one.
    const { installer, moved, saved, setManifest } = build()
    await installer.install(releaseSource())

    setManifest(manifestFor('other-thing'))
    const result = await installer.update('thing', releaseSource())

    expect(result.ok).toBe(false)
    expect(result.reason).toContain('"other-thing"')
    expect(moved).toHaveLength(1)
    expect(saved().installations.map((i) => i.name)).toEqual(['thing'])
  })

  it('refuses to update something that is not installed', async () => {
    const { installer } = build()
    expect((await installer.update('ghost', releaseSource())).reason).toBe('ghost is not installed.')
  })

  it('keeps the previous version when the new one will not stage', async () => {
    // The update's own staging fails — a moved tag, a corrupt download — and
    // what is already installed must survive it untouched.
    const { installer, saved, setManifest, deps } = build()
    await installer.install(releaseSource())

    setManifest('{ not json')
    const result = await installer.update('thing', releaseSource())

    expect(result.ok).toBe(false)
    expect(result.reason).toBe('This is not valid JSON.')
    expect(deps.remove).toHaveBeenCalledWith('/tmp/stage')
    expect(saved().installations[0].version).toBe('1.0.0')
  })

  it('reports a swap that fails during an update', async () => {
    const moveOnce = vi.fn()
    let calls = 0
    const { installer } = build({
      move: vi.fn(async (from, to) => {
        calls += 1
        if (calls > 1) throw new Error('EPERM')
        moveOnce(from, to)
      }),
    })
    await installer.install(releaseSource())

    expect((await installer.update('thing', releaseSource())).reason).toContain('Could not update thing')
  })

  it('keeps a disabled extension disabled through an update', async () => {
    const { installer } = build()
    await installer.install(releaseSource())
    await installer.setEnabled('thing', false)

    const { installation } = await installer.update('thing', releaseSource())

    expect(installation.enabled).toBe(false)
  })
})

describe('enable, disable and uninstall', () => {
  it('stops loading it without removing anything', async () => {
    const { installer, removed } = build()
    await installer.install(releaseSource())
    // An install clears its target before moving in, so what matters is that
    // disabling adds no removal of its own.
    const before = removed.length

    const { ok, installation } = await installer.setEnabled('thing', false)

    expect(ok).toBe(true)
    expect(installation.enabled).toBe(false)
    expect(removed).toHaveLength(before)
  })

  it('gives a re-enabled extension a clean slate', async () => {
    // Disabling is usually somebody stopping something that keeps failing;
    // turning it back on one strike from being disabled again is not a fix.
    const { installer } = build()
    await installer.install(releaseSource())
    await installer.recordFailure('thing')
    await installer.recordFailure('thing')

    const { installation } = await installer.setEnabled('thing', true)

    expect(installation.failures).toBe(0)
  })

  it('removes the directory and the record together', async () => {
    const { installer, removed, saved } = build()
    await installer.install(releaseSource())

    const { ok } = await installer.uninstall('thing')

    expect(ok).toBe(true)
    expect(removed).toContain('/ext/thing')
    expect(saved().installations).toEqual([])
  })

  it('unlinks a local folder rather than deleting the user’s own work', async () => {
    // Deleting it is emphatically not what "uninstall" means.
    const { installer, removed, saved } = build()
    await installer.install({ kind: 'local', path: '/work/ext' }, { linked: true })

    await installer.uninstall('thing')

    expect(removed).not.toContain('/work/ext')
    expect(saved().installations).toEqual([])
  })

  it('reports a directory it could not remove, and keeps the record', async () => {
    // Only the uninstall fails: an install clears its own target first, and
    // breaking that would fail the setup rather than the case under test.
    let installed = false
    const { installer, saved } = build({
      remove: vi.fn(async () => {
        if (installed) throw new Error('EBUSY')
      }),
    })
    await installer.install(releaseSource())
    installed = true

    expect((await installer.uninstall('thing')).reason).toContain('Could not remove thing')
    // Still listed, because it is still on disk — a record removed here would
    // leave an extension nothing could uninstall.
    expect(saved().installations).toHaveLength(1)
  })

  it('refuses to act on something that is not installed', async () => {
    const { installer } = build()

    expect((await installer.setEnabled('ghost', false)).reason).toBe('ghost is not installed.')
    expect((await installer.uninstall('ghost')).reason).toBe('ghost is not installed.')
    expect((await installer.recordFailure('ghost')).reason).toBe('ghost is not installed.')
  })
})

describe('recordFailure', () => {
  it('disables an extension that keeps failing', async () => {
    // One that crashes every load is not going to fix itself, and an app that
    // keeps loading it degrades for reasons a user cannot see.
    const { installer } = build()
    await installer.install(releaseSource())

    expect((await installer.recordFailure('thing')).disabled).toBe(false)
    expect((await installer.recordFailure('thing')).disabled).toBe(false)
    const third = await installer.recordFailure('thing')

    expect(third.disabled).toBe(true)
    expect(third.failures).toBe(3)
    expect((await installer.list())[0].enabled).toBe(false)
  })

  it('takes a limit, for a caller that wants a different one', async () => {
    const { installer } = build()
    await installer.install(releaseSource())

    expect((await installer.recordFailure('thing', 1)).disabled).toBe(true)
  })
})


/**
 * "Local" and "linked" are not the same thing, and conflating them deletes
 * somebody's working copy or leaks a directory forever.
 *
 * A linked folder is *recorded*, so uninstalling only forgets it. A local
 * folder installed unlinked is *copied* into the install directory, and that
 * copy has to go. The check used to be `source.kind === 'local'`, which is true
 * for both.
 */
describe('uninstalling a folder', () => {
  // The manifest the fake stage produces is called `thing`, so that is the
  // name every installation here ends up under.
  it('leaves a linked folder alone, because it is the user own copy', async () => {
    const { installer, removed } = build()
    await installer.install({ kind: 'local', path: '/home/me/thing' }, { linked: true })
    removed.length = 0

    expect((await installer.uninstall('thing')).ok).toBe(true)
    expect(removed).toEqual([])
  })

  it('removes the copy made from an unlinked one', async () => {
    const { installer, removed } = build()
    await installer.install({ kind: 'local', path: '/home/me/thing' })
    removed.length = 0

    await installer.uninstall('thing')

    expect(removed).toContain('/ext/thing')
  })

  // A record written before `linked` was stored says only that it was local,
  // and the safe reading of that is the one that cannot delete a user's folder.
  it('errs towards keeping a folder when an older record does not say', async () => {
    const { installer, removed } = build({
      store: {
        read: async () => ({
          installations: [{ name: 'thing', source: { kind: 'local', path: '/home/me/thing' } }],
        }),
        write: async () => {},
      },
    })

    await installer.uninstall('thing')

    expect(removed).toEqual([])
  })
})
