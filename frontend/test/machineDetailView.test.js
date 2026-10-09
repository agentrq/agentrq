// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * What the machine page does after you start an agent on it.
 *
 * The rules of the launch itself are tested in `agentLaunch.test.js`, and
 * where a terminal lives in `terminalView.test.js`. This is the join between
 * them, and it is the whole point of the page: an agent's first minute is when
 * it asks the questions that stop it dead — trust this folder, allow this
 * tool, paste a key — and a pseudo-terminal is the only place those appear.
 * Launching and staying put leaves somebody watching a row say "starting"
 * while the agent waits for an answer to a question nobody can see it asking.
 *
 * Mounted with plain `createApp` into the jsdom the suite already runs in,
 * the same way `startAgentPanel.test.js` does, so it costs no new dependency.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { createApp, h } from 'vue'

const push = vi.fn()
vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: 'm1' } }),
  useRouter: () => ({ push }),
}))

// The page opens a live stream on mount. Nothing here is about the stream, and
// a real EventSource in jsdom is a connection that never resolves.
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
const WORKSPACE = { id: 'ws1', name: 'Ops', agentConnected: false, workingDirectory: '/srv/app' }
// One live session, so the card's two actions are rendered — for another
// workspace, or its presence would block the launches tested below.
const SESSIONS = [
  { id: 'sess-1', kind: 'claude-code', status: 'running', workspaceId: 'ws9', workspaceName: 'Billing' },
]

let launchResult = { session: { id: 'sess-9', kind: 'claude-code', status: 'starting' } }
const launchAgent = vi.fn(() => Promise.resolve(launchResult))

vi.mock('../src/api', () => ({
  getMachine: () => Promise.resolve({ machine: MACHINE }),
  fetchMachineSessions: () => Promise.resolve({ sessions: SESSIONS }),
  fetchWorkspaces: () => Promise.resolve({ workspaces: [WORKSPACE] }),
  launchAgent: (...args) => launchAgent(...args),
  fetchAcpAgents: () => Promise.resolve({ agents: [] }),
  fetchAcpModels: () => Promise.resolve({ agent: '', models: [] }),
  updateMachine: vi.fn(),
  deleteMachine: vi.fn(),
  killSession: vi.fn(),
  restartDaemon: vi.fn(),
  API_BASE_URL: '/api/v1',
}))

const { default: MachineDetailView } = await import('../src/views/MachineDetailView.vue')
// Not mocked: the toast store is module-level state, so the one the page
// writes to is the one read here. It is the only place a refused launch is
// said out loud, since this page renders no error of its own.
const { useToasts } = await import('../src/composables/useToasts')

/** Let the page's own load() and the launch resolve. */
const settle = () => new Promise((resolve) => setTimeout(resolve, 30))

async function mount() {
  const el = document.createElement('div')
  document.body.appendChild(el)
  const app = createApp({ render: () => h(MachineDetailView) })
  // A blocker renders its fix as a link; the real router is mocked away.
  app.component('RouterLink', {
    props: ['to'],
    setup: (p, { slots }) => () => h('a', {}, slots.default?.()),
  })
  app.mount(el)
  await settle()

  // Neither choice is a dropdown any more: the workspaces are listed as
  // cards over a hidden radio each, and the kind is a segmented control. So
  // both are picked by clicking the option itself, which is also what a
  // person now does.
  const choose = async (id) => {
    el.querySelector(`#${id}`).click()
    await settle()
  }

  const clickStart = async () => {
    const button = [...el.querySelectorAll('button')].find(
      (b) => b.textContent.trim() === 'Start agent'
    )
    button.click()
    await settle()
  }

  const clickTab = async (label) => {
    const button = [...el.querySelectorAll('button')].find((b) => b.textContent.trim() === label)
    button.click()
    await settle()
  }

  return { el, choose, clickStart, clickTab }
}

describe('MachineDetailView: starting an agent', () => {
  beforeEach(() => {
    push.mockClear()
    launchAgent.mockClear()
    launchResult = { session: { id: 'sess-9', kind: 'claude-code', status: 'starting' } }
  })

  it('opens the terminal of the Claude Code session it just started', async () => {
    const { choose, clickStart, clickTab } = await mount()
    await clickTab('New Session')
    await choose('launch-workspace-ws1')
    await clickStart()

    expect(launchAgent).toHaveBeenCalledWith('ws1', expect.objectContaining({ kind: 'claude-code' }))
    expect(push).toHaveBeenCalledWith('/sessions/sess-9')
  })

  it('does the same for a gateway, which asks its own questions on the way up', async () => {
    launchResult = { session: { id: 'sess-10', kind: 'acp-gateway', status: 'starting' } }
    const { choose, clickStart, clickTab } = await mount()
    await clickTab('New Session')
    await choose('launch-workspace-ws1')
    await choose('launch-kind-acp-gateway')
    await clickStart()

    expect(launchAgent).toHaveBeenCalledWith('ws1', expect.objectContaining({ kind: 'acp-gateway' }))
    expect(push).toHaveBeenCalledWith('/sessions/sess-10')
  })

  it('sends the model and effort chosen on the sliders for Claude Code', async () => {
    localStorage.clear()
    const { el, choose, clickStart, clickTab } = await mount()
    await clickTab('New Session')
    await choose('launch-workspace-ws1')
    const haiku = [...el.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Haiku')
    haiku.click()
    const effort = el.querySelector('#launch-effort')
    effort.value = '1'
    effort.dispatchEvent(new Event('input'))
    await settle()
    await clickStart()

    expect(launchAgent).toHaveBeenCalledWith('ws1', expect.objectContaining({ kind: 'claude-code', model: 'haiku', effort: 'low' }))
    localStorage.clear()
  })

  it('stays put when the launch was refused', async () => {
    launchAgent.mockRejectedValueOnce(new Error('that workspace already has an agent'))
    const { choose, clickStart, clickTab } = await mount()
    await clickTab('New Session')
    await choose('launch-workspace-ws1')
    await clickStart()

    expect(push).not.toHaveBeenCalled()
    // And the refusal is said, not swallowed.
    const { toasts } = useToasts()
    expect(toasts.value.at(-1)).toMatchObject({
      type: 'error',
      message: 'that workspace already has an agent',
    })
  })

  it('stays put when the server names no session, rather than opening a dead page', async () => {
    // `/sessions/undefined` resolves, loads, finds nothing and reports that
    // the agent has ended — which is a lie, and worse than staying here.
    launchResult = { session: {} }
    const { choose, clickStart, clickTab } = await mount()
    await clickTab('New Session')
    await choose('launch-workspace-ws1')
    await clickStart()

    expect(launchAgent).toHaveBeenCalled()
    expect(push).not.toHaveBeenCalled()
  })
})

// The two actions on a live session card are icons, which say nothing on their
// own: a control that cannot be read out or hovered is not a control for
// everybody, and the label is the first thing a redraw loses.
describe('MachineDetailView: the session cards', () => {
  it('names its icon-only actions', async () => {
    const { el } = await mount()
    const labels = [...el.querySelectorAll('button[aria-label]')].map((b) => b.getAttribute('aria-label'))
    expect(labels).toContain('Open the terminal')
    expect(labels).toContain('Stop this session')
    // And each one says the same thing on hover.
    for (const b of el.querySelectorAll('button[aria-label]')) {
      expect(b.getAttribute('title')).toBe(b.getAttribute('aria-label'))
    }
  })
})

// Two things the page's layout promises, both invisible to a unit test of the
// composables: what it leads with, and what it no longer offers.
describe('MachineDetailView: the shape of the page', () => {
  it("offers no way to rename the machine: the name is the daemon's", async () => {
    const { el, clickTab } = await mount()
    expect(el.querySelector('#machine-name')).toBe(null)
    const labels = [...el.querySelectorAll('button')].map((b) => b.textContent.trim())
    expect(labels).not.toContain('Rename')

    // The settings card itself is still there, or this would pass by
    // rendering nothing at all.
    await clickTab('Settings')
    expect([...el.querySelectorAll('h2')].map((h) => h.textContent.trim())).toContain('Settings')
  })

  it('leads with the Sessions tab, before the one that starts another', async () => {
    const { el } = await mount()
    const tabLabels = [...el.querySelectorAll('button')]
      .map((b) => b.textContent.trim())
      .filter((t) => ['Sessions', 'New Session', 'Machine Info', 'Settings'].includes(t))

    expect(tabLabels).toEqual(['Sessions', 'New Session', 'Machine Info', 'Settings'])
  })

  it('opens on the Sessions tab when the machine already has sessions', async () => {
    const { el } = await mount()
    const headings = [...el.querySelectorAll('h2')].map((h) => h.textContent.trim())
    expect(headings.some((t) => t.startsWith('Sessions'))).toBe(true)
    expect(headings).not.toContain('Run an agent here')
  })
})
