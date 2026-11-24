// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, expect, it } from 'vitest'

import { formatPointer, getPointer, parsePointer, resolvePath, setPointer } from '../src/a2ui/pointer.js'

describe('a2ui pointer', () => {
  it('parses and formats RFC 6901 pointers, escapes included', () => {
    expect(parsePointer('')).toEqual([])
    expect(parsePointer('/')).toEqual([])
    expect(parsePointer('/a~1b/c~0d/0')).toEqual(['a/b', 'c~d', '0'])
    expect(parsePointer('rel/x')).toEqual(['rel', 'x'])
    // ~01 is "~1", not "/": ~1 is undone before ~0.
    expect(parsePointer('/~01')).toEqual(['~1'])
    expect(formatPointer(['a/b', 'c~d', 0])).toBe('/a~1b/c~0d/0')
    expect(formatPointer([])).toBe('')
  })

  it('resolves a relative path against the scope of a List item', () => {
    expect(resolvePath('/abs', ['items', '2'])).toEqual(['abs'])
    expect(resolvePath('name', ['items', '2'])).toEqual(['items', '2', 'name'])
    expect(resolvePath('name')).toEqual(['name'])
  })

  it('gets values, and undefined where nothing is', () => {
    const doc = { a: { b: [10, { c: 'x' }] }, n: null }
    expect(getPointer(doc, [])).toBe(doc)
    expect(getPointer(doc, ['a', 'b', '1', 'c'])).toBe('x')
    expect(getPointer(doc, ['a', 'missing'])).toBeUndefined()
    expect(getPointer(doc, ['n', 'x'])).toBeUndefined()
    expect(getPointer(doc, ['a', 'b', '0', 'x'])).toBeUndefined()
    expect(getPointer(doc, ['__proto__'])).toBeUndefined()
    expect(getPointer(doc, ['a', 'constructor'])).toBeUndefined()
    expect(getPointer({}, ['toString'])).toBeUndefined()
  })

  it('sets values, creating parents', () => {
    expect(setPointer({ a: 1 }, [], { b: 2 })).toEqual({ b: 2 })
    expect(setPointer({ a: 1 }, [], null)).toEqual({})
    expect(setPointer({ a: 1 }, ['x', 'y'], 3)).toEqual({ a: 1, x: { y: 3 } })
    expect(setPointer({ a: 5 }, ['a', 'y'], 3)).toEqual({ a: { y: 3 } })
    expect(setPointer(null, ['k'], 'v')).toEqual({ k: 'v' })
    expect(setPointer({ l: [1, 2] }, ['l', '1'], 9)).toEqual({ l: [1, 9] })
  })

  it('deletes on null, from objects and arrays', () => {
    expect(setPointer({ a: 1, b: 2 }, ['a'], null)).toEqual({ b: 2 })
    expect(setPointer({ l: [1, 2, 3] }, ['l', '1'], null)).toEqual({ l: [1, 3] })
    expect(setPointer({ l: [1] }, ['l', '5'], null)).toEqual({ l: [1] })
    expect(setPointer({ l: [1] }, ['l', 'x'], null)).toEqual({ l: [1] })
  })

  it('never writes through a prototype key', () => {
    const doc = {}
    expect(setPointer(doc, ['__proto__', 'polluted'], true)).toBe(doc)
    expect(setPointer(doc, ['constructor', 'prototype', 'polluted'], true)).toBe(doc)
    expect({}.polluted).toBeUndefined()
    expect(doc).toEqual({})
  })
})
