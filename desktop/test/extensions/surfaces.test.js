import { describe, it, expect, vi } from 'vitest'

import { applies, entriesFor, invokeEntry, registryFor, serialise } from '../../src/main/extensions/surfaces.js'

/**
 * A registry entry holds functions and a message cannot. These tests are about
 * the crossing: what survives it, what is decided on this side because it cannot
 * be decided on the other, and what happens when somebody else's code throws in
 * the middle of an IPC handler.
 */

const entry = (over = {}) => ({
  owner: 'task-stats',
  id: 'stats',
  surface: 'task-menu',
  label: 'Task Stats',
  order: 10,
  run: (task) => ({ title: `Stats for ${task?.title}`, nodes: [] }),
  ...over,
})

describe('serialise', () => {
  it('keeps what a menu needs and drops everything callable', () => {
    const described = serialise(entry({ when: () => true, view: () => ({}) }))

    expect(described).toEqual({ owner: 'task-stats', id: 'stats', surface: 'task-menu', label: 'Task Stats', order: 10 })
    // The point of the whole file: no function can cross, and none does.
    for (const value of Object.values(described)) expect(typeof value).not.toBe('function')
  })

  it('falls back to the id when there is no label', () => {
    expect(serialise({ owner: 'x', id: 'stats', order: 1 }).label).toBe('stats')
  })

  it('carries a shortcut key, which only shortcuts have', () => {
    expect(serialise({ owner: 'standup', id: 'open', key: 's', order: 1 }).key).toBe('s')
    expect(serialise(entry())).not.toHaveProperty('key')
  })
})

describe('applies', () => {
  it('says yes to an entry with no opinion', () => {
    expect(applies(entry(), { title: 'anything' })).toBe(true)
  })

  it('asks the predicate about this particular task', () => {
    const conditional = entry({ when: (task) => task.status === 'completed' })

    expect(applies(conditional, { status: 'completed' })).toBe(true)
    expect(applies(conditional, { status: 'ongoing' })).toBe(false)
  })

  // One extension deciding badly must not empty a menu other extensions are in.
  it('treats a predicate that throws as a no, and reports it', () => {
    const onError = vi.fn()
    const broken = entry({ when: () => { throw new Error('undefined is not an object') } })

    expect(applies(broken, {}, onError)).toBe(false)
    expect(onError).toHaveBeenCalledWith('task-stats', expect.any(Error))
  })

  it('swallows the failure when nobody is listening', () => {
    expect(applies(entry({ when: () => { throw new Error('x') } }), {})).toBe(false)
  })
})

describe('entriesFor', () => {
  const entries = [
    entry(),
    entry({ owner: 'digest', id: 'context', when: (task) => task.status === 'completed' }),
    entry({ owner: 'standup', id: 'today', surface: 'page' }),
    entry({ owner: 'nowhere', id: 'x', surface: undefined }),
  ]

  it('returns only the surface asked for, already filtered for this task', () => {
    const rows = entriesFor(entries, 'task-menu', { status: 'ongoing' })

    expect(rows.map((row) => row.owner)).toEqual(['task-stats'])
  })

  it('includes a conditional row on a task it applies to', () => {
    const rows = entriesFor(entries, 'task-menu', { status: 'completed' })

    expect(rows.map((row) => row.owner)).toEqual(['task-stats', 'digest'])
  })

  it('treats an entry with no surface as belonging to none', () => {
    expect(entriesFor(entries, '', {}).map((row) => row.owner)).toEqual(['nowhere'])
  })

  it('drops a row whose predicate threw even when nobody asked to be told', () => {
    const rows = entriesFor([entry({ when: () => { throw new Error('boom') } })], 'task-menu', {})

    expect(rows).toEqual([])
  })

  it('reports a predicate that threw against the extension that owns it', () => {
    const onError = vi.fn()

    entriesFor([entry({ when: () => { throw new Error('boom') } })], 'task-menu', {}, { onError })

    expect(onError).toHaveBeenCalledWith('task-stats', expect.any(Error))
  })
})

describe('invokeEntry', () => {
  it('runs the entry and answers with what it drew', async () => {
    const result = await invokeEntry([entry()], { owner: 'task-stats', id: 'stats', surface: 'task-menu' }, { title: 'Ship it' })

    expect(result).toEqual({ ok: true, view: { title: 'Stats for Ship it', nodes: [] } })
  })

  // An action and a page are the same thing under two names; an author should
  // not have to remember which surface takes which.
  it('accepts either run or view', async () => {
    const page = entry({ surface: 'page', run: undefined, view: () => ({ title: 'Standup', nodes: [] }) })

    const result = await invokeEntry([page], { owner: 'task-stats', id: 'stats', surface: 'page' }, {})

    expect(result.view.title).toBe('Standup')
  })

  it('waits for one that answers later', async () => {
    const slow = entry({ run: async () => ({ title: 'later', nodes: [] }) })

    expect((await invokeEntry([slow], { owner: 'task-stats', id: 'stats', surface: 'task-menu' }, {})).view.title).toBe('later')
  })

  // Ordinary rather than exceptional: an extension can be uninstalled between a
  // menu opening and something on it being clicked.
  it('says so plainly when the entry has gone', async () => {
    const result = await invokeEntry([], { owner: 'task-stats', id: 'stats', surface: 'task-menu' }, {})

    expect(result).toEqual({ ok: false, reason: 'That extension is no longer available.' })
  })

  it('does not confuse two entries that differ only by surface', async () => {
    const entries = [entry(), entry({ surface: 'page', run: () => ({ title: 'page', nodes: [] }) })]

    const result = await invokeEntry(entries, { owner: 'task-stats', id: 'stats', surface: 'page' }, {})

    expect(result.view.title).toBe('page')
  })

  it('matches an entry registered without a surface', async () => {
    const result = await invokeEntry([entry({ surface: undefined })], { owner: 'task-stats', id: 'stats' }, {})

    expect(result.ok).toBe(true)
  })

  it('names the extension that registered something with nothing to run', async () => {
    const result = await invokeEntry(
      [entry({ run: undefined, view: undefined })],
      { owner: 'task-stats', id: 'stats', surface: 'task-menu' },
      {},
    )

    expect(result.reason).toContain('task-stats')
    expect(result.reason).toContain('nothing to run')
  })

  // Thrown from an IPC handler, this would reject a bridge call and surface as
  // a stack trace from a file the user has never heard of.
  it('turns a throw into a refusal with the extension named', async () => {
    const broken = entry({ run: () => { throw new Error('Cannot read properties of undefined') } })

    const result = await invokeEntry([broken], { owner: 'task-stats', id: 'stats', surface: 'task-menu' }, {})

    expect(result.ok).toBe(false)
    expect(result.reason).toContain('task-stats failed:')
    expect(result.reason).toContain('Cannot read properties')
  })

  it('has something to say about a throw that carried no message', async () => {
    const broken = entry({ run: () => { throw 'nope' } }) // eslint-disable-line no-throw-literal

    expect((await invokeEntry([broken], { owner: 'task-stats', id: 'stats', surface: 'task-menu' }, {})).reason).toContain('nope')
  })

  // An entry that returns nothing has done something else, and an empty panel
  // would look like a failure rather than a completed action.
  it('distinguishes "drew nothing" from "failed"', async () => {
    const silent = entry({ run: () => undefined })

    expect(await invokeEntry([silent], { owner: 'task-stats', id: 'stats', surface: 'task-menu' }, {})).toEqual({
      ok: true,
      view: null,
    })
  })
})


/**
 * A shortcut lives in its own registry, because a key sequence has to be unique
 * across every extension while a page only has to be unique within one. The
 * renderer asks for surfaces and should not have to know which registry that
 * means, so the mapping is here.
 */
describe('registryFor', () => {
  it('sends the three drawn surfaces to the ui registry', () => {
    for (const surface of ['page', 'workspace-action', 'task-menu']) {
      expect(registryFor(surface)).toBe('ui')
    }
  })

  it('sends shortcuts to their own', () => {
    expect(registryFor('shortcut')).toBe('shortcuts')
  })

  it('falls back to ui for anything it has not heard of', () => {
    expect(registryFor('')).toBe('ui')
    expect(registryFor(undefined)).toBe('ui')
  })
})

describe('a registry whose entries are all one surface', () => {
  const shortcut = (over = {}) => ({
    owner: 'standup',
    id: 'open',
    key: 's',
    label: 'Standup',
    order: 20,
    run: () => ({ title: 'Standup', nodes: [] }),
    ...over,
  })

  // Asking authors to write `surface: 'shortcut'` beside the key they are
  // binding would be a field with exactly one legal value.
  it('does not need each entry to declare the surface', () => {
    const rows = entriesFor([shortcut()], 'shortcut', {})

    expect(rows).toHaveLength(1)
    expect(rows[0].key).toBe('s')
  })

  it('runs one without matching on a surface it does not carry', async () => {
    const result = await invokeEntry([shortcut()], { owner: 'standup', id: 'open', surface: 'shortcut' }, {})

    expect(result.ok).toBe(true)
    expect(result.view.title).toBe('Standup')
  })

  it('still tells two ui surfaces apart', async () => {
    const entries = [
      { owner: 'digest', id: 'x', surface: 'page', run: () => ({ title: 'page', nodes: [] }) },
      { owner: 'digest', id: 'x', surface: 'workspace-action', run: () => ({ title: 'action', nodes: [] }) },
    ]

    const result = await invokeEntry(entries, { owner: 'digest', id: 'x', surface: 'workspace-action' }, {})

    expect(result.view.title).toBe('action')
  })
})
