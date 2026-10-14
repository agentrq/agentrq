// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, describe, it, expect } from 'vitest'
import { createApp, h, nextTick, ref } from 'vue'
import AcpProtocolPicker from '../src/components/AcpProtocolPicker.vue'

let app

function mount(initial) {
  const value = ref(initial)
  const emitted = []
  const el = document.createElement('div')
  document.body.appendChild(el)
  app = createApp({
    render: () =>
      h(AcpProtocolPicker, {
        idPrefix: 'pick',
        modelValue: value.value,
        'onUpdate:modelValue': (next) => {
          emitted.push(next)
          value.value = next
        },
      }),
  })
  app.mount(el)
  const shown = () => el.querySelector('[data-test=pick-acp-version-value]').textContent.trim()
  return { el, value, emitted, shown }
}

afterEach(() => {
  app?.unmount()
  document.body.innerHTML = ''
})

describe('AcpProtocolPicker', () => {
  it('starts on Classic, with each step named under the track', () => {
    const { el, shown } = mount('')
    expect(shown()).toBe('Classic — the gateway speaks ACP')
    expect(el.querySelector('#pick-acp-version').value).toBe('0')
    expect(el.querySelector('#pick-acp-version').getAttribute('aria-valuetext')).toBe('Classic — the gateway speaks ACP')
    expect([...el.querySelectorAll('button')].map((b) => b.textContent.trim())).toEqual(['Classic', 'ACP v1'])
  })

  it('moving to ACP v1 emits "1"', async () => {
    const { el, emitted, shown } = mount('')
    const slider = el.querySelector('#pick-acp-version')
    slider.value = '1'
    slider.dispatchEvent(new Event('input'))
    await nextTick()
    expect(emitted).toEqual(['1'])
    expect(shown()).toBe('ACP v1 — AgentRQ speaks ACP')
  })

  it('two quick moves keep both', () => {
    const { el, emitted } = mount('')
    const slider = el.querySelector('#pick-acp-version')
    slider.value = '1'
    slider.dispatchEvent(new Event('input'))
    slider.value = '0'
    slider.dispatchEvent(new Event('input'))
    expect(emitted).toEqual(['1', ''])
  })

  it('naming a step picks it', async () => {
    const { el, value } = mount('1')
    ;[...el.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Classic').click()
    await nextTick()
    expect(value.value).toBe('')
  })

  it('shows a value it has no step for as Classic', () => {
    const { shown } = mount('9')
    expect(shown()).toBe('Classic — the gateway speaks ACP')
  })
})
