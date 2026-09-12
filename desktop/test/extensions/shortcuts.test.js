// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, it, expect } from 'vitest'

import {
  PREFIX,
  checkShortcuts,
  claimedShortcuts,
  isBindableKey,
  requestedKeys,
} from '../../src/main/extensions/shortcuts.js'

/**
 * Asked at install, from the manifest. A conflict found at runtime is a key that
 * silently does nothing, with both extensions looking correct in isolation and
 * nothing on screen to explain it.
 */

const manifest = (shortcuts, name = 'linear') => ({ name, shortcuts })

describe('isBindableKey', () => {
  it('takes one letter or digit, and not the prefix', () => {
    expect(isBindableKey('l')).toBe(true)
    expect(isBindableKey('4')).toBe(true)
    expect(isBindableKey(PREFIX)).toBe(false)
    expect(isBindableKey('Enter')).toBe(false)
    expect(isBindableKey('')).toBe(false)
    expect(isBindableKey(undefined)).toBe(false)
    expect(isBindableKey(null)).toBe(false)
  })
})

describe('requestedKeys', () => {
  it('reads what a manifest asks for', () => {
    const keys = requestedKeys(manifest([{ key: 'L', action: 'create', title: 'Create issue' }]))

    expect(keys).toEqual([{ key: 'l', action: 'create', title: 'Create issue' }])
  })

  it('drops an entry with no key, leaving the rest', () => {
    expect(requestedKeys(manifest([{ action: 'x' }, { key: 'l' }]))).toHaveLength(1)
  })

  it('copes with a manifest asking for none', () => {
    expect(requestedKeys(manifest())).toEqual([])
    expect(requestedKeys(undefined)).toEqual([])
  })
})

describe('checkShortcuts', () => {
  it('accepts keys nobody holds', () => {
    expect(checkShortcuts(manifest([{ key: 'l' }]), []).ok).toBe(true)
  })

  it('names who already holds one', () => {
    const { ok, problems } = checkShortcuts(manifest([{ key: 'l' }]), [{ key: 'l', owner: 'standup' }])

    expect(ok).toBe(false)
    expect(problems[0]).toBe('"x l" is already used by standup.')
  })

  it('lets an extension keep its own keys through an update', () => {
    // Otherwise every update after the first would conflict with the version
    // it is replacing, which makes updating impossible.
    const taken = [{ key: 'l', owner: 'linear' }]

    expect(checkShortcuts(manifest([{ key: 'l' }], 'linear'), taken).ok).toBe(true)
  })

  it('refuses the prefix, and says why', () => {
    expect(checkShortcuts(manifest([{ key: PREFIX }]), []).problems[0]).toContain('is the prefix itself')
  })

  it('refuses a key that could never be pressed as one', () => {
    expect(checkShortcuts(manifest([{ key: 'Enter' }]), []).problems[0]).toContain('single letter or digit')
  })

  it('catches an extension asking twice for the same key', () => {
    expect(checkShortcuts(manifest([{ key: 'l' }, { key: 'L' }]), []).problems[0]).toContain('twice')
  })

  it('reports every problem, so a manifest can be fixed in one pass', () => {
    const { problems } = checkShortcuts(manifest([{ key: 'l' }, { key: 'Enter' }]), [
      { key: 'l', owner: 'standup' },
    ])

    expect(problems).toHaveLength(2)
  })

  it('has nothing to check when nothing is asked for', () => {
    expect(checkShortcuts(manifest(), []).ok).toBe(true)
    expect(checkShortcuts(undefined).ok).toBe(true)
  })
})

describe('claimedShortcuts', () => {
  const installation = (name, keys, over = {}) => ({
    name,
    manifest: manifest(keys.map((key) => ({ key, action: `${key}-action`, title: `${name} ${key}` })), name),
    ...over,
  })

  it('lists what every installed extension holds', () => {
    const claimed = claimedShortcuts([installation('linear', ['l']), installation('standup', ['s'])])

    expect(claimed.map((c) => `${c.owner}:${c.key}`)).toEqual(['linear:l', 'standup:s'])
    expect(claimed[0].label).toBe('linear l')
  })

  it('leaves out a disabled extension, whose keys are free again', () => {
    const claimed = claimedShortcuts([installation('linear', ['l'], { enabled: false })])

    expect(claimed).toEqual([])
  })

  it('leaves out a key that could never be pressed', () => {
    expect(claimedShortcuts([installation('linear', ['Enter'])])).toEqual([])
  })

  it('falls back to the action when a shortcut has no title', () => {
    const bare = { name: 'linear', manifest: manifest([{ key: 'l', action: 'create' }], 'linear') }

    expect(claimedShortcuts([bare])[0].label).toBe('create')
  })

  it('copes with nothing installed', () => {
    expect(claimedShortcuts()).toEqual([])
  })
})
