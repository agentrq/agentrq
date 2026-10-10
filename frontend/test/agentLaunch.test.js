// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, describe, it, expect, vi } from 'vitest'
import { nextTick, ref } from 'vue'
import {
  useAgentLaunch,
  workspaceEligibility,
  workspaceOptions,
  machineEligibility,
  sessionEligibility,
  paramsEligibility,
  launchParamsPayload,
  lastAcpGatewayChoice,
  rememberAcpGatewayChoice,
  lastLaunchChoice,
  lastLaunchParams,
  rememberLaunchChoice,
  launchFolderNote,
  forkParent,
  KINDS,
  GATEWAY_DEFAULTS,
  CLAUDE_CODE_MODELS,
  CLAUDE_CODE_EFFORTS,
  lastClaudeCodeChoice,
  rememberClaudeCodeChoice,
  initialParams,
  rememberParams,
  useKindParams,
  stepIndex,
  claudeModelSteps,
  mergeAcpAgents,
  filterAcpAgents,
} from '../src/composables/useAgentLaunch.js'

const READY_WORKSPACE = {
  id: 'ws1',
  name: 'Ops',
  agentConnected: false,
  workingDirectory: '/srv/app',
}
const READY_MACHINE = { id: 'm1', name: 'rpi', enabled: true, online: true }

// What a real measurement would answer, standing in for the one this
// composable no longer hardcodes. What matters here is that `launch` sends
// whatever this resolves to; the measurement itself is `launchTerminalSize`'s
// own test.
const MEASURED_SIZE = { cols: 164, rows: 52 }

function harness(over = {}) {
  const machine = ref(over.machine === undefined ? { ...READY_MACHINE } : over.machine)
  const sessions = ref(over.sessions ?? [])
  const deps = {
    machine,
    sessions,
    fetchWorkspaces: vi.fn().mockResolvedValue({ workspaces: [{ ...READY_WORKSPACE }] }),
    launchAgent: vi.fn().mockResolvedValue({ session: { id: 's1', status: 'starting' } }),
    measureTerminalSize: vi.fn().mockResolvedValue(MEASURED_SIZE),
    // Real network otherwise: kind defaults to claude-code, so most tests
    // never call these, but a few flip to acp-gateway and would hit the real
    // api.js functions without a stand-in.
    fetchAcpAgents: vi.fn().mockResolvedValue({ agents: [] }),
    fetchAcpModels: vi.fn().mockResolvedValue({ agent: '', models: [] }),
    ...over.deps,
  }
  return { deps, machine, sessions, l: useAgentLaunch(deps) }
}

// Two ticks: one for the watcher itself to run and call the (mocked, already
// resolved) fetch, one for its `.then` continuation to land.
async function flush() {
  await nextTick()
  await nextTick()
}

// The reason is the useful half: "cannot launch" says somebody is stuck,
// "no working directory" says what to do about it.
describe('workspaceEligibility', () => {
  it('accepts a workspace with a folder and no agent', () => {
    expect(workspaceEligibility(READY_WORKSPACE).ok).toBe(true)
  })

  it('refuses one that already has an agent, with a green rather than a warning tone', () => {
    const e = workspaceEligibility({ ...READY_WORKSPACE, agentConnected: true })
    expect(e.ok).toBe(false)
    expect(e.reason).toContain('already has an agent')
    expect(e.note).toBe('agent running')
    expect(e.tone).toBe('good')
  })

  // The one refusal that is fixable from the interface, so it says where.
  it('refuses one with no folder, and names the page that sets it', () => {
    const e = workspaceEligibility({ ...READY_WORKSPACE, workingDirectory: '' })
    expect(e.ok).toBe(false)
    expect(e.reason).toContain('working directory')
    expect(e.fix.to).toBe('/workspaces/ws1/settings')
    expect(e.note).toBe('no folder set')
    expect(e.tone).toBe('warn')
  })

  it('refuses nothing at all', () => {
    expect(workspaceEligibility(null).ok).toBe(false)
    expect(workspaceEligibility(undefined).ok).toBe(false)
  })

  // Checked before the folder: an existing agent is not fixed by setting one.
  it('reports the agent before the folder when both are wrong', () => {
    const e = workspaceEligibility({ ...READY_WORKSPACE, agentConnected: true, workingDirectory: '' })
    expect(e.reason).toContain('already has an agent')
  })
})

// A different problem with a different fix: conflating the two would tell
// somebody to change a workspace setting when the machine is switched off.
describe('machineEligibility', () => {
  it('accepts an enabled machine that is online', () => {
    expect(machineEligibility(READY_MACHINE).ok).toBe(true)
  })

  it('refuses a disabled one, and says where the switch is', () => {
    const e = machineEligibility({ ...READY_MACHINE, enabled: false })
    expect(e.ok).toBe(false)
    expect(e.reason).toContain('disabled')
  })

  it('refuses an offline one, and says it reconnects on its own', () => {
    const e = machineEligibility({ ...READY_MACHINE, online: false })
    expect(e.ok).toBe(false)
    expect(e.reason).toContain('offline')
    expect(e.reason).toContain('reconnect')
  })

  it('says it is still loading rather than that the machine is broken', () => {
    expect(machineEligibility(null).reason).toContain('Loading')
  })
})

// Partial knowledge on purpose: this page knows its own machine's sessions and
// not another machine's, so it catches the common case and never claims the
// opposite.
describe('sessionEligibility', () => {
  it('accepts when nothing here is running for that workspace', () => {
    expect(sessionEligibility('ws1', []).ok).toBe(true)
    expect(sessionEligibility('ws1', undefined).ok).toBe(true)
    expect(sessionEligibility('ws1', [{ workspaceId: 'other', status: 'running' }]).ok).toBe(true)
  })

  it('refuses when this machine already has one for it', () => {
    const e = sessionEligibility('ws1', [
      { workspaceId: 'ws1', status: 'running', kind: 'claude-code' },
    ])
    expect(e.ok).toBe(false)
    expect(e.reason).toContain('claude-code')
  })

  // Starting counts: that window is exactly when somebody presses twice.
  it('counts one that is still starting', () => {
    expect(sessionEligibility('ws1', [{ workspaceId: 'ws1', status: 'starting' }]).ok).toBe(false)
  })

  it('ignores ones that have finished', () => {
    for (const status of ['exited', 'killed', 'failed']) {
      expect(sessionEligibility('ws1', [{ workspaceId: 'ws1', status }]).ok, status).toBe(true)
    }
  })

  it('names it an agent when the kind is missing', () => {
    expect(sessionEligibility('ws1', [{ workspaceId: 'ws1', status: 'running' }]).reason).toContain(
      'agent'
    )
  })
})

describe('paramsEligibility', () => {
  it('needs nothing extra for claude-code', () => {
    expect(paramsEligibility('claude-code', {}).ok).toBe(true)
  })

  it('needs only an agent for the gateway', () => {
    expect(paramsEligibility('acp-gateway', {}).reason).toContain('agent')
    expect(paramsEligibility('acp-gateway', { agent: 'a' }).ok).toBe(true)
    expect(paramsEligibility('acp-gateway', GATEWAY_DEFAULTS).ok).toBe(true)
  })

  it('treats a blank agent as missing, whitespace included', () => {
    expect(paramsEligibility('acp-gateway', { agent: '   ' }).ok).toBe(false)
  })

  // The required field's shape check is its own branch, distinct from the
  // optional one below — a bad model must not be the only way to reach it.
  it('refuses a badly shaped agent, the one required field', () => {
    const e = paramsEligibility('acp-gateway', { agent: 'a b' })
    expect(e.ok).toBe(false)
    expect(e.reason).toContain('will not accept')
  })

  // A model left blank is "let the gateway decide", not a value that failed
  // validation — the field is optional, not merely tolerant of anything.
  it('treats a blank model as not chosen rather than as a bad one', () => {
    expect(paramsEligibility('acp-gateway', { model: '   ', agent: 'a' }).ok).toBe(true)
  })

  // Mirrors the daemon's own rule, so the refusal arrives while somebody is
  // still looking at the field rather than after a round trip.
  it('refuses what the daemon would refuse', () => {
    for (const bad of ['--dangerously-skip-permissions', '-x', 'a b', 'a/b', '../escape', 'a;b']) {
      const e = paramsEligibility('acp-gateway', { model: bad, agent: 'a' })
      expect(e.ok, bad).toBe(false)
      expect(e.reason).toContain('will not accept')
    }
  })

  it('accepts the names people actually use', () => {
    for (const good of ['gemini-3.8-flash-high', 'antigravity-acp', 'claude_3', 'gpt.4o']) {
      expect(paramsEligibility('acp-gateway', { model: good, agent: good }).ok, good).toBe(true)
    }
  })

  it('refuses a kind the daemon does not run', () => {
    expect(paramsEligibility('bash', {}).ok).toBe(false)
  })

  // Format is still checked even though the field is optional: a value that
  // is present has to be a real identifier, or the daemon refuses it anyway.
  it('still refuses a bad model, even though the field is optional', () => {
    const e = paramsEligibility('acp-gateway', { model: 'a b', agent: 'a' })
    expect(e.ok).toBe(false)
    expect(e.reason).toContain('will not accept')
  })
})

describe('launchParamsPayload', () => {
  it('sends nothing for a kind the daemon does not run', () => {
    expect(launchParamsPayload('bash', {})).toEqual({})
  })

  it('needs nothing extra for claude-code', () => {
    expect(launchParamsPayload('claude-code', {})).toEqual({})
  })

  // A key the caller never set at all (not merely blank) is the other way
  // "no preference" arrives, and must be skipped the same as an empty string.
  it('omits an optional field the caller never set at all', () => {
    expect(launchParamsPayload('acp-gateway', { agent: 'a' })).toEqual({ agent: 'a' })
  })

  // A required field goes through the same nullish fallback as an optional
  // one before it's trimmed — a missing key must not throw, just come out blank.
  it('treats a missing required field as blank, not absent', () => {
    expect(launchParamsPayload('acp-gateway', {})).toEqual({ agent: '' })
  })
})

describe('loading workspaces', () => {
  it('loads them', async () => {
    const h = harness()
    await h.l.load()
    expect(h.l.workspaces.value).toHaveLength(1)
    expect(h.l.loading.value).toBe(false)
  })

  // Not somewhere anybody means to start new work.
  it('leaves archived workspaces out', async () => {
    const h = harness({
      deps: {
        fetchWorkspaces: vi.fn().mockResolvedValue({
          workspaces: [READY_WORKSPACE, { id: 'old', archivedAt: '2026-01-01T00:00:00Z' }],
        }),
      },
    })
    await h.l.load()
    expect(h.l.workspaces.value.map((w) => w.id)).toEqual(['ws1'])
  })

  it('says what went wrong', async () => {
    const h = harness({ deps: { fetchWorkspaces: vi.fn().mockRejectedValue(new Error('offline')) } })
    await h.l.load()
    expect(h.l.error.value).toBe('offline')
  })

  it('falls back to a message, and copes with an empty response', async () => {
    const h = harness({ deps: { fetchWorkspaces: vi.fn().mockRejectedValue({}) } })
    await h.l.load()
    expect(h.l.error.value).toBe('Failed to load workspaces')

    const h2 = harness({ deps: { fetchWorkspaces: vi.fn().mockResolvedValue({}) } })
    await h2.l.load()
    expect(h2.l.workspaces.value).toEqual([])
  })
})

// The page lists the workspaces instead of hiding them in a dropdown, which
// only helps if the list says which of them can actually take an agent.
describe('workspaceOptions', () => {
  it('notes a ready workspace with its folder, which is what tells two apart', () => {
    expect(workspaceOptions([READY_WORKSPACE])).toEqual([
      { id: 'ws1', name: 'Ops', ready: true, note: '/srv/app', tone: null },
    ])
  })

  it('notes why one cannot be picked, in the space a folder would use', () => {
    const [busy, homeless] = workspaceOptions([
      { ...READY_WORKSPACE, agentConnected: true },
      { ...READY_WORKSPACE, id: 'ws2', workingDirectory: '' },
    ])
    // Green: a workspace already running an agent elsewhere is not a problem,
    // only a reason this launch cannot start a second one.
    expect(busy).toMatchObject({ ready: false, note: 'agent running', tone: 'good' })
    expect(homeless).toMatchObject({ ready: false, note: 'no folder set', tone: 'warn' })
  })

  it('has nothing to list before the workspaces have loaded', () => {
    expect(workspaceOptions(undefined)).toEqual([])
  })

  it('is what the launcher hands the page', async () => {
    const h = harness()
    await h.l.load()
    expect(h.l.choices.value).toEqual([
      { id: 'ws1', name: 'Ops', ready: true, note: '/srv/app', tone: null },
    ])
  })
})

describe('what stops a launch', () => {
  it('says nothing is wrong once everything is chosen', async () => {
    const h = harness()
    await h.l.load()
    h.l.workspaceId.value = 'ws1'
    expect(h.l.blockers.value).toEqual([])
    expect(h.l.canLaunch.value).toBe(true)
  })

  // Nothing can run on a machine that is off, whatever the workspace says.
  it('reports the machine first', async () => {
    const h = harness({ machine: { ...READY_MACHINE, online: false } })
    await h.l.load()
    expect(h.l.blockers.value[0].reason).toContain('offline')
    expect(h.l.canLaunch.value).toBe(false)
  })

  it('stops a second launch for a workspace this machine is already running', async () => {
    const h = harness({ sessions: [{ workspaceId: 'ws1', status: 'running', kind: 'claude-code' }] })
    await h.l.load()
    h.l.workspaceId.value = 'ws1'
    expect(h.l.canLaunch.value).toBe(false)
    expect(h.l.blockers.value[0].reason).toContain('already running')
  })

  it('reports every reason at once rather than one at a time', async () => {
    const h = harness({
      machine: { ...READY_MACHINE, enabled: false },
      deps: {
        fetchWorkspaces: vi
          .fn()
          .mockResolvedValue({ workspaces: [{ ...READY_WORKSPACE, agentConnected: true }] }),
      },
    })
    await h.l.load()
    h.l.workspaceId.value = 'ws1'
    h.l.kind.value = 'acp-gateway'
    h.l.params.value = { model: '', agent: '' }
    expect(h.l.blockers.value).toHaveLength(3)
  })

  it('will not launch before a workspace is picked', async () => {
    const h = harness()
    await h.l.load()
    expect(h.l.canLaunch.value).toBe(false)
  })
})

describe('lastAcpGatewayChoice / rememberAcpGatewayChoice', () => {
  afterEach(() => localStorage.clear())

  it('has nothing to remember at first', () => {
    expect(lastAcpGatewayChoice()).toBeNull()
  })

  it('round-trips what was remembered', () => {
    rememberAcpGatewayChoice({ agent: 'codex-acp', model: 'gpt-5.5' })
    expect(lastAcpGatewayChoice()).toEqual({ agent: 'codex-acp', model: 'gpt-5.5' })
  })

  // The agent is the one field a launch cannot go without; a remembered
  // model is a bonus, not a condition for restoring the rest.
  it('remembers just the agent when no model was ever chosen', () => {
    localStorage.setItem('agentrq:lastAcpGateway', JSON.stringify({ agent: 'codex-acp' }))
    expect(lastAcpGatewayChoice()).toEqual({ agent: 'codex-acp', model: '' })
  })

  it('ignores a stored value missing the agent, the same as nothing stored', () => {
    localStorage.setItem('agentrq:lastAcpGateway', JSON.stringify({ model: 'gpt-5.5' }))
    expect(lastAcpGatewayChoice()).toBeNull()
  })

  it('fails open on unreadable storage rather than throwing', () => {
    localStorage.setItem('agentrq:lastAcpGateway', 'not json')
    expect(lastAcpGatewayChoice()).toBeNull()
  })

  it('opens the form on the remembered choice instead of GATEWAY_DEFAULTS', async () => {
    rememberAcpGatewayChoice({ agent: 'codex-acp', model: 'gpt-5.5' })
    const h = harness()
    h.l.kind.value = 'acp-gateway'
    expect(h.l.params.value).toEqual({ agent: 'codex-acp', model: 'gpt-5.5' })
  })

  it('remembers the gateway choice after a successful launch, not before', async () => {
    const h = harness()
    await h.l.load()
    h.l.workspaceId.value = 'ws1'
    h.l.kind.value = 'acp-gateway'
    h.l.params.value = { model: 'gpt-5.5', agent: 'codex-acp' }
    expect(lastAcpGatewayChoice()).toBeNull()

    await h.l.launch()
    expect(lastAcpGatewayChoice()).toEqual({ agent: 'codex-acp', model: 'gpt-5.5' })
  })

  it('does not remember a claude-code launch', async () => {
    const h = harness()
    await h.l.load()
    h.l.workspaceId.value = 'ws1'
    await h.l.launch()
    expect(lastAcpGatewayChoice()).toBeNull()
  })
})

describe('acp-gateway suggestions', () => {
  it('asks for agent suggestions once the kind is acp-gateway and a machine is present', async () => {
    const h = harness({
      deps: { fetchAcpAgents: vi.fn().mockResolvedValue({ agents: [{ id: 'codex-acp', name: 'Codex' }] }) },
    })
    h.l.kind.value = 'acp-gateway'
    await flush()

    expect(h.deps.fetchAcpAgents).toHaveBeenCalledWith('m1')
    expect(h.l.acpAgents.value).toEqual([{ id: 'codex-acp', name: 'Codex' }])
  })

  it('never asks while the kind is claude-code', async () => {
    const h = harness()
    await flush()
    expect(h.deps.fetchAcpAgents).not.toHaveBeenCalled()
  })

  it('asks for model suggestions once an agent is typed, with the workspace and machine', async () => {
    const h = harness({
      deps: {
        fetchAcpModels: vi
          .fn()
          .mockResolvedValue({ agent: 'codex-acp', models: [{ id: 'gpt-5.5', current: true }] }),
      },
    })
    h.l.workspaceId.value = 'ws1'
    h.l.kind.value = 'acp-gateway'
    h.l.params.value = { ...h.l.params.value, agent: 'codex-acp' }
    await flush()

    expect(h.deps.fetchAcpModels).toHaveBeenCalledWith('ws1', 'm1', 'codex-acp')
    expect(h.l.acpModels.value).toEqual([{ id: 'gpt-5.5', current: true }])
  })

  it('does not ask for models before a workspace is picked', async () => {
    const h = harness()
    h.l.kind.value = 'acp-gateway'
    h.l.params.value = { ...h.l.params.value, agent: 'codex-acp' }
    await flush()
    expect(h.deps.fetchAcpModels).not.toHaveBeenCalled()
  })

  it('lists the ACP registry first, then what only the machine knows', async () => {
    const registryAgents = ref([
      { id: 'codex-acp', name: 'Codex', description: 'OpenAI', runtimes: ['npx'] },
      { id: 'gemini', name: 'Gemini CLI', description: 'Google', runtimes: ['npx'] },
    ])
    const h = harness({
      deps: {
        registryAgents,
        fetchAcpAgents: vi.fn().mockResolvedValue({
          agents: [{ id: 'gemini', name: 'Gemini' }, { id: 'local-acp', name: 'Local' }],
        }),
      },
    })
    h.l.kind.value = 'acp-gateway'
    await flush()
    expect(h.l.acpAgents.value.map((a) => a.id)).toEqual(['codex-acp', 'gemini', 'local-acp'])
    expect(h.l.acpAgents.value[1].name).toBe('Gemini CLI')

    // An hourly reload reaches a form that is already open.
    registryAgents.value = [...registryAgents.value, { id: 'goose', name: 'goose' }]
    expect(h.l.acpAgents.value.map((a) => a.id)).toEqual(['codex-acp', 'gemini', 'goose', 'local-acp'])
  })

  it('fills the Agent field from the list, and counts the pick for the chosen workspace', async () => {
    const recordTelemetry = vi.fn()
    const h = harness({ deps: { recordTelemetry } })
    h.l.kind.value = 'acp-gateway'
    h.l.workspaceId.value = 'ws1'
    h.l.pickAcpAgent('codex-acp')
    expect(h.l.params.value.agent).toBe('codex-acp')
    expect(recordTelemetry).toHaveBeenCalledWith('ui_acp_agent_pick', 'ws1')
  })

  it('fails open: a rejected lookup leaves the suggestions empty rather than throwing', async () => {
    const h = harness({ deps: { fetchAcpAgents: vi.fn().mockRejectedValue(new Error('offline')) } })
    h.l.kind.value = 'acp-gateway'
    await flush()
    expect(h.l.acpAgents.value).toEqual([])
  })
})

describe('mergeAcpAgents', () => {
  it('keeps each id once, the first seen, and skips entries without one', () => {
    const a = { id: 'a', name: 'From the registry' }
    expect(mergeAcpAgents([a, null, { name: 'no id' }], [{ id: 'a', name: 'From the machine' }, { id: 'b' }])).toEqual([
      a,
      { id: 'b' },
    ])
  })

  it('takes either side missing', () => {
    expect(mergeAcpAgents()).toEqual([])
    expect(mergeAcpAgents(undefined, [{ id: 'b' }])).toEqual([{ id: 'b' }])
  })
})

describe('filterAcpAgents', () => {
  const AGENTS = [
    { id: 'codex-acp', name: 'Codex', description: 'OpenAI coding agent' },
    { id: 'gemini', name: 'Gemini CLI', description: "Google's agent" },
    { id: 'bare' },
  ]

  it('shows every agent while the field is empty', () => {
    expect(filterAcpAgents(AGENTS, '')).toBe(AGENTS)
    expect(filterAcpAgents(AGENTS, '  ')).toBe(AGENTS)
    expect(filterAcpAgents(AGENTS, undefined)).toBe(AGENTS)
  })

  it('shows every agent once the field names one exactly, so it can be swapped', () => {
    expect(filterAcpAgents(AGENTS, 'Gemini ')).toBe(AGENTS)
  })

  it('narrows by id, name or description, ignoring case', () => {
    expect(filterAcpAgents(AGENTS, 'acp').map((a) => a.id)).toEqual(['codex-acp'])
    expect(filterAcpAgents(AGENTS, 'cli').map((a) => a.id)).toEqual(['gemini'])
    expect(filterAcpAgents(AGENTS, 'OPENAI').map((a) => a.id)).toEqual(['codex-acp'])
    expect(filterAcpAgents(AGENTS, 'nothing like it')).toEqual([])
  })
})

describe('launching', () => {
  it('sends the machine, the kind and a usable terminal size', async () => {
    const h = harness()
    await h.l.load()
    h.l.workspaceId.value = 'ws1'

    const session = await h.l.launch()
    expect(session.id).toBe('s1')
    expect(h.deps.launchAgent).toHaveBeenCalledWith('ws1', {
      machineId: 'm1',
      kind: 'claude-code',
      cols: MEASURED_SIZE.cols,
      rows: MEASURED_SIZE.rows,
    })
  })

  it('sends the gateway its model and agent, trimmed', async () => {
    const h = harness()
    await h.l.load()
    h.l.workspaceId.value = 'ws1'
    h.l.kind.value = 'acp-gateway'
    h.l.params.value = { model: '  gemini-3.8-flash-high  ', agent: 'antigravity-acp' }

    await h.l.launch()
    expect(h.deps.launchAgent).toHaveBeenCalledWith(
      'ws1',
      expect.objectContaining({ model: 'gemini-3.8-flash-high', agent: 'antigravity-acp' })
    )
  })

  // Only the agent is mandatory: a launch with no model chosen must still go
  // through, and must not send a `model` the gateway never asked for.
  it('launches the gateway with just an agent, sending no model at all', async () => {
    const h = harness()
    await h.l.load()
    h.l.workspaceId.value = 'ws1'
    h.l.kind.value = 'acp-gateway'
    h.l.params.value = { model: '', agent: 'antigravity-acp' }

    await h.l.launch()
    const sent = h.deps.launchAgent.mock.calls[0][1]
    expect(sent.agent).toBe('antigravity-acp')
    expect(sent).not.toHaveProperty('model')
  })

  // The answer can change between the page loading and the button being
  // pressed, so every refusal is still handled rather than assumed away.
  it('reports a refusal the eligibility check could not have known about', async () => {
    const h = harness({
      deps: {
        launchAgent: vi.fn().mockRejectedValue(new Error('that machine is not connected')),
      },
    })
    await h.l.load()
    h.l.workspaceId.value = 'ws1'

    expect(await h.l.launch()).toBeNull()
    expect(h.l.error.value).toBe('that machine is not connected')
    expect(h.l.launching.value).toBe(false)
  })

  it('falls back to a message', async () => {
    const h = harness({ deps: { launchAgent: vi.fn().mockRejectedValue({}) } })
    await h.l.load()
    h.l.workspaceId.value = 'ws1'
    await h.l.launch()
    expect(h.l.error.value).toBe('Failed to start the agent')
  })

  it('sends nothing when something is blocking it', async () => {
    const h = harness({ machine: { ...READY_MACHINE, online: false } })
    await h.l.load()
    h.l.workspaceId.value = 'ws1'
    expect(await h.l.launch()).toBeNull()
    expect(h.deps.launchAgent).not.toHaveBeenCalled()
  })

  it('copes with a response carrying no session', async () => {
    const h = harness({ deps: { launchAgent: vi.fn().mockResolvedValue({}) } })
    await h.l.load()
    h.l.workspaceId.value = 'ws1'
    expect(await h.l.launch()).toBeNull()
    expect(h.l.error.value).toBe('')
  })
})

describe('the catalogue', () => {
  it('offers exactly the two things a daemon will run', () => {
    expect(KINDS.map((k) => k.id)).toEqual(['claude-code', 'acp-gateway'])
  })

  // An empty required field is a worse first impression than a working
  // default, and these are the repository's own.
  it('opens the gateway form on something that works', () => {
    expect(paramsEligibility('acp-gateway', GATEWAY_DEFAULTS).ok).toBe(true)
  })
})

describe('defaults', () => {
  it('reaches for the real API when nothing is injected', async () => {
    const l = useAgentLaunch({ machine: ref(READY_MACHINE) })
    await l.load()
    expect(l.error.value).not.toBe('')
  })

  // jsdom lays nothing out, so the real `launchTerminalSize` finds no content
  // box to measure here — this only checks that `launch` reaches for it
  // rather than the constant it used to hardcode.
  it('reaches for the real measurement when nothing is injected', async () => {
    const launchAgent = vi.fn().mockResolvedValue({ session: { id: 's1' } })
    const l = useAgentLaunch({
      machine: ref(READY_MACHINE),
      fetchWorkspaces: vi.fn().mockResolvedValue({ workspaces: [{ ...READY_WORKSPACE }] }),
      launchAgent,
    })
    await l.load()
    l.workspaceId.value = 'ws1'
    await l.launch()
    expect(launchAgent).toHaveBeenCalledWith(
      'ws1',
      expect.objectContaining({ cols: expect.any(Number), rows: expect.any(Number) })
    )
  })
})

describe('a fork', () => {
  // A fork's folder is made by the daemon from its parent's on the first
  // launch, so an empty working directory is the normal state of a new fork.
  const PARENT = { ...READY_WORKSPACE }
  const FORK = { id: 'f1', name: 'Ops fork', agentConnected: false, forkOfId: 'ws1', workingDirectory: '' }

  it('can run with no folder yet, made from the parent\'s', () => {
    expect(workspaceEligibility(FORK, PARENT)).toEqual({ ok: true, forkFrom: '/srv/app' })
    expect(workspaceEligibility(FORK)).toEqual({ ok: true, forkFrom: '' })
  })

  it('cannot when the parent has no folder to make it from', () => {
    const e = workspaceEligibility(FORK, { ...PARENT, workingDirectory: '' })
    expect(e.ok).toBe(false)
    expect(e.reason).toBe("This fork's folder is made from Ops's, and Ops has no working directory.")
    expect(e.fix).toEqual({ label: "Set one in Ops's settings", to: '/workspaces/ws1/settings' })
  })

  it('runs in its own folder once it has one, like any workspace', () => {
    expect(workspaceEligibility({ ...FORK, workingDirectory: '/home/me/.agentrq/forks/f1' }, PARENT)).toEqual({ ok: true })
  })

  it('says where it will run', () => {
    expect(launchFolderNote(FORK, PARENT)).toBe('a folder will be made from /srv/app')
    expect(launchFolderNote(FORK)).toBe('a folder will be made for this fork')
    expect(launchFolderNote({ ...FORK, workingDirectory: '/x' })).toBe('/x')
    expect(launchFolderNote(PARENT)).toBe('/srv/app')
    expect(launchFolderNote({ id: 'w' })).toBe('')
    expect(launchFolderNote(null)).toBe('')
  })

  it('finds its parent in a list', () => {
    expect(forkParent(FORK, [PARENT, FORK])).toBe(PARENT)
    expect(forkParent(FORK, [])).toBe(null)
    expect(forkParent(FORK, undefined)).toBe(null)
    expect(forkParent(PARENT, [PARENT])).toBe(null)
  })

  it('is offered on the machine page with the note, and judged by its parent', () => {
    expect(workspaceOptions([PARENT, FORK])[1]).toMatchObject({ id: 'f1', ready: true, note: 'a folder will be made from /srv/app' })
    expect(workspaceOptions([{ ...PARENT, workingDirectory: '' }, FORK])[1]).toMatchObject({ ready: false, note: 'no folder set' })
  })

  it('is launched from the machine page with its parent in view', async () => {
    const { l, deps } = harness({
      deps: { fetchWorkspaces: vi.fn().mockResolvedValue({ workspaces: [{ ...PARENT, workingDirectory: '' }, FORK] }) },
    })
    await l.load()
    l.workspaceId.value = 'f1'
    expect(l.blockers.value[0].reason).toMatch(/made from Ops's/)
    expect(await l.launch()).toBe(null)
    expect(deps.launchAgent).not.toHaveBeenCalled()
  })
})

describe('the last launch of each workspace', () => {
  afterEach(() => localStorage.clear())

  it('is remembered per workspace, and read back', () => {
    localStorage.clear()
    expect(lastLaunchChoice('ws1')).toBe(null)
    rememberLaunchChoice('ws1', { machineId: 'm1', kind: 'acp-gateway' })
    rememberLaunchChoice('ws2', { machineId: 'm2', kind: 'claude-code' })
    expect(lastLaunchChoice('ws1')).toEqual({ machineId: 'm1', kind: 'acp-gateway' })
    expect(lastLaunchChoice('ws2')).toEqual({ machineId: 'm2', kind: 'claude-code' })
  })

  it('ignores a launch with nothing to remember, and a stored kind the daemon does not run', () => {
    rememberLaunchChoice('', { machineId: 'm1', kind: 'claude-code' })
    rememberLaunchChoice('ws1', { machineId: '', kind: 'claude-code' })
    expect(localStorage.getItem('agentrq:lastLaunch')).toBe(null)
    localStorage.setItem('agentrq:lastLaunch', JSON.stringify({ ws1: { machineId: 'm1', kind: 'bash' } }))
    expect(lastLaunchChoice('ws1')).toBe(null)
  })

  it('reads anything unexpected as nothing stored', () => {
    for (const bad of ['not json', '[1]', '"x"', 'null']) {
      localStorage.setItem('agentrq:lastLaunch', bad)
      expect(lastLaunchChoice('ws1')).toBe(null)
    }
    // And writes over it rather than failing.
    rememberLaunchChoice('ws1', { machineId: 'm1', kind: 'claude-code' })
    expect(lastLaunchChoice('ws1')).toEqual({ machineId: 'm1', kind: 'claude-code' })
  })

  it('survives storage that throws', () => {
    const set = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('QuotaExceededError: storage is full') })
    expect(() => rememberLaunchChoice('ws1', { machineId: 'm1', kind: 'claude-code' })).not.toThrow()
    set.mockRestore()
  })

  it('is recorded by a launch from the machine page', async () => {
    const { l } = harness()
    await l.load()
    l.workspaceId.value = 'ws1'
    await l.launch()
    expect(lastLaunchChoice('ws1')).toEqual({ machineId: 'm1', kind: 'claude-code' })
  })
})

describe('each workspace\'s last agent', () => {
  afterEach(() => localStorage.clear())

  it('keeps each kind\'s parameters per workspace, beside the other kind\'s', () => {
    expect(lastLaunchParams('ws1', 'acp-gateway')).toBe(null)
    rememberLaunchChoice('ws1', { machineId: 'm1', kind: 'acp-gateway', params: { agent: 'gemini', model: 'pro' } })
    rememberLaunchChoice('ws1', { machineId: 'm1', kind: 'claude-code', params: { model: 'opus' } })
    rememberLaunchChoice('ws2', { machineId: 'm1', kind: 'acp-gateway', params: { agent: 'codex-acp' } })
    expect(lastLaunchParams('ws1', 'acp-gateway')).toEqual({ agent: 'gemini', model: 'pro' })
    expect(lastLaunchParams('ws1', 'claude-code')).toEqual({ model: 'opus', effort: '' })
    expect(lastLaunchParams('ws2', 'acp-gateway')).toEqual({ agent: 'codex-acp', model: '' })
    expect(lastLaunchParams('ws2', 'claude-code')).toBe(null)
    expect(lastLaunchParams('', 'acp-gateway')).toBe(null)

    // A launch that names no parameters keeps those already stored.
    rememberLaunchChoice('ws1', { machineId: 'm2', kind: 'acp-gateway' })
    expect(lastLaunchChoice('ws1')).toEqual({ machineId: 'm2', kind: 'acp-gateway' })
    expect(lastLaunchParams('ws1', 'acp-gateway')).toEqual({ agent: 'gemini', model: 'pro' })
  })

  it('reads stored parameters through the same checks as the browser-wide choice', () => {
    localStorage.setItem('agentrq:lastLaunch', JSON.stringify({
      ws1: { machineId: 'm1', kind: 'acp-gateway', params: { 'acp-gateway': { model: 'pro' }, 'claude-code': { model: 'gpt', effort: 'max' } } },
      ws2: { machineId: 'm1', kind: 'acp-gateway', params: 'not an object' },
    }))
    expect(lastLaunchParams('ws1', 'acp-gateway')).toBe(null)
    expect(lastLaunchParams('ws1', 'claude-code')).toEqual({ model: '', effort: 'max' })
    expect(lastLaunchParams('ws2', 'acp-gateway')).toBe(null)
    // And a launch writes over what it could not read.
    rememberLaunchChoice('ws2', { machineId: 'm1', kind: 'acp-gateway', params: { agent: 'goose' } })
    expect(lastLaunchParams('ws2', 'acp-gateway')).toEqual({ agent: 'goose', model: '' })
  })

  it('opens a workspace on its own last agent before the one used anywhere', () => {
    rememberAcpGatewayChoice({ agent: 'codex-acp', model: '' })
    rememberLaunchChoice('ws1', { machineId: 'm1', kind: 'acp-gateway', params: { agent: 'gemini' } })
    expect(initialParams('acp-gateway', 'ws1')).toEqual({ agent: 'gemini', model: '' })
    expect(initialParams('acp-gateway', 'ws2')).toEqual({ agent: 'codex-acp', model: '' })
    expect(initialParams('acp-gateway')).toEqual({ agent: 'codex-acp', model: '' })
  })

  it('brings back a workspace\'s own when it is picked, and leaves the fields alone for one never launched', () => {
    rememberLaunchChoice('ws1', { machineId: 'm1', kind: 'acp-gateway', params: { agent: 'gemini' } })
    rememberLaunchChoice('ws1', { machineId: 'm1', kind: 'claude-code', params: { model: 'opus', effort: 'high' } })
    const kind = ref('acp-gateway')
    const workspaceId = ref('')
    const params = useKindParams(kind, () => workspaceId.value)
    params.value = { ...params.value, agent: 'typed-by-hand' }

    workspaceId.value = 'ws-new'
    expect(params.value.agent).toBe('typed-by-hand')
    workspaceId.value = 'ws1'
    expect(params.value).toEqual({ agent: 'gemini', model: '' })
    kind.value = 'claude-code'
    expect(params.value).toEqual({ model: 'opus', effort: 'high' })
  })

  it('is what the machine page opens on, once that workspace is picked', async () => {
    const first = harness()
    await first.l.load()
    first.l.workspaceId.value = 'ws1'
    first.l.kind.value = 'acp-gateway'
    first.l.params.value = { agent: 'gemini', model: '' }
    await first.l.launch()
    localStorage.removeItem('agentrq:lastAcpGateway')

    const next = harness()
    next.l.kind.value = 'acp-gateway'
    expect(next.l.params.value.agent).not.toBe('gemini')
    next.l.workspaceId.value = 'ws1'
    expect(next.l.params.value).toEqual({ agent: 'gemini', model: '' })
  })
})

describe('choosing Claude Code\'s model and effort', () => {
  afterEach(() => localStorage.clear())

  it('offers the model as an optional field, checked like the gateway\'s', () => {
    expect(paramsEligibility('claude-code', { model: '' }).ok).toBe(true)
    expect(paramsEligibility('claude-code', { model: 'opus' }).ok).toBe(true)
    expect(paramsEligibility('claude-code', { model: 'claude-opus-5-5' }).ok).toBe(true)
    const bad = paramsEligibility('claude-code', { model: '--dangerously-skip-permissions' })
    expect(bad.ok).toBe(false)
    expect(bad.reason).toContain('That model has characters the daemon will not accept')
  })

  it('offers each model family and every effort level, each after Claude Code\'s default', () => {
    expect(CLAUDE_CODE_MODELS.map((m) => m.id)).toEqual(['', 'haiku', 'sonnet', 'opus', 'fable'])
    expect(CLAUDE_CODE_EFFORTS.map((e) => e.id)).toEqual(['', 'low', 'medium', 'high', 'xhigh', 'max'])
    for (const { id } of [...CLAUDE_CODE_MODELS, ...CLAUDE_CODE_EFFORTS]) {
      expect(paramsEligibility('claude-code', { model: id, effort: id }).ok).toBe(true)
    }
    expect(launchParamsPayload('claude-code', { model: '', effort: 'high' })).toEqual({ effort: 'high' })
  })

  it('builds the model steps from what the machine\'s Claude reported, fastest first', () => {
    const steps = claudeModelSteps([
      { id: 'default', name: 'Default (recommended)', description: 'Opus 5.5' },
      { id: 'opus', name: 'Opus 5.5' },
      { id: 'fable', name: 'Fable 5.1' },
      { id: 'mythos' },
      { id: 'haiku', name: 'Haiku 5.5' },
      { id: 'claude-sonnet-4-6', name: 'Sonnet 4.6' },
      { name: 'no id' },
      null,
    ])
    expect(steps).toEqual([
      { id: '', name: 'Default (Opus 5.5)' },
      { id: 'haiku', name: 'Haiku 5.5' },
      { id: 'opus', name: 'Opus 5.5' },
      { id: 'fable', name: 'Fable 5.1' },
      { id: 'mythos', name: 'mythos' },
    ])
    expect(claudeModelSteps([{ id: 'sonnet', name: 'Sonnet 5.5' }])[0]).toEqual({ id: '', name: 'Default' })
    expect(claudeModelSteps([])).toBe(CLAUDE_CODE_MODELS)
    expect(claudeModelSteps(undefined)).toBe(CLAUDE_CODE_MODELS)
  })

  it('places a value on its step, and anything else on the default', () => {
    expect(stepIndex(CLAUDE_CODE_MODELS, 'opus')).toBe(3)
    expect(stepIndex(CLAUDE_CODE_EFFORTS, 'max')).toBe(5)
    expect(stepIndex(CLAUDE_CODE_MODELS, 'opusplan')).toBe(0)
    expect(stepIndex(CLAUDE_CODE_MODELS, undefined)).toBe(0)
  })

  it('has nothing remembered at first, and round-trips what was', () => {
    expect(lastClaudeCodeChoice()).toBeNull()
    rememberClaudeCodeChoice({ model: 'sonnet', effort: 'xhigh' })
    expect(lastClaudeCodeChoice()).toEqual({ model: 'sonnet', effort: 'xhigh' })
    rememberClaudeCodeChoice({})
    expect(lastClaudeCodeChoice()).toEqual({ model: '', effort: '' })
  })

  it('treats unreadable or misshapen storage as nothing remembered', () => {
    for (const bad of ['not json', 'null', '3']) {
      localStorage.setItem('agentrq:lastClaudeCode', bad)
      expect(lastClaudeCodeChoice()).toBeNull()
    }
    // A value no slider step stands for opens on the default instead.
    localStorage.setItem('agentrq:lastClaudeCode', '{"model":"opusplan","effort":"ultra"}')
    expect(lastClaudeCodeChoice()).toEqual({ model: '', effort: '' })
    localStorage.setItem('agentrq:lastClaudeCode', '{"model":"opus"}')
    expect(lastClaudeCodeChoice()).toEqual({ model: 'opus', effort: '' })
  })

  it('survives storage that throws', () => {
    const set = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('QuotaExceededError: storage is full') })
    expect(() => rememberClaudeCodeChoice({ model: 'opus' })).not.toThrow()
    set.mockRestore()
  })

  it('opens each kind on its last launch, or its defaults', () => {
    expect(initialParams('claude-code')).toEqual({ model: '', effort: '' })
    expect(initialParams('acp-gateway')).toEqual(GATEWAY_DEFAULTS)
    rememberParams('claude-code', { model: 'haiku', effort: 'low' })
    rememberParams('acp-gateway', { agent: 'codex-acp', model: 'gpt-5.5' })
    expect(initialParams('claude-code')).toEqual({ model: 'haiku', effort: 'low' })
    expect(initialParams('acp-gateway')).toEqual({ agent: 'codex-acp', model: 'gpt-5.5' })
  })

  it('keeps each kind\'s fields apart when the kind is switched back and forth', () => {
    const kind = ref('claude-code')
    const params = useKindParams(kind)
    params.value = { model: 'opus' }
    kind.value = 'acp-gateway'
    expect(params.value).toEqual(GATEWAY_DEFAULTS)
    params.value = { ...params.value, agent: 'codex-acp' }
    kind.value = 'claude-code'
    expect(params.value).toEqual({ model: 'opus' })
    kind.value = 'acp-gateway'
    expect(params.value.agent).toBe('codex-acp')
  })

  it('sends the chosen model with a Claude Code launch, and remembers it', async () => {
    const h = harness()
    await h.l.load()
    h.l.workspaceId.value = 'ws1'
    h.l.params.value = { model: ' opus ', effort: 'max' }
    await h.l.launch()
    expect(h.deps.launchAgent).toHaveBeenCalledWith('ws1', expect.objectContaining({ kind: 'claude-code', model: 'opus', effort: 'max' }))
    expect(lastClaudeCodeChoice()).toEqual({ model: 'opus', effort: 'max' })
  })

  it('sends no model when none was chosen, so Claude Code starts on its default', async () => {
    const h = harness()
    await h.l.load()
    h.l.workspaceId.value = 'ws1'
    await h.l.launch()
    expect(h.deps.launchAgent.mock.calls[0][1]).not.toHaveProperty('model')
    expect(h.deps.launchAgent.mock.calls[0][1]).not.toHaveProperty('effort')
    expect(lastClaudeCodeChoice()).toEqual({ model: '', effort: '' })
  })
})
