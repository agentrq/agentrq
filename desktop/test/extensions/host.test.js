import { describe, it, expect, vi } from 'vitest'

import { FAILURE_LIMIT, buildContext, createHost, validateModule } from '../../src/main/extensions/host.js'
import { createRegistries } from '../../src/main/extensions/registry.js'

/**
 * Loading is only half of it. The half worth testing is what happens when an
 * extension misbehaves: a partially applied extension — loaded, half its
 * contributions missing, nothing saying why — is the worst outcome available
 * here, so every failure path retracts everything before it.
 */

const installation = (over = {}) => ({ name: 'linear', version: '1.0.0', enabled: true, ...over })

/** An extension module, with whatever apply the test needs. */
const moduleWith = (apply, over = {}) => ({ name: 'linear', inject: ['ui'], apply, ...over })

function build({ module = moduleWith(() => {}), config = {}, ...over } = {}) {
  const onDisabled = vi.fn(async () => {})
  const logger = { info: vi.fn(), warn: vi.fn() }
  const host = createHost({
    load: vi.fn(async () => module),
    readConfig: vi.fn(async () => config),
    onDisabled,
    logger,
    ...over,
  })
  return { host, onDisabled, logger }
}

/**
 * The default client, for a host assembled without a broker.
 *
 * It matters because the alternative — leaving `ctx.mcp` undefined — turns an
 * extension's first call into a `TypeError` inside the host rather than a
 * sentence its author can read.
 */
describe('an extension given no broker at all', () => {
  it('is refused with a reason rather than crashing on undefined', async () => {
    let seen
    const { host } = build({ module: moduleWith((ctx) => { seen = ctx.mcp }) })

    await host.start(installation())

    expect(await seen.workspace('getTask', {})).toEqual({
      ok: false,
      reason: 'This extension has not been granted any access.',
    })
    expect(await seen.supervisor('listAllTasks', {})).toMatchObject({ ok: false })
  })

  it('hands over the broker\'s client when there is one', async () => {
    const client = { workspace: vi.fn(), supervisor: vi.fn() }
    let seen
    const { host } = build({
      module: moduleWith((ctx) => { seen = ctx.mcp }),
      clientFor: vi.fn(() => client),
    })

    await host.start(installation())

    expect(seen).toBe(client)
  })
})

describe('validateModule', () => {
  it('accepts a module with an apply and reads what it injects', () => {
    const { ok, inject } = validateModule(moduleWith(() => {}), 'linear')

    expect(ok).toBe(true)
    expect(inject).toEqual(['ui'])
  })

  it('refuses a module with no apply, plainly', () => {
    // The alternative is a TypeError naming a line in somebody else's code.
    expect(validateModule({}, 'linear').reason).toBe('This extension exports no apply(ctx, config) function.')
    expect(validateModule(undefined, 'linear').reason).toBe('This extension exports nothing.')
    expect(validateModule('not a module', 'linear').ok).toBe(false)
  })

  it('catches a module that disagrees with its manifest about its own name', () => {
    // The manifest name is what everything else addresses it by, so half the
    // wiring would point somewhere else.
    const { ok, reason } = validateModule(moduleWith(() => {}, { name: 'linear-issues' }), 'linear')

    expect(ok).toBe(false)
    expect(reason).toBe('This extension calls itself "linear-issues" but its manifest says "linear".')
  })

  it('lets a module leave its name out', () => {
    expect(validateModule({ apply: () => {} }, 'linear').ok).toBe(true)
  })

  it('treats a missing or malformed inject as asking for nothing', () => {
    expect(validateModule({ apply: () => {} }, 'x').inject).toEqual([])
    expect(validateModule({ apply: () => {}, inject: 'ui' }, 'x').inject).toEqual([])
  })
})

describe('buildContext', () => {
  const registries = () => createRegistries()

  it('gives an extension only what it asked for', () => {
    // Handing over everything would make `inject` decorative, and the
    // declaration is what lets the install screen say what it touches.
    const { ctx } = buildContext({
      name: 'linear',
      registries: registries(),
      inject: ['ui'],
      config: {},
      logger: console,
    })

    expect(typeof ctx.ui.add).toBe('function')
    expect(ctx.shortcuts).toBeUndefined()
  })

  it('refuses an extension asking for something that does not exist', () => {
    const { ok, reason } = buildContext({
      name: 'linear',
      registries: registries(),
      inject: ['ui', 'telepathy'],
      config: {},
      logger: console,
    })

    expect(ok).toBe(false)
    expect(reason).toContain('telepathy')
  })

  it('throws on a rejected registration rather than returning a result', () => {
    // apply() is a procedure, and an author will not check a return value they
    // did not know to expect.
    const shared = registries()
    shared.ui.add('standup', { id: 'index' })

    const { ctx } = buildContext({
      name: 'standup',
      registries: shared,
      inject: ['ui'],
      config: {},
      logger: console,
    })

    expect(() => ctx.ui.add({ id: 'index' })).toThrow('registers the view "index" twice')
  })

  it('chains, so a run of registrations reads as one', () => {
    const { ctx } = buildContext({
      name: 'linear',
      registries: registries(),
      inject: ['ui'],
      config: {},
      logger: console,
    })

    expect(ctx.ui.add({ id: 'a' }).add({ id: 'b' })).toBe(ctx.ui)
  })

  it('prefixes what an extension logs with its name', () => {
    const logger = { info: vi.fn(), warn: vi.fn() }
    const { ctx } = buildContext({ name: 'linear', registries: registries(), inject: [], config: {}, logger })

    ctx.logger.info('hello')
    ctx.logger.warn('careful')

    expect(logger.info).toHaveBeenCalledWith('[linear]', 'hello')
    expect(logger.warn).toHaveBeenCalledWith('[linear]', 'careful')
  })

  it('survives a logger that offers nothing', () => {
    const { ctx } = buildContext({ name: 'x', registries: registries(), inject: [], config: {}, logger: {} })

    expect(() => ctx.logger.info('hi')).not.toThrow()
    expect(() => ctx.logger.warn('hi')).not.toThrow()
  })
})

describe('createHost', () => {
  it('loads an extension and keeps what it registered', async () => {
    const { host } = build({
      module: moduleWith((ctx) => {
        ctx.ui.add({ id: 'index' })
      }),
    })

    const { ok, registered } = await host.start(installation())

    expect(ok).toBe(true)
    expect(registered).toBe(1)
    expect(host.registries.ui.list()).toHaveLength(1)
    expect(host.list()).toEqual([{ name: 'linear', version: '1.0.0', failures: 0 }])
  })

  it('hands the extension its configuration', async () => {
    const apply = vi.fn()
    const { host } = build({ module: moduleWith(apply), config: { teamId: 'ENG' } })

    await host.start(installation())

    expect(apply).toHaveBeenCalledWith(expect.objectContaining({ config: { teamId: 'ENG' } }), {
      teamId: 'ENG',
    })
  })

  it('refuses to load one twice, or one that is disabled', async () => {
    const { host } = build()
    await host.start(installation())

    expect((await host.start(installation())).reason).toBe('linear is already loaded.')
    expect((await host.start(installation({ name: 'other', enabled: false }))).reason).toBe(
      'other is disabled.',
    )
  })

  it('retracts everything when apply throws halfway through', async () => {
    // A partially applied extension looks loaded, is missing half its
    // contributions, and says nothing about why.
    const { host } = build({
      module: moduleWith((ctx) => {
        ctx.ui.add({ id: 'index' })
        throw new Error('boom')
      }),
    })

    const { ok, reason } = await host.start(installation())

    expect(ok).toBe(false)
    expect(reason).toBe('boom')
    expect(host.registries.ui.list()).toEqual([])
  })

  it('reports a module that will not import', async () => {
    const { host } = build({
      load: vi.fn(async () => {
        throw new Error('Cannot find module')
      }),
    })

    expect((await host.start(installation())).reason).toBe('Cannot find module')
  })

  it('reports a module of the wrong shape', async () => {
    const { host } = build({ module: { name: 'linear' } })

    expect((await host.start(installation())).reason).toContain('exports no apply')
  })

  it('loads anyway when the configuration cannot be read', async () => {
    // Missing settings are the extension's problem to report; refusing to load
    // it entirely takes away its chance to say so.
    const { host, logger } = build({
      readConfig: vi.fn(async () => {
        throw new Error('EACCES')
      }),
    })

    expect((await host.start(installation())).ok).toBe(true)
    expect(logger.warn).toHaveBeenCalled()
  })

  it('unloads, taking every entry with it', async () => {
    // Keyed by owner precisely so this is complete without the author helping.
    const { host } = build({
      module: moduleWith((ctx) => {
        ctx.ui.add({ id: 'a' }).add({ id: 'b' })
      }),
    })
    await host.start(installation())

    const { ok, removed } = host.stop('linear')

    expect(ok).toBe(true)
    expect(removed).toBe(2)
    expect(host.registries.ui.list()).toEqual([])
    expect(host.list()).toEqual([])
  })

  it('refuses to unload what is not loaded', () => {
    expect(build().host.stop('ghost').reason).toBe('ghost is not loaded.')
  })

  it('disables an extension that keeps failing, and says so', async () => {
    // An app that keeps reloading something broken degrades for reasons nobody
    // can see from outside.
    const { host, onDisabled } = build({
      module: moduleWith(() => {
        throw new Error('always')
      }),
    })

    // A failed load retracts itself, so each attempt starts clean — the count
    // is what has to survive, and it is kept apart from the loaded state for
    // exactly that reason.
    for (let i = 1; i < FAILURE_LIMIT; i += 1) {
      expect((await host.start(installation())).disabled).toBe(false)
    }
    const last = await host.start(installation())

    expect(last.disabled).toBe(true)
    expect(onDisabled).toHaveBeenCalledWith('linear', 'always')
  })

  it('tolerates a transient failure rather than switching off on the first one', async () => {
    // A network blip inside apply should not permanently disable something
    // somebody relies on.
    const { host, onDisabled } = build({
      module: moduleWith(() => {
        throw new Error('blip')
      }),
    })

    await host.start(installation())

    expect(onDisabled).not.toHaveBeenCalled()
  })

  it('clears the count when a load finally works', async () => {
    // Otherwise two failures months apart would eventually disable something
    // that has been working perfectly well in between.
    let broken = true
    const { host, onDisabled } = build({
      module: moduleWith(() => {
        if (broken) throw new Error('blip')
      }),
    })

    await host.start(installation())
    broken = false
    await host.start(installation())
    expect(host.list()[0].failures).toBe(0)

    host.stop('linear')
    broken = true
    await host.start(installation())
    await host.start(installation())

    expect(onDisabled).not.toHaveBeenCalled()
  })

  it('refuses at load an extension asking for a registry that does not exist', async () => {
    // Told at load, not discovered when a call returns undefined.
    const { host } = build({ module: moduleWith(() => {}, { inject: ['telepathy'] }) })

    expect((await host.start(installation())).reason).toContain('telepathy')
  })

  it('describes a failure that carries nothing at all', async () => {
    const { host } = build({
      module: moduleWith(() => {
        throw null
      }),
    })

    expect((await host.start(installation())).reason).toBe('Unknown error')
  })

  it('disables without an onDisabled being supplied', async () => {
    // Production may not care to be told; the disabling still has to happen.
    const host = createHost({
      load: async () => moduleWith(() => {
        throw new Error('always')
      }),
      readConfig: async () => ({}),
      logger: { info: vi.fn(), warn: vi.fn() },
    })

    for (let i = 1; i < FAILURE_LIMIT; i += 1) await host.start(installation())

    expect((await host.start(installation())).disabled).toBe(true)
  })

  it('resolves a registry against a context', async () => {
    const { host } = build({
      module: moduleWith((ctx) => {
        ctx.ui.add({ id: 'a', value: (t) => (t.done ? 'shown' : undefined) })
      }),
    })
    await host.start(installation())

    expect(host.resolve('ui', { done: true })[0].value).toBe('shown')
    expect(host.resolve('ui', { done: false })).toEqual([])
  })

  it('reports an entry that throws while being resolved, without losing the rest', async () => {
    const { host, logger } = build({
      module: moduleWith((ctx) => {
        ctx.ui.add({ id: 'bad', value: () => { throw new Error('nope') } })
      }),
    })
    await host.start(installation())

    expect(host.resolve('ui', {})).toEqual([])
    expect(logger.warn).toHaveBeenCalledWith(expect.stringContaining('[linear]'), 'nope')
  })

  it('reports a resolve failure that is not an Error', async () => {
    const { host, logger } = build({
      module: moduleWith((ctx) => {
        ctx.ui.add({ id: 'bad', value: () => { throw 'a string' } })
      }),
    })
    await host.start(installation())

    host.resolve('ui', {})

    expect(logger.warn).toHaveBeenCalledWith(expect.stringContaining('[linear]'), 'a string')
  })

  it('answers with nothing for a registry that does not exist', () => {
    expect(build().host.resolve('telepathy', {})).toEqual([])
  })

  it('unloads everything for shutdown', async () => {
    const { host } = build({ module: moduleWith((ctx) => ctx.ui.add({ id: 'a' })) })
    await host.start(installation())

    host.stopAll()

    expect(host.list()).toEqual([])
    expect(host.registries.ui.list()).toEqual([])
  })

  it('describes a failure that is not an Error', async () => {
    const { host } = build({
      module: moduleWith(() => {
        throw 'just a string'
      }),
    })

    expect((await host.start(installation())).reason).toBe('just a string')
  })

  it('logs through a bare console when given no logger', async () => {
    const host = createHost({ load: async () => moduleWith(() => {}), readConfig: async () => ({}) })

    expect((await host.start(installation())).ok).toBe(true)
  })
})
