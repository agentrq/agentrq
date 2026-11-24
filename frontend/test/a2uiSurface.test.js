// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, expect, it } from 'vitest'
import { isReactive } from 'vue'

import { isDynamic, resolveArgs, resolveValue } from '../src/a2ui/resolve.js'
import {
  ROOT_ID, applyMessage, applyMessages, childEntries, createSurfaceState, loadSurface, surfaceContext,
} from '../src/a2ui/surface.js'
import { BASIC_CATALOG_ID } from '../src/a2ui/validate.js'

const v = (body) => ({ version: '1.0', ...body })
const create = (extra = {}) => v({ createSurface: { surfaceId: 's1', catalogId: BASIC_CATALOG_ID, ...extra } })
const update = (components) => v({ updateComponents: { surfaceId: 's1', components } })
const data = (value, path) => v({ updateDataModel: { surfaceId: 's1', value, ...(path === undefined ? {} : { path }) } })

describe('a2ui surface', () => {
  it('starts empty and reactive', () => {
    const s = createSurfaceState()
    expect(isReactive(s)).toBe(true)
    expect(s).toMatchObject({ surfaceId: null, catalogId: null, rootId: ROOT_ID, dataModel: {}, deleted: false })
    expect(s.components.size).toBe(0)
  })

  it('applies the four messages in order', () => {
    const msgs = [
      create({ components: [{ id: 'root', component: 'Text', text: 'inline' }], dataModel: { a: 1 } }),
      update([{ id: 'root', component: 'Card', child: 't' }, { id: 't', component: 'Text', text: { '@path': '/title' } }]),
      data({ title: 'Hello', list: [1, 2] }),
      data('Bye', '/title'),
      data(null, '/list/0'),
    ]
    const s = applyMessages(msgs)
    expect(s.surfaceId).toBe('s1')
    expect(s.catalogId).toBe(BASIC_CATALOG_ID)
    expect(s.components.get('root').component).toBe('Card') // the later definition wins
    expect(s.dataModel).toEqual({ title: 'Bye', list: [2] })
    // The stored messages are never changed by applying them.
    expect(msgs[2].updateDataModel.value).toEqual({ title: 'Hello', list: [1, 2] })
    expect(resolveValue(s.components.get('t').text, surfaceContext(s))).toBe('Bye')
  })

  it('createSurface starts over; deleteSurface empties; unknown messages change nothing', () => {
    const s = applyMessages([create(), update([{ id: 'root', component: 'Divider' }]), data({ a: 1 })])
    applyMessage(s, { version: '1.0', callRendererFunction: {} })
    applyMessage(s, null)
    expect(s.components.size).toBe(1)
    applyMessage(s, v({ deleteSurface: { surfaceId: 's1' } }))
    expect(s).toMatchObject({ deleted: true, dataModel: {} })
    expect(s.components.size).toBe(0)
    applyMessage(s, v({ createSurface: { surfaceId: 's2' } }))
    expect(s).toMatchObject({ surfaceId: 's2', catalogId: null, deleted: false, dataModel: {} })
    applyMessage(s, data(null))
    expect(s.dataModel).toEqual({})
  })

  it('loads a card from metadata, or null for the text fallback', () => {
    const messages = [create(), update([{ id: 'root', component: 'Text', text: 'hi' }])]
    expect(loadSurface({ type: 'a2ui', messages }).components.get('root').text).toBe('hi')
    expect(loadSurface(null)).toBeNull()
    expect(loadSurface({ type: 'other', messages })).toBeNull()
    expect(loadSurface({ type: 'a2ui' })).toBeNull()
    expect(loadSurface({ type: 'a2ui', messages: [update([{ id: 'root', component: 'Button' }])] })).toBeNull()
    expect(loadSurface({ type: 'a2ui', messages: [...messages, v({ deleteSurface: { surfaceId: 's1' } })] })).toBeNull()
    const interactive = [create(), update([{ id: 'root', component: 'Button', child: 't', action: { event: { name: 'go' } } }, { id: 't', component: 'Text', text: 'Go' }])]
    expect(loadSurface({ type: 'a2ui', messages: interactive })).toBeNull()
    expect(loadSurface({ type: 'a2ui', messages: interactive }, { interactive: true })).not.toBeNull()
  })

  it('expands static children and List templates, each item its own scope and @index', () => {
    const s = applyMessages([create(), data({ items: [{ name: 'a' }, { name: 'b' }], byKey: { x: { name: 'X' } }, n: 3 })])
    const ctx = surfaceContext(s, { locale: 'en-US' })
    expect(childEntries(s, ['a', 'b'], ctx)).toEqual([{ key: 'a', id: 'a', ctx }, { key: 'b', id: 'b', ctx }])
    const items = childEntries(s, { componentId: 'row', path: '/items' }, ctx)
    expect(items.map((e) => [e.key, e.id, e.ctx.scope, e.ctx.index])).toEqual([
      ['row/0', 'row', ['items', '0'], 0],
      ['row/1', 'row', ['items', '1'], 1],
    ])
    expect(items[1].ctx.locale).toBe('en-US')
    expect(resolveValue({ '@path': 'name' }, items[1].ctx)).toBe('b')
    expect(resolveValue({ '@call': '@index', args: { offset: 1 } }, items[1].ctx)).toBe(2)
    expect(resolveValue({ '@call': 'formatString', args: { value: '${name} of ${/n}' } }, items[0].ctx)).toBe('a of 3')
    // A nested template's path is relative to its item.
    const nested = applyMessages([create(), data({ groups: [{ rows: ['p', 'q'] }] })])
    const [group] = childEntries(nested, { componentId: 'g', path: '/groups' }, surfaceContext(nested))
    const rows = childEntries(nested, { componentId: 'r', path: 'rows' }, group.ctx)
    expect(rows.map((e) => e.ctx.scope)).toEqual([['groups', '0', 'rows', '0'], ['groups', '0', 'rows', '1']])
    expect(resolveValue({ '@path': '' }, rows[1].ctx)).toBe('q')
    // An object's keys are its items.
    const keyed = childEntries(s, { componentId: 'row', path: '/byKey' }, ctx)
    expect(keyed.map((e) => [e.key, e.ctx.index])).toEqual([['row/x', 0]])
    // Nothing to draw.
    expect(childEntries(s, { componentId: 'row', path: '/n' }, ctx)).toEqual([])
    expect(childEntries(s, { componentId: 'row', path: '/missing' }, ctx)).toEqual([])
    expect(childEntries(s, { componentId: 'row' }, ctx)).toEqual([])
    expect(childEntries(s, null, ctx)).toEqual([])
    expect(childEntries(s, 'root', ctx)).toEqual([])
  })
})

describe('a2ui resolve', () => {
  const s = applyMessages([create(), data({ name: 'Ada', n: 2, ok: true })])
  const ctx = surfaceContext(s, { locale: 'en-US' })

  it('passes literals through', () => {
    for (const lit of ['x', 3, true, null, undefined, ['a'], { plain: 1 }]) expect(resolveValue(lit, ctx)).toEqual(lit)
    expect(isDynamic('x')).toBe(false)
    expect(isDynamic({ plain: 1 })).toBe(false)
    expect(isDynamic(['@path'])).toBe(false)
    expect(isDynamic({ '@path': '/a' })).toBe(true)
    expect(isDynamic({ '@call': 'not' })).toBe(true)
  })

  it('reads bindings and calls functions, arguments resolved first', () => {
    expect(resolveValue({ '@path': '/name' }, ctx)).toBe('Ada')
    expect(resolveValue({ '@path': '/nope' }, ctx)).toBeUndefined()
    expect(resolveValue({ '@call': 'and', args: { values: [{ '@path': '/ok' }, { '@call': 'not', args: { value: false } }] } }, ctx)).toBe(true)
    expect(resolveValue({ '@call': 'formatNumber', args: { value: { '@path': '/n' }, decimals: 1 } }, ctx)).toBe('2.0')
    expect(resolveValue({ '@call': 'unknown' }, ctx)).toBeUndefined()
    expect(resolveArgs(undefined, ctx)).toEqual({})
    expect(resolveArgs([1], ctx)).toEqual({})
  })
})
