// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The card renderer, mounted: one block per display entry of the basic
 * catalog, the placeholder for anything else, the fallback to the message's
 * text, and every official display-only example drawn without a placeholder.
 */

import { afterEach, describe, expect, it } from 'vitest'
import { createApp, h, nextTick } from 'vue'
import { readdirSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import A2uiSurface from '../src/components/a2ui/A2uiSurface.vue'
import { ICON_PATHS } from '../src/a2ui/icons.js'
import { BASIC_CATALOG_ID, DISPLAY_COMPONENTS } from '../src/a2ui/validate.js'

const STORED = '/api/v1/workspaces/w1/tasks/t1/attachments/a1'
const BUCKET = 'https://bucket.example/w1/t1/a2'

const card = (components, dataModel) => ({
  type: 'a2ui',
  version: '1.0',
  surfaceId: 's1',
  messages: [
    { version: '1.0', createSurface: { surfaceId: 's1', catalogId: BASIC_CATALOG_ID } },
    { version: '1.0', updateComponents: { surfaceId: 's1', components } },
    ...(dataModel ? [{ version: '1.0', updateDataModel: { surfaceId: 's1', path: '/', value: dataModel } }] : []),
  ],
})

const apps = []
function mount(metadata, props = {}) {
  const el = document.createElement('div')
  document.body.appendChild(el)
  const app = createApp({ render: () => h(A2uiSurface, { metadata, text: 'the fallback', ...props }) })
  apps.push(app)
  app.mount(el)
  return el
}
const q = (el, test) => el.querySelector(`[data-test="${test}"]`)
const qa = (el, test) => [...el.querySelectorAll(`[data-test="${test}"]`)]

afterEach(() => {
  while (apps.length) apps.pop().unmount()
  document.body.innerHTML = ''
})

describe('A2uiSurface', () => {
  it('draws the card from its root', () => {
    const el = mount(card([{ id: 'root', component: 'Text', text: 'Hello' }]))
    expect(q(el, 'a2ui-surface').dataset.surfaceId).toBe('s1')
    expect(q(el, 'a2ui-text').textContent.trim()).toBe('Hello')
    expect(el.textContent).not.toContain('the fallback')
  })

  it('shows the text when the card fails the check', () => {
    const el = mount(card([{ id: 'root', component: 'Button', child: 'x', action: {} }]))
    expect(q(el, 'a2ui-surface')).toBeNull()
    expect(el.textContent).toContain('the fallback')
  })

  it('shows the text when there is no card at all', () => {
    expect(mount(null).textContent).toContain('the fallback')
    expect(mount({ type: 'a2ui' }).textContent).toContain('the fallback')
  })
})

describe('an unknown component', () => {
  it('draws a small placeholder, never a crash', () => {
    const metadata = card([{ id: 'root', component: 'Column', children: ['t'] }, { id: 't', component: 'Text', text: 'x' }])
    const el = mount(metadata)
    // A component that passed the check but has no renderer, as a newer
    // catalog entry would: change it after the check, as a later update would.
    const surface = apps[0]._instance.subTree.component.setupState.surface
    surface.components.set('t', { id: 't', component: 'Carousel' })
    return nextTick().then(() => {
      expect(q(el, 'a2ui-unknown').textContent).toContain('Carousel is not shown here')
    })
  })

  it('names the id when the component is missing', async () => {
    const el = mount(card([{ id: 'root', component: 'Column', children: ['t'] }, { id: 't', component: 'Text', text: 'x' }]))
    apps[0]._instance.subTree.component.setupState.surface.components.delete('t')
    await nextTick()
    expect(q(el, 'a2ui-unknown').textContent).toContain('t is not shown here')
  })
})

describe('Text', () => {
  it('renders markdown, bound values and the caption variant', () => {
    const el = mount(card([
      { id: 'root', component: 'Column', children: ['a', 'b', 'c'] },
      { id: 'a', component: 'Text', text: '## Title\n**bold**' },
      { id: 'b', component: 'Text', text: { '@path': '/n' }, variant: 'caption' },
      { id: 'c', component: 'Text', text: { '@call': 'formatString', args: { value: 'Hi ${/name}' } } },
    ], { n: 42, name: 'Ada' }))
    const [a, b, c] = qa(el, 'a2ui-text')
    expect(a.querySelector('h2').textContent).toBe('Title')
    expect(a.querySelector('strong').textContent).toBe('bold')
    expect(a.classList.contains('text-[13px]')).toBe(true)
    expect(b.textContent.trim()).toBe('42')
    expect(b.classList.contains('text-[11px]')).toBe(true)
    expect(c.textContent.trim()).toBe('Hi Ada')
  })

  it('draws no image or link from a bound value', () => {
    const el = mount(card([{ id: 'root', component: 'Text', text: { '@path': '/t' } }],
      { t: '![x](https://tracker.example/p.png) [go](https://example.com) <img src=x onerror=alert(1)>' }))
    expect(el.querySelector('img')).toBeNull()
    expect(el.querySelector('a')).toBeNull()
  })
})

describe('Image', () => {
  it('draws a stored file with its description, size and fit', () => {
    const el = mount(card([{ id: 'root', component: 'Image', url: STORED, description: 'Chart', variant: 'avatar', fit: 'contain' }]))
    const img = q(el, 'a2ui-image')
    expect(img.getAttribute('src')).toBe(STORED)
    expect(img.getAttribute('alt')).toBe('Chart')
    expect(img.classList.contains('rounded-full')).toBe(true)
    expect(img.classList.contains('object-contain')).toBe(true)
  })

  it('draws a bucket link that is one of the message attachments', () => {
    const el = mount(card([{ id: 'root', component: 'Image', url: BUCKET }]), { attachments: [{ url: BUCKET }] })
    expect(q(el, 'a2ui-image').getAttribute('src')).toBe(BUCKET)
  })

  it('draws an empty frame for a link of no attachment', () => {
    const el = mount(card([{ id: 'root', component: 'Image', url: BUCKET, description: 'Chart' }]))
    expect(q(el, 'a2ui-image')).toBeNull()
    expect(q(el, 'a2ui-media-refused').getAttribute('aria-label')).toBe('Chart')
    const bare = mount(card([{ id: 'root', component: 'Image', url: BUCKET }]))
    expect(q(bare, 'a2ui-media-refused').getAttribute('aria-label')).toBe('Image not shown')
  })
})

describe('Video', () => {
  it('draws a stored file with a stored poster', () => {
    const el = mount(card([{ id: 'root', component: 'Video', url: STORED, posterUrl: '/storage/p.png' }]))
    const video = q(el, 'a2ui-video')
    expect(video.getAttribute('src')).toBe(STORED)
    expect(video.getAttribute('poster')).toBe('/storage/p.png')
    expect(video.hasAttribute('controls')).toBe(true)
  })

  it('leaves out a poster of no attachment, and refuses a link of none', () => {
    const el = mount(card([{ id: 'root', component: 'Video', url: STORED, posterUrl: BUCKET }]))
    expect(q(el, 'a2ui-video').hasAttribute('poster')).toBe(false)
    const refused = mount(card([{ id: 'root', component: 'Video', url: BUCKET }]))
    expect(q(refused, 'a2ui-video')).toBeNull()
    expect(q(refused, 'a2ui-media-refused').textContent).toBe('Video not shown')
  })
})

describe('AudioPlayer', () => {
  it('draws a stored file under its description', () => {
    const el = mount(card([{ id: 'root', component: 'AudioPlayer', url: STORED, description: 'Episode 1' }]))
    expect(q(el, 'a2ui-audio').getAttribute('src')).toBe(STORED)
    expect(el.textContent).toContain('Episode 1')
  })

  it('refuses a link of no attachment, with no description', () => {
    const el = mount(card([{ id: 'root', component: 'AudioPlayer', url: BUCKET }]))
    expect(q(el, 'a2ui-audio')).toBeNull()
    expect(q(el, 'a2ui-media-refused').textContent).toBe('Audio not shown')
    expect(el.querySelector('span')).toBeNull()
  })
})

describe('Icon', () => {
  it('draws a catalog name as an outline, a custom path filled, and nothing for an unknown name', () => {
    const el = mount(card([
      { id: 'root', component: 'Row', children: ['a', 'b', 'c'] },
      { id: 'a', component: 'Icon', name: 'check' },
      { id: 'b', component: 'Icon', name: { svgPath: 'M0 0h24v24H0z' } },
      { id: 'c', component: 'Icon', name: { '@path': '/icon' } },
    ], { icon: 'notAnIcon' }))
    const [a, b, c] = qa(el, 'a2ui-icon')
    expect(a.querySelector('path').getAttribute('d')).toBe(ICON_PATHS.check)
    expect(a.querySelector('path').getAttribute('stroke')).toBe('currentColor')
    expect(b.querySelector('path').getAttribute('d')).toBe('M0 0h24v24H0z')
    expect(b.querySelector('path').getAttribute('fill')).toBe('currentColor')
    expect(c.querySelector('path')).toBeNull()
  })
})

describe('Row and Column', () => {
  it('lay children out by justify, align and weight', () => {
    const el = mount(card([
      { id: 'root', component: 'Column', children: ['row'], justify: 'center', align: 'start' },
      { id: 'row', component: 'Row', children: ['a', 'b'], justify: 'spaceBetween', align: 'center' },
      { id: 'a', component: 'Text', text: 'A', weight: 2 },
      { id: 'b', component: 'Text', text: 'B' },
    ]))
    const column = q(el, 'a2ui-column')
    expect(column.classList.contains('flex-col')).toBe(true)
    expect(column.classList.contains('justify-center')).toBe(true)
    expect(column.classList.contains('items-start')).toBe(true)
    const row = q(el, 'a2ui-row')
    expect(row.classList.contains('justify-between')).toBe(true)
    expect(row.classList.contains('items-center')).toBe(true)
    // Narrow cards wrap; from 24rem of card the row stays on one line.
    expect(row.classList.contains('flex-wrap')).toBe(true)
    expect(row.classList.contains('@min-[24rem]/a2ui:flex-nowrap')).toBe(true)
    const [a, b] = qa(el, 'a2ui-text')
    expect(a.style.flex).toBe('2 1 0%')
    expect(b.style.flex).toBe('')
  })

  it('pass a component its accessibility attributes', () => {
    const el = mount(card([{ id: 'root', component: 'Row', children: [], '@accessibility': { label: 'Header' } }]))
    expect(q(el, 'a2ui-row').getAttribute('aria-label')).toBe('Header')
  })
})

describe('List', () => {
  it('draws a static list vertically', () => {
    const el = mount(card([
      { id: 'root', component: 'List', children: ['a', 'b'] },
      { id: 'a', component: 'Text', text: 'A' },
      { id: 'b', component: 'Text', text: 'B' },
    ]))
    const list = q(el, 'a2ui-list')
    expect(list.classList.contains('flex-col')).toBe(true)
    expect([...list.querySelectorAll('[role="listitem"]')].map((i) => i.textContent.trim())).toEqual(['A', 'B'])
  })

  it('draws a template once per item, horizontally', () => {
    const el = mount(card([
      { id: 'root', component: 'List', direction: 'horizontal', align: 'center', children: { componentId: 'item', path: '/people' } },
      { id: 'item', component: 'Text', text: { '@call': 'formatString', args: { value: '${@index()}: ${name}' } } },
    ], { people: [{ name: 'Ada' }, { name: 'Linus' }] }))
    const list = q(el, 'a2ui-list')
    expect(list.classList.contains('flex-row')).toBe(true)
    expect(list.classList.contains('items-center')).toBe(true)
    const items = [...list.querySelectorAll('[role="listitem"]')]
    expect(items.map((i) => i.textContent.trim())).toEqual(['0: Ada', '1: Linus'])
    expect(items[0].classList.contains('shrink-0')).toBe(true)
  })
})

describe('Card', () => {
  it('frames its child in the app card style', () => {
    const el = mount(card([{ id: 'root', component: 'Card', child: 't' }, { id: 't', component: 'Text', text: 'Inside' }]))
    const frame = q(el, 'a2ui-card')
    expect(frame.classList.contains('rounded-xl')).toBe(true)
    expect(frame.classList.contains('dark:bg-zinc-900')).toBe(true)
    expect(frame.textContent.trim()).toBe('Inside')
  })
})

describe('Tabs', () => {
  it('shows the first tab, and another when its pill is picked', async () => {
    const el = mount(card([
      { id: 'root', component: 'Tabs', tabs: [{ title: 'One', child: 'a' }, { title: { '@path': '/second' }, child: 'b' }] },
      { id: 'a', component: 'Text', text: 'First' },
      { id: 'b', component: 'Text', text: 'Second' },
    ], { second: 'Two' }))
    const tabs = q(el, 'a2ui-tabs')
    expect(tabs.querySelector('[role="radiogroup"]').textContent.replace(/\s+/g, ' ').trim()).toBe('One Two')
    expect(qa(el, 'a2ui-text').map((t) => t.textContent.trim())).toEqual(['First'])
    const radio = q(el, 'a2ui-tab-1').querySelector('input')
    radio.checked = true
    radio.dispatchEvent(new Event('change'))
    await nextTick()
    expect(qa(el, 'a2ui-text').map((t) => t.textContent.trim())).toEqual(['Second'])
  })

  it('falls back to the first tab when the picked one is gone', async () => {
    const el = mount(card([
      { id: 'root', component: 'Tabs', tabs: [{ title: 'One', child: 'a' }, { title: 'Two', child: 'b' }] },
      { id: 'a', component: 'Text', text: 'First' },
      { id: 'b', component: 'Text', text: 'Second' },
    ]))
    const radio = q(el, 'a2ui-tab-1').querySelector('input')
    radio.checked = true
    radio.dispatchEvent(new Event('change'))
    await nextTick()
    apps[0]._instance.subTree.component.setupState.surface.components.get('root').tabs.pop()
    await nextTick()
    expect(qa(el, 'a2ui-text').map((t) => t.textContent.trim())).toEqual(['First'])
  })
})

describe('Divider', () => {
  it('draws across by default and down when vertical', () => {
    const el = mount(card([
      { id: 'root', component: 'Row', children: ['h', 'v'] },
      { id: 'h', component: 'Divider' },
      { id: 'v', component: 'Divider', axis: 'vertical' },
    ]))
    const [across, down] = qa(el, 'a2ui-divider')
    expect(across.tagName).toBe('HR')
    expect(down.getAttribute('aria-orientation')).toBe('vertical')
  })
})

describe('the official examples', () => {
  const dir = resolve(__dirname, '../../backend/internal/service/a2ui/testdata/examples')
  const MEDIA = { Image: ['url'], Video: ['url', 'posterUrl'], AudioPlayer: ['url'] }
  const examples = readdirSync(dir).filter((f) => f.endsWith('.json')).map((f) => ({ f, ...JSON.parse(readFileSync(resolve(dir, f), 'utf8')) }))
  // The server stores media only as our own links: stand one in for each.
  const stored = (messages) => messages.map((m) => (m.updateComponents
    ? { ...m, updateComponents: { ...m.updateComponents, components: m.updateComponents.components.map((c) => {
      const out = { ...c }
      for (const p of MEDIA[c.component] ?? []) if (p in out) out[p] = STORED
      return out
    }) } }
    : m))
  const displayOnly = examples.filter((e) => e.messages.every((m) =>
    !m.updateComponents || m.updateComponents.components.every((c) => DISPLAY_COMPONENTS.includes(c.component))))

  it('has display-only examples to draw', () => {
    expect(displayOnly.length).toBeGreaterThan(15)
  })

  it.each(displayOnly.map((e) => [e.f, e]))('draws %s with no placeholder', (_, e) => {
    const el = mount({ type: 'a2ui', messages: stored(e.messages) })
    expect(q(el, 'a2ui-surface')).not.toBeNull()
    expect(q(el, 'a2ui-unknown')).toBeNull()
    expect(q(el, 'a2ui-media-refused')).toBeNull()
  })
})
