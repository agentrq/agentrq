// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The row of a new task's controls, mounted on its own: every button says what
 * it does when pointed at, the menus open and edit the options they were given,
 * and voice input writes into the text the row was given. The wiring into the
 * Pano's chat box is in `PanoViews.test.js`, and into the New Task form in
 * `taskFormUntitled.test.js`.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h, nextTick, ref } from 'vue'
import { createPinia } from 'pinia'

const stt = vi.hoisted(() => ({}))
vi.mock('../src/composables/useSpeechToText', async () => {
  const { ref } = await import('vue')
  return {
    useSpeechToText: (target) => {
      stt.target = target
      stt.recording = ref(false)
      stt.transcribing = ref(false)
      stt.loading = ref(false)
      stt.progress = ref(0)
      stt.supported = ref(true)
      stt.toggle = vi.fn(() => { target.value = `${target.value} dictated` })
      return {
        isRecording: stt.recording,
        isTranscribing: stt.transcribing,
        isModelLoading: stt.loading,
        modelProgress: stt.progress,
        isSupported: stt.supported,
        toggleRecording: stt.toggle,
      }
    },
  }
})

const { default: TaskOptionsTools } = await import('../src/components/TaskOptionsTools.vue')
const { default: TaskAttachmentChips } = await import('../src/components/TaskAttachmentChips.vue')
const { useTaskOptions } = await import('../src/composables/useTaskOptions')
const { useTooltipStore } = await import('../src/stores/tooltipStore')

const settle = () => new Promise((r) => setTimeout(r, 0))
const $ = (el, test) => el.querySelector(`[data-test="${test}"]`)
const click = async (node) => {
  node.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
  await settle()
}
const hover = async (node) => {
  node.dispatchEvent(new MouseEvent('mouseenter'))
  node.dispatchEvent(new MouseEvent('mouseleave'))
  await settle()
}

let app
let host

/** Mounts the row over `opts`, with `text` as the text a parent would own. */
async function mount(props = {}, opts = useTaskOptions()) {
  host = document.createElement('div')
  document.body.append(host)
  const text = ref('hello')
  app = createApp({
    render: () => h(TaskOptionsTools, {
      opts, workspaceId: 'ws1', text: text.value, 'onUpdate:text': (v) => { text.value = v }, ...props,
    }),
  })
  app.use(createPinia())
  app.directive('click-outside', {})
  app.mount(host)
  await settle()
  return { el: host, opts, text }
}

beforeEach(() => vi.spyOn(console, 'warn').mockImplementation(() => {}))
afterEach(() => {
  app?.unmount()
  host?.remove()
  vi.restoreAllMocks()
})

describe('pointing at a button', () => {
  it('says what each one does, in each state it can be in', async () => {
    const { el, opts } = await mount({ clearContextOffered: true })
    const tooltip = useTooltipStore()
    const shown = vi.spyOn(tooltip, 'show')
    const said = () => shown.mock.calls.map((c) => c[1])

    // Voice input: idle, recording, then waiting on the model, then transcribing.
    const mic = [...el.querySelectorAll('button')].find((b) => b.className.includes('relative') && b.querySelector('svg path[d^="M19 11"]'))
    await hover(mic)
    stt.recording.value = true
    await nextTick()
    await hover(mic)
    stt.recording.value = false
    stt.transcribing.value = true
    stt.loading.value = true
    stt.progress.value = 40
    await nextTick()
    await hover(mic)
    stt.loading.value = false
    await nextTick()
    await hover(mic)
    expect(said()).toEqual(['Voice input', 'Stop recording', 'Loading STT model... 40%', 'Transcribing...'])
    shown.mockClear()

    await hover($(el, 'task-assignee-agent'))
    await hover($(el, 'task-assignee-human'))
    await click($(el, 'task-assignee-human'))
    await click($(el, 'task-assignee-agent'))
    await hover($(el, 'task-clear-context'))
    opts.clearContext.value = true
    await nextTick()
    await hover($(el, 'task-clear-context'))
    await hover($(el, 'task-yolo'))
    opts.allowAllCommands.value = true
    await nextTick()
    await hover($(el, 'task-yolo'))
    await hover($(el, 'task-schedule'))
    opts.cronSchedule.value = '0 * * * *'
    await nextTick()
    await hover($(el, 'task-schedule'))
    expect(said()).toEqual([
      'Assign to Agent',
      'Assign to Human',
      expect.stringContaining('Start this task on a clean context'),
      expect.stringContaining('Clear context: the agent will be sent /clear'),
      'YOLO Mode: Skip approval for sensitive commands',
      'YOLO Active: Agent will execute all commands without approval',
      'Set Schedule',
      'Set Schedule',
    ])
  })

  it('names the schedule and the completion trigger once they are set', async () => {
    const { el, opts } = await mount()
    opts.events.value = [{ id: 'e1', name: 'shipped' }]
    await nextTick()
    const tooltip = useTooltipStore()
    const shown = vi.spyOn(tooltip, 'show')
    await click($(el, 'task-schedule'))
    await click($(el, 'task-schedule-onetime'))
    await hover($(el, 'task-schedule'))
    await click($(el, 'task-schedule-repeated'))
    await hover($(el, 'task-schedule'))
    await hover($(el, 'task-completion'))
    opts.selectedEventId.value = 'e1'
    await nextTick()
    await hover($(el, 'task-completion'))
    expect(shown.mock.calls.map((c) => c[1])).toEqual([
      'Schedule Active',
      opts.cronSchedule.value,
      'Emit Event or Run Workflow on Completion',
      'Event Emitted on Completion',
    ])
  })
})

describe('voice input', () => {
  it('is drawn in each of its states, and writes into the text it was given', async () => {
    const { el, text } = await mount({ clearContextOffered: false })
    const mic = [...el.querySelectorAll('button')].find((b) => b.querySelector('svg path[d^="M19 11"]'))
    expect(mic.className).toContain('text-gray-500')
    await click(mic)
    expect(stt.toggle).toHaveBeenCalledTimes(1)
    expect(text.value).toBe('hello dictated')

    stt.recording.value = true
    await nextTick()
    expect(el.querySelector('.animate-pulse')).not.toBeNull()
    stt.recording.value = false
    stt.transcribing.value = true
    await nextTick()
    expect(el.querySelector('svg.animate-spin')).not.toBeNull()
    expect([...el.querySelectorAll('button')].some((b) => b.disabled)).toBe(true)
  })

  it('is left out where the browser has none', async () => {
    await mount()
    app.unmount()
    host.remove()
    const { el } = await mount()
    stt.supported.value = false
    await nextTick()
    expect([...el.querySelectorAll('button')].some((b) => b.querySelector('svg path[d^="M19 11"]'))).toBe(false)
  })
})

describe('the schedule menu', () => {
  it('edits the options it was given, from one-time to a custom set of days', async () => {
    const { el, opts } = await mount()
    await click($(el, 'task-schedule'))
    await click($(el, 'task-schedule-onetime'))
    expect($(el, 'task-onetime-date')).not.toBeNull()
    await click($(el, 'task-schedule-repeated'))
    await click(el.querySelector('[class*="flex-1 rounded-sm"]'))
    expect(opts.scheduleType.value).toBe('none')
    await click($(el, 'task-schedule-repeated'))
    const preset = $(el, 'task-repeat-preset')
    preset.value = 'custom'
    preset.dispatchEvent(new Event('change'))
    await settle()
    const days = [...el.querySelectorAll('button')].filter((b) => /^(Sun|Mon|Tue|Wed|Thu|Fri|Sat)$/.test(b.textContent.trim()))
    expect(days.length).toBe(7)
    await click(days[6])
    expect(opts.selectedDays.value).toContain(opts.daysOptions[6].value)
    expect(opts.scheduleType.value).toBe('repeated')
    await click(days[6])
    expect(opts.selectedDays.value).not.toContain(opts.daysOptions[6].value)
    const time = el.querySelector('input[type=time]')
    time.value = '07:30'
    time.dispatchEvent(new Event('input'))
    await settle()
    expect(opts.repeatTime.value).toBe('07:30')
    await click($(el, 'task-schedule'))
    expect(el.querySelector('input[type=time]')).toBeNull()
  })
})

describe('the completion menu', () => {
  it('is absent with nothing to run, and for a task being edited', async () => {
    const { el } = await mount()
    expect($(el, 'task-completion')).toBeNull()
    app.unmount()
    host.remove()
    const opts = useTaskOptions()
    opts.events.value = [{ id: 'e1', name: 'shipped' }]
    const edited = await mount({ isEditMode: true }, opts)
    expect($(edited.el, 'task-completion')).toBeNull()
  })

  it('offers a workflow with no start event as disabled, and picks one kind at a time', async () => {
    const opts = useTaskOptions()
    opts.workflows.value = [{ id: 'w1', name: 'release', startEventId: 'e1' }, { id: 'w2', name: 'draft' }]
    const { el } = await mount({}, opts)
    await click($(el, 'task-completion'))
    expect($(el, 'task-event')).not.toBeNull()
    await click($(el, 'task-completion-workflow'))
    const options = [...$(el, 'task-workflow').querySelectorAll('option')]
    expect(options.map((o) => [o.textContent.trim(), o.disabled])).toEqual([['None', false], ['release', false], ['draft (no start event)', true]])
    opts.selectedEventId.value = 'e1'
    await click($(el, 'task-completion-event'))
    expect(opts.selectedWorkflowId.value).toBe('')
    await click($(el, 'task-completion-workflow'))
    expect(opts.selectedEventId.value).toBe('')
    await click($(el, 'task-completion'))
    expect($(el, 'task-workflow')).toBeNull()
  })
})

describe('the files', () => {
  it('read what was chosen, and let the same file be chosen again', async () => {
    const { el, opts } = await mount()
    const input = $(el, 'task-file-input')
    Object.defineProperty(input, 'files', { value: [new File(['x'], 'x.txt', { type: 'text/plain' })], configurable: true })
    input.dispatchEvent(new Event('change'))
    await new Promise((r) => setTimeout(r, 30))
    expect(opts.attachments.value.map((a) => a.filename)).toEqual(['x.txt'])
    expect(input.value).toBe('')
    const open = vi.spyOn(input, 'click').mockImplementation(() => {})
    await click($(el, 'task-attach'))
    expect(open).toHaveBeenCalled()
  })
})

describe('the attachment chips', () => {
  it('draw nothing for no files, and name the one to take back', async () => {
    host = document.createElement('div')
    document.body.append(host)
    const removed = vi.fn()
    const files = ref([])
    app = createApp({ render: () => h(TaskAttachmentChips, { attachments: files.value, onRemove: removed }) })
    app.mount(host)
    expect(host.querySelector('[data-test="attachment-chip"]')).toBeNull()
    files.value = [{ filename: 'a' }, { filename: 'b' }]
    app.unmount()
    app = createApp({ render: () => h(TaskAttachmentChips, { attachments: files.value, onRemove: removed }) })
    app.mount(host)
    await click(host.querySelectorAll('[data-test="attachment-remove"]')[1])
    expect(removed).toHaveBeenCalledWith(1)
  })
})
