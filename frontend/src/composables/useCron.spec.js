import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { useCron } from './useCron'

const { formatCron, getNextRunDate, getNextRunDateTime, getNextRunLabel } = useCron()

describe('formatCron', () => {
  it('returns empty string for falsy input', () => {
    expect(formatCron('')).toBe('')
    expect(formatCron(null)).toBe('')
  })

  it('detects one-time schedules (specific day-of-month and month)', () => {
    expect(formatCron('0 9 15 6 *')).toBe('ONE-TIME')
  })

  it('recognizes known presets', () => {
    expect(formatCron('0 * * * *')).toBe('Hourly')
    expect(formatCron('*/15 * * * *')).toBe('Every 15m')
    expect(formatCron('*/30 * * * *')).toBe('Every 30m')
  })

  it('returns the raw string for an unparseable cron expression', () => {
    expect(formatCron('not a cron')).toBe('not a cron')
  })

  describe('with a fixed clock (2026-01-01T00:00:00Z, Thursday)', () => {
    beforeEach(() => {
      vi.useFakeTimers()
      vi.setSystemTime(new Date('2026-01-01T00:00:00Z'))
    })

    afterEach(() => {
      vi.useRealTimers()
    })

    it('formats a daily schedule', () => {
      expect(formatCron('0 9 * * *')).toBe('Daily at 09:00')
    })

    it('formats a single-day weekly schedule with the localized day name', () => {
      // Wednesday (3); next occurrence from Thu 2026-01-01 is Wed 2026-01-07
      expect(formatCron('0 10 * * 3')).toBe('Weekly (Wed) at 10:00')
    })

    it('formats a multi-day weekly schedule without a single day name', () => {
      expect(formatCron('0 10 * * 1,3')).toBe('Weekly at 10:00')
    })

    it('formats a monthly schedule', () => {
      expect(formatCron('0 8 15 * *')).toBe('Monthly (Day 15) at 08:00')
    })
  })
})

describe('getNextRunDate', () => {
  it('returns a far-future date for empty or invalid input', () => {
    const farFuture = new Date(8640000000000000)
    expect(getNextRunDate('')).toEqual(farFuture)
    expect(getNextRunDate('garbage')).toEqual(farFuture)
  })

  it('resolves the next occurrence under a fixed clock', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-01-01T00:00:00Z'))
    expect(getNextRunDate('5 0 * * *').toISOString()).toBe('2026-01-01T00:05:00.000Z')
    vi.useRealTimers()
  })
})

describe('getNextRunDateTime', () => {
  it('returns empty string for empty or invalid input', () => {
    expect(getNextRunDateTime('')).toBe('')
    expect(getNextRunDateTime('garbage')).toBe('')
  })

  it('returns a non-empty formatted string for a valid cron', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-01-01T00:00:00Z'))
    expect(getNextRunDateTime('5 0 * * *')).not.toBe('')
    vi.useRealTimers()
  })
})

describe('getNextRunLabel', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-01-01T00:00:00Z'))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('returns empty string for empty input', () => {
    expect(getNextRunLabel('')).toBe('')
  })

  it('reports minutes for a near-term run', () => {
    expect(getNextRunLabel('5 0 * * *')).toBe('In 5 mins')
  })

  it('reports hours for a same-day run', () => {
    expect(getNextRunLabel('0 1 * * *')).toBe('In 1 hour')
  })

  it('reports days for a multi-day-out run', () => {
    expect(getNextRunLabel('0 0 3 * *')).toBe('In 2 days')
  })
})
