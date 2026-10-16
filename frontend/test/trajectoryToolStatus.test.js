// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * A native ACP agent's own tool calls carry how they ran — running, completed
 * or failed — rather than a permission verdict, and the trajectory names each.
 */

import { describe, it, expect, afterEach } from 'vitest'
import { createApp, h, nextTick } from 'vue'

import TrajectoryPanel from '../src/components/TrajectoryPanel.vue'

let app
let el

afterEach(() => {
  app?.unmount()
  el?.remove()
})

async function mountWith(status) {
  el = document.createElement('div')
  document.body.appendChild(el)
  app = createApp({
    render: () => h(TrajectoryPanel, {
      toolCalls: [{ id: 't1', toolName: 'Read a.go', description: 'Read a.go', inputPreview: '{}', status, createdAt: new Date().toISOString() }],
    }),
  })
  app.mount(el)
  await nextTick()
  await nextTick()
}

describe('a tool call\'s run status in the trajectory', () => {
  it.each([
    ['running', 'Running', 'text-amber-700', 'bg-amber-400'],
    ['completed', 'Completed', 'text-gray-600', 'bg-emerald-500'],
    ['failed', 'Failed', 'text-red-700', 'bg-red-500'],
  ])('%s reads as %s', async (status, label, badge, dot) => {
    await mountWith(status)
    const badgeEl = [...el.querySelectorAll('span')].find((s) => s.textContent.trim() === label && s.className.includes('uppercase'))
    expect(badgeEl, `no ${label} badge`).toBeTruthy()
    expect(badgeEl.className).toContain(badge)
    expect(el.querySelector(`button.${dot}`), `no ${dot} dot`).toBeTruthy()
  })
})
