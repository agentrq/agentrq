// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * A card's Progress, from our catalog: checked as the server checks it, drawn
 * as a bar with its percent, and moved in place when the agent updates it.
 */

import { afterEach, describe, expect, it } from 'vitest'
import { createApp, h, nextTick, reactive } from 'vue'

import A2uiSurface from '../src/components/a2ui/A2uiSurface.vue'
import { progressPercent } from '../src/a2ui/render.js'
import { AGENTRQ_CATALOG_ID, BASIC_CATALOG_ID, validateMessages } from '../src/a2ui/validate.js'

const v = (body) => ({ version: '1.0', ...body })
const messages = (components, { catalogId = AGENTRQ_CATALOG_ID, dataModel } = {}) => [
  v({ createSurface: { surfaceId: 's1', catalogId } }),
  v({ updateComponents: { surfaceId: 's1', components: [{ id: 'root', component: 'Column', children: components.map((c) => c.id) }, ...components] } }),
  ...(dataModel ? [v({ updateDataModel: { surfaceId: 's1', path: '/', value: dataModel } })] : []),
]
const bar = (extra = {}) => ({ id: 'p', component: 'Progress', value: 3, max: 5, ...extra })
const metadata = (components, opts) => ({ type: 'a2ui', version: '1.0', surfaceId: 's1', messages: messages(components, opts) })

const apps = []
function mount(meta) {
  const el = document.createElement('div')
  document.body.appendChild(el)
  const props = reactive({ metadata: meta, text: 'the fallback' })
  const app = createApp({ render: () => h(A2uiSurface, { ...props }) })
  apps.push(app)
  app.mount(el)
  return { el, props }
}
const q = (el, test) => el.querySelector(`[data-test="${test}"]`)

afterEach(() => {
  while (apps.length) apps.pop().unmount()
  document.body.innerHTML = ''
})

describe('a Progress, checked', () => {
  const ok = (components, opts) => validateMessages(messages(components, opts))
  it('is drawn only on a surface in our catalog', () => {
    expect(ok([bar()]).ok).toBe(true)
    expect(ok([bar()], { catalogId: BASIC_CATALOG_ID }).problems[0]).toMatch(/component "Progress" is not allowed here/)
  })
  it('takes bound values, and refuses what the server refuses', () => {
    expect(ok([bar({ label: { '@path': '/l' }, value: { '@path': '/done' }, max: { '@path': '/total' } })]).ok).toBe(true)
    expect(ok([{ id: 'p', component: 'Progress' }]).problems[0]).toMatch(/value is required/)
    expect(ok([bar({ value: -1 })]).ok).toBe(false)
    expect(ok([bar({ max: 0 })]).ok).toBe(false)
    expect(ok([bar({ color: 'red' })]).ok).toBe(false)
  })
})

describe('progressPercent', () => {
  it('is value of max, rounded and clamped, of 100 without a usable max', () => {
    expect(progressPercent(3, 5)).toBe(60)
    expect(progressPercent(1, 3)).toBe(33)
    expect(progressPercent(42)).toBe(42)
    expect(progressPercent(42, 0)).toBe(42)
    expect(progressPercent(42, 'x')).toBe(42)
    expect(progressPercent(9, 5)).toBe(100)
    expect(progressPercent(-2, 5)).toBe(0)
  })
  it('is null when value is not a number', () => {
    expect(progressPercent(undefined, 5)).toBeNull()
    expect(progressPercent('3', 5)).toBeNull()
    expect(progressPercent(NaN)).toBeNull()
  })
})

describe('A2uiProgress', () => {
  it('draws the bar to its percent, with its label', () => {
    const { el } = mount(metadata([bar({ label: '3 of 5 checks' })]))
    expect(q(el, 'a2ui-progress').textContent).toContain('3 of 5 checks')
    expect(q(el, 'a2ui-progress-value').textContent).toBe('60%')
    expect(q(el, 'a2ui-progress-bar').style.width).toBe('60%')
    const role = el.querySelector('[role="progressbar"]')
    expect(role.getAttribute('aria-valuenow')).toBe('60')
    expect(role.getAttribute('aria-label')).toBe('3 of 5 checks')
  })

  it('shows no percent, and an empty bar, while its value is unknown', () => {
    const { el } = mount(metadata([bar({ value: { '@path': '/none' } })]))
    expect(q(el, 'a2ui-progress-value')).toBeNull()
    expect(q(el, 'a2ui-progress').querySelector('.mb-1\\.5')).toBeNull()
    expect(q(el, 'a2ui-progress-bar').style.width).toBe('0%')
    const role = el.querySelector('[role="progressbar"]')
    expect(role.hasAttribute('aria-valuenow')).toBe(false)
    expect(role.getAttribute('aria-label')).toBe('Progress')
  })

  it('moves in place as the agent updates the data it is bound to', async () => {
    const bound = [bar({ label: { '@call': 'formatString', args: { value: '${/done} of ${/total} checks' } }, value: { '@path': '/done' }, max: { '@path': '/total' } })]
    const { el, props } = mount(metadata(bound, { dataModel: { done: 1, total: 4 } }))
    const drawn = q(el, 'a2ui-progress-bar')
    expect(drawn.style.width).toBe('25%')

    props.metadata = metadata(bound, { dataModel: { done: 3, total: 4 } })
    await nextTick()
    expect(q(el, 'a2ui-progress-bar'), 'the same bar, not drawn again').toBe(drawn)
    expect(drawn.style.width).toBe('75%')
    expect(q(el, 'a2ui-progress').textContent).toContain('3 of 4 checks')
  })
})
