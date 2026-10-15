// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, describe, it, expect } from 'vitest'
import { createApp, h, nextTick, ref } from 'vue'
import AcpEffortPicker from '../src/components/AcpEffortPicker.vue'

let app

function mount(initial) {
  const value = ref(initial)
  const emitted = []
  const el = document.createElement('div')
  document.body.appendChild(el)
  app = createApp({
    render: () =>
      h(AcpEffortPicker, {
        idPrefix: 'pick',
        modelValue: value.value,
        'onUpdate:modelValue': (next) => {
          emitted.push(next)
          value.value = next
        },
      }),
  })
  app.mount(el)
  const shown = () => el.querySelector('[data-test=pick-acp-effort-value]').textContent.trim()
  return { el, value, emitted, shown }
}

afterEach(() => {
  app?.unmount()
  document.body.innerHTML = ''
})

describe('AcpEffortPicker', () => {
  it("starts on Default, with Claude Code's levels named under the track", () => {
    const { el, shown } = mount('')
    expect(shown()).toBe('Default')
    expect(el.querySelector('#pick-acp-effort').value).toBe('0')
    expect(el.querySelector('#pick-acp-effort').getAttribute('aria-valuetext')).toBe('Default')
    expect([...el.querySelectorAll('button')].map((b) => b.textContent.trim())).toEqual([
      'Default',
      'Low',
      'Medium',
      'High',
      'Extra high',
      'Max',
    ])
  })

  it('moving the slider emits the level', async () => {
    const { el, emitted, shown } = mount('')
    const slider = el.querySelector('#pick-acp-effort')
    slider.value = '3'
    slider.dispatchEvent(new Event('input'))
    await nextTick()
    expect(emitted).toEqual(['high'])
    expect(shown()).toBe('High')
  })

  it('naming a step picks it', async () => {
    const { el, value } = mount('high')
    ;[...el.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Default').click()
    await nextTick()
    expect(value.value).toBe('')
  })

  it('shows a level it has no step for as Default', () => {
    const { shown } = mount('ultra')
    expect(shown()).toBe('Default')
  })
})
