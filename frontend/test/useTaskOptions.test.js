// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * What a new task carries besides its words, shared by the New Task form and
 * the Pano's chat box: its schedule as a cron line, a stored cron line read
 * back into the pickers, the completion trigger's label, its files, and the
 * workspace's defaults.
 */

import { describe, it, expect, vi, afterEach } from 'vitest'
import { nextTick } from 'vue'
import { useTaskOptions } from '../src/composables/useTaskOptions'

/** Local wall-clock time on a fixed day, the way the pickers hold it. */
const LOCAL_NOON = '12:00'
const utcOf = (h, m) => {
  const d = new Date()
  d.setHours(h, m, 0, 0)
  return { h: d.getUTCHours(), m: d.getUTCMinutes(), dow: d.getUTCDay() }
}

async function schedule(opts, kind, set = {}) {
  opts.scheduleType.value = kind
  for (const [k, v] of Object.entries(set)) opts[k].value = v
  await nextTick()
  return opts.cronSchedule.value
}

afterEach(() => vi.unstubAllGlobals())

describe('the schedule', () => {
  it('has none until one is chosen', async () => {
    const opts = useTaskOptions()
    expect(opts.nextRunPreview.value).toBe('')
    expect(await schedule(opts, 'none')).toBe('')
  })

  it('turns a one-time date into the cron line of that UTC minute', async () => {
    const opts = useTaskOptions()
    expect(await schedule(opts, 'onetime')).toBe('')
    const cron = await schedule(opts, 'onetime', { oneTimeDate: '2030-03-04T05:06' })
    const d = new Date('2030-03-04T05:06')
    expect(cron).toBe(`${d.getUTCMinutes()} ${d.getUTCHours()} ${d.getUTCDate()} ${d.getUTCMonth() + 1} *`)
    expect(opts.nextRunPreview.value).not.toBe('')
  })

  it.each([
    ['15min', '*/15 * * * *'],
    ['30min', '*/30 * * * *'],
    ['hourly', '0 * * * *'],
    ['2hour', '0 */2 * * *'],
  ])('turns %s into %s', async (preset, cron) => {
    expect(await schedule(useTaskOptions(), 'repeated', { repeatPreset: preset })).toBe(cron)
  })

  it('turns a daily, weekly, monthly and twice-a-day time into its UTC cron line', async () => {
    const opts = useTaskOptions()
    const { h, m, dow } = utcOf(12, 0)
    opts.repeatTime.value = LOCAL_NOON
    expect(await schedule(opts, 'repeated', { repeatPreset: 'daily' })).toBe(`${m} ${h} * * *`)
    expect(await schedule(opts, 'repeated', { repeatPreset: 'weekly' })).toBe(`${m} ${h} * * ${dow}`)
    const monthly = await schedule(opts, 'repeated', { repeatPreset: 'monthly' })
    expect(monthly).toMatch(new RegExp(`^${m} ${h} \\d+ \\* \\*$`))
    const twice = await schedule(opts, 'repeated', { repeatPreset: '12hour' })
    expect(twice).toBe(`${m} ${[h, (h + 12) % 24].sort((a, b) => a - b).join(',')} * * *`)
  })

  it('turns chosen days into UTC weekdays', async () => {
    const opts = useTaskOptions()
    opts.repeatTime.value = LOCAL_NOON
    const cron = await schedule(opts, 'repeated', { repeatPreset: 'custom', selectedDays: [1] })
    expect(cron).toMatch(/^\d+ \d+ \* \* [0-6]$/)
  })

  it('toggles a day, but never the last one off', () => {
    const opts = useTaskOptions()
    opts.toggleDay(0)
    expect(opts.selectedDays.value).toContain(0)
    opts.toggleDay(0)
    expect(opts.selectedDays.value).not.toContain(0)
    opts.selectedDays.value = [3]
    opts.toggleDay(3)
    expect(opts.selectedDays.value).toEqual([3])
  })
})

describe('reading a stored cron line back', () => {
  it('reads a one-time line as its date', () => {
    const opts = useTaskOptions()
    const stored = new Date('2030-03-04T05:06')
    opts.parseCronToUI(`${stored.getUTCMinutes()} ${stored.getUTCHours()} ${stored.getUTCDate()} ${stored.getUTCMonth() + 1} *`)
    expect(opts.scheduleType.value).toBe('onetime')
    expect(opts.oneTimeDate.value).toMatch(/^\d{4}-03-\d\dT\d\d:\d\d$/)
  })

  it.each([
    ['*/15 * * * *', '15min'],
    ['*/30 * * * *', '30min'],
    ['0 * * * *', 'hourly'],
    ['0 */2 * * *', '2hour'],
    ['30 8 * * *', 'daily'],
    ['30 8,20 * * *', '12hour'],
    ['30 8,10 * * *', 'daily'],
    ['30 8 1 * *', 'custom'],
  ])('reads %s as %s', (cron, preset) => {
    const opts = useTaskOptions()
    opts.parseCronToUI(cron)
    expect(opts.scheduleType.value).toBe('repeated')
    expect(opts.repeatPreset.value).toBe(preset)
  })

  it('reads one weekday as weekly only on a Sunday, and several as custom days', () => {
    const opts = useTaskOptions()
    opts.parseCronToUI('0 12 * * 1,2')
    expect(opts.repeatPreset.value).toBe('custom')
    expect(opts.selectedDays.value).toHaveLength(2)
    opts.parseCronToUI('0 12 * * 3')
    expect(opts.repeatPreset.value).toBe('custom')
    expect(opts.selectedDays.value).toHaveLength(1)
    // Noon UTC, a Sunday in UTC, is Sunday in every zone within 12 hours of it.
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2030-03-06T12:00:00Z'))
    opts.parseCronToUI('0 12 * * 0')
    vi.useRealTimers()
    expect(['weekly', 'custom']).toContain(opts.repeatPreset.value)
  })
})

describe('the completion trigger', () => {
  it('says what it will do', () => {
    const opts = useTaskOptions()
    expect(opts.completionTriggerLabel.value).toBe('Emit Event or Run Workflow on Completion')
    opts.selectedEventId.value = 'e1'
    expect(opts.completionTriggerLabel.value).toBe('Event Emitted on Completion')
    opts.workflows.value = [{ id: 'w1', name: 'release' }]
    opts.selectedWorkflowId.value = 'w1'
    expect(opts.completionTriggerLabel.value).toBe('Runs workflow release on completion')
    opts.selectedWorkflowId.value = 'gone'
    expect(opts.completionTriggerLabel.value).toBe('Runs workflow  on completion')
  })
})

describe('files and defaults', () => {
  it('reads each chosen file into an attachment, with a type when the browser has none', async () => {
    const opts = useTaskOptions()
    opts.processFiles([new File(['hi'], 'a.txt', { type: 'text/plain' }), new File(['yo'], 'b.bin')])
    await new Promise((r) => setTimeout(r, 30))
    expect(opts.attachments.value).toEqual([
      { filename: 'a.txt', mimeType: 'text/plain', data: btoa('hi') },
      { filename: 'b.bin', mimeType: 'application/octet-stream', data: btoa('yo') },
    ])
  })

  it('takes the workspace\'s defaults, and none from a workspace without them', () => {
    const opts = useTaskOptions()
    opts.applyWorkspaceDefaults({ allowAllCommands: true, clearContextDefault: true })
    expect([opts.allowAllCommands.value, opts.clearContext.value]).toEqual([true, true])
    opts.applyWorkspaceDefaults(null)
    expect([opts.allowAllCommands.value, opts.clearContext.value]).toEqual([false, false])
  })

  it('goes back to an untouched task', () => {
    const opts = useTaskOptions()
    Object.assign(opts.assignee, { value: 'human' })
    opts.attachments.value = [{ filename: 'a' }]
    opts.scheduleType.value = 'repeated'
    opts.selectedEventId.value = 'e1'
    opts.selectedWorkflowId.value = 'w1'
    opts.completionKind.value = 'workflow'
    opts.reset({ allowAllCommands: true })
    expect(opts.assignee.value).toBe('agent')
    expect(opts.attachments.value).toEqual([])
    expect(opts.scheduleType.value).toBe('none')
    expect(opts.selectedEventId.value).toBe('')
    expect(opts.selectedWorkflowId.value).toBe('')
    expect(opts.completionKind.value).toBe('event')
    expect(opts.allowAllCommands.value).toBe(true)
  })
})
