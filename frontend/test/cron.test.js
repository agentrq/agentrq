// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

// A schedule is written in the zone it names, so nothing here converts a day
// or an hour. The round-trip block is what holds that: it pins zones and dates
// on both sides of a DST change and asserts the cron comes back byte for byte.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { useCron } from '../src/composables/useCron'

const {
  formatCron, getNextRunDate, getNextRunDateTime, getNextRunLabel,
  cronFromSchedule, scheduleFromCron, splitSchedule, localZone, daysOptions,
} = useCron()

const HAD_TZ = 'TZ' in process.env
const ORIGINAL_TZ = process.env.TZ

/** Pin the machine's zone and clock for the rest of the test. */
function pin(tz, iso) {
  process.env.TZ = tz
  vi.setSystemTime(new Date(iso))
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
})

afterEach(() => {
  vi.useRealTimers()
  // Assigning undefined would store the string 'undefined' and drop the
  // process into UTC for every test after this one.
  if (HAD_TZ) process.env.TZ = ORIGINAL_TZ
  else delete process.env.TZ
})

describe('splitSchedule', () => {
  it.each([
    ['0 9 * * *', 'UTC', '0 9 * * *'],
    ['CRON_TZ=Pacific/Auckland 0 9 1 * *', 'Pacific/Auckland', '0 9 1 * *'],
    ['TZ=America/New_York 30 6 * * *', 'America/New_York', '30 6 * * *'],
    ['  0 9 * * *  ', 'UTC', '0 9 * * *'],
    ['', 'UTC', ''],
    [undefined, 'UTC', ''],
  ])('splits %s', (cron, tz, spec) => {
    expect(splitSchedule(cron)).toEqual({ tz, spec })
  })
})

describe('localZone', () => {
  // Restored here rather than at the end of a test, which a failed expect skips.
  const original = Intl.DateTimeFormat
  afterEach(() => {
    Intl.DateTimeFormat = original
    vi.restoreAllMocks()
  })

  it('reads the browser zone', () => {
    pin('Pacific/Auckland', '2026-03-03T12:00:00Z')
    expect(localZone()).toBe('Pacific/Auckland')
  })

  it('falls back to UTC where Intl gives nothing', () => {
    Intl.DateTimeFormat = () => ({ resolvedOptions: () => ({ timeZone: '' }) })
    expect(localZone()).toBe('UTC')
    Intl.DateTimeFormat = () => { throw new Error('no Intl') }
    expect(localZone()).toBe('UTC')
  })

  // What ICU says when it cannot work the zone out; the server refuses it.
  it('falls back to UTC for a zone nothing can resolve', () => {
    vi.spyOn(Intl.DateTimeFormat.prototype, 'resolvedOptions')
      .mockReturnValue({ timeZone: 'Etc/Unknown' })
    expect(localZone()).toBe('UTC')
  })
})

describe('cronFromSchedule', () => {
  beforeEach(() => pin('Pacific/Auckland', '2026-03-03T12:00:00Z'))

  it('writes the picked fields, in the picked zone', () => {
    expect(cronFromSchedule({ type: 'repeated', preset: 'monthly', time: '09:00', monthDay: 1 }))
      .toBe('CRON_TZ=Pacific/Auckland 0 9 1 * *')
  })

  it('leaves a UTC schedule unprefixed, so it reads as it always did', () => {
    expect(cronFromSchedule({ type: 'repeated', preset: 'daily', time: '09:00', zone: 'UTC' }))
      .toBe('0 9 * * *')
  })

  it.each([
    ['15min', '*/15 * * * *'],
    ['30min', '*/30 * * * *'],
    ['hourly', '0 * * * *'],
    ['2hour', '0 */2 * * *'],
  ])('writes %s with no zone, having no time of day in it', (preset, cron) => {
    expect(cronFromSchedule({ type: 'repeated', preset, time: '09:00' })).toBe(cron)
  })

  it.each([
    [{ preset: 'daily', time: '09:30' }, 'CRON_TZ=Pacific/Auckland 30 9 * * *'],
    [{ preset: '12hour', time: '08:30' }, 'CRON_TZ=Pacific/Auckland 30 8,20 * * *'],
    [{ preset: '12hour', time: '20:30' }, 'CRON_TZ=Pacific/Auckland 30 8,20 * * *'],
    [{ preset: 'monthly', time: '09:00', monthDay: 31 }, 'CRON_TZ=Pacific/Auckland 0 9 31 * *'],
    [{ preset: 'monthly', time: '09:00' }, 'CRON_TZ=Pacific/Auckland 0 9 1 * *'],
    [{ preset: 'custom', time: '09:00', days: [5, 1, 1, 3] }, 'CRON_TZ=Pacific/Auckland 0 9 * * 1,3,5'],
  ])('writes %o', (picked, cron) => {
    expect(cronFromSchedule({ type: 'repeated', ...picked })).toBe(cron)
  })

  it('writes weekly on the day the week is set from', () => {
    expect(cronFromSchedule({ type: 'repeated', preset: 'weekly', time: '09:00' }))
      .toBe(`CRON_TZ=Pacific/Auckland 0 9 * * ${new Date().getDay()}`)
  })

  it('clamps a day of month that is out of range', () => {
    expect(cronFromSchedule({ type: 'repeated', preset: 'monthly', time: '09:00', monthDay: 44 }))
      .toBe('CRON_TZ=Pacific/Auckland 0 9 31 * *')
    expect(cronFromSchedule({ type: 'repeated', preset: 'monthly', time: '09:00', monthDay: 0 }))
      .toBe('CRON_TZ=Pacific/Auckland 0 9 1 * *')
  })

  it('writes a one-time schedule from the instant on the local clock', () => {
    expect(cronFromSchedule({ type: 'onetime', oneTimeDate: '2026-06-01T14:30' }))
      .toBe('CRON_TZ=Pacific/Auckland 30 14 1 6 *')
  })

  it.each([
    ['nothing scheduled', { type: 'none' }, ''],
    ['a one-time with no date', { type: 'onetime', oneTimeDate: '' }, ''],
    ['a one-time with a bad date', { type: 'onetime', oneTimeDate: 'not-a-date' }, ''],
    ['no time of day', { type: 'repeated', preset: 'daily', time: '' }, ''],
    ['a time that is not one', { type: 'repeated', preset: 'daily', time: 'abc' }, ''],
    ['a time with no minutes', { type: 'repeated', preset: 'daily', time: '09:xx' }, ''],
    ['custom with no days', { type: 'repeated', preset: 'custom', time: '09:00', days: [] }, ''],
    ['custom with no days at all', { type: 'repeated', preset: 'custom', time: '09:00' }, ''],
    ['no time at all', { type: 'repeated', preset: 'daily' }, ''],
    ['a preset it does not know', { type: 'repeated', preset: 'fortnightly', time: '09:00' }, ''],
  ])('writes nothing for %s', (_label, picked, cron) => {
    expect(cronFromSchedule(picked)).toBe(cron)
  })

  it('writes a one-time schedule with no prefix on a UTC machine', () => {
    pin('UTC', '2026-03-03T12:00:00Z')
    expect(cronFromSchedule({ type: 'onetime', oneTimeDate: '2026-06-01T14:30' }))
      .toBe('30 14 1 6 *')
  })
})

describe('scheduleFromCron', () => {
  beforeEach(() => pin('Pacific/Auckland', '2026-03-03T12:00:00Z'))

  it.each([
    ['0 9 * * *', { type: 'repeated', preset: 'daily', time: '09:00', zone: 'UTC' }],
    ['CRON_TZ=America/New_York 30 6 * * *',
      { type: 'repeated', preset: 'daily', time: '06:30', zone: 'America/New_York' }],
    ['30 8,20 * * *', { type: 'repeated', preset: '12hour', time: '08:30', zone: 'UTC' }],
    ['*/15 * * * *', { type: 'repeated', preset: '15min', zone: 'UTC' }],
    ['0 */2 * * *', { type: 'repeated', preset: '2hour', zone: 'UTC' }],
    ['0 9 * * 0', { type: 'repeated', preset: 'weekly', time: '09:00', days: [0], zone: 'UTC' }],
    ['0 9 * * 1,2,3,4,5',
      { type: 'repeated', preset: 'custom', time: '09:00', days: [1, 2, 3, 4, 5], zone: 'UTC' }],
    ['0 9 15 * *', { type: 'repeated', preset: 'monthly', time: '09:00', monthDay: 15, zone: 'UTC' }],
  ])('reads %s', (cron, picked) => {
    expect(scheduleFromCron(cron)).toEqual(picked)
  })

  it('reads a one-time schedule as the instant on the local clock', () => {
    expect(scheduleFromCron('30 14 1 6 *'))
      .toEqual({ type: 'onetime', oneTimeDate: '2026-06-02T02:30', zone: 'Pacific/Auckland' })
  })

  // Valid schedules with no control to show them. A null here is what stops
  // the form rewriting them: it is not a parse failure.
  it.each([
    ['a day list', '0 9 1,15 * *'],
    ['a day range', '0 9 1-3 * *'],
    ['a day step', '0 9 1/2 * *'],
    ['a day out of range', '0 9 41 * *'],
    ['an hour list that is not twelve apart', '0 9,14 * * *'],
    ['an hour step', '0 */6 1 * *'],
    ['an hour range', '0 9-17 * * *'],
    ['a minute step', '*/5 9 * * *'],
    ['a weekday as well as a day', '0 9 1 * 1'],
    ['a weekday range', '0 9 * * 1-5'],
    ['a weekday out of range', '0 9 * * 9'],
    ['a named month with a wildcard day', '0 9 * 6 *'],
    ['six fields', '0 0 9 1 * *'],
    ['three fields', '0 9 *'],
    ['nothing at all', ''],
    ['a prefix and nothing else', 'CRON_TZ=Pacific/Auckland'],
  ])('refuses %s', (_label, cron) => {
    expect(scheduleFromCron(cron)).toBeNull()
  })

  it('refuses a one-time schedule it cannot resolve to an instant', () => {
    expect(scheduleFromCron('0 9 31 2 *')).toBeNull()
  })
})

// The property the old UTC-shifting code could not hold: what the picker reads
// back regenerates the same schedule, on any date, in any zone.
describe('round trip', () => {
  const ZONES = ['Pacific/Auckland', 'America/New_York', 'Asia/Kolkata', 'UTC', 'Europe/Moscow']
  // Either side of the US and NZ daylight-saving changes.
  const DATES = ['2026-03-03T12:00:00Z', '2026-03-20T12:00:00Z', '2026-09-20T12:00:00Z', '2026-11-05T12:00:00Z']
  const SCHEDULES = [
    '0 9 * * *', '30 6 * * *', '0 0 * * *', '30 8,20 * * *',
    '*/15 * * * *', '*/30 * * * *', '0 * * * *', '0 */2 * * *',
    '0 9 * * 0', '0 9 * * 1,2,3,4,5', '30 23 * * 6',
    '0 9 1 * *', '30 6 15 * *', '0 23 28 * *', '0 0 31 * *',
  ]

  for (const zone of ZONES) {
    for (const date of DATES) {
      it.each(SCHEDULES)(`${zone} on ${date.slice(0, 10)} keeps %s`, (cron) => {
        pin(zone, date)
        const picked = scheduleFromCron(cron)
        expect(picked).not.toBeNull()
        expect(cronFromSchedule(picked)).toBe(cron)
      })

      it(`${zone} on ${date.slice(0, 10)} keeps a schedule written in another zone`, () => {
        pin(zone, date)
        const cron = 'CRON_TZ=Pacific/Auckland 0 9 15 * *'
        expect(cronFromSchedule(scheduleFromCron(cron))).toBe(cron)
      })
    }
  }
})

describe('formatCron', () => {
  beforeEach(() => pin('Europe/Moscow', '2026-03-03T12:00:00Z'))

  it.each([
    ['', ''],
    ['0 9 1 6 *', 'ONE-TIME'],
    ['0 * * * *', 'Hourly'],
    ['*/15 * * * *', 'Every 15m'],
    ['*/30 * * * *', 'Every 30m'],
    ['CRON_TZ=Europe/Moscow 0 9 * * *', 'Daily at 09:00'],
    ['CRON_TZ=Europe/Moscow 30 6 15 * *', 'Monthly (Day 15) at 06:30'],
    ['CRON_TZ=Europe/Moscow 0 9 * * 1', 'Weekly (Mon) at 09:00'],
    ['CRON_TZ=Europe/Moscow 0 9 * * 1,3', 'Weekly at 09:00'],
    ['CRON_TZ=Europe/Moscow 0 9 * * 1-3', 'Weekly at 09:00'],
  ])('describes %s', (cron, label) => {
    expect(formatCron(cron)).toBe(label)
  })

  it('hands back anything it cannot describe', () => {
    expect(formatCron('0 9 1,15 6 *')).toBe('ONE-TIME')
    expect(formatCron('0 9 * 6 *')).toBe('0 9 * 6 *')
    expect(formatCron('not a cron')).toBe('not a cron')
  })

  it('hands back a schedule naming a zone it cannot resolve', () => {
    // The backend rejects these on write, so one can only arrive by hand.
    expect(formatCron('CRON_TZ=Bogus/Zone 0 9 * * *')).toBe('CRON_TZ=Bogus/Zone 0 9 * * *')
  })
})

describe('next run', () => {
  it('gives the next occurrence as a date', () => {
    pin('Europe/Moscow', '2026-03-03T12:00:00Z')
    expect(getNextRunDate('CRON_TZ=Europe/Moscow 0 9 * * *').toISOString())
      .toBe('2026-03-04T06:00:00.000Z')
  })

  it.each([
    ['', 'nothing'],
    ['not a cron', 'an unparseable schedule'],
  ])('gives a far-future date for %s', (cron) => {
    expect(getNextRunDate(cron).getTime()).toBe(8640000000000000)
  })

  it('formats the next occurrence for reading', () => {
    pin('Europe/Moscow', '2026-03-03T12:00:00Z')
    expect(getNextRunDateTime('CRON_TZ=Europe/Moscow 0 9 * * *')).toMatch(/4/)
    expect(getNextRunDateTime('')).toBe('')
    expect(getNextRunDateTime('not a cron')).toBe('')
  })

  it.each([
    ['0 9 * * *', '2026-03-03T08:50:00Z', 'In 10 mins'],
    ['0 9 * * *', '2026-03-03T08:59:30Z', 'Soon'],
    ['0 9 * * *', '2026-03-03T08:59:00Z', 'In 1 min'],
    ['0 9 * * *', '2026-03-03T07:00:00Z', 'In 2 hours'],
    ['0 9 * * *', '2026-03-03T08:00:00Z', 'In 1 hour'],
    ['0 9 * * *', '2026-03-03T07:30:00Z', 'In 1h 30m'],
    ['0 9 5 * *', '2026-03-04T09:00:00Z', 'In 1 day'],
    ['0 9 5 * *', '2026-03-03T09:00:00Z', 'In 2 days'],
  ])('describes %s from %s as %s', (cron, now, label) => {
    pin('UTC', now)
    expect(getNextRunLabel(cron)).toBe(label)
  })

  it('says nothing about a schedule it cannot read', () => {
    expect(getNextRunLabel('')).toBe('')
    expect(getNextRunLabel('not a cron')).toBe('')
  })
})

describe('daysOptions', () => {
  it('names all seven days, Sunday first', () => {
    expect(daysOptions.map((d) => d.label)).toEqual(['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'])
    expect(daysOptions.map((d) => d.value)).toEqual([0, 1, 2, 3, 4, 5, 6])
  })
})
