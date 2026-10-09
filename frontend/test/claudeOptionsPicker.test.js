// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, describe, it, expect } from 'vitest'
import { createApp, h, nextTick, ref } from 'vue'
import ClaudeOptionsPicker from '../src/components/ClaudeOptionsPicker.vue'

let app

function mount(initial, models) {
  const value = ref(initial)
  const steps = ref(models)
  const el = document.createElement('div')
  document.body.appendChild(el)
  app = createApp({
    render: () =>
      h(ClaudeOptionsPicker, {
        idPrefix: 'pick',
        modelValue: value.value,
        ...(steps.value ? { models: steps.value } : {}),
        'onUpdate:modelValue': (next) => {
          value.value = next
        },
      }),
  })
  app.mount(el)
  const shown = (field) => el.querySelector(`[data-test=pick-${field}-value]`).textContent.trim()
  return { el, value, steps, shown }
}

afterEach(() => {
  app?.unmount()
  document.body.innerHTML = ''
})

describe('ClaudeOptionsPicker', () => {
  it('opens on the default for both, with each step named under its track', () => {
    const { el, shown } = mount({ model: '', effort: '' })
    expect(shown('model')).toBe('Default')
    expect(shown('effort')).toBe('Default')
    expect(el.querySelector('#pick-model').getAttribute('aria-valuetext')).toBe('Default')
    const names = [...el.querySelectorAll('button')].map((b) => b.textContent.trim())
    expect(names).toEqual(['Default', 'Haiku', 'Sonnet', 'Opus', 'Fable', 'Default', 'Low', 'Medium', 'High', 'Extra high', 'Max'])
  })

  it('keeps both changes when the two sliders move before the form re-renders', async () => {
    const { el, value, shown } = mount({ model: '', effort: '' })
    const model = el.querySelector('#pick-model')
    const effort = el.querySelector('#pick-effort')
    model.value = '2'
    model.dispatchEvent(new Event('input'))
    effort.value = '5'
    effort.dispatchEvent(new Event('input'))
    await nextTick()
    expect(value.value).toEqual({ model: 'sonnet', effort: 'max' })
    expect(shown('model')).toBe('Sonnet')
    expect(shown('effort')).toBe('Max')
  })

  it('picks a step when its name is clicked', async () => {
    const { el, value } = mount({ model: 'opus', effort: 'high' })
    ;[...el.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Low').click()
    await nextTick()
    expect(value.value).toEqual({ model: 'opus', effort: 'low' })
  })

  it('follows a value the form replaces, and shows one no step stands for as the default', async () => {
    const { value, shown } = mount({ model: 'haiku', effort: '' })
    value.value = { model: 'opusplan', effort: 'xhigh' }
    await nextTick()
    expect(shown('model')).toBe('Default')
    expect(shown('effort')).toBe('Extra high')
  })

  it('shows the machine\'s own model names, and drops a model that machine does not offer', async () => {
    const { el, value, steps, shown } = mount({ model: 'fable', effort: 'high' }, [
      { id: '', name: 'Default (Opus 5.5)' },
      { id: 'opus', name: 'Opus 5.5' },
      { id: 'fable', name: 'Fable 5.1' },
    ])
    expect(shown('model')).toBe('Fable 5.1')
    expect(el.querySelector('#pick-model').max).toBe('2')
    steps.value = [
      { id: '', name: 'Default (Opus 5.5)' },
      { id: 'opus', name: 'Opus 5.5' },
    ]
    await nextTick()
    await nextTick()
    expect(value.value).toEqual({ model: '', effort: 'high' })
    expect(shown('model')).toBe('Default (Opus 5.5)')
  })
})
