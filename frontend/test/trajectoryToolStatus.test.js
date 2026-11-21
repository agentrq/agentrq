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

async function mountWith(status, toolCalls) {
  el = document.createElement('div')
  document.body.appendChild(el)
  app = createApp({
    render: () => h(TrajectoryPanel, {
      toolCalls: toolCalls ?? [{ id: 't1', toolName: 'Read a.go', description: 'Read a.go', inputPreview: '{}', status, createdAt: new Date().toISOString() }],
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

describe('a failed tool call', () => {
  const at = (s) => new Date(Date.UTC(2026, 10, 21, 12, 0, s)).toISOString()
  const calls = [
    { id: 't1', toolName: 'Bash', inputPreview: '{}', status: 'failed', output: 'exit status 1', createdAt: at(0) },
    { id: 't2', toolName: 'Read', inputPreview: '{}', status: 'completed', createdAt: at(1) },
  ]
  const rows = () => [...el.querySelectorAll('[data-trajectory-id]')].map((r) => r.dataset.trajectoryId)

  it('shows what it said under its summary', async () => {
    await mountWith(null, calls)
    el.querySelector('[data-trajectory-id="t-t1"]').click()
    await nextTick()
    expect(el.querySelector('[data-testid="trajectory-tool-output"]').textContent).toBe('exit status 1')
    el.querySelector('[data-trajectory-id="t-t2"]').click()
    await nextTick()
    expect(el.querySelector('[data-testid="trajectory-tool-output"]')).toBeNull()
  })

  it('can be the only thing listed, and the filter toggles off', async () => {
    await mountWith(null, calls)
    const chip = el.querySelector('[data-testid="trajectory-failed-filter"]')
    expect(chip.textContent).toContain('1')
    expect(chip.className).toContain('text-red-600')
    chip.click()
    await nextTick()
    expect(rows()).toEqual(['t-t1'])
    expect(chip.className).toContain('bg-red-600')
    chip.click()
    await nextTick()
    expect(rows()).toEqual(['t-t1', 't-t2'])
  })

  it('offers no failed filter when nothing failed', async () => {
    await mountWith('completed')
    expect(el.querySelector('[data-testid="trajectory-failed-filter"]')).toBeNull()
  })
})
