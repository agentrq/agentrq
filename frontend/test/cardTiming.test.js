// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect } from 'vitest'
import { cardFooter, shortAgo, timingAfterMove, workedSeconds } from '../src/composables/useCardTiming'

const now = Date.parse('2026-11-26T12:00:00Z')
const ago = (m) => new Date(now - m * 60000).toISOString()

describe('cardFooter', () => {
  it('counts an ongoing task’s running stretch into its total, live', () => {
    const t = { status: 'ongoing', timing: { workedSeconds: 4980, workingSince: ago(9), statusSince: ago(9) } }
    expect(cardFooter(t, now)).toEqual({ kind: 'working', total: '1h 32m', state: '9m' })
    expect(cardFooter(t, now + 60000)).toEqual({ kind: 'working', total: '1h 33m', state: '10m' })
  })

  it('shows the total on an ongoing task in its first stretch too', () => {
    const t = { status: 'ongoing', timing: { workedSeconds: 0, workingSince: ago(12), statusSince: ago(12) } }
    expect(cardFooter(t, now)).toEqual({ kind: 'working', total: '12m', state: '12m' })
  })

  it('keeps an ongoing task’s total still while a question waits, and its time in the column running', () => {
    const t = { status: 'ongoing', timing: { workedSeconds: 600, statusSince: ago(30) } }
    expect(cardFooter(t, now)).toEqual({ kind: 'working', total: '10m', state: '30m' })
  })

  it('says how long a blocked task has waited, and what was worked before', () => {
    const t = { status: 'blocked', timing: { workedSeconds: 1380, statusSince: ago(47) } }
    expect(cardFooter(t, now)).toEqual({ kind: 'blocked', total: '23m', state: '47m' })
  })

  it('marks a completed task ✓ and a rejected one ✗, with when', () => {
    expect(cardFooter({ status: 'completed', timing: { workedSeconds: 3660, statusSince: ago(42) } }, now))
      .toEqual({ kind: 'completed', total: '1h 1m', state: '✓ 42m ago' })
    expect(cardFooter({ status: 'rejected', timing: { workedSeconds: 0, statusSince: ago(600) } }, now))
      .toEqual({ kind: 'rejected', total: '', state: '✗ 10h ago' })
  })

  it('has nothing for a task not started, a schedule, or one with no timing', () => {
    expect(cardFooter({ status: 'notstarted', timing: { workedSeconds: 0, statusSince: ago(5) } }, now)).toBeNull()
    expect(cardFooter({ status: 'cron', timing: { workedSeconds: 0, statusSince: ago(5) } }, now)).toBeNull()
    expect(cardFooter({ status: 'completed' }, now)).toBeNull()
    expect(cardFooter({ status: 'completed', timing: { workedSeconds: 60 } }, now)).toBeNull()
    expect(cardFooter(null, now)).toBeNull()
  })
})

describe('shortAgo', () => {
  it('rounds down to the largest unit', () => {
    expect(shortAgo(ago(0.5), now)).toBe('now')
    expect(shortAgo(ago(59), now)).toBe('59m ago')
    expect(shortAgo(ago(180), now)).toBe('3h ago')
    expect(shortAgo(ago(3 * 1440), now)).toBe('3d ago')
    expect(shortAgo(new Date(now + 60000).toISOString(), now)).toBe('now')
  })

  it('reads the clock by default', () => {
    expect(shortAgo(new Date().toISOString())).toBe('now')
  })
})

describe('workedSeconds', () => {
  it('is zero without timing', () => {
    expect(workedSeconds(undefined, now)).toBe(0)
    expect(workedSeconds({}, now)).toBe(0)
  })
})

describe('timingAfterMove', () => {
  it('banks the running stretch when the task stops working', () => {
    expect(timingAfterMove({ workedSeconds: 60, workingSince: ago(10), statusSince: ago(10) }, 'completed', new Date(now).toISOString()))
      .toEqual({ workedSeconds: 660, statusSince: new Date(now).toISOString() })
  })

  it('starts a stretch on a move to ongoing, from a task with no timing too', () => {
    const at = new Date(now).toISOString()
    expect(timingAfterMove(undefined, 'ongoing', at)).toEqual({ workedSeconds: 0, workingSince: at, statusSince: at })
    expect(timingAfterMove({ workedSeconds: 300, statusSince: ago(20) }, 'ongoing', at)).toEqual({ workedSeconds: 300, workingSince: at, statusSince: at })
  })

  it('moves now when it is not told when', () => {
    const t = timingAfterMove(undefined, 'blocked')
    expect(Date.now() - Date.parse(t.statusSince)).toBeLessThan(5000)
  })
})
