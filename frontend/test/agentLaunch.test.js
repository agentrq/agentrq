// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, it, expect, vi } from 'vitest'
import { ref } from 'vue'
import {
  useAgentLaunch,
  workspaceEligibility,
  machineEligibility,
  sessionEligibility,
  paramsEligibility,
  KINDS,
  GATEWAY_DEFAULTS,
  INITIAL_COLS,
  INITIAL_ROWS,
} from '../src/composables/useAgentLaunch.js'

const READY_WORKSPACE = {
  id: 'ws1',
  name: 'Ops',
  agentConnected: false,
  workingDirectory: '/srv/app',
}
const READY_MACHINE = { id: 'm1', name: 'rpi', enabled: true, online: true }

function harness(over = {}) {
  const machine = ref(over.machine === undefined ? { ...READY_MACHINE } : over.machine)
  const sessions = ref(over.sessions ?? [])
  const deps = {
    machine,
    sessions,
    fetchWorkspaces: vi.fn().mockResolvedValue({ workspaces: [{ ...READY_WORKSPACE }] }),
    launchAgent: vi.fn().mockResolvedValue({ session: { id: 's1', status: 'starting' } }),
    ...over.deps,
  }
  return { deps, machine, sessions, l: useAgentLaunch(deps) }
}

// The reason is the useful half: "cannot launch" says somebody is stuck,
// "no working directory" says what to do about it.
describe('workspaceEligibility', () => {
  it('accepts a workspace with a folder and no agent', () => {
    expect(workspaceEligibility(READY_WORKSPACE).ok).toBe(true)
  })

  it('refuses one that already has an agent', () => {
    const e = workspaceEligibility({ ...READY_WORKSPACE, agentConnected: true })
    expect(e.ok).toBe(false)
    expect(e.reason).toContain('already has an agent')
  })

  // The one refusal that is fixable from the interface, so it says where.
  it('refuses one with no folder, and names the page that sets it', () => {
    const e = workspaceEligibility({ ...READY_WORKSPACE, workingDirectory: '' })
    expect(e.ok).toBe(false)
    expect(e.reason).toContain('working directory')
    expect(e.fix.to).toBe('/workspaces/ws1/settings')
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

  it('needs a model and an agent for the gateway', () => {
    expect(paramsEligibility('acp-gateway', {}).reason).toContain('model')
    expect(paramsEligibility('acp-gateway', { model: 'm' }).reason).toContain('agent')
    expect(paramsEligibility('acp-gateway', GATEWAY_DEFAULTS).ok).toBe(true)
  })

  it('treats whitespace as empty', () => {
    expect(paramsEligibility('acp-gateway', { model: '   ', agent: 'a' }).ok).toBe(false)
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
      cols: INITIAL_COLS,
      rows: INITIAL_ROWS,
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

  // Two empty required fields is a worse first impression than a working
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
})
