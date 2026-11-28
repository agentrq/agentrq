// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The time fields drawn on the account's clock: a time of day, and a date
 * with a time. The value stays on the 24-hour clock whichever is shown.
 */

import { describe, it, expect, afterEach, vi } from 'vitest'
import { createApp, h, nextTick, ref } from 'vue'
import ClockInput from '../src/components/ClockInput.vue'
import DateTimeInput from '../src/components/DateTimeInput.vue'
import { timeFormat } from '../src/composables/useTimeFormat'

let app
afterEach(() => {
  app?.unmount()
  app = null
  document.body.innerHTML = ''
  timeFormat.value = 'browser'
  vi.useRealTimers()
})

function mount(component, value, props = {}) {
  const model = ref(value)
  const el = document.createElement('div')
  document.body.appendChild(el)
  app = createApp({ render: () => h(component, { modelValue: model.value, 'onUpdate:modelValue': (v) => { model.value = v }, ...props }) })
  app.mount(el)
  return { el, model }
}

const $ = (el, t) => el.querySelector(`[data-test="${t}"]`)
const options = (select) => [...select.options].map((o) => o.textContent)
async function pick(select, value) {
  select.value = String(value)
  select.dispatchEvent(new Event('change'))
  await nextTick()
}

describe('ClockInput', () => {
  it('on the 24-hour clock picks 00 to 23, with no AM or PM', async () => {
    timeFormat.value = '24h'
    const { el, model } = mount(ClockInput, '09:05')
    expect(options($(el, 'clock-hour'))).toEqual(Array.from({ length: 24 }, (_, i) => String(i).padStart(2, '0')))
    expect([$(el, 'clock-hour').value, $(el, 'clock-minute').value]).toEqual(['9', '5'])
    expect($(el, 'clock-period')).toBeNull()
    expect(el.querySelector('[role="group"]').getAttribute('aria-label')).toBe('Time')
    await pick($(el, 'clock-hour'), 17)
    expect(model.value).toBe('17:05')
    await pick($(el, 'clock-minute'), 45)
    expect(model.value).toBe('17:45')
  })

  it('on the 12-hour clock picks 12 to 11 and AM or PM, keeping the half of the day', async () => {
    timeFormat.value = '12h'
    const { el, model } = mount(ClockInput, '15:30')
    expect(options($(el, 'clock-hour'))).toEqual(['12', '01', '02', '03', '04', '05', '06', '07', '08', '09', '10', '11'])
    expect([$(el, 'clock-hour').value, $(el, 'clock-period').value]).toEqual(['3', 'PM'])
    await pick($(el, 'clock-hour'), 12)
    expect(model.value).toBe('12:30')
    await pick($(el, 'clock-period'), 'AM')
    expect(model.value).toBe('00:30')
    expect([$(el, 'clock-hour').value, $(el, 'clock-period').value]).toEqual(['12', 'AM'])
    await pick($(el, 'clock-hour'), 7)
    expect(model.value).toBe('07:30')
    await pick($(el, 'clock-period'), 'PM')
    expect(model.value).toBe('19:30')
  })

  it('with no time shows dashes, and starts at the top of the hour, in the morning', async () => {
    timeFormat.value = '12h'
    let { el, model } = mount(ClockInput, '', { label: 'Launch' })
    expect([$(el, 'clock-hour').value, $(el, 'clock-minute').value, $(el, 'clock-period').value]).toEqual(['', '', 'AM'])
    expect(el.querySelector('[role="group"]').getAttribute('aria-label')).toBe('Launch')
    await pick($(el, 'clock-hour'), 8)
    expect(model.value).toBe('08:00')
    app.unmount();
    ({ el, model } = mount(ClockInput, 'not a time'))
    await pick($(el, 'clock-minute'), 15)
    expect(model.value).toBe('00:15')
    app.unmount();
    ({ el, model } = mount(ClockInput, '25:00'))
    await pick($(el, 'clock-period'), 'PM')
    expect(model.value).toBe('12:00')
    app.unmount()
    timeFormat.value = '24h';
    ({ el, model } = mount(ClockInput, ''))
    await pick($(el, 'clock-hour'), 22)
    expect(model.value).toBe('22:00')
  })

  it('follows a change of clock at once, and a disabled one picks nothing', async () => {
    timeFormat.value = '24h'
    const { el } = mount(ClockInput, '18:00', { disabled: true })
    expect($(el, 'clock-period')).toBeNull()
    timeFormat.value = '12h'
    await nextTick()
    expect([$(el, 'clock-hour').value, $(el, 'clock-period').value]).toEqual(['6', 'PM'])
    expect([...el.querySelectorAll('select')].every((s) => s.disabled)).toBe(true)
  })
})

describe('DateTimeInput', () => {
  it('shows the date and the time apart, and keeps the other when one changes', async () => {
    timeFormat.value = '24h'
    const { el, model } = mount(DateTimeInput, '2030-03-04T05:06', { min: '2030-01-01T00:00', max: '2030-12-31' })
    const date = $(el, 'datetime-date')
    expect([date.type, date.value, date.min, date.max]).toEqual(['date', '2030-03-04', '2030-01-01', '2030-12-31'])
    expect([$(el, 'clock-hour').value, $(el, 'clock-minute').value]).toEqual(['5', '6'])
    date.value = '2030-04-01'
    date.dispatchEvent(new Event('input'))
    await nextTick()
    expect(model.value).toBe('2030-04-01T05:06')
    await pick($(el, 'clock-hour'), 21)
    expect(model.value).toBe('2030-04-01T21:06')
    date.value = ''
    date.dispatchEvent(new Event('change'))
    await nextTick()
    expect(model.value).toBe('')
  })

  it('starts a date picked first at midnight, and a time picked first today', async () => {
    vi.useFakeTimers({ now: new Date(2031, 0, 9, 12) })
    timeFormat.value = '24h'
    let { el, model } = mount(DateTimeInput, '')
    expect([$(el, 'datetime-date').min, $(el, 'datetime-date').max]).toEqual(['', ''])
    await pick($(el, 'clock-hour'), 9)
    expect(model.value).toBe('2031-01-09T09:00')
    app.unmount();
    ({ el, model } = mount(DateTimeInput, undefined, { disabled: true }))
    const date = $(el, 'datetime-date')
    expect(date.disabled).toBe(true)
    date.value = '2031-02-02'
    date.dispatchEvent(new Event('change'))
    await nextTick()
    expect(model.value).toBe('2031-02-02T00:00')
  })
})
