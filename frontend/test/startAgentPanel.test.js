// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * What the start-an-agent panel actually puts on the page.
 *
 * The rules it follows are tested in `workspaceAgentLaunch.test.js`; this is
 * the other half — that the right one of them reaches the screen. Mounted with
 * plain `createApp` into the jsdom the suite already runs in, so it costs no
 * new dependency.
 *
 * It earns its place: the first version of this panel told somebody with one
 * machine that it would run "on the machine you pick" and then, two lines
 * below, named the only machine there was. Both strings were individually
 * fine and the pair was nonsense, which is not something a test of the
 * composable could ever have seen.
 */

import { describe, it, expect, vi } from 'vitest'
import { createApp, h } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { useWorkspaceStore } from '../src/stores/workspaceStore'

const push = vi.fn()
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))

let machines = []
const launchAgent = vi.fn(() => Promise.resolve({ session: { id: 'sess-9' } }))
vi.mock('../src/api', () => ({
  fetchMachines: () => Promise.resolve({ machines }),
  launchAgent: (...args) => launchAgent(...args),
  fetchAcpAgents: () => Promise.resolve({ agents: [{ id: 'local-acp', name: 'Local' }] }),
  fetchAcpModels: () => Promise.resolve({ agent: '', models: [] }),
  fetchWorkspaces: () => Promise.resolve({ workspaces: [] }),
  recordTelemetry: (...args) => recordTelemetry(...args),
  TELEMETRY_UI_ACP_AGENT_PICK: 'ui_acp_agent_pick',
}))
const recordTelemetry = vi.fn()

const { loadAcpRegistry } = await import('../src/composables/useAcpRegistry')
await loadAcpRegistry(() =>
  Promise.resolve({ agents: [{ id: 'codex-acp', name: 'Codex', description: 'OpenAI coding agent', runtimes: ['npx'] }] })
)

const { default: StartAgentPanel } = await import('../src/components/StartAgentPanel.vue')

const WORKSPACE = { id: 'ws1', name: 'Ops', agentConnected: false, workingDirectory: '/srv/app' }
const ONLINE = { id: 'm1', name: 'workshop-pi', enabled: true, online: true }
const SECOND = { id: 'm2', name: 'laptop', enabled: true, online: true }

/** Let the mounted component's own load() resolve. */
const settle = () => new Promise((resolve) => setTimeout(resolve, 30))

async function mount(props, list, workspaces = []) {
  machines = list
  const pinia = createPinia()
  setActivePinia(pinia)
  useWorkspaceStore(pinia).workspaces = workspaces
  const el = document.createElement('div')
  document.body.appendChild(el)
  const availability = []
  const noMachine = []
  const app = createApp({
    render: () =>
      h(StartAgentPanel, {
        ...props,
        onAvailability: (v) => availability.push(v),
        onNoMachine: (v) => noMachine.push(v),
      }),
  })
  // Blockers render their fix as a link; the real router is mocked away.
  app.use(pinia)
  app.component('RouterLink', {
    props: ['to'],
    setup: (p, { slots }) => () => h('a', {}, slots.default?.()),
  })
  app.mount(el)
  await settle()
  const text = () => el.textContent.replace(/\s+/g, ' ').trim()
  const open = async () => {
    el.querySelector('button').click()
    await settle()
  }
  return { el, text, open, availability, noMachine }
}

describe('StartAgentPanel', () => {
  it('renders nothing, and says so, when no machine is online', async () => {
    // The setup guide is still the honest answer here, so the page must be
    // able to keep it as its primary action.
    const { el, availability, noMachine } = await mount({ workspace: WORKSPACE }, [
      { id: 'x', name: 'off', enabled: true, online: false },
    ])
    expect(el.textContent.trim()).toBe('')
    expect(availability.at(-1)).toBe(false)
    expect(noMachine.at(-1)).toBe(true)
  })

  it('offers one folded action naming where it will run', async () => {
    const { text, el, availability } = await mount({ workspace: WORKSPACE }, [ONLINE])
    expect(text()).toBe('Start an agent on workshop-pi')
    expect(el.querySelectorAll('select')).toHaveLength(0)
    expect(availability.at(-1)).toBe(true)
  })

  it('does not say there is no machine when one is online', async () => {
    const { noMachine } = await mount({ workspace: WORKSPACE }, [ONLINE])
    expect(noMachine).toEqual([false])
  })

  it('does not ask which machine when there is only one', async () => {
    const { el, text, open } = await mount({ workspace: WORKSPACE }, [ONLINE])
    await open()
    // The only dropdown left on this form is the machine, and there is
    // nothing to pick: what to run is a segmented control.
    expect(el.querySelectorAll('select')).toHaveLength(0)
    expect(el.querySelector('#start-agent-kind-claude-code')).toBeTruthy()
    expect(text()).toMatch(/Runs in \/srv\/app on workshop-pi, your only machine that is online\./)
  })

  it('asks which machine when there are several, and says so before the button', async () => {
    const { el, text, open } = await mount({ workspace: WORKSPACE }, [ONLINE, SECOND])
    await open()
    // Every machine is on the page to be picked, rather than behind a dropdown.
    expect([...el.querySelectorAll('input[name=start-agent-machine]')].map((i) => i.id)).toEqual([
      'start-agent-machine-m1',
      'start-agent-machine-m2',
    ])
    expect(el.querySelectorAll('select')).toHaveLength(0)
    expect(text()).toMatch(/on the machine you pick/)
    expect(text()).toMatch(/Pick a machine to run on/)
    expect(el.querySelector('button[disabled]')).toBeTruthy()
  })

  it('launches on the machine that was picked', async () => {
    // None is preselected with several to choose from, so the choice made here
    // is the one that has to reach the daemon.
    const { el, open } = await mount({ workspace: WORKSPACE }, [ONLINE, SECOND])
    await open()
    const second = el.querySelector('#start-agent-machine-m2')
    second.checked = true
    second.dispatchEvent(new Event('change'))
    await settle()

    const go = [...el.querySelectorAll('button')].find((b) => /Start an agent/i.test(b.textContent))
    go.click()
    await settle()
    expect(launchAgent).toHaveBeenCalledWith('ws1', expect.objectContaining({ machineId: 'm2' }))
  })

  it('opens straight into the form on the setup page, with nothing to cancel back to', async () => {
    const { el, text } = await mount({ workspace: WORKSPACE, variant: 'card' }, [ONLINE])
    expect(el.querySelector('#start-agent-kind-claude-code')).toBeTruthy()
    expect(text()).not.toMatch(/Cancel/)
  })

  it('explains a workspace with no folder, and links to the fix', async () => {
    // Likely to be common: a workspace that has never run an agent is exactly
    // the one somebody is looking at when it is offline.
    const { el, text } = await mount(
      { workspace: { ...WORKSPACE, workingDirectory: '' }, variant: 'card' },
      [ONLINE]
    )
    expect(text()).toMatch(/no working directory/)
    expect(text()).toMatch(/Set one in workspace settings/)
    expect(el.querySelector('button[disabled]')).toBeTruthy()
  })

  it('starts the agent and goes to its terminal', async () => {
    // Where the daemon reports what happened next, and the only place a
    // missing folder or a failed boot is visible.
    const { el, variant } = await mount({ workspace: WORKSPACE, variant: 'card' }, [ONLINE])
    const go = [...el.querySelectorAll('button')].find((b) => /Start an agent/i.test(b.textContent))
    go.click()
    await settle()
    expect(launchAgent).toHaveBeenCalledWith('ws1', expect.objectContaining({ machineId: 'm1' }))
    expect(push).toHaveBeenCalledWith('/sessions/sess-9')
  })

  it('picks Claude Code\'s model and effort on two sliders, and shows neither for the gateway', async () => {
    localStorage.clear()
    const { el } = await mount({ workspace: WORKSPACE, variant: 'card' }, [ONLINE])
    const model = el.querySelector('#start-agent-model')
    const effort = el.querySelector('#start-agent-effort')
    expect(model.type).toBe('range')
    expect(effort.type).toBe('range')
    expect(el.querySelector('[data-test=start-agent-model-value]').textContent.trim()).toBe('Default')

    model.value = '3'
    model.dispatchEvent(new Event('input'))
    effort.value = '4'
    effort.dispatchEvent(new Event('input'))
    await settle()
    expect(el.querySelector('[data-test=start-agent-model-value]').textContent.trim()).toBe('Opus')
    expect(el.querySelector('[data-test=start-agent-effort-value]').textContent.trim()).toBe('Extra high')

    el.querySelector('#start-agent-kind-acp-gateway').click()
    await settle()
    expect(el.querySelector('#start-agent-effort')).toBeNull()
    expect(el.querySelector('#start-agent-agent')).toBeTruthy()
    el.querySelector('#start-agent-kind-claude-code').click()
    await settle()

    const go = [...el.querySelectorAll('button')].find((b) => /Start an agent/i.test(b.textContent))
    go.click()
    await settle()
    expect(launchAgent).toHaveBeenLastCalledWith('ws1', expect.objectContaining({ kind: 'claude-code', model: 'opus', effort: 'xhigh' }))
    localStorage.clear()
  })

  describe('on a fork', () => {
    // A fork's folder is made from its parent's on the first launch, so "no
    // working directory" is not a reason to refuse it — its parent's is.
    const FORK = { id: 'f1', name: 'ops fork', agentConnected: false, forkOfId: 'ws1', workingDirectory: '' }

    it('says a folder will be made from the parent\'s, instead of refusing', async () => {
      const { el, text } = await mount({ workspace: FORK, variant: 'card' }, [ONLINE], [WORKSPACE, FORK])
      expect(el.querySelector('[data-test=fork-folder-note]').textContent).toBe(
        'Runs in a folder of its own — a folder will be made from /srv/app'
      )
      expect(text()).not.toMatch(/no working directory/)
      const go = [...el.querySelectorAll('button')].find((b) => /Start an agent/i.test(b.textContent))
      expect(go.disabled).toBe(false)
    })

    it('refuses when the parent has no folder to make it from, and links to the parent', async () => {
      const parent = { ...WORKSPACE, workingDirectory: '' }
      const { el, text } = await mount({ workspace: FORK, variant: 'card' }, [ONLINE], [parent, FORK])
      expect(text()).toMatch(/This fork's folder is made from Ops's, and Ops has no working directory\./)
      expect(text()).toMatch(/Set one in Ops's settings/)
      expect(el.querySelector('button[disabled]')).toBeTruthy()
    })

    it('shows the server\'s refusal from a machine whose agentrqd is too old', async () => {
      launchAgent.mockImplementationOnce(() => Promise.reject(new Error('update agentrqd on this machine to run a fork')))
      const { el, text } = await mount({ workspace: FORK, variant: 'card' }, [ONLINE], [WORKSPACE, FORK])
      ;[...el.querySelectorAll('button')].find((b) => /Start an agent/i.test(b.textContent)).click()
      await settle()
      expect(text()).toMatch(/update agentrqd on this machine to run a fork/)
    })
  })

  it('lists the available gateway agents under Agent, and a click fills it in', async () => {
    localStorage.clear()
    const { el } = await mount({ workspace: WORKSPACE, variant: 'card' }, [ONLINE])
    expect(el.querySelector('[data-test=start-agent-acp-agents]')).toBeNull()
    el.querySelector('#start-agent-kind-acp-gateway').click()
    await settle()

    const rows = [...el.querySelectorAll('[data-test=start-agent-acp-agents] li button')]
    // The registry's first, then the one only this machine reports.
    expect(rows.map((b) => b.querySelector('span span').textContent.trim())).toEqual(['Codex', 'Local'])
    expect(rows[0].textContent).toContain('OpenAI coding agent')
    rows[0].click()
    await settle()
    expect(el.querySelector('#start-agent-agent').value).toBe('codex-acp')
    expect(recordTelemetry).toHaveBeenCalledWith('ui_acp_agent_pick', 'ws1')

    const go = [...el.querySelectorAll('button')].find((b) => /Start an agent/i.test(b.textContent))
    go.click()
    await settle()
    expect(launchAgent).toHaveBeenLastCalledWith('ws1', expect.objectContaining({ kind: 'acp-gateway', agent: 'codex-acp' }))
  })
})
