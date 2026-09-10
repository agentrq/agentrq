import { describe, it, expect, vi } from 'vitest'

import { SCOPES, createRegistries, createRegistry } from '../../src/main/extensions/registry.js'

/**
 * Three rules carry this: names are unique and a duplicate is rejected loudly,
 * order never depends on load sequence, and a value may be a function so an
 * entry can decide whether it applies at all.
 */

describe('createRegistry', () => {
  const build = (scope) => createRegistry({ name: 'shortcut', scope })

  it('accepts an entry and hands it back with its owner', () => {
    const registry = build()

    expect(registry.add('linear', { id: 'l' }).ok).toBe(true)
    expect(registry.list()).toEqual([{ id: 'l', owner: 'linear', order: 100 }])
  })

  it('names who already holds a colliding id', () => {
    // "Duplicate id" tells an author nothing they can act on; the useful fact
    // is who they are colliding with.
    const registry = build()
    registry.add('standup', { id: 's' })

    const { ok, reason } = registry.add('linear', { id: 's' })

    expect(ok).toBe(false)
    expect(reason).toBe('linear cannot register the shortcut "s": standup already has it.')
  })

  it('catches an extension colliding with itself', () => {
    const registry = build()
    registry.add('linear', { id: 'l' })

    expect(registry.add('linear', { id: 'l' }).reason).toBe('linear registers the shortcut "l" twice.')
  })

  it('lets two extensions use the same id where the scope is per-extension', () => {
    // A page is addressed by owner and id, so there is nothing to collide.
    const registry = build(SCOPES.owner)

    expect(registry.add('linear', { id: 'index' }).ok).toBe(true)
    expect(registry.add('standup', { id: 'index' }).ok).toBe(true)
    expect(registry.list()).toHaveLength(2)
  })

  it('refuses an entry with nothing to call it', () => {
    const registry = build()

    expect(registry.add('linear', {}).reason).toBe('A shortcut needs an id.')
    expect(registry.add('linear', { id: '  ' }).ok).toBe(false)
    expect(registry.add('', { id: 'l' }).reason).toContain('without an extension')
  })

  it('orders by order, then stably, whatever the load sequence', () => {
    // What a user sees must not depend on which extension loaded first.
    const registry = build()
    registry.add('zeta', { id: 'b', order: 10 })
    registry.add('alpha', { id: 'a', order: 10 })
    registry.add('mid', { id: 'c', order: 1 })

    expect(registry.list().map((e) => e.id)).toEqual(['c', 'a', 'b'])
  })

  it('defaults the order rather than making every author pick a number', () => {
    const registry = build()
    registry.add('a', { id: 'x' })
    registry.add('b', { id: 'y', order: 5 })

    expect(registry.list().map((e) => e.id)).toEqual(['y', 'x'])
  })

  it('ignores an order that is not a number', () => {
    const registry = build()
    registry.add('a', { id: 'x', order: 'first' })

    expect(registry.list()[0].order).toBe(100)
  })

  it('resolves a lazy value against the context it is given', () => {
    const registry = build()
    registry.add('linear', { id: 'l', value: (task) => `issue for ${task.title}` })

    expect(registry.resolve({ title: 'Ship it' })[0].value).toBe('issue for Ship it')
  })

  it('drops a lazy entry that opts out', () => {
    // This is what stops ten installed extensions meaning ten permanent rows on
    // every task.
    const registry = build()
    registry.add('linear', { id: 'l', value: (task) => (task.done ? 'x' : undefined) })

    expect(registry.resolve({ done: false })).toEqual([])
    expect(registry.resolve({ done: true })).toHaveLength(1)
  })

  it('drops an entry that throws, and reports it, without emptying the menu', () => {
    // One extension deciding badly must not take out the ones beside it.
    const onError = vi.fn()
    const registry = build()
    registry.add('good', { id: 'g', value: () => 'fine' })
    registry.add('bad', {
      id: 'b',
      value: () => {
        throw new Error('nope')
      },
    })

    const resolved = registry.resolve({}, { onError })

    expect(resolved.map((e) => e.id)).toEqual(['g'])
    expect(onError).toHaveBeenCalledWith('bad', expect.any(Error))
  })

  it('leaves a static value alone', () => {
    const registry = build()
    registry.add('linear', { id: 'l', value: 'plain' })

    expect(registry.resolve({})[0].value).toBe('plain')
  })

  it('resolves with no error handler given', () => {
    const registry = build()
    registry.add('bad', { id: 'b', value: () => { throw new Error('nope') } })

    expect(registry.resolve({})).toEqual([])
  })

  it('gives up every name an extension held', () => {
    const registry = build()
    registry.add('linear', { id: 'a' })
    registry.add('linear', { id: 'b' })
    registry.add('standup', { id: 'c' })

    expect(registry.removeOwner('linear')).toBe(2)
    expect(registry.list().map((e) => e.id)).toEqual(['c'])
    // And the name is free again.
    expect(registry.add('other', { id: 'a' }).ok).toBe(true)
  })

  it('lists just one extension’s entries, which is what an uninstall needs', () => {
    const registry = build()
    registry.add('linear', { id: 'a' })
    registry.add('standup', { id: 'b' })

    expect(registry.listFor('linear').map((e) => e.id)).toEqual(['a'])
  })

  it('says who holds a name, or nobody', () => {
    const registry = build()
    registry.add('linear', { id: 'l' })

    expect(registry.claimedBy('anyone', 'l')).toBe('linear')
    expect(registry.claimedBy('anyone', 'free')).toBe('')
  })
})

describe('createRegistries', () => {
  it('offers the surfaces an extension can contribute to', () => {
    const registries = createRegistries()

    expect(Object.keys(registries).sort()).toEqual(['schedules', 'shortcuts', 'ui'])
  })

  it('scopes each one the way that surface is addressed', () => {
    const registries = createRegistries()

    // A key sequence has one meaning whoever asked for it; a page is reached at
    // /extensions/:name/:pageId and only has to be unique within its extension.
    expect(registries.shortcuts.scope).toBe(SCOPES.global)
    expect(registries.ui.scope).toBe(SCOPES.owner)
  })
})
