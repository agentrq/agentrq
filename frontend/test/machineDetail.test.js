// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi } from 'vitest'
import {
  useMachineDetail,
  updateConsequence,
  deleteConsequence,
  versionAtLeast,
  REMOTE_CONTROL_MIN_VERSION,
} from '../src/composables/useMachineDetail.js'

const MACHINE = { id: 'm1', name: 'rpi', enabled: true, sessions: 1, metrics: { cpuPercent: 4 } }
const SESSIONS = [
  { id: 's1', machineId: 'm1', kind: 'claude-code', status: 'running' },
  { id: 's2', machineId: 'm1', kind: 'acp-gateway', status: 'exited' },
]

function harness(over = {}) {
  const deps = {
    machineId: 'm1',
    getMachine: vi.fn().mockResolvedValue({ machine: { ...MACHINE } }),
    fetchMachineSessions: vi.fn().mockResolvedValue({ sessions: SESSIONS.map((s) => ({ ...s })) }),
    updateMachine: vi.fn().mockResolvedValue({ machine: { ...MACHINE, name: 'renamed' } }),
    deleteMachine: vi.fn().mockResolvedValue(true),
    killSession: vi.fn().mockResolvedValue(true),
    restartDaemon: vi.fn().mockResolvedValue(true),
    ...over,
  }
  return { deps, d: useMachineDetail(deps) }
}

// A bare "Update?" is not consent. The person pressing the button is usually
// not the person whose agent is mid-task.
describe('updateConsequence', () => {
  it('names what it destroys, and says they come back', () => {
    const text = updateConsequence(3)
    expect(text).toContain('3 running sessions')
    expect(text).toContain('starts them again')
    expect(text).toContain('Unsaved work is lost')
  })

  it('counts one session as one', () => {
    expect(updateConsequence(1)).toContain('1 running session and starts it again')
    expect(updateConsequence(1)).not.toContain('1 running sessions')
  })

  it('says plainly when nothing is at stake', () => {
    expect(updateConsequence(0)).toContain('Nothing is running on it')
    expect(updateConsequence(undefined)).toContain('Nothing is running on it')
    expect(updateConsequence(-4)).toContain('Nothing is running on it')
  })
})

// Deleting is not disabling, and that is the part people do not expect.
describe('deleteConsequence', () => {
  it('says the machine has to be enrolled again by hand', () => {
    const text = deleteConsequence({ name: 'rpi' }, 0)
    expect(text).toContain('rpi')
    expect(text).toContain('enrolment command on the machine itself')
  })

  it('mentions what is still running on it', () => {
    expect(deleteConsequence({ name: 'rpi' }, 2)).toContain('2 sessions are')
    expect(deleteConsequence({ name: 'rpi' }, 1)).toContain('1 session is')
    expect(deleteConsequence({ name: 'rpi' }, 0)).not.toContain('still running')
  })

  it('copes with a machine that has no name yet', () => {
    expect(deleteConsequence(null, undefined)).toContain('this machine')
  })
})

describe('loading', () => {
  it('loads the machine and its sessions together', async () => {
    const h = harness()
    await h.d.load()
    expect(h.d.machine.value.name).toBe('rpi')
    expect(h.d.sessions.value).toHaveLength(2)
    expect(h.d.liveSessions.value).toHaveLength(1)
    expect(h.d.loading.value).toBe(false)
  })

  it('re-reads quietly when asked, so a refresh does not blank the page', async () => {
    let resolve
    const h = harness()
    h.deps.getMachine.mockImplementationOnce(() => Promise.resolve({ machine: { ...MACHINE, name: 'renamed' } }))
    await h.d.load()
    h.deps.getMachine.mockImplementation(() => new Promise((r) => { resolve = r }))
    const pending = h.d.load({ quiet: true })
    expect(h.d.loading.value).toBe(false)
    resolve({ machine: { ...MACHINE, name: 'renamed' } })
    await pending
    expect(h.d.machine.value.name).toBe('renamed')
  })

  it('says what went wrong', async () => {
    const h = harness({ getMachine: vi.fn().mockRejectedValue(new Error('not found')) })
    await h.d.load()
    expect(h.d.error.value).toBe('not found')
  })

  it('falls back to a message when the failure has none', async () => {
    const h = harness({ getMachine: vi.fn().mockRejectedValue({}) })
    await h.d.load()
    expect(h.d.error.value).toBe('Failed to load this machine')
  })

  it('copes with empty responses', async () => {
    const h = harness({
      getMachine: vi.fn().mockResolvedValue({}),
      fetchMachineSessions: vi.fn().mockResolvedValue({}),
    })
    await h.d.load()
    expect(h.d.machine.value).toBeNull()
    expect(h.d.sessions.value).toEqual([])
  })
})

describe('renaming', () => {
  it('sends the trimmed name', async () => {
    const h = harness()
    await h.d.load()
    expect(await h.d.rename('  renamed  ')).toBe(true)
    expect(h.deps.updateMachine).toHaveBeenCalledWith('m1', { name: 'renamed' })
  })

  // Taking the response whole would blank both every time somebody renamed a
  // box: an update carries the machine without its count or its metrics.
  it('keeps the count and the metrics the page already had', async () => {
    const h = harness({
      updateMachine: vi.fn().mockResolvedValue({ machine: { id: 'm1', name: 'renamed' } }),
    })
    await h.d.load()
    await h.d.rename('renamed')
    expect(h.d.machine.value.name).toBe('renamed')
    expect(h.d.machine.value.sessions).toBe(1)
    expect(h.d.machine.value.metrics.cpuPercent).toBe(4)
  })

  it('does nothing for an empty name or the name it already has', async () => {
    const h = harness()
    await h.d.load()
    expect(await h.d.rename('   ')).toBe(false)
    expect(await h.d.rename(undefined)).toBe(false)
    expect(await h.d.rename('rpi')).toBe(false)
    expect(h.deps.updateMachine).not.toHaveBeenCalled()
  })

  it('reports a rename that failed', async () => {
    const h = harness({ updateMachine: vi.fn().mockRejectedValue(new Error('name taken')) })
    await h.d.load()
    expect(await h.d.rename('renamed')).toBe(false)
    expect(h.d.error.value).toBe('name taken')
    expect(h.d.busy.value).toBe(false)
  })

  it('copes with an update response carrying no machine', async () => {
    const h = harness({ updateMachine: vi.fn().mockResolvedValue({}) })
    await h.d.load()
    expect(await h.d.rename('renamed')).toBe(true)
    expect(h.d.machine.value.name).toBe('rpi')
  })
})

describe('the kill switch', () => {
  it('turns a machine off and on', async () => {
    const h = harness()
    await h.d.load()
    expect(await h.d.setEnabled(false)).toBe(true)
    expect(h.deps.updateMachine).toHaveBeenCalledWith('m1', { enabled: false })
    await h.d.setEnabled(true)
    expect(h.deps.updateMachine).toHaveBeenLastCalledWith('m1', { enabled: true })
  })

  it('says which direction failed', async () => {
    const h = harness({ updateMachine: vi.fn().mockRejectedValue({}) })
    await h.d.load()
    await h.d.setEnabled(false)
    expect(h.d.error.value).toBe('Failed to disable this machine')
    await h.d.setEnabled(true)
    expect(h.d.error.value).toBe('Failed to enable this machine')
  })
})

describe('the agent limit', () => {
  it('sends a number, 0 or null as it is', async () => {
    const h = harness()
    await h.d.load()
    for (const n of [3, 0, null]) {
      expect(await h.d.setMaxSessions(n)).toBe(true)
      expect(h.deps.updateMachine).toHaveBeenLastCalledWith('m1', { maxSessions: n })
    }
  })

  it('says what failed', async () => {
    const h = harness({ updateMachine: vi.fn().mockRejectedValue({}) })
    await h.d.load()
    expect(await h.d.setMaxSessions(2)).toBe(false)
    expect(h.d.error.value).toBe("Failed to set this machine's agent limit")
  })
})

describe('deleting', () => {
  it('deletes', async () => {
    const h = harness()
    expect(await h.d.remove()).toBe(true)
    expect(h.deps.deleteMachine).toHaveBeenCalledWith('m1')
  })

  it('reports a delete that failed, rather than navigating away', async () => {
    const h = harness({ deleteMachine: vi.fn().mockRejectedValue(new Error('still connected')) })
    expect(await h.d.remove()).toBe(false)
    expect(h.d.error.value).toBe('still connected')
  })

  it('falls back to a message', async () => {
    const h = harness({ deleteMachine: vi.fn().mockRejectedValue({}) })
    await h.d.remove()
    expect(h.d.error.value).toBe('Failed to delete this machine')
  })
})

describe('stopping a session', () => {
  // A list that said "killed" for a process still running would be a kill
  // switch that lies about having worked.
  it('does not mark the row dead on its own', async () => {
    const h = harness()
    await h.d.load()
    expect(await h.d.stop('s1')).toBe(true)
    expect(h.deps.killSession).toHaveBeenCalledWith('s1')
    expect(h.d.sessions.value[0].status).toBe('running')
  })

  it('reports a kill that could not be sent', async () => {
    const h = harness({ killSession: vi.fn().mockRejectedValue(new Error('machine not connected')) })
    await h.d.load()
    expect(await h.d.stop('s1')).toBe(false)
    expect(h.d.error.value).toBe('machine not connected')
  })

  it('falls back to a message', async () => {
    const h = harness({ killSession: vi.fn().mockRejectedValue({}) })
    await h.d.stop('s1')
    expect(h.d.error.value).toBe('Failed to stop the session')
  })
})

describe('updating, which is the same command', () => {
  // The version travels with it, so a release that appeared between the offer
  // and the yes is refused rather than installed.
  it('sends the version the machine actually offered', async () => {
    const h = harness({
      getMachine: vi.fn().mockResolvedValue({ machine: { ...MACHINE, availableVersion: '0.7.1' } }),
    })
    await h.d.load()
    expect(h.d.updating.value).toBe(true)
    expect(await h.d.restart()).toBe(true)
    expect(h.deps.restartDaemon).toHaveBeenCalledWith('m1', '0.7.1')
  })

  it('is a plain restart when nothing has been offered', async () => {
    const h = harness()
    await h.d.load()
    expect(h.d.updating.value).toBe(false)
    await h.d.restart()
    expect(h.deps.restartDaemon).toHaveBeenCalledWith('m1', '')
  })

  it('falls back to a message that says it was an update', async () => {
    const h = harness({
      getMachine: vi.fn().mockResolvedValue({ machine: { ...MACHINE, availableVersion: '0.7.1' } }),
      restartDaemon: vi.fn().mockRejectedValue({}),
    })
    await h.d.load()
    expect(await h.d.restart()).toBe(false)
    expect(h.d.error.value).toBe('Failed to update this machine')
  })
})

// The same answers as wire.VersionAtLeast, which the server refuses by.
describe('versionAtLeast', () => {
  it('compares as semver, not as text', () => {
    expect(versionAtLeast('0.9.3', '0.9.3')).toBe(true)
    expect(versionAtLeast('0.9.10', '0.9.3')).toBe(true)
    expect(versionAtLeast('1.0.0', '0.9.3')).toBe(true)
    expect(versionAtLeast('0.9.2', '0.9.3')).toBe(false)
    expect(versionAtLeast('0.8.9', '0.9.3')).toBe(false)
  })

  it('puts a pre-release below its release', () => {
    expect(versionAtLeast('0.9.3-rc.1', '0.9.3')).toBe(false)
    expect(versionAtLeast('0.9.4-rc.1', '0.9.3')).toBe(true)
  })

  it('puts anything that is not a release below everything', () => {
    for (const v of ['dev', '', undefined, null, 'v0.9.3', '0.9']) {
      expect(versionAtLeast(v, '0.9.3'), String(v)).toBe(false)
    }
    expect(versionAtLeast('0.9.3', 'not a version')).toBe(false)
  })
})

describe('restarting', () => {
  it('can be asked of a daemon new enough to do it', async () => {
    const h = harness({
      getMachine: vi.fn().mockResolvedValue({ machine: { ...MACHINE, version: REMOTE_CONTROL_MIN_VERSION } }),
    })
    await h.d.load()
    expect(h.d.canControl.value).toBe(true)
    expect(await h.d.restart()).toBe(true)
    expect(h.deps.restartDaemon).toHaveBeenCalledWith('m1', '')
    expect(h.d.busy.value).toBe(false)
  })

  it('cannot be asked of an older one', async () => {
    const h = harness({ getMachine: vi.fn().mockResolvedValue({ machine: { ...MACHINE, version: '0.9.2' } }) })
    await h.d.load()
    expect(h.d.canControl.value).toBe(false)
  })

  it('reports a restart the server refused', async () => {
    const h = harness({ restartDaemon: vi.fn().mockRejectedValue(new Error('that machine is not connected')) })
    expect(await h.d.restart()).toBe(false)
    expect(h.d.error.value).toBe('that machine is not connected')
  })

  it('falls back to a message', async () => {
    const h = harness({ restartDaemon: vi.fn().mockRejectedValue({}) })
    await h.d.restart()
    expect(h.d.error.value).toBe('Failed to restart this machine')
  })
})

describe('live updates', () => {
  it('folds metrics into the machine', async () => {
    const h = harness()
    await h.d.load()
    h.d.handleEvent({ type: 'machine.updated', payload: { id: 'm1', metrics: { cpuPercent: 9 } } })
    expect(h.d.machine.value.metrics.cpuPercent).toBe(9)
    expect(h.d.machine.value.name).toBe('rpi')
  })

  it('ignores an update for a different machine', async () => {
    const h = harness()
    await h.d.load()
    h.d.handleEvent({ type: 'machine.updated', payload: { id: 'other', metrics: { cpuPercent: 9 } } })
    h.d.handleEvent({ type: 'machine.updated', payload: {} })
    expect(h.d.machine.value.metrics.cpuPercent).toBe(4)
  })

  it('updates a live session in place', async () => {
    const h = harness()
    await h.d.load()
    h.d.handleEvent({
      type: 'session.updated',
      payload: { id: 's1', machineId: 'm1', status: 'starting' },
    })
    expect(h.d.sessions.value[0].status).toBe('starting')
    // The fields the event does not carry are the ones the page already had.
    expect(h.d.sessions.value[0].kind).toBe('claude-code')
  })

  // The row behind it has gone too: this page answers "what is running on this
  // machine", and a finished agent is not an answer to that.
  it('drops a session that has ended', async () => {
    for (const status of ['exited', 'killed', 'failed']) {
      const h = harness()
      await h.d.load()
      h.d.handleEvent({ type: 'session.updated', payload: { id: 's1', machineId: 'm1', status } })
      expect(h.d.sessions.value.find((s) => s.id === 's1'), status).toBeUndefined()
    }
  })

  // One that ended before the page loaded is not added just to be removed.
  it('never adds a session that has already ended', async () => {
    const h = harness()
    await h.d.load()
    const before = h.d.sessions.value.length
    h.d.handleEvent({
      type: 'session.updated',
      payload: { id: 'new', machineId: 'm1', status: 'exited' },
    })
    expect(h.d.sessions.value).toHaveLength(before)
  })

  it('shows a session that started while somebody was watching', async () => {
    const h = harness()
    await h.d.load()
    h.d.handleEvent({
      type: 'session.updated',
      payload: { id: 's3', machineId: 'm1', status: 'starting' },
    })
    expect(h.d.sessions.value).toHaveLength(3)
    expect(h.d.sessions.value[0].id).toBe('s3')
  })

  it('does not adopt a session from another machine', async () => {
    const h = harness()
    await h.d.load()
    h.d.handleEvent({
      type: 'session.updated',
      payload: { id: 's9', machineId: 'elsewhere', status: 'running' },
    })
    expect(h.d.sessions.value).toHaveLength(2)
  })

  it('ignores events it does not understand', async () => {
    const h = harness()
    await h.d.load()
    h.d.handleEvent({ type: 'task.updated', payload: { id: 's1' } })
    h.d.handleEvent({ type: 'session.updated', payload: {} })
    h.d.handleEvent(undefined)
    expect(h.d.sessions.value).toHaveLength(2)
    expect(h.d.sessions.value[0].status).toBe('running')
  })
})

// Every dependency defaults to the real API, so a view supplies only the id.
describe('defaults', () => {
  it('reaches for the real API when nothing is injected', async () => {
    const d = useMachineDetail({ machineId: 'm1' })
    await d.load()
    expect(d.error.value).not.toBe('')
  })
})
