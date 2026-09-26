// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

// The form is the only place a person can change a scheduled task, so opening
// one to fix a typo has to leave its schedule exactly as it was — including
// the cron shapes the picker has no controls for.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h, ref, computed } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import { CronExpressionParser } from 'cron-parser'

const push = vi.fn()
vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: 'ws1', taskId: 't1' }, query: {} }),
  useRouter: () => ({ push }),
}))

const WORKSPACE = { id: 'ws1', name: 'Ops', allowAllCommands: false, clearContextDefault: false }

// The task the page is opened on. Set per test, before mounting.
let TASK = null

const updateScheduledTask = vi.fn(() => Promise.resolve({ task: TASK }))

vi.mock('../src/api', () => ({
  getWorkspace: () => Promise.resolve({ workspace: WORKSPACE }),
  getTask: () => Promise.resolve({ task: TASK }),
  createTask: vi.fn(() => Promise.resolve({ task: TASK })),
  updateScheduledTask: (...args) => updateScheduledTask(...args),
  fetchEvents: () => Promise.resolve({ events: [] }),
  fetchWorkflows: () => Promise.resolve({ workflows: [] }),
  recordTelemetry: vi.fn(),
  API_BASE_URL: '/api/v1',
}))

// Both load a transformers.js model into a web worker on demand. Neither has
// anything to do with the schedule, and jsdom has no worker to give them.
vi.mock('../src/composables/useAutoTitle', () => ({
  useAutoTitle: () => ({
    isSupported: ref(false),
    isGenerating: ref(false),
    isModelLoading: ref(false),
    modelProgress: ref(0),
    isOverridden: ref(true),
    markOverridden: vi.fn(),
    generateTitle: vi.fn(),
  }),
}))
vi.mock('../src/composables/useSpeechToText', () => ({
  useSpeechToText: () => ({
    isRecording: ref(false),
    isTranscribing: ref(false),
    isModelLoading: ref(false),
    modelProgress: ref(0),
    error: ref(''),
    isSupported: ref(false),
    toggleRecording: vi.fn(),
  }),
}))
// Asks the workspace for a running Claude Code session. No terminal here, so
// the icon is never offered and the flag is never sent.
vi.mock('../src/composables/useClearContext', () => ({
  useClearContext: () => ({ offered: computed(() => false), load: () => Promise.resolve() }),
  clearContextTooltip: () => '',
}))
vi.mock('../src/components/AgentModelPicker.vue', () => ({
  default: { name: 'AgentModelPicker', render: () => h('div') },
}))

const { default: TaskFormView } = await import('../src/views/TaskFormView.vue')

const settle = () => new Promise((resolve) => setTimeout(resolve, 20))

async function setValue(node, value) {
  node.value = value
  node.dispatchEvent(new Event(node.tagName === 'SELECT' ? 'change' : 'input'))
  await settle()
}

/** Torn down in afterEach, so a failed assertion cannot leave one running. */
let mounted = null

async function mount() {
  const el = document.createElement('div')
  document.body.appendChild(el)
  const app = createApp({ render: () => h(TaskFormView) })
  app.component('RouterLink', {
    props: ['to'],
    setup: (p, { slots }) => () => h('a', {}, slots.default?.()),
  })
  // Registered app-wide in main.js, which this mount does not go through.
  app.directive('click-outside', {})
  app.mount(el)
  mounted = { app, el }
  await settle()

  /** Submit the form exactly as the Save button does. */
  const save = async () => {
    el.querySelector('#taskForm').dispatchEvent(new Event('submit', { cancelable: true }))
    await settle()
  }

  /** The schedule controls live in a popover behind the toolbar's clock. */
  const openSchedule = async () => {
    el.querySelector('button[aria-label="Set schedule"]').click()
    await settle()
  }

  const frequencySelect = () =>
    [...el.querySelectorAll('select')]
      .find((s) => [...s.options].some((o) => o.value === 'monthly'))
  const frequency = () => frequencySelect().value

  const day = () => Number(el.querySelector('select[aria-label="Day of month"]').value)

  const setTitle = (value) =>
    setValue(el.querySelector('input[type="text"], input:not([type])'), value)
  const setTime = (value) => setValue(el.querySelector('input[type="time"]'), value)
  const setDay = (value) =>
    setValue(el.querySelector('select[aria-label="Day of month"]'), String(value))

  /** None, One-time or Repeated, by the label on its button. */
  const pickType = async (name) => {
    [...el.querySelectorAll('button')].find((b) => b.textContent.trim() === name).click()
    await settle()
  }
  const setOneTime = (value) => setValue(el.querySelector('input[type="datetime-local"]'), value)
  const setFrequency = (value) => setValue(frequencySelect(), value)

  return {
    el, save, openSchedule, frequency, day, setTitle, setTime, setDay,
    pickType, setOneTime, setFrequency,
  }
}

afterEach(() => {
  mounted?.app.unmount()
  mounted?.el.remove()
  mounted = null
})

/** The cronSchedule argument of `updateScheduledTask(ws, id, title, body, assignee, cron, …)`. */
const savedSchedule = () => updateScheduledTask.mock.calls.at(-1)[5]

/** When a cron next fires, read on the local clock. */
const nextRun = (cron) => {
  const prefixed = /^(?:CRON_TZ|TZ)=(\S+)\s+(.+)$/.exec(cron)
  const [spec, tz] = prefixed ? [prefixed[2], prefixed[1]] : [cron, 'UTC']
  return CronExpressionParser.parse(spec, { tz }).next().toDate()
}

const zone = () => Intl.DateTimeFormat().resolvedOptions().timeZone

/** A schedule in this machine's zone, written the way the form writes one. */
const inZone = (spec) => (zone() === 'UTC' ? spec : `CRON_TZ=${zone()} ${spec}`)

const scheduled = (cronSchedule) => ({
  id: 't1', title: 'Monthly invoice run', body: 'Generate and send the invoices.',
  assignee: 'agent', status: 'cron', cronSchedule,
})

describe('editing a scheduled task', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    updateScheduledTask.mockClear()
    push.mockClear()
  })

  it('keeps a monthly schedule monthly when only the title is changed', async () => {
    TASK = scheduled('0 9 1 * *')

    const { setTitle, save } = await mount()
    await setTitle('Monthly invoice run (Q3)')
    await save()

    expect(updateScheduledTask).toHaveBeenCalledTimes(1)
    expect(savedSchedule()).toBe('0 9 1 * *')
  })

  it('reads a monthly schedule back on the day the cron names', async () => {
    TASK = scheduled(inZone('30 6 15 * *'))

    const { openSchedule, frequency, day } = await mount()
    await openSchedule()

    expect(frequency()).toBe('monthly')
    expect(day()).toBe(15)
  })

  // Every shape the picker has controls for, plus the ones it has none for:
  // those used to be rewritten from the default Mon-Fri on load, before
  // anybody had touched the form.
  it.each([
    ['daily', '0 9 * * *'],
    ['hourly', '0 * * * *'],
    ['bi-hourly', '0 */2 * * *'],
    ['every 15 minutes', '*/15 * * * *'],
    ['every 30 minutes', '*/30 * * * *'],
    ['twice a day', '30 8,20 * * *'],
    ['weekdays', '0 9 * * 1,2,3,4,5'],
    ['weekly', '0 9 * * 0'],
    ['monthly on the 1st', '0 9 1 * *'],
    ['monthly on the 15th', '30 6 15 * *'],
    ['monthly on the 28th', '0 23 28 * *'],
    ['monthly on the 31st', '0 0 31 * *'],
    ['monthly on two days', '0 9 1,15 * *'],
    ['monthly at two times', '0 9,21 15 * *'],
    ['a day range', '0 9 1-3 * *'],
    ['an hour step', '0 */6 1 * *'],
    ['a day and a weekday', '0 9 1 * 1'],
    ['a single month', '0 9 1 3 *'],
    ['a zoned daily', 'CRON_TZ=Pacific/Auckland 0 9 * * *'],
    ['a zoned monthly', 'CRON_TZ=America/New_York 30 6 15 * *'],
  ])('leaves a %s schedule alone', async (_label, schedule) => {
    TASK = scheduled(schedule)

    const { save } = await mount()
    await save()

    expect(savedSchedule()).toBe(schedule)
  })

  it('says so when the stored schedule is one it cannot show', async () => {
    TASK = scheduled('0 9 1,15 * *')

    const { el, openSchedule } = await mount()
    await openSchedule()

    expect(el.textContent).toContain('Set outside this form')
  })

  it('names the zone when the schedule was not written in this one', async () => {
    TASK = scheduled('CRON_TZ=Pacific/Kiritimati 0 9 * * *')

    const { el, openSchedule } = await mount()
    await openSchedule()

    expect(el.textContent).toContain('Pacific/Kiritimati')
  })

  // The zone named is the one the cron being saved is read in, whatever zone
  // the picker was loaded with.
  it('names no stored zone for a one-time date, which is in this browser\'s', async () => {
    TASK = scheduled('CRON_TZ=Pacific/Kiritimati 0 9 * * *')

    const { el, openSchedule, pickType, setOneTime, save } = await mount()
    await openSchedule()
    await pickType('One-time')
    await setOneTime('2026-12-01T09:00')

    expect(el.textContent).not.toContain('Times are in')
    await save()
    expect(savedSchedule()).toBe(inZone('0 9 1 12 *'))
  })

  it('names no zone once the schedule is removed', async () => {
    TASK = scheduled('CRON_TZ=Pacific/Kiritimati 0 9 * * *')

    const { el, openSchedule, pickType } = await mount()
    await openSchedule()
    await pickType('None')

    expect(el.textContent).not.toContain('Times are in')
  })

  it('names no stored zone for a sub-hourly preset, which is in UTC', async () => {
    TASK = scheduled('CRON_TZ=Pacific/Kiritimati 0 9 * * *')

    const { el, openSchedule, setFrequency, save } = await mount()
    await openSchedule()
    await setFrequency('15min')

    expect(el.textContent).not.toContain('Pacific/Kiritimati')
    await save()
    expect(savedSchedule()).toBe('*/15 * * * *')
  })

  // Trying another type and coming back must not move the schedule into this
  // browser's zone.
  it('keeps the stored zone when switched away and back to repeated', async () => {
    TASK = scheduled('CRON_TZ=Pacific/Kiritimati 0 9 * * *')

    const { el, openSchedule, pickType, save } = await mount()
    await openSchedule()
    await pickType('One-time')
    await pickType('Repeated')

    expect(el.textContent).toContain('Times are in Pacific/Kiritimati')
    await save()
    expect(savedSchedule()).toBe('CRON_TZ=Pacific/Kiritimati 0 9 * * *')
  })

  it('still writes a legal cron when the picker cannot express the old one', async () => {
    TASK = scheduled('0 */6 1 * *')

    const { openSchedule, setTime, save } = await mount()
    await openSchedule()
    await setTime('22:45')
    await save()

    expect(savedSchedule()).not.toContain('NaN')
    expect(() => nextRun(savedSchedule())).not.toThrow()
  })

  it('keeps the day of the month when the time is changed', async () => {
    TASK = scheduled(inZone('30 6 15 * *'))

    const { openSchedule, frequency, day, setTime, save } = await mount()
    await openSchedule()
    await setTime('22:45')
    await save()

    expect(frequency()).toBe('monthly')
    expect(day()).toBe(15)
    expect(savedSchedule()).toBe(inZone('45 22 15 * *'))
  })

  it('fires on the day and at the time the picker was set to', async () => {
    TASK = scheduled(inZone('30 6 15 * *'))

    const { openSchedule, setTime, setDay, save } = await mount()
    await openSchedule()
    await setTime('09:00')
    await setDay(8)
    await save()

    const next = nextRun(savedSchedule())
    expect([next.getDate(), next.getHours(), next.getMinutes()]).toEqual([8, 9, 0])
  })

  // A schedule written before zones existed is UTC, and an edit that does not
  // touch the schedule must not quietly move it into this browser's zone.
  it('leaves a zoneless schedule zoneless until the schedule itself is edited', async () => {
    TASK = scheduled('0 9 15 * *')

    const { setTitle, save } = await mount()
    await setTitle('Renamed')
    await save()

    expect(savedSchedule()).toBe('0 9 15 * *')
  })

  it('keeps a zoneless schedule in UTC when its own fields are edited', async () => {
    TASK = scheduled('0 9 15 * *')

    const { openSchedule, setTime, save } = await mount()
    await openSchedule()
    await setTime('22:45')
    await save()

    expect(savedSchedule()).toBe('45 22 15 * *')
  })
})
