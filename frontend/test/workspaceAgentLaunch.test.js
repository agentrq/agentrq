// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, it, expect, vi } from 'vitest'
import { nextTick, ref } from 'vue'
import {
  launchableMachines,
  machineChoiceEligibility,
  useWorkspaceAgentLaunch,
} from '../src/composables/useWorkspaceAgentLaunch.js'
import { KINDS, lastLaunchChoice, lastLaunchParams } from '../src/composables/useAgentLaunch.js'

const OFFLINE_WORKSPACE = {
  id: 'ws1',
  name: 'Ops',
  agentConnected: false,
  workingDirectory: '/srv/app',
}
const READY_MACHINE = { id: 'm1', name: 'rpi', enabled: true, online: true }
const SECOND_MACHINE = { id: 'm2', name: 'laptop', enabled: true, online: true }

// What a real measurement would answer, standing in for the one this
// composable no longer hardcodes. The measurement itself is
// `launchTerminalSize`'s own test.
const MEASURED_SIZE = { cols: 164, rows: 52 }

function harness(over = {}) {
  const workspace = ref(
    over.workspace === undefined ? { ...OFFLINE_WORKSPACE } : over.workspace
  )
  const deps = {
    workspace,
    ...(over.parent ? { parent: ref(over.parent) } : {}),
    fetchMachines: vi.fn().mockResolvedValue({
      machines: over.machines ?? [{ ...READY_MACHINE }],
    }),
    measureTerminalSize: vi.fn().mockResolvedValue(MEASURED_SIZE),
    launchAgent: vi.fn().mockResolvedValue({ session: { id: 's1', status: 'starting' } }),
    fetchAcpAgents: vi.fn().mockResolvedValue({ agents: [] }),
    fetchAcpModels: vi.fn().mockResolvedValue({ agent: '', models: [] }),
    ...over.deps,
  }
  return { deps, workspace, l: useWorkspaceAgentLaunch(deps) }
}

// Two ticks: one for the watcher to run and call the (mocked, already
// resolved) fetch, one for its `.then` continuation to land.
async function flush() {
  await nextTick()
  await nextTick()
}

// A launch remembers its machine and kind, and the next harness opens on them.
beforeEach(() => localStorage.clear())

describe('launchableMachines', () => {
  it('keeps machines that are both enabled and online', () => {
    expect(launchableMachines([READY_MACHINE, SECOND_MACHINE])).toHaveLength(2)
  })

  it('drops a machine failing either flag, because they mean different things', () => {
    const machines = [
      READY_MACHINE,
      { id: 'off', enabled: true, online: false },
      { id: 'disabled', enabled: false, online: true },
      { id: 'neither', enabled: false, online: false },
    ]
    expect(launchableMachines(machines).map((m) => m.id)).toEqual(['m1'])
  })

  it('copes with nothing at all', () => {
    expect(launchableMachines()).toEqual([])
    expect(launchableMachines([null])).toEqual([])
  })
})

describe('machineChoiceEligibility', () => {
  it('accepts a chosen machine that can run', () => {
    expect(machineChoiceEligibility('m1', [READY_MACHINE]).ok).toBe(true)
  })

  it('points at the machines page when there is nowhere to run', () => {
    // The fix matters more than the refusal: somebody with no machine needs to
    // go and make one, not read that they cannot launch.
    const e = machineChoiceEligibility('', [{ id: 'off', enabled: true, online: false }])
    expect(e.ok).toBe(false)
    expect(e.reason).toMatch(/No machine is online/)
    expect(e.fix).toEqual({ label: 'Set up a machine', to: '/machines' })
  })

  it('asks for a choice when several could run', () => {
    const e = machineChoiceEligibility('', [READY_MACHINE, SECOND_MACHINE])
    expect(e.ok).toBe(false)
    expect(e.reason).toMatch(/Pick a machine/)
  })

  it('says so when the chosen machine has since gone offline', () => {
    // The list is live; the machine picked a moment ago can drop out.
    const e = machineChoiceEligibility('m1', [SECOND_MACHINE])
    expect(e.ok).toBe(false)
    expect(e.reason).toMatch(/no longer online/)
  })
})

describe('useWorkspaceAgentLaunch: loading machines', () => {
  it('loads the machines and preselects the only one that can run', async () => {
    const { l } = harness()
    await l.load()
    expect(l.machines.value).toHaveLength(1)
    expect(l.machineId.value).toBe('m1')
    expect(l.loaded.value).toBe(true)
    expect(l.loading.value).toBe(false)
  })

  it('leaves the choice alone when several machines could run', async () => {
    // Guessing would start an agent on the wrong computer.
    const { l } = harness({ machines: [READY_MACHINE, SECOND_MACHINE] })
    await l.load()
    expect(l.machineId.value).toBe('')
    expect(l.available.value).toHaveLength(2)
  })

  it('does not preselect an offline machine', async () => {
    const { l } = harness({ machines: [{ id: 'off', enabled: true, online: false }] })
    await l.load()
    expect(l.machineId.value).toBe('')
    expect(l.available.value).toEqual([])
  })

  it('keeps a choice already made', async () => {
    const { l } = harness()
    l.machineId.value = 'm9'
    await l.load()
    expect(l.machineId.value).toBe('m9')
  })

  it('copes with a response carrying no machines', async () => {
    const { l } = harness({ deps: { fetchMachines: vi.fn().mockResolvedValue({}) } })
    await l.load()
    expect(l.machines.value).toEqual([])
    expect(l.loaded.value).toBe(true)
  })

  it('reports a failure to load and stays unloaded', async () => {
    const { l } = harness({
      deps: { fetchMachines: vi.fn().mockRejectedValue(new Error('network gone')) },
    })
    await l.load()
    expect(l.error.value).toBe('network gone')
    expect(l.loaded.value).toBe(false)
    expect(l.loading.value).toBe(false)
  })

  it('falls back to a message when the failure carries none', async () => {
    const { l } = harness({ deps: { fetchMachines: vi.fn().mockRejectedValue({}) } })
    await l.load()
    expect(l.error.value).toBe('Failed to load machines')
  })
})

describe('useWorkspaceAgentLaunch: whether to offer it at all', () => {
  it('offers once machines are known and one can run', async () => {
    const { l } = harness()
    expect(l.offered.value).toBe(false)
    await l.load()
    expect(l.offered.value).toBe(true)
  })

  it('stays quiet while the machines are still arriving', () => {
    // Otherwise the button flickers in as the list lands.
    const { l } = harness()
    expect(l.loaded.value).toBe(false)
    expect(l.offered.value).toBe(false)
  })

  it('stays quiet when there is nowhere to run', async () => {
    // An offer that resolves to "you have no machines" is worse than the setup
    // guide that was already there: it looks like a way forward and is not.
    const { l } = harness({ machines: [{ id: 'off', enabled: false, online: false }] })
    await l.load()
    expect(l.offered.value).toBe(false)
  })

  it('stays quiet when the workspace already has an agent', async () => {
    const { l } = harness({ workspace: { ...OFFLINE_WORKSPACE, agentConnected: true } })
    await l.load()
    expect(l.offered.value).toBe(false)
  })

  it('stays quiet until there is a workspace to launch for', async () => {
    // The detail page renders before its workspace arrives; offering to start
    // an agent for nothing is an offer that cannot be honoured.
    const { l } = harness({ workspace: null })
    await l.load()
    expect(l.offered.value).toBe(false)
  })
})

describe('useWorkspaceAgentLaunch: whether there is no machine at all', () => {
  it('says nothing while the machines are still arriving', () => {
    const { l } = harness({ machines: [] })
    expect(l.noMachine.value).toBe(false)
  })

  it('says so when none is online and enabled', async () => {
    const { l } = harness({
      machines: [
        { id: 'off', enabled: true, online: false },
        { id: 'disabled', enabled: false, online: true },
      ],
    })
    await l.load()
    expect(l.noMachine.value).toBe(true)
  })

  it('does not, when one can run, even with an agent already connected', async () => {
    // Unlike the offer: a connected agent is no reason to set up a machine.
    const { l } = harness({ workspace: { ...OFFLINE_WORKSPACE, agentConnected: true } })
    await l.load()
    expect(l.noMachine.value).toBe(false)
  })
})

describe('useWorkspaceAgentLaunch: blockers', () => {
  it('has none when everything is ready', async () => {
    const { l } = harness()
    await l.load()
    expect(l.blockers.value).toEqual([])
    expect(l.canLaunch.value).toBe(true)
  })

  it('leads with the machine, because nothing runs on one that is off', async () => {
    const { l } = harness({
      machines: [],
      workspace: { ...OFFLINE_WORKSPACE, workingDirectory: '' },
    })
    await l.load()
    expect(l.blockers.value[0].reason).toMatch(/No machine is online/)
  })

  it('surfaces the workspace having no working directory, with the fix', async () => {
    // Likely to be common: a workspace that has never run an agent is exactly
    // the one that is offline.
    const { l } = harness({ workspace: { ...OFFLINE_WORKSPACE, workingDirectory: '' } })
    await l.load()
    const blocker = l.blockers.value.find((b) => /working directory/.test(b.reason))
    expect(blocker.fix).toEqual({
      label: 'Set one in workspace settings',
      to: '/workspaces/ws1/settings',
    })
    expect(l.canLaunch.value).toBe(false)
  })

  it('refuses a gateway with its parameters missing or unsafe', async () => {
    const { l } = harness()
    await l.load()
    l.kind.value = 'acp-gateway'
    l.params.value = { model: '', agent: '' }
    expect(l.canLaunch.value).toBe(false)

    l.params.value = { model: '--rm -rf', agent: 'antigravity-acp' }
    expect(l.blockers.value[0].reason).toMatch(/characters the daemon will not accept/)

    // The agent alone is enough — the model is the gateway's own choice to
    // make when nobody names one.
    l.params.value = { model: '', agent: 'antigravity-acp' }
    expect(l.canLaunch.value).toBe(true)

    l.params.value = { model: 'gemini-3.8-flash-high', agent: 'antigravity-acp' }
    expect(l.canLaunch.value).toBe(true)
  })

  it('cannot launch while a launch is in flight', async () => {
    let release
    const launchAgent = vi.fn(
      () => new Promise((resolve) => {
        release = () => resolve({ session: { id: 's1' } })
      })
    )
    const { l } = harness({ deps: { launchAgent } })
    await l.load()

    const pending = l.launch()
    expect(l.launching.value).toBe(true)
    expect(l.canLaunch.value).toBe(false)
    // `launch` awaits the terminal size before it awaits `launchAgent`, so
    // `release` is not assigned until that measurement has resolved.
    await vi.waitFor(() => expect(release).toBeInstanceOf(Function))
    release()
    await pending
    expect(l.launching.value).toBe(false)
  })
})

describe('useWorkspaceAgentLaunch: acp-gateway suggestions', () => {
  it('asks for agent suggestions once a machine is chosen and the kind is acp-gateway', async () => {
    const fetchAcpAgents = vi.fn().mockResolvedValue({ agents: [{ id: 'codex-acp', name: 'Codex' }] })
    const { l } = harness({ deps: { fetchAcpAgents } })
    await l.load() // auto-picks the only machine, m1
    l.kind.value = 'acp-gateway'
    await flush()

    expect(fetchAcpAgents).toHaveBeenCalledWith('m1')
    expect(l.acpAgents.value).toEqual([{ id: 'codex-acp', name: 'Codex' }])
  })

  it('copes with a response carrying no agents key at all', async () => {
    const fetchAcpAgents = vi.fn().mockResolvedValue(undefined)
    const { l } = harness({ deps: { fetchAcpAgents } })
    await l.load()
    l.kind.value = 'acp-gateway'
    await flush()
    expect(l.acpAgents.value).toEqual([])
  })

  it('asks for model suggestions once an agent is typed, with the workspace and machine', async () => {
    const fetchAcpModels = vi
      .fn()
      .mockResolvedValue({ agent: 'codex-acp', models: [{ id: 'gpt-5.5', current: true }] })
    const { l } = harness({ deps: { fetchAcpModels } })
    await l.load()
    l.kind.value = 'acp-gateway'
    l.params.value = { ...l.params.value, agent: 'codex-acp' }
    await flush()

    expect(fetchAcpModels).toHaveBeenCalledWith('ws1', 'm1', 'codex-acp')
    expect(l.acpModels.value).toEqual([{ id: 'gpt-5.5', current: true }])
  })

  it('copes with a response carrying no models key at all', async () => {
    const fetchAcpModels = vi.fn().mockResolvedValue(undefined)
    const { l } = harness({ deps: { fetchAcpModels } })
    await l.load()
    l.kind.value = 'acp-gateway'
    l.params.value = { ...l.params.value, agent: 'codex-acp' }
    await flush()
    expect(l.acpModels.value).toEqual([])
  })

  it('does not ask for models before a machine is chosen', async () => {
    const fetchAcpModels = vi.fn()
    const { l } = harness({ deps: { fetchAcpModels } })
    // Not calling load(): machineId stays '', even though the workspace and
    // an agent are both already there.
    l.kind.value = 'acp-gateway'
    l.params.value = { ...l.params.value, agent: 'codex-acp' }
    await flush()
    expect(fetchAcpModels).not.toHaveBeenCalled()
  })

  it('does not ask for models while the agent field is blank', async () => {
    const fetchAcpModels = vi.fn()
    const { l } = harness({ deps: { fetchAcpModels } })
    await l.load() // machine and workspace are both present
    l.kind.value = 'acp-gateway'
    l.params.value = { ...l.params.value, agent: '' }
    await flush()
    // Only the gateway's own lookup; Claude's is asked while the kind is Claude Code.
    expect(fetchAcpModels.mock.calls.filter(([, , agent]) => agent !== 'claude-acp')).toEqual([])
  })

  it('copes with params carrying no agent key at all, not only an empty one', async () => {
    const fetchAcpModels = vi.fn()
    const { l } = harness({ deps: { fetchAcpModels } })
    await l.load()
    l.kind.value = 'acp-gateway'
    l.params.value = { model: 'gemini-3.8-flash-high' } // no `agent` property
    await flush()
    // Only the gateway's own lookup; Claude's is asked while the kind is Claude Code.
    expect(fetchAcpModels.mock.calls.filter(([, , agent]) => agent !== 'claude-acp')).toEqual([])
  })

  it('fails open: a rejected lookup leaves the suggestions empty rather than throwing', async () => {
    const { l } = harness({ deps: { fetchAcpAgents: vi.fn().mockRejectedValue(new Error('offline')) } })
    await l.load()
    l.kind.value = 'acp-gateway'
    await flush()
    expect(l.acpAgents.value).toEqual([])
  })

  it('fails open: a rejected model lookup leaves the models empty rather than throwing', async () => {
    const { l } = harness({ deps: { fetchAcpModels: vi.fn().mockRejectedValue(new Error('offline')) } })
    await l.load()
    l.kind.value = 'acp-gateway'
    l.params.value = { ...l.params.value, agent: 'codex-acp' }
    await flush()
    expect(l.acpModels.value).toEqual([])
  })
})

describe('useWorkspaceAgentLaunch: launching', () => {
  it('asks the chosen machine to run the default kind', async () => {
    const { l, deps } = harness()
    await l.load()
    const session = await l.launch()

    expect(deps.launchAgent).toHaveBeenCalledWith('ws1', {
      machineId: 'm1',
      kind: KINDS[0].id,
      cols: MEASURED_SIZE.cols,
      rows: MEASURED_SIZE.rows,
    })
    expect(session).toEqual({ id: 's1', status: 'starting' })
  })

  it('sends the gateway parameters, trimmed', async () => {
    const { l, deps } = harness()
    await l.load()
    l.kind.value = 'acp-gateway'
    l.params.value = { model: '  gemini-3.8-flash-high  ', agent: ' antigravity-acp ' }
    await l.launch()

    expect(deps.launchAgent).toHaveBeenCalledWith('ws1', {
      machineId: 'm1',
      kind: 'acp-gateway',
      cols: MEASURED_SIZE.cols,
      rows: MEASURED_SIZE.rows,
      model: 'gemini-3.8-flash-high',
      agent: 'antigravity-acp',
    })
  })

  // Only the agent is mandatory: a launch with no model chosen must still
  // succeed, and must not send a `model` the gateway never asked for.
  it('sends the gateway just an agent when no model was chosen', async () => {
    const { l, deps } = harness()
    await l.load()
    l.kind.value = 'acp-gateway'
    l.params.value = { model: '', agent: 'antigravity-acp' }
    await l.launch()

    const sent = deps.launchAgent.mock.calls[0][1]
    expect(sent.agent).toBe('antigravity-acp')
    expect(sent).not.toHaveProperty('model')
  })

  it('sends nothing when it already knows the launch would be refused', async () => {
    // The button is disabled, but a keyboard or a stale page can still get here.
    const { l, deps } = harness({ machines: [] })
    await l.load()
    expect(await l.launch()).toBeNull()
    expect(deps.launchAgent).not.toHaveBeenCalled()
  })

  it('reports a refusal from the server, which is the real authority', async () => {
    // Two people can press at the same moment, and only the server sees both.
    const { l } = harness({
      deps: {
        launchAgent: vi.fn().mockRejectedValue(new Error('workspace already has an agent')),
      },
    })
    await l.load()
    expect(await l.launch()).toBeNull()
    expect(l.error.value).toBe('workspace already has an agent')
    expect(l.launching.value).toBe(false)
  })

  it('falls back to a message when the refusal carries none', async () => {
    const { l } = harness({ deps: { launchAgent: vi.fn().mockRejectedValue({}) } })
    await l.load()
    await l.launch()
    expect(l.error.value).toBe('Failed to start the agent')
  })

  it('returns null when the server answers without a session', async () => {
    const { l } = harness({ deps: { launchAgent: vi.fn().mockResolvedValue({}) } })
    await l.load()
    expect(await l.launch()).toBeNull()
  })

  it('exposes the selected machine so the page can name it', async () => {
    const { l } = harness({ machines: [READY_MACHINE, SECOND_MACHINE] })
    await l.load()
    expect(l.selected.value).toBeNull()
    l.machineId.value = 'm2'
    expect(l.selected.value.name).toBe('laptop')
  })
})

describe('a fork, and remembering the launch', () => {
  const FORK = { id: 'f1', name: 'Ops fork', agentConnected: false, forkOfId: 'ws1', workingDirectory: '' }

  it('is judged by its parent\'s folder', async () => {
    const ok = harness({ workspace: FORK, parent: OFFLINE_WORKSPACE })
    await ok.l.load()
    expect(ok.l.blockers.value).toEqual([])
    const bad = harness({ workspace: FORK, parent: { ...OFFLINE_WORKSPACE, workingDirectory: '' } })
    await bad.l.load()
    expect(bad.l.blockers.value[0].reason).toMatch(/made from Ops's/)
  })

  it('remembers the machine and kind for the workspace', async () => {
    localStorage.clear()
    const { l } = harness()
    await l.load()
    await l.launch()
    expect(lastLaunchChoice('ws1')).toEqual({ machineId: 'm1', kind: 'claude-code' })
    localStorage.clear()
  })

  it('opens on the machine and kind this workspace last launched with', async () => {
    const first = harness({ machines: [{ ...READY_MACHINE }, { ...SECOND_MACHINE }] })
    await first.l.load()
    expect(first.l.machineId.value).toBe('')
    first.l.machineId.value = 'm2'
    first.l.kind.value = 'acp-gateway'
    first.l.params.value = { agent: 'gemini', model: 'pro' }
    await first.l.launch()

    const next = harness({ machines: [{ ...READY_MACHINE }, { ...SECOND_MACHINE }] })
    await next.l.load()
    expect(next.l.machineId.value).toBe('m2')
    expect(next.l.kind.value).toBe('acp-gateway')
    expect(next.l.params.value).toEqual({ agent: 'gemini', model: 'pro' })
  })

  it('keeps the last kind, and asks for a machine, when the last one is offline', async () => {
    localStorage.setItem('agentrq:lastLaunch', JSON.stringify({ ws1: { machineId: 'gone', kind: 'acp-gateway' } }))
    const { l } = harness({ machines: [{ ...READY_MACHINE }, { ...SECOND_MACHINE }] })
    await l.load()
    expect(l.machineId.value).toBe('')
    expect(l.kind.value).toBe('acp-gateway')
  })

  it('leaves a machine already picked alone when the list reloads', async () => {
    localStorage.setItem('agentrq:lastLaunch', JSON.stringify({ ws1: { machineId: 'm2', kind: 'acp-gateway' } }))
    const { l } = harness({ machines: [{ ...READY_MACHINE }, { ...SECOND_MACHINE }] })
    l.machineId.value = 'm1'
    await l.load()
    expect(l.machineId.value).toBe('m1')
    expect(l.kind.value).toBe(KINDS[0].id)
  })

  it('lists the registry\'s agents it is handed, and counts a pick for the workspace', async () => {
    const recordTelemetry = vi.fn()
    const { l } = harness({ deps: { registryAgents: ref([{ id: 'codex-acp', name: 'Codex' }]), recordTelemetry } })
    l.kind.value = 'acp-gateway'
    expect(l.acpAgents.value).toEqual([{ id: 'codex-acp', name: 'Codex' }])
    l.pickAcpAgent('codex-acp')
    expect(l.params.value.agent).toBe('codex-acp')
    expect(recordTelemetry).toHaveBeenCalledWith('ui_acp_agent_pick', 'ws1')
  })

  it('opens the next time on the agent this workspace last ran', async () => {
    localStorage.clear()
    const first = harness()
    await first.l.load()
    first.l.kind.value = 'acp-gateway'
    first.l.params.value = { agent: 'gemini', model: 'pro' }
    await first.l.launch()
    expect(lastLaunchParams('ws1', 'acp-gateway')).toEqual({ agent: 'gemini', model: 'pro' })
    localStorage.removeItem('agentrq:lastAcpGateway')

    const next = harness()
    next.l.kind.value = 'acp-gateway'
    expect(next.l.params.value).toEqual({ agent: 'gemini', model: 'pro' })
    localStorage.clear()
  })
})

describe('useWorkspaceAgentLaunch: Claude Code\'s models', () => {
  const REPORTED = [
    { id: 'default', name: 'Default (recommended)', description: 'Opus 5.5' },
    { id: 'opus', name: 'Opus 5.5' },
    { id: 'sonnet', name: 'Sonnet 5.5' },
    { id: 'haiku', name: 'Haiku 5.5' },
    { id: 'claude-opus-4-8', name: 'Opus 4.8' },
  ]

  it('asks the chosen machine\'s Claude through the gateway, from the workspace\'s folder', async () => {
    const fetchAcpModels = vi.fn().mockResolvedValue({ agent: 'claude-acp', models: REPORTED })
    const { l } = harness({ deps: { fetchAcpModels } })
    await l.load()
    await flush()
    expect(fetchAcpModels).toHaveBeenCalledWith(OFFLINE_WORKSPACE.id, READY_MACHINE.id, 'claude-acp')
    expect(l.claudeModels.value.map((m) => m.name)).toEqual(['Default (Opus 5.5)', 'Haiku 5.5', 'Sonnet 5.5', 'Opus 5.5'])
  })

  it('keeps the fixed list when the machine cannot answer', async () => {
    const fetchAcpModels = vi.fn().mockRejectedValue(new Error('network unreachable'))
    const { l } = harness({ deps: { fetchAcpModels } })
    await l.load()
    await flush()
    expect(l.claudeModels.value.map((m) => m.id)).toEqual(['', 'haiku', 'sonnet', 'opus', 'fable'])
  })

  it('does not ask while the gateway is picked', async () => {
    const fetchAcpModels = vi.fn().mockResolvedValue({ models: [] })
    const { l } = harness({ deps: { fetchAcpModels } })
    l.kind.value = 'acp-gateway'
    l.params.value = { ...l.params.value, agent: '' }
    await l.load()
    await flush()
    expect(fetchAcpModels.mock.calls.filter(([, , agent]) => agent === 'claude-acp')).toEqual([])
  })
})
