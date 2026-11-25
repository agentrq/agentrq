// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * A card's inputs and buttons, mounted: each input writes the card's own
 * data model, checks hold a Button back with their messages, a click emits
 * the action with what was entered, and an answered card is read-only with
 * its chosen Button marked.
 */

import { afterEach, describe, expect, it, vi } from 'vitest'
import { createApp, h, nextTick, reactive } from 'vue'
import { readdirSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import A2uiSurface from '../src/components/a2ui/A2uiSurface.vue'
import { BASIC_CATALOG_ID, INTERACTIVE_COMPONENTS } from '../src/a2ui/validate.js'

const card = (components, dataModel, extra = {}) => ({
  type: 'a2ui',
  version: '1.0',
  surfaceId: 's1',
  messages: [
    { version: '1.0', createSurface: { surfaceId: 's1', catalogId: BASIC_CATALOG_ID } },
    { version: '1.0', updateComponents: { surfaceId: 's1', components } },
    ...(dataModel ? [{ version: '1.0', updateDataModel: { surfaceId: 's1', path: '/', value: dataModel } }] : []),
  ],
  ...extra,
})

const text = (id, t) => ({ id, component: 'Text', text: t })
const button = (id, name, extra = {}) => ({ id, component: 'Button', child: `${id}-label`, action: { event: { name } }, ...extra })
const col = (...children) => ({ id: 'root', component: 'Column', children })

const apps = []
function mount(metadata, extra = {}) {
  const el = document.createElement('div')
  document.body.appendChild(el)
  const props = reactive({ metadata, text: 'the fallback', readOnly: false, busy: false, ...extra })
  const actions = []
  const app = createApp({ render: () => h(A2uiSurface, { ...props, onAction: (a) => actions.push(a) }) })
  apps.push(app)
  app.mount(el)
  return { el, props, actions }
}
const q = (el, test) => el.querySelector(`[data-test="${test}"]`)
const qa = (el, test) => [...el.querySelectorAll(`[data-test="${test}"]`)]
const type = async (input, value, event = 'input') => {
  input.value = value
  input.dispatchEvent(new Event(event))
  await nextTick()
}
const blur = async (input) => {
  input.dispatchEvent(new Event('blur'))
  await nextTick()
}

afterEach(() => {
  while (apps.length) apps.pop().unmount()
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})

describe('Button', () => {
  it('sends its event with its context and userMessage resolved, and a copy of the data model', async () => {
    const { el, actions } = mount(card([
      col('ok'),
      button('ok', 'approve', {
        variant: 'primary',
        action: { event: { name: 'approve', userMessage: { '@path': '/msg' }, context: { id: 'pr-1', who: { '@path': '/who' }, list: [{ '@path': '/who' }] } } },
      }),
      text('ok-label', 'Approve'),
    ], { msg: '  Ship it  ', who: 'me' }))
    const b = q(el, 'a2ui-button')
    expect(b.textContent.trim()).toBe('Approve')
    expect(b.classList.contains('bg-black'), "a primary Button is the app's primary button").toBe(true)
    b.click()
    await nextTick()
    expect(actions).toEqual([{
      surfaceId: 's1', name: 'approve', sourceComponentId: 'ok',
      context: { id: 'pr-1', who: 'me', list: ['me'] },
      userMessage: 'Ship it',
      dataModel: { msg: '  Ship it  ', who: 'me' },
    }])
  })

  it('draws each variant, and an empty userMessage and context when it has none', async () => {
    const { el, actions } = mount(card([
      col('a', 'b'),
      button('a', 'x', { variant: 'borderless' }), text('a-label', 'A'),
      button('b', 'y'), text('b-label', 'B'),
    ]))
    const [a, b] = qa(el, 'a2ui-button')
    expect(a.className).toContain('border-transparent')
    expect(b.className).toContain('bg-white')
    b.click()
    expect(actions[0]).toMatchObject({ name: 'y', context: {}, userMessage: '', dataModel: {} })
  })

  it('is held back while a check fails, saying why, and freed once it passes', async () => {
    const { el, actions } = mount(card([
      col('name', 'go'),
      { id: 'name', component: 'TextField', label: 'Name', value: { '@path': '/name' } },
      button('go', 'go', { '@checks': [
        { condition: { '@call': 'required', args: { value: { '@path': '/name' } } }, message: 'Enter a name first' },
        { condition: { '@path': '/result' } },
      ] }),
      text('go-label', 'Go'),
    ], { name: '', result: { valid: false, message: 'Not yet' } }))
    const go = q(el, 'a2ui-button')
    expect(go.disabled).toBe(true)
    expect(qa(el, 'a2ui-check').map((p) => p.textContent)).toEqual(['Enter a name first', 'Not yet'])
    go.click()
    expect(actions).toEqual([])
    await type(q(el, 'a2ui-textfield').querySelector('input'), 'Ada')
    expect(qa(el, 'a2ui-check').map((p) => p.textContent)).toEqual(['Not yet'])
  })

  it('falls back to a plain message for a failing check with none', () => {
    const { el } = mount(card([col('go'), button('go', 'go', { '@checks': [{ condition: { '@path': '/no' } }] }), text('go-label', 'Go')]))
    expect(q(el, 'a2ui-check').textContent).toBe('This value is not valid')
  })

  it('sends nothing while a click is on its way', async () => {
    const { el, props, actions } = mount(card([col('go'), button('go', 'go'), text('go-label', 'Go')]), { busy: true })
    const go = q(el, 'a2ui-button')
    expect(go.disabled).toBe(true)
    go.click()
    expect(actions).toEqual([])
    props.busy = false
    await nextTick()
    expect(go.disabled).toBe(false)
  })

  it('spins on the pressed Button only, while its answer is on its way', async () => {
    const { el, props, actions } = mount(card([col('go', 'stop'), button('go', 'go'), text('go-label', 'Go'), button('stop', 'stop'), text('stop-label', 'Stop')]))
    const [go, stop] = qa(el, 'a2ui-button')
    go.click()
    expect(actions.map((a) => a.name), 'what was sent').toEqual(['go'])
    props.busy = true
    await nextTick()
    expect(q(go, 'a2ui-sending'), 'the spinner on the pressed Button').not.toBeNull()
    expect(go.getAttribute('aria-busy'), 'the pressed Button, busy for a screen reader').toBe('true')
    expect(q(stop, 'a2ui-sending'), 'no spinner on the other Button').toBeNull()
    expect(stop.getAttribute('aria-busy'), 'the other Button, not busy').toBeNull()
    props.busy = false
    await nextTick()
    expect(q(go, 'a2ui-sending'), 'the spinner, once sent').toBeNull()
  })

  it('ties a failing check to its Button for a screen reader', () => {
    const { el } = mount(card([col('go'), button('go', 'go', { '@checks': [{ condition: { '@path': '/no' }, message: 'Not yet' }] }), text('go-label', 'Go')]))
    const go = q(el, 'a2ui-button')
    const described = el.querySelector(`[id="${go.getAttribute('aria-describedby')}"]`)
    expect(described?.textContent.trim(), "the Button's description").toBe('Not yet')
  })

  it('on an answered card marks the chosen one and sends nothing', async () => {
    const metadata = card([col('yes', 'no'), button('yes', 'approve'), text('yes-label', 'Yes'), button('no', 'reject'), text('no-label', 'No')],
      null, { answered: { name: 'approve', at: '2026-11-25T00:00:00Z' } })
    const { el, actions } = mount(metadata)
    expect(q(el, 'a2ui-surface').dataset.answered).toBe('true')
    const [yes, no] = qa(el, 'a2ui-button')
    expect(yes.dataset.chosen).toBe('true')
    expect(yes.getAttribute('aria-pressed')).toBe('true')
    expect(q(yes, 'a2ui-chosen')).not.toBeNull()
    expect(no.dataset.chosen).toBeUndefined()
    expect(no.getAttribute('aria-pressed')).toBe('false')
    expect(yes.disabled && no.disabled).toBe(true)
    expect(yes.classList.contains('disabled:opacity-30'), 'the chosen Button, at full strength').toBe(false)
    expect(no.classList.contains('disabled:opacity-30'), 'the other Button, faded').toBe(true)
    yes.click()
    expect(actions).toEqual([])
  })

  it('shows no check messages on a card that takes no answer', () => {
    const { el } = mount(card([col('go'), button('go', 'go', { '@checks': [{ condition: false, message: 'never' }] }), text('go-label', 'Go')]), { readOnly: true })
    expect(qa(el, 'a2ui-check')).toEqual([])
  })

  it('runs a function on a click: openUrl opens an http(s) link, without an opener', async () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    const link = (id, url) => ({ id, component: 'Button', child: `${id}-label`, action: { functionCall: { '@call': 'openUrl', args: { url } } } })
    const { el, actions } = mount(card([
      col('web', 'js'), link('web', { '@path': '/url' }), text('web-label', 'Open'), link('js', 'javascript:alert(1)'), text('js-label', 'Bad'),
    ], { url: 'https://agentrq.com/pr/1' }), { readOnly: true })
    // Drawing it opened nothing.
    expect(open).not.toHaveBeenCalled()
    const [web, js] = qa(el, 'a2ui-button')
    // A link answers nothing, so it works on a closed card too.
    expect(web.disabled).toBe(false)
    web.click()
    js.click()
    expect(open.mock.calls).toEqual([['https://agentrq.com/pr/1', '_blank', 'noopener,noreferrer']])
    expect(actions).toEqual([])
  })
})

describe('TextField', () => {
  const field = (extra) => card([col('f'), { id: 'f', component: 'TextField', label: 'Note', value: { '@path': '/note' }, ...extra }], { note: 'hi' })

  it('writes what is typed into the data model', async () => {
    const { el, actions } = mount(card([
      col('f', 'echo', 'go'),
      { id: 'f', component: 'TextField', label: 'Note', placeholder: 'Say something', value: { '@path': '/note' } },
      text('echo', { '@path': '/note' }),
      button('go', 'go'), text('go-label', 'Go'),
    ], { note: 'hi' }))
    const input = q(el, 'a2ui-textfield').querySelector('input')
    expect(input.type).toBe('text')
    expect(input.value).toBe('hi')
    expect(input.placeholder).toBe('Say something')
    expect(q(el, 'a2ui-textfield').querySelector('label').textContent).toContain('Note')
    await type(input, 'hello')
    expect(q(el, 'a2ui-text').textContent.trim()).toBe('hello')
    q(el, 'a2ui-button').click()
    expect(actions[0].dataModel).toEqual({ note: 'hello' })
  })

  it('draws a paragraph, a number and a secret, a number kept as a string', async () => {
    let { el, actions } = mount(card([
      col('n', 'go'),
      { id: 'n', component: 'TextField', label: 'N', variant: 'number', value: { '@path': '/n' } },
      button('go', 'go'), text('go-label', 'Go'),
    ]))
    const n = q(el, 'a2ui-textfield').querySelector('input')
    expect(n.type).toBe('number')
    expect(n.getAttribute('inputmode')).toBe('decimal')
    await type(n, '42')
    q(el, 'a2ui-button').click()
    expect(actions[0].dataModel).toEqual({ n: '42' })

    el = mount(field({ variant: 'obscured' })).el
    expect(q(el, 'a2ui-textfield').querySelector('input').type).toBe('password')

    el = mount(field({ variant: 'longText' })).el
    const area = q(el, 'a2ui-textfield').querySelector('textarea')
    expect(area.value).toBe('hi')
    await type(area, 'two\nlines')
    expect(area.value).toBe('two\nlines')
    await blur(area)
  })

  it('keeps an edit of a literal value to itself', async () => {
    const { el } = mount(card([col('f'), { id: 'f', component: 'TextField', label: 'L', value: 'start' }]))
    const input = q(el, 'a2ui-textfield').querySelector('input')
    expect(input.value).toBe('start')
    await type(input, 'changed')
    expect(input.value).toBe('changed')
  })

  it("shows its checks' messages once touched", async () => {
    const { el } = mount(field({ '@checks': [{ condition: { '@call': 'length', args: { value: { '@path': '/note' }, min: 5 } }, message: 'At least 5' }] }))
    const input = q(el, 'a2ui-textfield').querySelector('input')
    expect(q(el, 'field-error')).toBeNull()
    await blur(input)
    expect(q(el, 'field-error').textContent).toContain('At least 5')
    expect(input.getAttribute('aria-invalid')).toBe('true')
    await type(input, 'hello')
    expect(q(el, 'field-error')).toBeNull()
  })

  it('is read-only on a card that takes no answer', () => {
    const { el } = mount(field({}), { readOnly: true })
    expect(q(el, 'a2ui-textfield').querySelector('input').disabled).toBe(true)
  })
})

describe('CheckBox', () => {
  it('ticks into the data model, and is read-only on an answered card', async () => {
    const metadata = card([
      col('c', 'echo'),
      { id: 'c', component: 'CheckBox', label: 'I agree', value: { '@path': '/ok' }, '@checks': [{ condition: { '@path': '/ok' }, message: 'You must agree' }] },
      text('echo', { '@call': 'formatString', args: { value: 'ok=${/ok}' } }),
    ], { ok: false })
    const { el, props } = mount(metadata)
    const box = q(el, 'a2ui-checkbox').querySelector('input')
    expect(q(el, 'a2ui-checkbox').textContent).toContain('I agree')
    expect(box.checked).toBe(false)
    box.click()
    await nextTick()
    expect(q(el, 'a2ui-text').textContent.trim()).toBe('ok=true')
    box.click()
    await nextTick()
    expect(q(el, 'field-error').textContent).toContain('You must agree')
    expect(box.getAttribute('aria-describedby')).toMatch(/-error$/)
    props.readOnly = true
    await nextTick()
    expect(box.disabled).toBe(true)
    expect(q(el, 'field-error')).toBeNull()
  })
})

describe('ChoicePicker', () => {
  const options = [{ label: 'Red', value: 'r' }, { label: { '@path': '/blue' }, value: 'b' }, { label: '', value: 'g' }]
  const picker = (extra, dataModel = { picked: [], blue: 'Blue' }) => card([
    col('p'), { id: 'p', component: 'ChoicePicker', label: 'Colour', options, value: { '@path': '/picked' }, ...extra },
  ], dataModel)
  const picked = (el) => qa(el, 'a2ui-choice').filter((l) => l.querySelector('input').checked).map((l) => l.dataset.value)

  it('picks one of a list of boxes, the label resolved or the value when empty', async () => {
    const { el } = mount(picker({}))
    expect(q(el, 'a2ui-choicepicker').querySelector('legend').textContent).toBe('Colour')
    expect(qa(el, 'a2ui-choice').map((l) => l.textContent.trim())).toEqual(['Red', 'Blue', 'g'])
    const [red, blue] = qa(el, 'a2ui-choice').map((l) => l.querySelector('input'))
    expect(red.type).toBe('radio')
    red.click()
    await nextTick()
    expect(picked(el)).toEqual(['r'])
    blue.click()
    await nextTick()
    expect(picked(el)).toEqual(['b'])
  })

  it('picks several, as boxes or chips, and takes one back', async () => {
    let { el } = mount(picker({ variant: 'multipleSelection' }))
    const [red, blue] = qa(el, 'a2ui-choice').map((l) => l.querySelector('input'))
    expect(red.type).toBe('checkbox')
    red.click(); await nextTick()
    blue.click(); await nextTick()
    expect(picked(el)).toEqual(['r', 'b'])
    red.click(); await nextTick()
    expect(picked(el)).toEqual(['b'])

    el = mount(picker({ variant: 'multipleSelection', displayStyle: 'chips' })).el
    const chips = qa(el, 'a2ui-chip')
    expect(chips[0].getAttribute('aria-pressed')).toBe('false')
    chips[0].click(); await nextTick()
    chips[2].click(); await nextTick()
    expect(chips.map((c) => c.getAttribute('aria-pressed'))).toEqual(['true', 'false', 'true'])
    expect(chips[0].classList.contains('shadow-sm'), 'a picked chip, raised').toBe(true)
    expect(chips[1].classList.contains('shadow-sm'), 'a chip not picked, lowered').toBe(false)
  })

  it('picks one chip, as a radio group', async () => {
    const { el } = mount(picker({ displayStyle: 'chips' }, { picked: ['b'], blue: 'Blue' }))
    const chips = qa(el, 'a2ui-chip')
    expect(chips.map((c) => c.getAttribute('aria-checked'))).toEqual(['false', 'true', 'false'])
    chips[0].click(); await nextTick()
    expect(chips.map((c) => c.getAttribute('aria-checked'))).toEqual(['true', 'false', 'false'])
  })

  it('filters its options, and says when none match', async () => {
    const { el } = mount(picker({ filterable: true }))
    const filter = q(el, 'a2ui-choice-filter')
    expect(filter.getAttribute('aria-label')).toBe('Filter Colour')
    await type(filter, 'BL')
    expect(qa(el, 'a2ui-choice').map((l) => l.dataset.value)).toEqual(['b'])
    await type(filter, 'zzz')
    expect(qa(el, 'a2ui-choice')).toEqual([])
    expect(q(el, 'a2ui-choice-none')).not.toBeNull()
  })

  it('has a plain filter label when it has none, and treats a non-list value as nothing picked', () => {
    const { el } = mount(card([col('p'), { id: 'p', component: 'ChoicePicker', options, value: { '@path': '/x' }, filterable: true }], { x: 'r' }))
    expect(q(el, 'a2ui-choice-filter').getAttribute('aria-label')).toBe('Filter')
    expect(q(el, 'a2ui-choicepicker').querySelector('legend')).toBeNull()
    expect(picked(el)).toEqual([])
  })

  it('shows its checks once touched, and changes nothing on an answered card', async () => {
    const checks = [{ condition: { '@call': 'required', args: { value: { '@path': '/picked' } } }, message: 'Pick one' }]
    const { el, props } = mount(picker({ variant: 'multipleSelection', displayStyle: 'chips', '@checks': checks }))
    const chips = qa(el, 'a2ui-chip')
    chips[0].click(); await nextTick()
    chips[0].click(); await nextTick()
    expect(q(el, 'field-error').textContent).toContain('Pick one')
    props.metadata = { ...props.metadata, answered: { name: 'x' } }
    await nextTick()
    expect(q(el, 'a2ui-choicepicker').disabled).toBe(true)
    // A click a disabled fieldset would never deliver still changes nothing.
    qa(el, 'a2ui-chip')[1].dispatchEvent(new MouseEvent('click'))
    await nextTick()
    expect(qa(el, 'a2ui-chip').map((c) => c.getAttribute('aria-pressed'))).toEqual(['false', 'false', 'false'])
  })
})

describe('Slider', () => {
  it('slides from min to max in steps, into the data model', async () => {
    const { el, actions } = mount(card([
      col('s', 'go'),
      { id: 's', component: 'Slider', label: 'Effort', min: 10, max: 50, steps: 4, value: { '@path': '/effort' } },
      button('go', 'go'), text('go-label', 'Go'),
    ], { effort: 20 }))
    const input = q(el, 'a2ui-slider').querySelector('input')
    expect([input.min, input.max, input.step, input.value]).toEqual(['10', '50', '10', '20'])
    expect(q(el, 'a2ui-slider-value').textContent).toBe('20')
    expect(q(el, 'a2ui-slider').querySelector('label').textContent).toBe('Effort')
    await type(input, '40')
    expect(q(el, 'a2ui-slider-value').textContent).toBe('40')
    q(el, 'a2ui-button').click()
    expect(actions[0].dataModel).toEqual({ effort: 40 })
  })

  it('starts at min with no value, slides freely without steps, and shows its checks', async () => {
    const { el } = mount(card([
      col('s'),
      { id: 's', component: 'Slider', max: 5, value: { '@path': '/v' }, '@checks': [{ condition: { '@call': 'numeric', args: { value: { '@path': '/v' }, min: 3 } }, message: 'At least 3' }] },
    ]))
    const input = q(el, 'a2ui-slider').querySelector('input')
    expect([input.min, input.step, input.value]).toEqual(['0', 'any', '0'])
    await type(input, '1')
    expect(q(el, 'field-error').textContent).toContain('At least 3')
    expect(input.getAttribute('aria-describedby')).toMatch(/-error$/)
  })
})

describe('DateTimeInput', () => {
  const picker = (extra, dataModel) => card([
    col('d'), { id: 'd', component: 'DateTimeInput', label: 'When', value: { '@path': '/when' }, ...extra },
  ], dataModel)

  it('picks a date, within its bounds, into the data model', async () => {
    const { el } = mount(picker({ enableDate: true, min: '2026-01-01', max: { '@path': '/max' } }, { when: '2026-11-25T10:00:00Z', max: '2026-12-31' }))
    const input = q(el, 'a2ui-datetime').querySelector('input')
    expect([input.type, input.value, input.min, input.max]).toEqual(['date', '2026-11-25', '2026-01-01', '2026-12-31'])
    await type(input, '2026-12-01')
    expect(input.value).toBe('2026-12-01')
    await type(input, '2026-12-02', 'change')
    expect(input.value).toBe('2026-12-02')
  })

  it('picks a time, or both, and labels itself when the card does not', async () => {
    const noLabel = picker({ enableTime: true }, { when: '2026-11-25T10:30:00Z' })
    delete noLabel.messages[1].updateComponents.components[1].label
    let { el } = mount(noLabel)
    let input = q(el, 'a2ui-datetime').querySelector('input')
    expect([input.type, input.value]).toEqual(['time', '10:30'])
    expect(q(el, 'a2ui-datetime').querySelector('label').textContent).toContain('Date')
    el = mount(picker({ enableDate: true, enableTime: true }, { when: '2026-11-25T10:30:00Z' })).el
    input = q(el, 'a2ui-datetime').querySelector('input')
    expect([input.type, input.value, input.min]).toEqual(['datetime-local', '2026-11-25T10:30', ''])
  })

  it("shows its checks' messages once changed", async () => {
    const { el } = mount(picker({ '@checks': [{ condition: { '@call': 'required', args: { value: { '@path': '/when' } } }, message: 'Pick a day' }] }, { when: '2026-11-25' }))
    const input = q(el, 'a2ui-datetime').querySelector('input')
    await type(input, '')
    expect(q(el, 'field-error').textContent).toContain('Pick a day')
  })
})

describe('Modal', () => {
  const modal = () => card([
    col('m'),
    { id: 'm', component: 'Modal', trigger: 'open', content: 'body' },
    button('open', 'openModalEvent'), text('open-label', 'Details'),
    { id: 'body', component: 'Column', children: ['inside', 'ok'] },
    text('inside', 'The details'),
    button('ok', 'approve'), text('ok-label', 'Approve'),
  ])

  it('opens its content from its trigger, which answers nothing; a Button inside answers', async () => {
    const { el, actions } = mount(modal())
    expect(q(el, 'a2ui-modal-dialog')).toBeNull()
    q(el, 'a2ui-button').click()
    await nextTick(); await nextTick()
    const dialog = q(el, 'a2ui-modal-dialog')
    expect(dialog.hasAttribute('open')).toBe(true)
    expect(dialog.textContent).toContain('The details')
    expect(actions).toEqual([])
    dialog.querySelector('[data-test="a2ui-button"]').click()
    expect(actions.map((a) => a.name)).toEqual(['approve'])
    q(el, 'a2ui-modal-close').click()
    await nextTick()
    expect(q(el, 'a2ui-modal-dialog')).toBeNull()
  })

  it('opens as a modal where the browser can, and closes with it', async () => {
    const showModal = vi.fn(function () { this.setAttribute('open', '') })
    HTMLDialogElement.prototype.showModal = showModal
    try {
      const { el } = mount(modal(), { readOnly: true })
      // The trigger still opens on a closed card: it answers nothing.
      const trigger = q(el, 'a2ui-button')
      expect(trigger.disabled).toBe(false)
      trigger.click()
      await nextTick(); await nextTick()
      expect(showModal).toHaveBeenCalledTimes(1)
      q(el, 'a2ui-modal-dialog').dispatchEvent(new Event('close'))
      await nextTick()
      expect(q(el, 'a2ui-modal-dialog')).toBeNull()
    } finally {
      delete HTMLDialogElement.prototype.showModal
    }
  })
})

describe('the surface', () => {
  it('keeps what was entered when the task around it is refreshed, and reloads a changed card', async () => {
    const metadata = card([col('f'), { id: 'f', component: 'TextField', label: 'L', value: { '@path': '/v' } }], { v: 'a' })
    const { el, props } = mount(metadata)
    await type(q(el, 'a2ui-textfield').querySelector('input'), 'typed')
    // The same card, a new object: what a refreshed thread hands it.
    props.metadata = JSON.parse(JSON.stringify(metadata))
    await nextTick()
    expect(q(el, 'a2ui-textfield').querySelector('input').value).toBe('typed')
    props.metadata = card([col('f'), { id: 'f', component: 'TextField', label: 'L', value: { '@path': '/v' } }], { v: 'b' })
    await nextTick()
    expect(q(el, 'a2ui-textfield').querySelector('input').value).toBe('b')
  })

  it('draws a question asked as a card, read-only with what was sent once answered', async () => {
    const question = {
      ...card([col('f', 'go'), { id: 'f', component: 'TextField', label: 'L', value: { '@path': '/v' } }, button('go', 'send'), text('go-label', 'Send')], { v: '' }),
      type: 'elicitation_request', mode: 'a2ui', status: 'pending', requestId: 'r1',
    }
    const { el, props } = mount(question)
    expect(q(el, 'a2ui-button').disabled).toBe(false)
    props.metadata = { ...question, status: 'accept', content: { name: 'send', context: {}, dataModel: { v: 'my answer' } } }
    await nextTick()
    expect(q(el, 'a2ui-textfield').querySelector('input').value).toBe('my answer')
    expect(q(el, 'a2ui-textfield').querySelector('input').disabled).toBe(true)
    expect(q(el, 'a2ui-button').dataset.chosen).toBe('true')
  })

  it('draws every input of the catalog, and the official interactive examples, with no placeholder', () => {
    const dir = resolve(__dirname, '../../backend/internal/service/a2ui/testdata/examples')
    const drawn = new Set()
    for (const file of readdirSync(dir).filter((f) => f.endsWith('.json'))) {
      const example = JSON.parse(readFileSync(resolve(dir, file), 'utf8'))
      const components = example.messages.flatMap((m) => m.updateComponents?.components ?? m.createSurface?.components ?? [])
      if (!components.some((c) => INTERACTIVE_COMPONENTS.includes(c.component))) continue
      const media = components.some((c) => ['Image', 'Video', 'AudioPlayer'].includes(c.component))
      if (media) continue
      const { el } = mount({ type: 'a2ui', messages: example.messages })
      if (!q(el, 'a2ui-surface')) continue // refused by the check, as on the server
      expect(qa(el, 'a2ui-unknown'), file).toEqual([])
      for (const c of components) drawn.add(c.component)
    }
    // Slider appears in no example; its own tests above draw it.
    expect(INTERACTIVE_COMPONENTS.filter((name) => drawn.has(name))).toEqual(['Button', 'TextField', 'CheckBox', 'ChoicePicker', 'DateTimeInput', 'Modal'])
  })
})

describe('a read-only list of boxes', () => {
  it('shows the boxes as not clickable, its pick kept', () => {
    const { el } = mount(card([
      col('p'), { id: 'p', component: 'ChoicePicker', options: [{ label: 'A', value: 'a' }], value: { '@path': '/p' } },
    ], { p: ['a'] }, { answered: {} }))
    const box = q(el, 'a2ui-choice')
    expect(box.classList.contains('cursor-default')).toBe(true)
    expect(box.classList.contains('opacity-50'), 'the pick, at full strength').toBe(false)
    expect(box.querySelector('input').checked).toBe(true)
    // An answer that named no Button (answered elsewhere) marks none.
    expect(q(el, 'a2ui-surface').dataset.answered).toBe('true')
  })
})

describe('interact', async () => {
  const { cardAnswer, clickButton } = await import('../src/a2ui/interact.js')

  it('sends nothing for a click on a closed or busy card, which the drawn button never delivers', () => {
    const send = vi.fn()
    const c = { id: 'b', action: { event: { name: 'go' } } }
    clickButton(c, { ui: { readOnly: true, busy: false, send } })
    clickButton(c, { ui: { readOnly: false, busy: true, send } })
    expect(send).not.toHaveBeenCalled()
  })

  it("reads an answer's name, or none", () => {
    expect(cardAnswer({ type: 'a2ui', answered: {} })).toEqual({ name: null })
    expect(cardAnswer({ type: 'a2ui' })).toBeNull()
    expect(cardAnswer({ type: 'elicitation_request', status: 'pending' })).toBeNull()
    expect(cardAnswer({ type: 'elicitation_request' })).toBeNull()
    expect(cardAnswer({ type: 'elicitation_request', status: 'cancel' })).toEqual({ name: null })
    expect(cardAnswer(null)).toBeNull()
  })
})
