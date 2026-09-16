// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, it, expect, vi } from 'vitest'
import {
  useMachines,
  mergeMachine,
  applyMachineEvent,
  applySessionEventToMachines,
} from '../src/composables/useMachines.js'

const machine = (over = {}) => ({
  id: 'm1',
  name: 'rpi',
  os: 'linux',
  arch: 'arm64',
  online: true,
  sessions: 0,
  ...over,
})

function harness(over = {}) {
  const fetchMachines = vi.fn().mockResolvedValue({ machines: [machine()] })
  const createEnrolmentCode = vi.fn().mockResolvedValue({ code: 'ABCD-EFGH', expiresAt: 'later' })
  const serverOrigin = vi.fn().mockResolvedValue('https://agentrq.example')
  return {
    fetchMachines,
    createEnrolmentCode,
    serverOrigin,
    m: useMachines({ fetchMachines, createEnrolmentCode, serverOrigin, ...over }),
  }
}

describe('mergeMachine', () => {
  // A heartbeat carries metrics and nothing else. Replacing would blank the
  // name, the OS and everything else the list had, on every heartbeat.
  it('keeps what a partial update does not mention', () => {
    const merged = mergeMachine(machine(), { id: 'm1', metrics: { cpuPercent: 4 } })
    expect(merged.name).toBe('rpi')
    expect(merged.metrics.cpuPercent).toBe(4)
  })

  it('refuses to merge one machine into another', () => {
    const current = machine()
    expect(mergeMachine(current, { id: 'm2', name: 'other' })).toBe(current)
  })

  it('handles an update with no machine and a machine with no update', () => {
    expect(mergeMachine(machine(), {})).toEqual(machine())
    expect(mergeMachine(null, { id: 'm1' })).toBeNull()
  })

  it('ignores fields that carry nothing', () => {
    const merged = mergeMachine(machine(), { id: 'm1', name: null, os: undefined, arch: 'x86' })
    expect(merged.name).toBe('rpi')
    expect(merged.os).toBe('linux')
    expect(merged.arch).toBe('x86')
  })
})

describe('applyMachineEvent', () => {
  it('folds an update into the right row', () => {
    const list = [machine(), machine({ id: 'm2', name: 'other' })]
    const next = applyMachineEvent(list, {
      type: 'machine.updated',
      payload: { id: 'm2', metrics: { cpuPercent: 9 } },
    })
    expect(next[0].metrics).toBeUndefined()
    expect(next[1].metrics.cpuPercent).toBe(9)
  })

  // A row built from a fragment would be a machine with no name. The next load
  // has the whole thing.
  it('ignores a machine the page has never heard of', () => {
    const list = [machine()]
    expect(applyMachineEvent(list, { type: 'machine.updated', payload: { id: 'ghost' } })).toEqual(list)
  })

  it('ignores anything that is not a machine update', () => {
    const list = [machine()]
    expect(applyMachineEvent(list, { type: 'task.updated', payload: { id: 'm1' } })).toBe(list)
    expect(applyMachineEvent(list, { type: 'machine.updated' })).toBe(list)
    expect(applyMachineEvent(list, undefined)).toBe(list)
  })
})

describe('applySessionEventToMachines', () => {
  it('steps the count up when an agent starts', () => {
    const next = applySessionEventToMachines(
      [machine({ sessions: 1 })],
      { type: 'session.updated', payload: { id: 's1', machineId: 'm1', status: 'running' } },
      false
    )
    expect(next[0].sessions).toBe(2)
  })

  it('steps it down when one ends', () => {
    const next = applySessionEventToMachines(
      [machine({ sessions: 2 })],
      { type: 'session.updated', payload: { id: 's1', machineId: 'm1', status: 'exited' } },
      true
    )
    expect(next[0].sessions).toBe(1)
  })

  // A second "running" report for a session that was already running would
  // otherwise count the same agent twice.
  it('does not count the same agent twice', () => {
    const list = [machine({ sessions: 1 })]
    const next = applySessionEventToMachines(
      list,
      { type: 'session.updated', payload: { id: 's1', machineId: 'm1', status: 'running' } },
      true
    )
    expect(next).toBe(list)
  })

  it('leaves every other machine alone', () => {
    const next = applySessionEventToMachines(
      [machine({ sessions: 1 }), machine({ id: 'm2', sessions: 5 })],
      { type: 'session.updated', payload: { id: 's1', machineId: 'm1', status: 'running' } },
      false
    )
    expect(next[0].sessions).toBe(2)
    expect(next[1].sessions).toBe(5)
  })

  it('never goes below zero', () => {
    const next = applySessionEventToMachines(
      [machine({ sessions: 0 })],
      { type: 'session.updated', payload: { id: 's1', machineId: 'm1', status: 'exited' } },
      true
    )
    expect(next[0].sessions).toBe(0)
  })

  it('counts from zero for a machine that has never had a count', () => {
    const next = applySessionEventToMachines(
      [{ id: 'm1' }],
      { type: 'session.updated', payload: { id: 's1', machineId: 'm1', status: 'running' } },
      false
    )
    expect(next[0].sessions).toBe(1)
  })

  it('ignores anything that is not a session update', () => {
    const list = [machine()]
    expect(applySessionEventToMachines(list, { type: 'machine.updated' }, false)).toBe(list)
    expect(applySessionEventToMachines(list, { type: 'session.updated', payload: {} }, false)).toBe(list)
    expect(applySessionEventToMachines(list, undefined, false)).toBe(list)
  })
})

describe('useMachines', () => {
  it('loads the list', async () => {
    const h = harness()
    await h.m.load()
    expect(h.m.machines.value).toHaveLength(1)
    expect(h.m.loading.value).toBe(false)
    expect(h.m.error.value).toBe('')
  })

  it('says what went wrong rather than showing an empty page', async () => {
    const h = harness({ fetchMachines: vi.fn().mockRejectedValue(new Error('server is down')) })
    await h.m.load()
    expect(h.m.error.value).toBe('server is down')
    expect(h.m.loading.value).toBe(false)
  })

  it('falls back to a message when the failure has none', async () => {
    const h = harness({ fetchMachines: vi.fn().mockRejectedValue({}) })
    await h.m.load()
    expect(h.m.error.value).toBe('Failed to load machines')
  })

  it('copes with a response carrying no machines', async () => {
    const h = harness({ fetchMachines: vi.fn().mockResolvedValue({}) })
    await h.m.load()
    expect(h.m.machines.value).toEqual([])
  })

  it('asks for an enrolment code and can put it away', async () => {
    const h = harness()
    const code = await h.m.requestCode()
    expect(code.code).toBe('ABCD-EFGH')
    expect(h.m.enrolmentCode.value.code).toBe('ABCD-EFGH')
    h.m.dismissCode()
    expect(h.m.enrolmentCode.value).toBeNull()
  })

  // On the desktop build the page's own origin is `app://`, which no daemon
  // could ever reach. Printing it would hand somebody a command that cannot
  // work and no clue why.
  it('builds the command against the server, not the page', async () => {
    const h = harness()
    expect(h.m.enrolCommand.value).toBe('')
    await h.m.requestCode()
    expect(h.m.enrolCommand.value).toBe(
      'agentrqd enroll --server https://agentrq.example --code ABCD-EFGH'
    )
    expect(h.serverOrigin).toHaveBeenCalled()
    h.m.dismissCode()
    expect(h.m.enrolCommand.value).toBe('')
  })

  it('still shows the code when the address could not be resolved', async () => {
    const h = harness({ serverOrigin: vi.fn().mockResolvedValue('') })
    await h.m.requestCode()
    expect(h.m.enrolCommand.value).toContain('ABCD-EFGH')
  })

  it('reports a code that could not be minted', async () => {
    const h = harness({ createEnrolmentCode: vi.fn().mockRejectedValue(new Error('rate limited')) })
    expect(await h.m.requestCode()).toBeNull()
    expect(h.m.error.value).toBe('rate limited')
  })

  it('falls back to a message when minting fails silently', async () => {
    const h = harness({ createEnrolmentCode: vi.fn().mockRejectedValue({}) })
    await h.m.requestCode()
    expect(h.m.error.value).toBe('Failed to create an enrolment code')
  })

  it('folds live metrics into the row', async () => {
    const h = harness()
    await h.m.load()
    h.m.handleEvent({ type: 'machine.updated', payload: { id: 'm1', metrics: { cpuPercent: 4.4 } } })
    expect(h.m.machines.value[0].metrics.cpuPercent).toBe(4.4)
    expect(h.m.machines.value[0].name).toBe('rpi')
  })

  // The count is stepped rather than assigned, so the page has to remember
  // which sessions it already believes are live.
  it('tracks a session through its whole life without double counting', async () => {
    const h = harness()
    await h.m.load()
    const running = { type: 'session.updated', payload: { id: 's1', machineId: 'm1', status: 'running' } }

    h.m.handleEvent({ type: 'session.updated', payload: { id: 's1', machineId: 'm1', status: 'starting' } })
    expect(h.m.machines.value[0].sessions).toBe(1)

    h.m.handleEvent(running)
    expect(h.m.machines.value[0].sessions).toBe(1)

    h.m.handleEvent({ type: 'session.updated', payload: { id: 's1', machineId: 'm1', status: 'exited' } })
    expect(h.m.machines.value[0].sessions).toBe(0)

    // And an exit reported twice does not go negative.
    h.m.handleEvent({ type: 'session.updated', payload: { id: 's1', machineId: 'm1', status: 'exited' } })
    expect(h.m.machines.value[0].sessions).toBe(0)
  })

  it('ignores a session event with no session on it', async () => {
    const h = harness()
    await h.m.load()
    h.m.handleEvent({ type: 'session.updated', payload: { machineId: 'm1', status: 'running' } })
    expect(h.m.machines.value[0].sessions).toBe(0)
  })

  // Every dependency defaults to the real API, so a view supplies none.
  it('reaches for the real API when nothing is injected', async () => {
    const m = useMachines()
    await m.load()
    expect(m.error.value).not.toBe('')
  })

  it('ignores an event it does not understand', async () => {
    const h = harness()
    await h.m.load()
    h.m.handleEvent({ type: 'task.updated', payload: {} })
    h.m.handleEvent(undefined)
    expect(h.m.machines.value[0]).toEqual(machine())
  })
})
