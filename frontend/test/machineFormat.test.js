// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, it, expect } from 'vitest'
import {
  formatBytes,
  formatPercent,
  formatUptime,
  diskUsedPercent,
  memoryUsedPercent,
  formatLoadAvg,
  formatAge,
  isSessionLive,
  sessionTone,
  sessionLabel,
  sessionSummary,
} from '../src/composables/useMachineFormat.js'

describe('formatBytes', () => {
  it('reads at the scale a person would say it', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(512)).toBe('512 B')
    expect(formatBytes(4096)).toBe('4.1 KB')
    expect(formatBytes(4_203_741_184)).toBe('4.2 GB')
    expect(formatBytes(494_384_795_648)).toBe('494 GB')
    // The scale stops at petabytes; nothing smaller is worth a unit nobody
    // would recognise on a machines page.
    expect(formatBytes(1_500_000_000_000_000)).toBe('1.5 PB')
  })

  it('says nothing rather than guessing', () => {
    expect(formatBytes(undefined)).toBe('—')
    expect(formatBytes(null)).toBe('—')
    expect(formatBytes(-1)).toBe('—')
    expect(formatBytes(NaN)).toBe('—')
  })
})

describe('formatPercent', () => {
  it('keeps one decimal', () => {
    expect(formatPercent(37.44)).toBe('37.4%')
    expect(formatPercent(0)).toBe('0.0%')
  })
  it('says nothing when there is no reading', () => {
    expect(formatPercent(undefined)).toBe('—')
  })
})

describe('formatUptime', () => {
  // A glance, not an audit: the seconds have never mattered to anyone reading
  // how long a box has been up.
  it('uses the two largest units that apply', () => {
    expect(formatUptime(918_273)).toBe('10d 15h')
    expect(formatUptime(7_320)).toBe('2h 2m')
    expect(formatUptime(125)).toBe('2m')
    expect(formatUptime(9)).toBe('9s')
    expect(formatUptime(0)).toBe('0s')
  })
  it('says nothing when there is no reading', () => {
    expect(formatUptime(undefined)).toBe('—')
    expect(formatUptime(-5)).toBe('—')
  })
})

describe('diskUsedPercent', () => {
  it('measures what is used, because that is what a bar fills with', () => {
    expect(diskUsedPercent({ total: 100, free: 40 })).toBe(60)
    expect(diskUsedPercent({ total: 100, free: 0 })).toBe(100)
  })

  // A mount of unknown size gets no bar rather than an empty one, which would
  // read as a disk with nothing on it.
  it('gives no answer for a mount it cannot measure', () => {
    expect(diskUsedPercent({ total: 0, free: 0 })).toBeNull()
    expect(diskUsedPercent(null)).toBeNull()
    expect(diskUsedPercent({ free: 1 })).toBeNull()
  })

  it('never leaves the bar', () => {
    expect(diskUsedPercent({ total: 100, free: 200 })).toBe(0)
    expect(diskUsedPercent({ total: 100, free: -50 })).toBe(100)
    expect(diskUsedPercent({ total: 100 })).toBe(100)
  })
})

describe('memoryUsedPercent', () => {
  it('derives from available, for the reason available is reported at all', () => {
    expect(memoryUsedPercent({ memTotal: 100, memAvailable: 25 })).toBe(75)
  })
  it('gives no answer without a total', () => {
    expect(memoryUsedPercent({ memTotal: 0, memAvailable: 0 })).toBeNull()
    expect(memoryUsedPercent(null)).toBeNull()
  })
  it('treats a missing available figure as none free', () => {
    expect(memoryUsedPercent({ memTotal: 100 })).toBe(100)
  })
})

describe('formatLoadAvg', () => {
  it('shows the three figures', () => {
    expect(formatLoadAvg([1.2, 0.9, 0.7])).toBe('1.20  0.90  0.70')
  })

  // Windows has none, and three zeroes would say "perfectly idle" — the most
  // misleading thing this row could claim.
  it('shows nothing at all when the machine reports none', () => {
    expect(formatLoadAvg(undefined)).toBeNull()
    expect(formatLoadAvg([])).toBeNull()
    expect(formatLoadAvg(null)).toBeNull()
  })

  it('survives a figure that is not a number', () => {
    expect(formatLoadAvg([1, 'x', 3])).toBe('1.00  —  3.00')
  })
})

describe('formatAge', () => {
  // A machine that went offline keeps its last numbers, and they stop being
  // true the moment it does.
  it('says how stale a snapshot is', () => {
    const now = new Date('2026-09-15T12:00:00Z').getTime()
    expect(formatAge('2026-09-15T11:59:50Z', now)).toBe('just now')
    expect(formatAge('2026-09-15T11:50:00Z', now)).toBe('10m ago')
    expect(formatAge('2026-09-15T08:00:00Z', now)).toBe('4h 0m ago')
  })

  it('says nothing when there is no snapshot', () => {
    expect(formatAge(null)).toBeNull()
    expect(formatAge('not a date')).toBeNull()
  })

  it('uses the clock when no time is given', () => {
    expect(formatAge(new Date().toISOString())).toBe('just now')
  })
})

describe('session status', () => {
  it('knows which sessions can still change', () => {
    expect(isSessionLive('running')).toBe(true)
    expect(isSessionLive('starting')).toBe(true)
    expect(isSessionLive('exited')).toBe(false)
    expect(isSessionLive('killed')).toBe(false)
    expect(isSessionLive('failed')).toBe(false)
    expect(isSessionLive(undefined)).toBe(false)
  })

  it('gives each status a tone', () => {
    expect(sessionTone('running')).toBe('good')
    expect(sessionTone('starting')).toBe('pending')
    expect(sessionTone('failed')).toBe('bad')
    expect(sessionTone('exited')).toBe('muted')
  })
})

describe('what a session row is called', () => {
  // A machine runs agents for several workspaces at once, and most of them are
  // the same kind. A list headed "claude-code" three times answers none of the
  // questions somebody opened the page with.
  it('names the workspace, not the kind', () => {
    expect(sessionLabel({ workspaceName: 'Q3 migration', kind: 'claude-code' })).toBe('Q3 migration')
  })

  // A session whose workspace has been deleted, or one recorded before the
  // name was carried, still has to say something.
  it('falls back to the kind when there is no workspace name', () => {
    expect(sessionLabel({ kind: 'acp-gateway' })).toBe('acp-gateway')
    expect(sessionLabel({ workspaceName: '   ', kind: 'acp-gateway' })).toBe('acp-gateway')
    expect(sessionLabel(null)).toBe('agent')
  })

  it('puts the kind and the state underneath', () => {
    expect(sessionSummary({ workspaceName: 'Q3 migration', kind: 'claude-code', status: 'running' }))
      .toBe('claude-code · running')
  })

  // Without this a session with no workspace name reads "claude-code ·
  // claude-code", which looks like a bug because it is one.
  it('does not repeat the kind it is already headed by', () => {
    expect(sessionSummary({ kind: 'claude-code', status: 'running' })).toBe('running')
  })

  it('says how a session ended, and whether it came back', () => {
    expect(sessionSummary({ workspaceName: 'W', kind: 'claude-code', status: 'exited', exitCode: 0 }))
      .toBe('claude-code · exited · exit 0')
    expect(sessionSummary({ workspaceName: 'W', kind: 'claude-code', status: 'failed', exitCode: 137 }))
      .toBe('claude-code · failed · exit 137')
    expect(sessionSummary({ workspaceName: 'W', kind: 'claude-code', status: 'running', restored: true }))
      .toBe('claude-code · running · restored')
  })

  // exit 0 is a real exit code and the falsy one, which is exactly the value a
  // truthiness check would drop.
  it('shows a zero exit code', () => {
    expect(sessionSummary({ kind: 'claude-code', status: 'exited', exitCode: 0 })).toContain('exit 0')
  })

  it('says something for a session it knows nothing about', () => {
    expect(sessionSummary({})).toBe('unknown')
  })
})
