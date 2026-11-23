// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The what-to-run control, and the one thing about it that is not obvious.
 *
 * jsdom has no layout, so the width this is really about cannot be measured
 * here. What can be pinned down is the pair of classes that produce it: an
 * `inline-flex` is as wide as its widest option demands, and without
 * `max-w-full` on the group and `min-w-0` on the options it cannot be made
 * narrower than that. In the workspace panel's 420px card the demand exceeded
 * the column, so the control spilled through the card's right padding and the
 * modal looked lopsided.
 *
 * Both launch forms — the machine page's and the workspace panel's — render
 * this same component, so losing either class breaks both at once. Hence a
 * test of the component rather than of one of its two callers.
 */

import { describe, it, expect } from 'vitest'
import { createApp } from 'vue'
import AgentKindPicker from '../src/components/AgentKindPicker.vue'
import { KINDS } from '../src/composables/useAgentLaunch'

function mount(props = {}) {
  const el = document.createElement('div')
  document.body.appendChild(el)
  createApp(AgentKindPicker, { idPrefix: 'kind', ...props }).mount(el)
  return el
}

describe('AgentKindPicker', () => {
  it('lets the control shrink to the column it is given', () => {
    const el = mount()
    const group = el.querySelector('[role=radiogroup]')

    // Without this the group's width is a floor, not a preference.
    expect(group.className).toContain('max-w-full')

    // A flex item's automatic minimum is its content, so the options need
    // this before the group's cap can take any effect.
    for (const option of group.querySelectorAll('[role=radio]')) {
      expect(option.className).toContain('min-w-0')
    }
  })

  it('offers every kind, addressable by id, with the chosen one marked', () => {
    const el = mount({ modelValue: 'acp-gateway' })

    for (const k of KINDS) {
      const option = el.querySelector(`#kind-${k.id}`)
      expect(option).toBeTruthy()
      expect(option.getAttribute('aria-checked')).toBe(String(k.id === 'acp-gateway'))
    }
  })

  it('describes whichever kind is chosen', () => {
    const gateway = KINDS.find((k) => k.id === 'acp-gateway')
    expect(mount({ modelValue: 'acp-gateway' }).textContent).toContain(gateway.description)

    // Nothing chosen is not an error state; there is simply nothing to explain.
    const none = mount({ modelValue: '' })
    for (const k of KINDS) expect(none.textContent).not.toContain(k.description)
  })
})
