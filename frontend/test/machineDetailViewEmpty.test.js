// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The one tab-default rule the machine page promises: a machine that has
 * never run a session opens on the form that starts one, not on an empty
 * Sessions list nobody asked to see.
 *
 * A separate file rather than a case in `machineDetailView.test.js`: that
 * file's `vi.mock('../src/api', ...)` is module-level and fixed to one
 * non-empty session list, and Vitest resolves ES module mocks once per file
 * rather than per test.
 */

import { describe, it, expect, vi } from 'vitest'
import { createApp, h } from 'vue'

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: 'm1' } }),
  useRouter: () => ({ push: vi.fn() }),
}))

vi.mock('../src/useEventBus', () => ({
  useEventBus: () => ({ connect() {}, disconnect() {}, onEvent() {} }),
}))

const MACHINE = {
  id: 'm1',
  name: 'workshop-pi',
  enabled: true,
  online: true,
  os: 'linux',
  arch: 'arm64',
  version: '0.7.1',
}

vi.mock('../src/api', () => ({
  getMachine: () => Promise.resolve({ machine: MACHINE }),
  fetchMachineSessions: () => Promise.resolve({ sessions: [] }),
  fetchWorkspaces: () => Promise.resolve({ workspaces: [] }),
  launchAgent: vi.fn(),
  fetchAcpAgents: () => Promise.resolve({ agents: [] }),
  fetchAcpModels: () => Promise.resolve({ agent: '', models: [] }),
  updateMachine: vi.fn(),
  deleteMachine: vi.fn(),
  killSession: vi.fn(),
  restartDaemon: vi.fn(),
  API_BASE_URL: '/api/v1',
}))

const { default: MachineDetailView } = await import('../src/views/MachineDetailView.vue')

const settle = () => new Promise((resolve) => setTimeout(resolve, 30))

describe('MachineDetailView: a machine with no sessions yet', () => {
  it('opens on the New Session tab rather than an empty Sessions list', async () => {
    const el = document.createElement('div')
    document.body.appendChild(el)
    const app = createApp({ render: () => h(MachineDetailView) })
    app.component('RouterLink', {
      props: ['to'],
      setup: (p, { slots }) => () => h('a', {}, slots.default?.()),
    })
    app.mount(el)
    await settle()

    const headings = [...el.querySelectorAll('h2')].map((h2) => h2.textContent.trim())
    expect(headings).toContain('Run an agent here')
    expect(headings.some((t) => t.startsWith('Sessions'))).toBe(false)
  })
})
