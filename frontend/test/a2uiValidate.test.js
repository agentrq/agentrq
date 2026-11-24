// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { readFileSync, readdirSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

import { resolveValue } from '../src/a2ui/resolve.js'
import { applyMessages, childEntries, surfaceContext } from '../src/a2ui/surface.js'
import {
  BASIC_CATALOG_ID, INTERACTIVE_COMPONENTS, MAX_BYTES, MAX_COMPONENTS, MAX_DEPTH, isStoredMediaUrl, validateMessages,
} from '../src/a2ui/validate.js'

// The official v1.0 examples T1 vendored, with their licence.
const EXAMPLES = resolve(__dirname, '../../backend/internal/service/a2ui/testdata/examples')
const MEDIA = { Image: ['url'], Video: ['url', 'posterUrl'], AudioPlayer: ['url'] }

function loadExamples() {
  return readdirSync(EXAMPLES)
    .filter((f) => f.endsWith('.json'))
    .map((f) => ({ file: f, ...JSON.parse(readFileSync(resolve(EXAMPLES, f), 'utf8')) }))
}

const componentsOf = (messages) =>
  messages.flatMap((m) => [...(m.createSurface?.components ?? []), ...(m.updateComponents?.components ?? [])])

// What the server stores: every media value replaced by a stored link.
function stored(messages) {
  const copy = structuredClone(messages)
  for (const c of componentsOf(copy)) {
    for (const p of MEDIA[c.component] ?? []) if (p in c) c[p] = `/storage/w-1/2/${c.id}-${p}`
  }
  return copy
}

const v = (body) => ({ version: '1.0', ...body })
const create = (extra = {}) => v({ createSurface: { surfaceId: 's1', catalogId: BASIC_CATALOG_ID, ...extra } })
const update = (components, surfaceId = 's1') => v({ updateComponents: { surfaceId, components } })
const text = (id, extra = {}) => ({ id, component: 'Text', text: 'x', ...extra })
const check = (messages, options) => validateMessages(messages, options)
const refused = (messages, pattern, options) => {
  const r = check(messages, options)
  expect(r.ok).toBe(false)
  expect(r.problems[0]).toMatch(pattern)
}
const card = (...components) => [create(), update([...components])]

describe('a2ui validate: the official examples', () => {
  const examples = loadExamples()

  it('are all here', () => {
    expect(examples).toHaveLength(43)
  })

  it.each(examples.map((e) => [e.file, e]))('%s passes as the server stores it', (_, ex) => {
    const msgs = stored(ex.messages)
    const interactive = componentsOf(msgs).some((c) => INTERACTIVE_COMPONENTS.includes(c.component))
    expect(check(msgs, { interactive: true })).toEqual({ ok: true, problems: [] })
    expect(check(msgs).ok).toBe(!interactive)
    // Media from a binding is refused, as the server refuses it.
    const bound = componentsOf(ex.messages).some((c) => (MEDIA[c.component] ?? []).some((p) => typeof c[p] === 'object'))
    if (bound) refused(ex.messages, /must be a stored file/, { interactive: true })
  })

  it.each(examples.map((e) => [e.file, e]))('%s resolves every value without throwing', (_, ex) => {
    const s = applyMessages(stored(ex.messages))
    const walk = (id, ctx, depth) => {
      const c = s.components.get(id)
      expect(c, id).toBeDefined()
      for (const [k, val] of Object.entries(c)) if (k !== 'children') resolveValue(val, ctx)
      if (depth > MAX_DEPTH) return
      const kids = [c.child, c.trigger, c.content, ...(c.tabs ?? []).map((t) => t.child)].filter(Boolean)
      for (const k of kids) walk(k, ctx, depth + 1)
      if (c.children) for (const e of childEntries(s, c.children, ctx)) walk(e.id, e.ctx, depth + 1)
    }
    walk('root', surfaceContext(s, { locale: 'en-US' }), 0)
  })

  it('draws the product card and the track list as written', () => {
    const byFile = Object.fromEntries(examples.map((e) => [e.file, e]))
    const product = applyMessages(byFile['product-card.json'].messages)
    const ctx = surfaceContext(product, { locale: 'en-US' })
    expect(resolveValue(product.components.get('reviews').text, ctx)).toBe('(2,847 reviews)')
    expect(resolveValue(product.components.get('price').text, ctx)).toBe('$199.99')
    const tracks = applyMessages(byFile['track-list.json'].messages)
    const tctx = surfaceContext(tracks, { locale: 'en-US' })
    const items = childEntries(tracks, tracks.components.get('tracks_list').children, tctx)
    expect(items.map((e) => resolveValue(tracks.components.get('track_title').text, e.ctx))).toEqual(['Weightless', 'Clair de Lune', 'Ambient Light'])
    expect(resolveValue(tracks.components.get('track_num').text, items[2].ctx)).toBe('3')
  })
})

describe('a2ui validate: messages', () => {
  it('accepts a minimal card', () => {
    expect(check(card(text('root')))).toEqual({ ok: true, problems: [] })
    expect(check([create()]).ok).toBe(true) // nothing to draw yet, but nothing wrong
  })

  it('refuses what is not a list of messages', () => {
    refused(null, /non-empty array/)
    refused([], /non-empty array/)
    refused('x', /non-empty array/)
    refused([create(), update([text('root', { text: 'x'.repeat(MAX_BYTES) })])], /larger than/)
  })

  it('refuses envelopes the server refuses', () => {
    refused(['x'], /version/)
    refused([{ createSurface: {} }], /version/)
    refused([{ version: '0.9', createSurface: {} }], /version/)
    refused([v({})], /exactly one/)
    refused([v({ createSurface: {}, deleteSurface: {} })], /exactly one/)
    refused([v({ callRendererFunction: {} })], /exactly one/)
    refused([v({ createSurface: 'x' })], /not valid/)
    refused([v({ createSurface: { catalogId: BASIC_CATALOG_ID } })], /not valid/)
    refused([create({ extra: 1 })], /not valid/)
    refused([create({ components: [] })], /not valid/)
    refused([create({ dataModel: [] })], /not valid/)
    refused([create({ metadata: { other: 1 } })], /not valid/)
    refused([v({ updateComponents: { surfaceId: 's1' } })], /not valid/)
    refused([v({ updateDataModel: { surfaceId: 's1' } })], /not valid/)
    refused([v({ updateDataModel: { surfaceId: 's1', value: 1, path: 3 } })], /not valid/)
    refused([v({ deleteSurface: { surfaceId: 3 } })], /not valid/)
    refused([v({ createSurface: { surfaceId: 'has space', catalogId: BASIC_CATALOG_ID } })], /surfaceId/)
    refused([v({ createSurface: { surfaceId: 'x'.repeat(65), catalogId: BASIC_CATALOG_ID } })], /surfaceId/)
    refused([create(), update([text('root')], 's2')], /one surface/)
    refused([create({ catalogId: 'basic' })], /catalogId/)
    refused([v({ createSurface: { surfaceId: 's1' } })], /catalogId/)
    expect(check([create({ sendDataModel: true, dataModel: { a: 1 }, metadata: { extensions: {} }, components: [text('root')] })]).ok).toBe(true)
    expect(check([v({ updateDataModel: { surfaceId: 's1', path: '/a', value: null } }), v({ deleteSurface: { surfaceId: 's1' } })]).ok).toBe(true)
  })
})

describe('a2ui validate: components', () => {
  it('allows display components only, unless interactive', () => {
    refused(card({ id: 'root', component: 'Button', child: 'root', action: { event: { name: 'x' } } }), /"Button" is not allowed/)
    refused(card({ id: 'root', component: 'Surface' }), /"Surface" is not allowed/)
    refused(card({ id: 'root', component: 'toString' }), /not allowed/)
    refused(card({ id: 'root' }), /not allowed/)
    refused(card('root'), /not an object/)
    refused(card(null), /not an object/)
    refused(card({ component: 'Divider' }), /id must be/)
    refused(card({ id: '', component: 'Divider' }), /id must be/)
  })

  it('refuses unknown properties, wrong types, bad enums and missing fields', () => {
    refused(card(text('root', { color: 'red' })), /color is not a property of Text/)
    refused(card(text('root', { text: 3 })), /text is not valid/)
    refused(card(text('root', { variant: 'h1' })), /variant is not valid/)
    refused(card(text('root', { weight: '1' })), /weight is not valid/)
    refused(card({ id: 'root', component: 'Text' }), /text is required/)
    refused(card({ id: 'root', component: 'Row' }), /children is required/)
    refused(card({ id: 'root', component: 'Row', children: [1] }), /children is not valid/)
    refused(card({ id: 'root', component: 'List', children: { componentId: 'x' } }), /children is not valid/)
    refused(card({ id: 'root', component: 'List', children: { componentId: 'x', path: '/a', extra: 1 } }), /children is not valid/)
    refused(card({ id: 'root', component: 'Tabs', tabs: [] }), /tabs is not valid/)
    refused(card({ id: 'root', component: 'Tabs', tabs: [{ title: 'a' }] }), /tabs is not valid/)
    refused(card({ id: 'root', component: 'Icon', name: 'nope' }), /name is not valid/)
    refused(card({ id: 'root', component: 'Icon', name: { svgPath: 'M0', x: 1 } }), /name is not valid/)
    refused(card({ id: 'root', component: 'Divider', axis: 'diagonal' }), /axis is not valid/)
    refused(card(text('root', { '@accessibility': { live: 'loud' } })), /@accessibility is not valid/)
    refused(card(text('root', { '@metadata': { other: {} } })), /@metadata is not valid/)
    refused(card(text('root', { '@catalogId': 'basic' })), /@catalogId must be/)
    refused(card(text('root', { '@catalogId': 3 })), /@catalogId is not valid/)
  })

  it('accepts every display component in its documented forms', () => {
    const ok = card(
      { id: 'root', component: 'Column', children: ['t', 'i', 'ic', 'ic2', 'ic3', 'vid', 'aud', 'row', 'list', 'tabs', 'div', 'c'], justify: 'spaceBetween', align: 'stretch' },
      text('t', { variant: 'caption', weight: 1, '@catalogId': BASIC_CATALOG_ID, '@accessibility': { label: 'L', description: { '@path': '/d' }, live: 'polite', hidden: false }, '@metadata': { extensions: { a: 1 } } }),
      { id: 'i', component: 'Image', url: '/storage/a', description: 'pic', fit: 'cover', variant: 'avatar' },
      { id: 'ic', component: 'Icon', name: 'star' },
      { id: 'ic2', component: 'Icon', name: { svgPath: 'M0 0' } },
      { id: 'ic3', component: 'Icon', name: { '@path': '/icon' } },
      { id: 'vid', component: 'Video', url: 'https://bucket.example/v.mp4', posterUrl: '/api/v1/workspaces/a/tasks/b/attachments/c' },
      { id: 'aud', component: 'AudioPlayer', url: '/storage/a.mp3', description: 'clip' },
      { id: 'row', component: 'Row', children: [], justify: 'start', align: 'center' },
      { id: 'list', component: 'List', children: { componentId: 'item', path: '/items' }, direction: 'horizontal', align: 'end' },
      text('item', { text: { '@call': 'formatString', args: { value: '${name}' } } }),
      { id: 'tabs', component: 'Tabs', tabs: [{ title: { '@path': '/tab' }, child: 'item' }] },
      { id: 'div', component: 'Divider', axis: 'vertical' },
      { id: 'c', component: 'Card', child: 'item' },
    )
    expect(check(ok)).toEqual({ ok: true, problems: [] })
  })

  it('takes media only as a stored link', () => {
    for (const url of ['attachment:a.png', 'http://example.com/a.png', 'data:image/png;base64,x', 'javascript:x', '//evil/a', '/other/a', 'https:///a']) {
      refused(card({ id: 'root', component: 'Image', url }), /url must be a stored file/)
    }
    refused(card({ id: 'root', component: 'Image', url: { '@path': '/u' } }), /url must be a stored file/)
    refused(card({ id: 'root', component: 'Video', url: '/storage/v', posterUrl: 'attachment:p.png' }), /posterUrl must be a stored file/)
    refused(card({ id: 'root', component: 'AudioPlayer', url: 'attachment:a.mp3' }), /url must be a stored file/)
  })

  it('checks dynamic values and function calls', () => {
    const t = (value) => card(text('root', { text: value }))
    expect(check(t({ '@path': '/a' })).ok).toBe(true)
    expect(check(t({ '@call': 'formatString', args: { value: 'x' }, catalogId: BASIC_CATALOG_ID })).ok).toBe(true)
    expect(check(t({ '@call': '@index' })).ok).toBe(true)
    expect(check(t({ '@call': 'and', args: { values: [true, { '@path': '/b' }, { nested: [1, { '@@literal': 2 }] }] } })).ok).toBe(true)
    refused(t({ '@path': 3 }), /text is not valid/)
    refused(t({ '@path': '/a', extra: 1 }), /text is not valid/)
    refused(t({ '@call': 'eval' }), /text is not valid/)
    refused(t({ '@call': 'not', other: 1 }), /text is not valid/)
    refused(t({ '@call': 'not', catalogId: 'basic' }), /text is not valid/)
    refused(t({ '@call': 'not', args: [] }), /text is not valid/)
    refused(t({ '@call': 'and', args: { values: [{ '@unknown': 1 }] } }), /text is not valid/)
    refused(t({ '@call': 'not', args: { value: { '@': 1 } } }), /text is not valid/)
    refused(t({ '@call': 'not', args: { value: undefined } }), /text is not valid/)
    refused(t({ plain: 'object' }), /text is not valid/)
  })

  it('checks the input components when interactive', () => {
    const i = { interactive: true }
    const ok = card(
      { id: 'root', component: 'Column', children: ['b', 'b2', 'tf', 'cb', 'cp', 'sl', 'dt', 'm'] },
      { id: 'b', component: 'Button', child: 'l', variant: 'primary', action: { event: { name: 'go', userMessage: 'Go', context: { a: { '@path': '/a' } } } }, '@checks': [{ condition: { '@call': 'required', args: { value: { '@path': '/a' } } }, message: 'm' }] },
      { id: 'b2', component: 'Button', child: 'l', action: { functionCall: { '@call': 'openUrl', args: { url: 'https://x.example' } } } },
      text('l'),
      { id: 'tf', component: 'TextField', label: 'Name', value: { '@path': '/n' }, placeholder: 'p', variant: 'longText', '@checks': [{ condition: { '@path': '/ok' } }] },
      { id: 'cb', component: 'CheckBox', label: 'Yes', value: true },
      { id: 'cp', component: 'ChoicePicker', label: 'Pick', variant: 'multipleSelection', options: [{ label: 'A', value: 'a' }], value: ['a'], displayStyle: 'chips', filterable: true },
      { id: 'sl', component: 'Slider', label: 'S', min: 0, max: 10, value: 3, steps: 2 },
      { id: 'dt', component: 'DateTimeInput', value: '2026-01-01', enableDate: true, enableTime: false, min: '2025-01-01', max: { '@path': '/max' }, label: 'When' },
      { id: 'm', component: 'Modal', trigger: 'l', content: 'tf' },
    )
    expect(check(ok, i)).toEqual({ ok: true, problems: [] })
    const bad = (c, pattern) => refused(card({ ...c, id: 'root' }), pattern, i)
    bad({ component: 'Button', child: 'root', action: { event: {} } }, /action is not valid/)
    bad({ component: 'Button', child: 'root', action: { event: { name: 'x', context: [] } } }, /action is not valid/)
    bad({ component: 'Button', child: 'root', action: { event: { name: 'x', context: { a: undefined } } } }, /action is not valid/)
    bad({ component: 'Button', child: 'root', action: { functionCall: { '@call': 'eval' } } }, /action is not valid/)
    bad({ component: 'Button', child: 'root', action: { event: { name: 'x' } }, '@checks': [{ condition: true }] }, /@checks is not valid/)
    bad({ component: 'Slider', value: 1, max: 2, steps: 0 }, /steps is not valid/)
    bad({ component: 'Slider', value: 1, max: 2, steps: 1.5 }, /steps is not valid/)
    bad({ component: 'Slider', value: 1 }, /max is required/)
    bad({ component: 'ChoicePicker', options: [{ label: 'a' }], value: [] }, /options is not valid/)
    bad({ component: 'ChoicePicker', options: [], value: [1] }, /value is not valid/)
    bad({ component: 'CheckBox', label: 'x', value: 'yes' }, /value is not valid/)
  })
})

describe('a2ui validate: the component graph', () => {
  it('needs a root, and every named child to exist', () => {
    refused(card(text('main')), /no component has id "root"/)
    refused(card({ id: 'root', component: 'Card', child: 'gone' }), /root: names "gone"/)
    refused(card({ id: 'root', component: 'List', children: { componentId: 'gone', path: '/x' } }), /names "gone"/)
    refused(card({ id: 'root', component: 'Tabs', tabs: [{ title: 't', child: 'gone' }] }), /names "gone"/)
    refused(card({ id: 'root', component: 'Modal', trigger: 'gone', content: 'root' }), /names "gone"/, { interactive: true })
  })

  it('refuses an id used twice in one list, not across updates', () => {
    refused(card(text('root'), text('root')), /used twice/)
    expect(check([create(), update([text('root')]), update([text('root', { text: 'y' })])]).ok).toBe(true)
  })

  it('refuses cycles', () => {
    refused(card({ id: 'root', component: 'Card', child: 'a' }, { id: 'a', component: 'Card', child: 'root' }), /cycle/)
    refused(card({ id: 'root', component: 'Card', child: 'root' }), /cycle/)
    refused(card(text('root'), { id: 'a', component: 'Card', child: 'b' }, { id: 'b', component: 'Column', children: ['a'] }), /cycle/)
  })

  it('caps depth and the number of components', () => {
    const chain = (n) => Array.from({ length: n }, (_, k) => (k === n - 1 ? text(`c${k}`) : { id: `c${k}`, component: 'Card', child: `c${k + 1}` }))
    const asRoot = (n) => chain(n).map((c) => ({ ...c, id: c.id === 'c0' ? 'root' : c.id }))
    expect(check(card(...asRoot(MAX_DEPTH))).ok).toBe(true)
    refused(card(...asRoot(MAX_DEPTH + 1)), /deeper than/)
    const many = Array.from({ length: MAX_COMPONENTS + 1 }, (_, k) => text(k === 0 ? 'root' : `t${k}`))
    refused(card(...many), /more than 200/)
    expect(check(card(...many.slice(0, MAX_COMPONENTS))).ok).toBe(true)
  })
})

describe('a2ui validate: stored media links', () => {
  it('names our storage routes and https bucket links', () => {
    expect(isStoredMediaUrl('/storage/w-1/2/3')).toBe(true)
    expect(isStoredMediaUrl('/api/v1/workspaces/a/tasks/b/attachments/c')).toBe(true)
    expect(isStoredMediaUrl('https://bucket.example/k')).toBe(true)
    expect(isStoredMediaUrl('http://bucket.example/k')).toBe(false)
    expect(isStoredMediaUrl('/api/v1/other')).toBe(false)
    expect(isStoredMediaUrl(undefined)).toBe(false)
  })
})
