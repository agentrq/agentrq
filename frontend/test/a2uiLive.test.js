// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * An agent's update to a card on the page: patched into the surface, not
 * drawn again; the person's entries kept where the agent left the value
 * alone; what it changed marked for a moment.
 */

import { afterEach, describe, expect, it, vi } from 'vitest'
import { createApp, h, nextTick, reactive } from 'vue'

import A2uiSurface from '../src/components/a2ui/A2uiSurface.vue'
import { mergeDataModel, patchSurface, sameJSON, shownValues } from '../src/a2ui/live.js'
import { applyMessages } from '../src/a2ui/surface.js'
import { BASIC_CATALOG_ID } from '../src/a2ui/validate.js'

const v = (body) => ({ version: '1.0', ...body })
const surfaceOf = (components, dataModel, surfaceId = 's1') => applyMessages([
  v({ createSurface: { surfaceId, catalogId: BASIC_CATALOG_ID } }),
  v({ updateComponents: { surfaceId, components } }),
  v({ updateDataModel: { surfaceId, path: '/', value: dataModel } }),
])

describe('mergeDataModel', () => {
  it("keeps the person's value where the agent left it alone, and takes the agent's where it changed it", () => {
    const base = { step: 1, note: '', steps: [{ done: false }, { done: false }], who: { name: 'a' } }
    const local = { step: 1, note: 'typed', steps: [{ done: false }, { done: false }], who: { name: 'mine' }, extra: 'own' }
    const next = { step: 3, note: '', steps: [{ done: true }, { done: false }], who: { name: 'a' } }
    const { value, changed } = mergeDataModel(base, local, next)
    expect(value).toEqual({ step: 3, note: 'typed', steps: [{ done: true }, { done: false }], who: { name: 'mine' }, extra: 'own' })
    expect(changed).toEqual(['/step', '/steps/0/done'])
  })

  it('drops what the agent removed, and replaces a list whose length changed', () => {
    const { value, changed } = mergeDataModel({ a: 1, list: [1] }, { a: 2, list: [9] }, { list: [1, 2] })
    expect(value).toEqual({ list: [1, 2] })
    expect(changed).toEqual(['/list', '/a'])
  })

  it('takes the whole model when it is no object to merge into', () => {
    expect(mergeDataModel(null, {}, { a: 1 })).toEqual({ value: { a: 1 }, changed: [''] })
    expect(mergeDataModel({}, {}, [1])).toEqual({ value: {}, changed: [''] })
  })

  it('compares JSON values', () => {
    expect(sameJSON({ a: [1] }, { a: [1] })).toBe(true)
    expect(sameJSON({ a: 1 }, { a: 2 })).toBe(false)
  })
})

describe('patchSurface', () => {
  const comps = (label) => [
    { id: 'root', component: 'Column', children: ['t', 'gone'] },
    { id: 't', component: 'Text', text: label },
    { id: 'gone', component: 'Text', text: 'x' },
  ]

  it('changes, adds and removes components in the same state, and merges the data model', () => {
    const state = surfaceOf(comps('one'), { n: 1 })
    const before = state.components
    const next = surfaceOf([{ id: 'root', component: 'Column', children: ['t', 'new'] }, { id: 't', component: 'Text', text: 'two' },
      { id: 'new', component: 'Text', text: 'n' }], { n: 2 })
    const changes = patchSurface(state, { n: 1 }, next)
    expect(changes).toEqual({ paths: ['/n'], ids: ['root', 't'] })
    expect(state.components).toBe(before)
    expect([...state.components.keys()]).toEqual(['root', 't', 'new'])
    expect(state.components.get('t').text).toBe('two')
    expect(state.dataModel).toEqual({ n: 2 })
  })

  it('cannot patch another surface, or none', () => {
    const state = surfaceOf(comps('one'), {})
    expect(patchSurface(state, {}, surfaceOf(comps('one'), {}, 's2'))).toBe(null)
    expect(patchSurface(state, {}, null)).toBe(null)
    expect(patchSurface(null, {}, state)).toBe(null)
  })
})

describe('shownValues', () => {
  const ctx = { surface: { dataModel: { n: 2, steps: ['a', 'b'] } }, scope: [] }
  it('is what a component shows, its bindings and calls resolved, its action left out', () => {
    const c = { id: 'x', component: 'Text', text: { '@call': 'formatString', args: { value: '${/n} of ${/steps/1}' } } }
    expect(shownValues(c, ctx)).toBe(JSON.stringify({ id: 'x', component: 'Text', text: '2 of b' }))
    expect(shownValues({ id: 'b', component: 'Button', child: 'l', action: { functionCall: { '@call': 'openUrl', args: { url: 'https://x' } } } }, ctx))
      .toBe(JSON.stringify({ id: 'b', component: 'Button', child: 'l' }))
  })

  it('is nothing for a layout component, or none', () => {
    expect(shownValues({ id: 'r', component: 'Column', children: [] }, ctx)).toBe('')
    expect(shownValues(undefined, ctx)).toBe('')
  })
})

// A progress card, mounted, as the task page draws it.
const progress = (done, extra = {}) => ({
  type: 'a2ui',
  version: '1.0',
  surfaceId: 'p',
  messages: [
    v({ createSurface: { surfaceId: 'p', catalogId: BASIC_CATALOG_ID } }),
    v({ updateComponents: { surfaceId: 'p', components: [
      { id: 'root', component: 'Column', children: ['status', 'note'] },
      { id: 'status', component: 'Text', text: { '@call': 'formatString', args: { value: 'Step ${/done} of 3' } } },
      { id: 'note', component: 'TextField', label: 'Note', value: { '@path': '/note' } },
    ] } }),
    v({ updateDataModel: { surfaceId: 'p', path: '/', value: { done, note: '' } } }),
  ],
  ...extra,
})

const apps = []
function mount(metadata) {
  const el = document.createElement('div')
  document.body.appendChild(el)
  const props = reactive({ metadata, text: 'the fallback' })
  const app = createApp({ render: () => h(A2uiSurface, { ...props }) })
  apps.push(app)
  app.mount(el)
  return { el, props }
}
const q = (el, test) => el.querySelector(`[data-test="${test}"]`)

afterEach(() => {
  while (apps.length) apps.pop().unmount()
  document.body.innerHTML = ''
  vi.useRealTimers()
})

describe('A2uiSurface, updated in place', () => {
  it('patches the drawn card, keeps what the person typed, and marks the changed value for a moment', async () => {
    vi.useFakeTimers()
    const { el, props } = mount(progress(1))
    const status = q(el, 'a2ui-text')
    const input = q(el, 'a2ui-textfield').querySelector('input')
    input.value = 'my note'
    input.dispatchEvent(new Event('input'))
    await nextTick()

    props.metadata = progress(3)
    await nextTick()
    expect(q(el, 'a2ui-text'), 'the same element, not drawn again').toBe(status)
    expect(status.textContent.trim()).toBe('Step 3 of 3')
    expect(input.value, 'what the person typed').toBe('my note')
    expect(status.dataset.changed).toBe('true')
    expect(status.className).toContain('a2ui-changed')
    expect(q(el, 'a2ui-textfield').dataset.changed, 'an input the agent left alone').toBeUndefined()

    vi.advanceTimersByTime(1600)
    await nextTick()
    expect(status.dataset.changed).toBeUndefined()
  })

  it("takes the agent's value for an input it changed, and marks nothing when nothing changed", async () => {
    const { el, props } = mount(progress(1))
    const input = q(el, 'a2ui-textfield').querySelector('input')
    input.value = 'mine'
    input.dispatchEvent(new Event('input'))
    await nextTick()

    const next = progress(1)
    next.messages[2].updateDataModel.value.note = 'from the agent'
    props.metadata = next
    await nextTick()
    expect(input.value).toBe('from the agent')
    expect(q(el, 'a2ui-textfield').dataset.changed).toBe('true')

    // An answered mark alone changes no value.
    props.metadata = { ...next, answered: { name: 'x' } }
    await nextTick()
    expect(q(el, 'a2ui-text').dataset.changed).toBeUndefined()
  })

  it('marks a component whose own definition changed, and nothing when the card draws the same', async () => {
    const { el, props } = mount(progress(1))
    const same = progress(1)
    same.messages.push(v({ updateDataModel: { surfaceId: 'p', path: '/done', value: 1 } }))
    props.metadata = same
    await nextTick()
    expect(q(el, 'a2ui-text').dataset.changed).toBeUndefined()

    const relabeled = progress(1)
    relabeled.messages[1].updateComponents.components[2].label = 'Your note'
    props.metadata = relabeled
    await nextTick()
    expect(q(el, 'a2ui-textfield').dataset.changed).toBe('true')
    expect(q(el, 'a2ui-text').dataset.changed).toBeUndefined()
  })

  it('draws a card anew when its surface changes, and shows the text once it is deleted', async () => {
    const { el, props } = mount(progress(1))
    const other = progress(2)
    other.messages = other.messages.map((m) => JSON.parse(JSON.stringify(m).replaceAll('"p"', '"p2"')))
    props.metadata = other
    await nextTick()
    expect(q(el, 'a2ui-surface').dataset.surfaceId).toBe('p2')
    expect(q(el, 'a2ui-text').textContent.trim()).toBe('Step 2 of 3')

    props.metadata = { ...other, messages: [...other.messages, v({ deleteSurface: { surfaceId: 'p2' } })] }
    await nextTick()
    expect(q(el, 'a2ui-surface')).toBe(null)
    expect(el.textContent).toContain('the fallback')
  })

  it("draws a question's card anew when it is answered", async () => {
    const asked = { ...progress(1), type: 'elicitation_request', mode: 'a2ui', status: 'pending' }
    const { el, props } = mount(asked)
    props.metadata = { ...asked, status: 'accepted', content: { name: 'go', dataModel: { done: 2, note: 'sent' } } }
    await nextTick()
    expect(q(el, 'a2ui-text').textContent.trim()).toBe('Step 2 of 3')
    expect(q(el, 'a2ui-text').dataset.changed).toBeUndefined()
  })
})
