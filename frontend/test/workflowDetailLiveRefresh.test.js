// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * A browser agent's change to the workflow the person has open shows up on it.
 *
 * The join the unit tests cannot see: the agent's call goes through the real
 * WebMCP catalogue, and the page — loaded once on mount — has to re-read.
 * Before this, setting a start event and adding steps through WebMCP stayed
 * invisible until the person left the page and came back.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { createApp, h } from 'vue'
import { createPinia } from 'pinia'

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: 'wf1' } }),
  useRouter: () => ({ push: vi.fn() }),
}))

// What the server holds; the agent's calls below change it.
const server = {}
function reset() {
  server.workflow = { id: 'wf1', name: 'release', startEventId: '' }
  server.steps = []
  server.text = 'workflow release'
}

vi.mock('../src/api', () => ({
  getWorkflow: vi.fn(async () => ({ workflow: { ...server.workflow } })),
  updateWorkflow: vi.fn(async (_id, fields) => {
    Object.assign(server.workflow, fields)
    return { workflow: { ...server.workflow } }
  }),
  fetchWorkflowSteps: vi.fn(async () => ({ workflowSteps: server.steps.map((s) => ({ ...s })) })),
  createWorkflowStep: vi.fn(async (_id, step) => {
    const created = { id: `st${server.steps.length + 1}`, emitEventId: '', ...step }
    server.steps.push(created)
    return { workflowStep: created }
  }),
  deleteWorkflowStep: vi.fn(),
  fetchWorkflowTasks: vi.fn(async () => ({ tasks: [] })),
  fetchWorkflowText: vi.fn(async () => ({ text: server.text })),
  replaceWorkflowFromText: vi.fn(),
  fetchWorkflows: vi.fn(async () => ({ workflows: [{ ...server.workflow }] })),
  fetchEvents: vi.fn(async () => ({ events: [{ id: 'e1', name: 'deploy_requested' }] })),
  fetchEventTriggers: vi.fn(async () => ({ eventTriggers: [] })),
  fetchWorkspaces: vi.fn(async () => ({ workspaces: [{ id: 'ws1', name: 'Payments' }] })),
  API_BASE_URL: '/api/v1',
}))

const api = await import('../src/api')
const { default: WorkflowDetailView } = await import('../src/views/WorkflowDetailView.vue')
const { connectWebMCP } = await import('../src/composables/useWebMCP')

const settle = () => new Promise((resolve) => setTimeout(resolve, 30))

let apps = []
let tools = {}

async function mount() {
  const el = document.createElement('div')
  document.body.appendChild(el)
  const app = createApp({ render: () => h(WorkflowDetailView) })
  app.use(createPinia())
  app.component('RouterLink', { setup: (_, { slots }) => () => h('a', {}, slots.default?.()) })
  app.directive('click-outside', {})
  app.mount(el)
  apps.push(app)
  await settle()
  return el
}

/** An agent calling a tool, through the same wiring the app registers. */
async function agentCalls(name, args) {
  await tools[name].execute(args, {})
  await settle()
}

beforeEach(async () => {
  apps.forEach((app) => app.unmount())
  apps = []
  document.body.innerHTML = ''
  localStorage.clear()
  reset()
  vi.clearAllMocks()

  const context = { registerTool: vi.fn().mockResolvedValue(undefined) }
  const router = { push: vi.fn(), resolve: () => ({ matched: [{}] }), currentRoute: { value: { path: '/workflows/wf1' } } }
  await connectWebMCP({ api, router, context })
  tools = Object.fromEntries(context.registerTool.mock.calls.map(([t]) => [t.name, t]))
})

describe('the open workflow page', () => {
  it('shows a start event and a step an agent added, without being left', async () => {
    const el = await mount()
    expect(el.textContent).toContain('Pick a start event')

    // The boxes on the canvas; the palette beside it names every workspace.
    const nodes = () => [...el.querySelectorAll('div.select-none.rounded-xl')].map((n) => n.textContent.trim())

    await agentCalls('updateWorkflow', { workflowId: 'wf1', startEventId: 'e1' })
    expect(el.textContent).not.toContain('Pick a start event')
    expect(nodes()).toEqual([expect.stringContaining('deploy_requested')])

    await agentCalls('createWorkflowStep', { workflowId: 'wf1', eventId: 'e1', workspaceId: 'ws1', title: 'Ship it' })
    expect(nodes()).toHaveLength(2)
    expect(nodes()[1]).toContain('Payments')
  })

  it('re-reads in place, without the loading screen', async () => {
    const el = await mount()

    const call = tools.updateWorkflow.execute({ workflowId: 'wf1', startEventId: 'e1' }, {})
    await Promise.resolve()
    expect(el.textContent).not.toContain('Loading workflow')
    await call
    await settle()
  })

  it('does not re-read for an agent that only looked', async () => {
    await mount()
    api.getWorkflow.mockClear()

    await agentCalls('getWorkflow', { workflowId: 'wf1' })
    api.getWorkflow.mockClear()
    await agentCalls('listWorkflows', {})

    expect(api.getWorkflow).not.toHaveBeenCalled()
  })

  it('refreshes the text view too, unless it holds unsaved edits', async () => {
    // Text mode is offered only once there is a start event.
    server.workflow.startEventId = 'e1'
    const el = await mount()
    const button = (label) => [...el.querySelectorAll('button')].find((b) => b.textContent.trim() === label)
    button('Text').click()
    await settle()
    const textarea = () => el.querySelector('textarea')
    expect(textarea().value).toBe('workflow release')

    server.text = 'workflow release\nstep one'
    await agentCalls('updateWorkflow', { workflowId: 'wf1', startEventId: 'e1' })
    expect(textarea().value).toBe('workflow release\nstep one')

    textarea().value = 'my draft'
    textarea().dispatchEvent(new Event('input'))
    server.text = 'workflow release\nstep two'
    await agentCalls('updateWorkflow', { workflowId: 'wf1', startEventId: 'e1' })
    expect(textarea().value).toBe('my draft')
  })

  it('stops refreshing once the page is gone', async () => {
    await mount()
    apps.pop().unmount()
    api.getWorkflow.mockClear()

    await agentCalls('updateWorkflow', { workflowId: 'wf1', startEventId: 'e1' })

    expect(api.getWorkflow).not.toHaveBeenCalled()
  })
})
