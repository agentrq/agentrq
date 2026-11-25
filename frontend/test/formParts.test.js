// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/** The pieces the hook and event forms are built from. */

import { describe, it, expect, afterEach } from 'vitest'
import { createApp, h, nextTick, ref } from 'vue'
import FormField from '../src/components/FormField.vue'
import ChoiceCards from '../src/components/ChoiceCards.vue'
import ToggleSwitch from '../src/components/ToggleSwitch.vue'
import SegmentedControl from '../src/components/SegmentedControl.vue'
import { FIELD, fieldClass } from '../src/utils/formStyles'
import { focusFirstInvalid, formKeyAction, submitHint } from '../src/utils/forms'

const apps = []
function mount(render) {
  const el = document.createElement('div')
  document.body.append(el)
  const app = createApp({ render })
  app.mount(el)
  apps.push(app)
  return el
}

afterEach(() => {
  apps.splice(0).forEach((app) => app.unmount())
  document.body.innerHTML = ''
})

describe('the form helpers', () => {
  it('reads Cmd/Ctrl+Enter as submit and Escape as cancel', () => {
    expect(formKeyAction({ key: 'Enter', metaKey: true })).toBe('submit')
    expect(formKeyAction({ key: 'Enter', ctrlKey: true })).toBe('submit')
    expect(formKeyAction({ key: 'Enter' })).toBe('')
    expect(formKeyAction({ key: 'Escape' })).toBe('cancel')
    expect(formKeyAction({ key: 'a', metaKey: true })).toBe('')
  })

  it('names the keys for the keyboard in use', () => {
    expect(submitHint(true)).toBe('⌘ Enter')
    expect(submitHint(false)).toBe('Ctrl Enter')
  })

  it('focuses the first invalid field, and does nothing without a form', () => {
    const form = document.createElement('form')
    form.innerHTML = '<input id="a"><input id="b" aria-invalid="true"><input id="c" aria-invalid="true">'
    document.body.append(form)
    focusFirstInvalid(form)
    expect(document.activeElement.id).toBe('b')
    expect(() => focusFirstInvalid(null)).not.toThrow()
  })

  it('draws a field red only while it is invalid', () => {
    expect(FIELD).toBe(fieldClass(false))
    expect(fieldClass(true)).toContain('border-red-500')
    expect(FIELD).not.toContain('red')
    expect(FIELD, 'a field, faded while disabled').toContain('disabled:opacity-50')
    expect(fieldClass(false, { answered: true }), "an answered card's field, read in full").not.toContain('disabled:opacity-50')
    expect(fieldClass(false, { answered: true }), "an answered card's field").toContain('disabled:bg-gray-50')
  })
})

describe('FormField', () => {
  it('ties its label, error and description to the control by id', async () => {
    const el = mount(() => h(FormField, { label: 'Name', id: 'nm', required: true, error: 'Too short.', description: 'Yours.' }, {
      default: (f) => h('input', { id: f.id, 'aria-describedby': f.describedBy, 'aria-invalid': f.invalid }),
    }))
    await nextTick()
    const input = el.querySelector('input')
    expect(el.querySelector('label').htmlFor).toBe('nm')
    expect(input.getAttribute('aria-describedby')).toBe('nm-error nm-description')
    expect(document.getElementById('nm-error').textContent.trim()).toBe('Too short.')
    expect(document.getElementById('nm-description').textContent.trim()).toBe('Yours.')
    expect(el.querySelector('[aria-hidden="true"]').textContent.trim()).toBe('*')
  })

  it('makes its own id, takes a description slot, says Optional, and describes nothing when bare', async () => {
    const el = mount(() => [
      h(FormField, { label: 'A', optional: true }, {
        default: (f) => h('input', { id: f.id, 'aria-describedby': f.describedBy }),
        description: () => 'From a slot',
      }),
      h(FormField, { label: 'B' }, { default: (f) => h('input', { id: f.id, 'aria-describedby': f.describedBy }) }),
    ])
    await nextTick()
    const [a, b] = el.querySelectorAll('input')
    expect(a.id).toMatch(/^field-/)
    expect(a.id).not.toBe(b.id)
    expect(document.getElementById(a.getAttribute('aria-describedby')).textContent.trim()).toBe('From a slot')
    expect(el.textContent).toContain('Optional')
    expect(b.hasAttribute('aria-describedby')).toBe(false)
  })
})

describe('ChoiceCards', () => {
  it('is a fieldset of radios that marks the one picked and names its error', async () => {
    const picked = ref('a')
    const el = mount(() => h(ChoiceCards, {
      legend: 'Pick', modelValue: picked.value, 'onUpdate:modelValue': (v) => { picked.value = v },
      options: [{ value: 'a', label: 'A', description: 'The first' }, { value: 'b', label: 'B' }], columns: 'grid-cols-3',
    }))
    await nextTick()
    expect(el.querySelector('legend').textContent.trim()).toBe('Pick')
    expect(el.querySelector('.grid').classList.contains('grid-cols-3')).toBe(true)
    const radios = el.querySelectorAll('input[type=radio]')
    expect(radios[0].checked).toBe(true)
    expect(radios[0].name).toBe(radios[1].name)
    radios[1].checked = true
    radios[1].dispatchEvent(new Event('change'))
    await nextTick()
    expect(picked.value).toBe('b')
    expect(el.querySelector('fieldset').hasAttribute('aria-describedby')).toBe(false)

    const el2 = mount(() => h(ChoiceCards, { legend: 'Pick', required: true, error: 'Pick one.', options: [{ value: 'a', label: 'A' }] }))
    await nextTick()
    const fieldset = el2.querySelector('fieldset')
    expect(document.getElementById(fieldset.getAttribute('aria-describedby')).textContent.trim()).toBe('Pick one.')
    expect(el2.querySelector('label').className).toContain('border-red-500')
  })
})

describe('SegmentedControl', () => {
  it('is a radio group whose picked pill is raised, uppercase on request', async () => {
    const picked = ref(true)
    const el = mount(() => [
      h(SegmentedControl, {
        modelValue: picked.value, 'onUpdate:modelValue': (v) => { picked.value = v }, uppercase: true,
        options: [{ value: true, label: 'On', test: 'on' }, { value: false, label: 'Off', test: 'off' }],
      }),
      h(SegmentedControl, { options: [{ value: 'a', label: 'A' }] }),
    ])
    await nextTick()
    const [group, loose] = el.querySelectorAll('[role="radiogroup"]')
    const on = group.querySelector('[data-test="on"]')
    const off = group.querySelector('[data-test="off"]')
    expect(on.className).toContain('shadow-sm')
    expect(on.className).toContain('uppercase')
    expect(off.className).not.toContain('shadow-sm')
    const radio = off.querySelector('input')
    radio.checked = true
    radio.dispatchEvent(new Event('change'))
    await nextTick()
    expect(picked.value).toBe(false)
    expect(off.className).toContain('shadow-sm')
    expect(loose.querySelector('label').className).not.toContain('uppercase')
    expect(loose.querySelector('input').name).not.toBe(radio.name)
  })

  it("draws the chat's smaller toggle inside the task chat, as Agent | Human there", async () => {
    const picked = ref('a')
    const el = mount(() => h(SegmentedControl, {
      chat: true, modelValue: picked.value, 'onUpdate:modelValue': (v) => { picked.value = v },
      options: [{ value: 'a', label: 'A', test: 'a' }, { value: 'b', label: 'B', test: 'b' }],
    }))
    await nextTick()
    const group = el.querySelector('[role="radiogroup"]')
    expect(group.classList.contains('bg-gray-100'), 'the grey track').toBe(true)
    const [a, b] = [group.querySelector('[data-test="a"]'), group.querySelector('[data-test="b"]')]
    expect(a.classList.contains('text-[10px]'), "the chat buttons' type").toBe(true)
    expect(a.classList.contains('shadow-sm'), 'the picked segment, raised').toBe(true)
    b.querySelector('input').checked = true
    b.querySelector('input').dispatchEvent(new Event('change'))
    await nextTick()
    expect(picked.value, 'the pick').toBe('b')
    expect(b.classList.contains('shadow-sm'), 'the new pick, raised').toBe(true)
    expect(a.classList.contains('shadow-sm'), 'the old pick, lowered').toBe(false)
  })
})

describe('ToggleSwitch', () => {
  it('flips itself when nobody holds its value, and stays put while off', async () => {
    const el = mount(() => [h(ToggleSwitch, { 'aria-label': 'Loose' }), h(ToggleSwitch, { 'aria-label': 'Held', modelValue: true, disabled: true })])
    await nextTick()
    const [loose, held] = el.querySelectorAll('[role="switch"]')
    expect(loose.getAttribute('aria-checked')).toBe('false')
    loose.click()
    await nextTick()
    expect(loose.getAttribute('aria-checked')).toBe('true')
    expect(loose.getAttribute('aria-label')).toBe('Loose')
    expect(held.disabled).toBe(true)
    expect(held.getAttribute('aria-checked')).toBe('true')
  })
})
