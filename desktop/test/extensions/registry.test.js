// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

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

    expect(Object.keys(registries).sort()).toEqual(['renderers', 'schedules', 'shortcuts', 'ui'])
  })

  /**
   * A fenced-code language has one renderer, whoever asked first. Two
   * extensions both drawing ```mermaid would be resolved by whichever loaded
   * first, which is no answer at all — so the second is refused at install with
   * the first extension named.
   */
  it('gives a fenced-code language to exactly one extension', () => {
    const { renderers } = createRegistries()

    expect(renderers.add('mermaid-agentrq', { id: 'mermaid', language: 'mermaid' }).ok).toBe(true)

    const clash = renderers.add('other', { id: 'diagrams', language: 'mermaid' })
    expect(clash.ok).toBe(false)
    expect(clash.reason).toContain('mermaid-agentrq')
  })

  it('lets two extensions claim different languages', () => {
    const { renderers } = createRegistries()

    expect(renderers.add('mermaid-agentrq', { id: 'mermaid', language: 'mermaid' }).ok).toBe(true)
    expect(renderers.add('vega', { id: 'vega', language: 'vega-lite' }).ok).toBe(true)
  })

  it('scopes each one the way that surface is addressed', () => {
    const registries = createRegistries()

    // A key sequence has one meaning whoever asked for it; a page is reached at
    // /extensions/:name/:pageId and only has to be unique within its extension.
    expect(registries.shortcuts.scope).toBe(SCOPES.global)
    expect(registries.ui.scope).toBe(SCOPES.owner)
  })
})


/**
 * A shortcut is claimed by the key it binds, not by the name its author gave
 * the entry.
 *
 * Keyed on `id`, two extensions both calling theirs `open` — which the shipped
 * `standup` example does — collided over a name nobody presses, while two
 * genuinely fighting over `x s` did not collide at all. The registry's own
 * comment said it stopped exactly that.
 */
describe('a registry that is unique by something other than the id', () => {
  const shortcuts = () => createRegistry({ name: 'shortcut', scope: SCOPES.global, uniqueBy: 'key' })

  it('lets two extensions name their entries the same thing', () => {
    const registry = shortcuts()

    expect(registry.add('standup', { id: 'open', key: 's' }).ok).toBe(true)
    expect(registry.add('linear', { id: 'open', key: 'l' }).ok).toBe(true)
  })

  it('refuses two extensions the same key, naming who has it', () => {
    const registry = shortcuts()
    registry.add('standup', { id: 'open', key: 's' })

    const clash = registry.add('linear', { id: 'search', key: 's' })

    expect(clash.ok).toBe(false)
    expect(clash.reason).toContain('standup')
  })

  it('folds case, so S and s are one claim', () => {
    // The dispatcher lowercases what it reads from the keyboard, so a registry
    // that did not would hand out a second claim on a key already spoken for.
    const registry = shortcuts()
    registry.add('standup', { id: 'open', key: 'S' })

    expect(registry.add('linear', { id: 'search', key: 's' }).ok).toBe(false)
    expect(registry.claimedBy('linear', 's')).toBe('standup')
  })

  it('refuses an entry with nothing to claim', () => {
    const registry = shortcuts()

    const { ok, reason } = registry.add('standup', { id: 'open' })

    expect(ok).toBe(false)
    expect(reason).toContain('needs a key')
  })

  it('still tells one extension it asked twice', () => {
    const registry = shortcuts()
    registry.add('standup', { id: 'open', key: 's' })

    expect(registry.add('standup', { id: 'other', key: 's' }).reason).toContain('twice')
  })
})
