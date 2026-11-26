// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/** A pinned progress card: its bar, its fold, its unpin, and its Buttons. */

import { describe, it, expect, afterEach } from 'vitest'
import { createApp, h, nextTick } from 'vue'
import A2uiPinnedCard from '../src/components/a2ui/A2uiPinnedCard.vue'

const CATALOG = 'https://a2ui.org/specification/v1_0/catalogs/basic/catalog.json'
const message = (text) => ({
  id: 'm1',
  text,
  metadata: {
    type: 'a2ui',
    messages: [
      { version: '1.0', createSurface: { surfaceId: 's', catalogId: CATALOG } },
      { version: '1.0', updateComponents: { surfaceId: 's', components: [
        { id: 'root', component: 'Button', child: 't', action: { event: { name: 'go' } } },
        { id: 't', component: 'Text', text: 'Go' },
      ] } },
    ],
  },
})

const apps = []
afterEach(() => { while (apps.length) apps.pop().unmount() })

async function mount(props) {
  const events = []
  const el = document.createElement('div')
  const app = createApp({ render: () => h(A2uiPinnedCard, {
    ...props,
    onToggle: () => events.push(['toggle']),
    onUnpin: () => events.push(['unpin']),
    onAction: (a) => events.push(['action', a]),
  }) })
  apps.push(app)
  app.mount(el)
  await nextTick()
  return { el, events }
}

describe('A2uiPinnedCard', () => {
  it('draws the card, and sends its fold, unpin and Button clicks on', async () => {
    const { el, events } = await mount({ message: message('Step 2 of 3') })
    expect(el.querySelector('[data-test="pinned-card"]').dataset.collapsed).toBeUndefined()
    expect(el.querySelector('[data-test="pinned-card-toggle"]').title).toBe('Collapse card')
    el.querySelector('[data-test="pinned-card-toggle"]').click()
    el.querySelector('[data-test="pinned-card-unpin"]').click()
    el.querySelector('[data-test="pinned-card-body"] button').click()
    expect(events.map((e) => e[0])).toEqual(['toggle', 'unpin', 'action'])
    expect(JSON.stringify(events[2][1])).toContain('go')
  })

  it('keeps only its top when collapsed', async () => {
    const { el } = await mount({ message: message('Step 2 of 3'), collapsed: true })
    expect(el.querySelector('[data-test="pinned-card"]').dataset.collapsed).toBe('true')
    expect(el.querySelector('[data-test="pinned-card-body"]').classList.contains('overflow-hidden')).toBe(true)
    expect(el.querySelector('[data-test="pinned-card-toggle"]').title).toBe('Expand card')
  })
})
