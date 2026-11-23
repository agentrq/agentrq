// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * An agentrqd too old for workspace forks reads in red, with the hint written
 * out beside it, on both machines pages. The rule itself is tested in
 * `machineFormat.test.js`; this is the wiring.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h } from 'vue'

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: 'm1' } }),
  useRouter: () => ({ push: vi.fn() }),
}))
vi.mock('../src/useEventBus', () => ({
  useEventBus: () => ({ connect() {}, disconnect() {}, onEvent() {} }),
}))

let machines = []
let machine = {}
vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  fetchMachines: () => Promise.resolve({ machines }),
  getMachine: () => Promise.resolve({ machine }),
  fetchMachineSessions: () => Promise.resolve({ sessions: [] }),
  fetchWorkspaces: () => Promise.resolve({ workspaces: [] }),
  fetchAcpAgents: () => Promise.resolve({ agents: [] }),
  fetchAcpModels: () => Promise.resolve({ agent: '', models: [] }),
}))

const { default: MachinesView } = await import('../src/views/MachinesView.vue')
const { default: MachineDetailView } = await import('../src/views/MachineDetailView.vue')
const { DAEMON_OUTDATED_HINT } = await import('../src/composables/useMachineFormat.js')

const settle = () => new Promise((resolve) => setTimeout(resolve, 30))

let app
async function mount(view) {
  const el = document.createElement('div')
  document.body.appendChild(el)
  app = createApp({ render: () => h(view) })
  app.component('RouterLink', { setup: (p, { slots }) => () => h('a', {}, slots.default?.()) })
  app.mount(el)
  await settle()
  return el
}

/** The span holding "agentrqd <version>". */
const versionSpan = (el) => [...el.querySelectorAll('span')].find((s) => s.textContent.startsWith('agentrqd '))
const red = (node) => node.className.includes('text-red-600')
const hints = (el) => [...el.querySelectorAll('p')].filter((p) => p.textContent.includes(DAEMON_OUTDATED_HINT))

beforeEach(() => {
  localStorage.clear()
})
afterEach(() => {
  app?.unmount()
  document.body.innerHTML = ''
})

const base = { id: 'm1', name: 'box', enabled: true, online: true, os: 'linux', arch: 'amd64' }

describe('MachinesView: an old agentrqd', () => {
  it('reads in red with the hint below it', async () => {
    machines = [{ ...base, version: '0.9.2' }]
    const el = await mount(MachinesView)
    expect(versionSpan(el).textContent).toBe('agentrqd 0.9.2')
    expect(red(versionSpan(el))).toBe(true)
    expect(hints(el)).toHaveLength(1)
  })

  it('is plain from 0.9.3 on, and when the version is unknown', async () => {
    machines = [
      { ...base, version: '0.9.3' },
      { ...base, id: 'm2', version: '0.9.10' },
      { ...base, id: 'm3', version: '' },
      { ...base, id: 'm4', version: 'dev' },
    ]
    const el = await mount(MachinesView)
    const spans = [...el.querySelectorAll('span')].filter((s) => s.textContent.startsWith('agentrqd '))
    expect(spans.map((s) => s.textContent)).toEqual(['agentrqd 0.9.3', 'agentrqd 0.9.10', 'agentrqd —', 'agentrqd dev'])
    expect(spans.some(red)).toBe(false)
    expect(hints(el)).toHaveLength(0)
  })
})

describe('MachineDetailView: an old agentrqd', () => {
  it('puts the hint in the update banner, which offers no button below 0.9.3', async () => {
    machine = { ...base, version: '0.9.2', availableVersion: '0.9.4' }
    const el = await mount(MachineDetailView)
    expect(red(versionSpan(el))).toBe(true)
    const [hint] = hints(el)
    expect(hints(el)).toHaveLength(1)
    const banner = hint.parentElement.parentElement
    expect(banner.textContent).toContain('agentrqd 0.9.4 is available')
    expect(banner.textContent).toContain('by hand')
    expect(banner.querySelectorAll('button')).toHaveLength(0)
  })

  it('says it under the version, with no update on offer', async () => {
    machine = { ...base, version: '0.9.2' }
    const el = await mount(MachineDetailView)
    expect(red(versionSpan(el))).toBe(true)
    const [hint] = hints(el)
    expect(hints(el)).toHaveLength(1)
    expect(hint.parentElement.textContent).toContain('agentrqd 0.9.2')
    expect(el.textContent).not.toContain('is available')
  })

  it('is plain on 0.9.3, even with an update on offer', async () => {
    machine = { ...base, version: '0.9.3', availableVersion: '0.9.4' }
    const el = await mount(MachineDetailView)
    expect(red(versionSpan(el))).toBe(false)
    expect(hints(el)).toHaveLength(0)
    expect(el.textContent).toContain('agentrqd 0.9.4 is available')
  })
})
