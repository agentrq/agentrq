// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest'
import { useCron } from '../src/composables/useCron.js'

const { formatCron } = useCron()

// Labels show the next run in the reader's zone, so the clock is pinned to the
// day `local` converts on, or a DST change between the two moves the hour.
const TODAY = Date.UTC(2026, 0, 15)
beforeAll(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(TODAY)
})
afterAll(() => vi.useRealTimers())

// The reader's own clock time of a UTC hour on TODAY, as the labels show it.
const local = (utcHour, utcMin = 0) => {
  const d = new Date(TODAY + (utcHour * 60 + utcMin) * 60000)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

// The task form's "Repeat" dropdown writes these crons (TaskFormView.vue),
// and the task list and the schedule page show formatCron's label for them.
describe('formatCron labels every schedule the task form can create', () => {
  it('labels the presets it already knows', () => {
    expect(formatCron('*/15 * * * *')).toBe('Every 15m')
    expect(formatCron('*/30 * * * *')).toBe('Every 30m')
    expect(formatCron('0 * * * *')).toBe('Hourly')
    expect(formatCron('30 9 * * *')).toBe(`Daily at ${local(9, 30)}`)
  })

  it('labels "Bi-hourly" as every two hours, not daily', () => {
    expect(formatCron('0 */2 * * *')).toBe('Every 2h')
  })

  it('labels "Twice a day" with both of its times, not daily', () => {
    const [a, b] = [local(9), local(21)].sort()
    expect(formatCron('0 9,21 * * *')).toBe(`Twice a day at ${a} & ${b}`)
  })

  it('names every time of a schedule that runs more than twice a day', () => {
    const times = [local(0), local(8), local(16)].sort().join(', ')
    expect(formatCron('0 0,8,16 * * *')).toBe(`3 times a day at ${times}`)
  })

  it('shows the cron itself rather than call anything else daily', () => {
    expect(formatCron('*/5 * * * *')).toBe('*/5 * * * *')
    expect(formatCron('30 * * * *')).toBe('30 * * * *')
    expect(formatCron('0 9-17 * * *')).toBe('0 9-17 * * *')
  })
})
