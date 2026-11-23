// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi, afterEach } from 'vitest'
import { createApp, h, ref, reactive, nextTick } from 'vue'
import ChatMinimap from '../src/components/ChatMinimap.vue'
import { minimapEntries, minimapWindow, activeMessageId, useChatMinimap, useAutoscrollPause, resetAutoscrollPauses } from '../src/composables/useChatMinimap'

const mounted = []
afterEach(() => {
  while (mounted.length) mounted.pop().unmount()
  document.body.innerHTML = ''
})

// A scroll pane whose marked messages sit at the given tops (relative to the
// pane, which itself starts at y=100), with the pane scrolled by `scrollTop`.
function pane(tops, { scrollTop = 0, clientHeight = 300, scrollHeight = 2000 } = {}) {
  const el = document.createElement('div')
  el.getBoundingClientRect = () => ({ top: 100 })
  Object.defineProperty(el, 'clientHeight', { value: clientHeight })
  Object.defineProperty(el, 'scrollHeight', { value: scrollHeight })
  el.scrollTop = scrollTop
  tops.forEach((t, i) => {
    const m = document.createElement('div')
    m.dataset.minimapId = `m${i}`
    m.getBoundingClientRect = () => ({ top: 100 + t - el.scrollTop })
    m.scrollIntoView = vi.fn()
    el.appendChild(m)
  })
  document.body.appendChild(el)
  return el
}

describe('minimapEntries', () => {
  it('keeps only messages the person sent, with a one-line preview', () => {
    const entries = minimapEntries([
      { id: 1, sender: 'human', text: '  first\n\n  line  ' },
      { id: 2, sender: 'agent', text: 'reply' },
      { id: 3, sender: 'human' },
      { id: 4, sender: 'human', text: 'counting down', _pending: true },
      { id: 5, sender: 'human', text: 'queued', _queued: true },
      { id: 6, sender: 'human', text: 'x'.repeat(400) },
    ])
    expect(entries.map((e) => e.id)).toEqual(['1', '3', '6'])
    expect(entries.map((e) => e.n)).toEqual([1, 2, 3])
    expect(entries[0].preview).toBe('first line')
    expect(entries[1].preview).toBe('')
    expect(entries[2].preview).toHaveLength(280)
  })

  it('takes a missing thread as empty', () => {
    expect(minimapEntries(undefined)).toEqual([])
  })
})

describe('minimapWindow', () => {
  const list = (k) => Array.from({ length: k }, (_, i) => ({ id: `m${i}` }))
  const ids = (w) => w.map((e) => e.id)

  it('is every message while they fit', () => {
    expect(ids(minimapWindow(list(3), 'm0'))).toEqual(['m0', 'm1', 'm2'])
  })

  it('is the newest ones with nothing read yet', () => {
    expect(ids(minimapWindow(list(20), null, 1))).toEqual(['m17', 'm18', 'm19'])
  })

  it('reaches either side of the message being read', () => {
    expect(ids(minimapWindow(list(20), 'm10', 1))).toEqual(['m9', 'm10', 'm11'])
  })

  it('keeps its size near either end', () => {
    expect(ids(minimapWindow(list(20), 'm0', 1))).toEqual(['m0', 'm1', 'm2'])
    expect(ids(minimapWindow(list(20), 'm19', 1))).toEqual(['m17', 'm18', 'm19'])
  })

  it('reaches six each way by default', () => {
    const w = ids(minimapWindow(list(30), 'm15'))
    expect(w).toHaveLength(13)
    expect(w[0]).toBe('m9')
    expect(w[12]).toBe('m21')
  })
})

describe('activeMessageId', () => {
  it('is null with no pane or no marked messages', () => {
    expect(activeMessageId(null)).toBeNull()
    expect(activeMessageId(pane([]))).toBeNull()
  })

  it('is the last message whose top passed the upper third of the pane', () => {
    const el = pane([0, 500, 1000])
    expect(activeMessageId(el)).toBe('m0')
    el.scrollTop = 450 // m1 at 50, under the 100px line
    expect(activeMessageId(el)).toBe('m1')
    el.scrollTop = 950
    expect(activeMessageId(el)).toBe('m2')
  })

  it('is the first message while none has reached the line yet', () => {
    expect(activeMessageId(pane([200, 600]))).toBe('m0')
  })

  it('is the last message once the pane is at its bottom', () => {
    expect(activeMessageId(pane([0, 500, 1000], { scrollTop: 1700 }))).toBe('m2')
  })
})

describe('useChatMinimap', () => {
  function setup(el) {
    const container = ref(el)
    const entries = ref([{ id: 'm0' }, { id: 'm1' }])
    const jobs = []
    let api
    const app = createApp({
      setup() {
        api = useChatMinimap(container, entries, (fn) => jobs.push(fn))
        return () => h('div')
      },
    })
    app.mount(document.createElement('div'))
    mounted.push(app)
    const flush = () => { while (jobs.length) jobs.shift()() }
    return { api, container, entries, jobs, flush, app }
  }

  it('tracks the message in view as the pane scrolls, once per frame', async () => {
    const el = pane([0, 500])
    const { api, jobs, flush } = setup(el)
    flush()
    expect(api.activeId.value).toBe('m0')
    el.scrollTop = 450
    el.dispatchEvent(new Event('scroll'))
    el.dispatchEvent(new Event('scroll'))
    expect(jobs).toHaveLength(1)
    flush()
    expect(api.activeId.value).toBe('m1')
  })

  it('follows the pane when it is replaced, and leaves the old one', async () => {
    const first = pane([0, 500])
    const { api, container, flush } = setup(first)
    flush()
    const second = pane([0, 500], { scrollTop: 450 })
    container.value = second
    await nextTick()
    flush()
    expect(api.activeId.value).toBe('m1')
    first.scrollTop = 0
    first.dispatchEvent(new Event('scroll'))
    flush()
    expect(api.activeId.value).toBe('m1')
    container.value = null
    await nextTick()
    flush()
    expect(api.activeId.value).toBeNull()
  })

  it('looks again when the messages change', async () => {
    const el = pane([0, 500], { scrollTop: 450 })
    const { api, entries, jobs, flush } = setup(el)
    flush()
    entries.value = [...entries.value, { id: 'm2' }]
    await nextTick()
    expect(jobs).toHaveLength(1)
    flush()
    expect(api.activeId.value).toBe('m1')
  })

  it('stops listening once unmounted', () => {
    const el = pane([0])
    const spy = vi.spyOn(el, 'removeEventListener')
    const { app, flush } = setup(el)
    flush()
    mounted.pop()
    app.unmount()
    expect(spy).toHaveBeenCalledWith('scroll', expect.any(Function))
  })

  it('shows only from three messages, and only while the chat scrolls', async () => {
    const el = pane([0, 500, 1000])
    const { api, entries, flush } = setup(el)
    flush()
    expect(api.shown.value).toBe(false) // two messages
    entries.value = [...entries.value, { id: 'm2' }]
    await nextTick()
    flush()
    expect(api.shown.value).toBe(true)
    const fits = pane([0, 100, 200], { scrollHeight: 300 })
    const short = setup(fits)
    short.entries.value = [{ id: 'm0' }, { id: 'm1' }, { id: 'm2' }]
    await nextTick()
    short.flush()
    expect(short.api.shown.value).toBe(false)
  })

  it('looks again when the pane is resized, and follows it to a new pane', async () => {
    const observers = []
    const RO = class {
      constructor(cb) { this.cb = cb; this.observed = new Set(); this.disconnect = vi.fn(); observers.push(this) }
      observe(el) { this.observed.add(el) }
      unobserve(el) { this.observed.delete(el) }
    }
    vi.stubGlobal('ResizeObserver', RO)
    const first = pane([0, 500])
    const { api, container, jobs, flush, app } = setup(first)
    flush()
    const [ro] = observers
    expect(ro.observed.has(first)).toBe(true)
    ro.cb()
    expect(jobs).toHaveLength(1)
    flush()
    const second = pane([0, 500])
    container.value = second
    await nextTick()
    expect(ro.observed.has(first)).toBe(false)
    expect(ro.observed.has(second)).toBe(true)
    container.value = null
    await nextTick()
    expect(ro.observed.size).toBe(0)
    flush()
    expect(api.shown.value).toBe(false)
    mounted.splice(mounted.indexOf(app), 1)
    app.unmount()
    expect(ro.disconnect).toHaveBeenCalled()
    vi.unstubAllGlobals()
  })

  it('scrolls to a message and marks it at once', () => {
    const el = pane([0, 500])
    const { api, flush } = setup(el)
    flush()
    api.jumpTo('m1')
    expect(el.children[1].scrollIntoView).toHaveBeenCalledWith({ behavior: 'smooth', block: 'start' })
    expect(api.activeId.value).toBe('m1')
    api.jumpTo('gone')
    expect(api.activeId.value).toBe('m1')
  })

  it('does nothing on a jump with no pane', () => {
    const { api, container } = setup(null)
    container.value = null
    expect(() => api.jumpTo('m0')).not.toThrow()
  })

  it('defaults to scheduling on the next animation frame', () => {
    const raf = vi.spyOn(window, 'requestAnimationFrame').mockImplementation(() => 0)
    const app = createApp({ setup() { useChatMinimap(ref(pane([0])), ref([])); return () => h('div') } })
    app.mount(document.createElement('div'))
    mounted.push(app)
    expect(raf).toHaveBeenCalled()
    raf.mockRestore()
  })
})

describe('useAutoscrollPause', () => {
  afterEach(() => resetAutoscrollPauses())

  it('pauses and resumes one task, remembering it across uses, and reports each pause', () => {
    const taskId = ref('t1')
    const onPause = vi.fn()
    const { paused, toggle } = useAutoscrollPause(taskId, onPause)
    expect(paused.value).toBe(false)
    toggle()
    expect(paused.value).toBe(true)
    expect(onPause).toHaveBeenCalledTimes(1)
    // Another task still follows its chat; coming back finds the pause.
    taskId.value = 't2'
    expect(paused.value).toBe(false)
    taskId.value = 't1'
    expect(useAutoscrollPause('t1').paused.value).toBe(true)
    toggle()
    expect(paused.value).toBe(false)
    expect(onPause).toHaveBeenCalledTimes(1)
  })

  it('pauses without toggling, reporting only a pause that begins', () => {
    const onPause = vi.fn()
    const { paused, pause } = useAutoscrollPause('t3', onPause)
    pause()
    pause()
    expect(paused.value).toBe(true)
    expect(onPause).toHaveBeenCalledTimes(1)
  })

  it('reports nothing by default', () => {
    const { paused, toggle } = useAutoscrollPause(() => 't9')
    toggle()
    expect(paused.value).toBe(true)
  })
})

describe('ChatMinimap', () => {
  function mount(entries, activeId = null) {
    const state = reactive({ entries, activeId, jumps: [], paused: false, toggles: 0 })
    const el = document.createElement('div')
    document.body.appendChild(el)
    const app = createApp({
      render: () => h(ChatMinimap, {
        entries: state.entries, activeId: state.activeId, paused: state.paused,
        onJump: (id) => state.jumps.push(id), onToggleAutoscroll: () => { state.toggles++ },
      }),
    })
    app.mount(el)
    mounted.push(app)
    return { el, state }
  }
  const dots = (el) => [...el.querySelectorAll('[data-minimap-dot]')]
  const preview = (el) => el.querySelector('[data-testid="chat-minimap-preview"]')

  it('draws a dot per message and marks the one in view', async () => {
    const { el, state } = mount([{ id: 'a', n: 4, preview: 'one' }, { id: 'b', n: 5, preview: 'two' }], 'b')
    expect(dots(el)).toHaveLength(2)
    expect(dots(el)[0].getAttribute('aria-current')).toBeNull()
    expect(dots(el)[1].getAttribute('aria-current')).toBe('true')
    expect(dots(el)[0].getAttribute('aria-label')).toBe('Message 4: one')
    state.activeId = 'a'
    await nextTick()
    expect(dots(el)[0].getAttribute('aria-current')).toBe('true')
  })

  it('jumps to a message on click', () => {
    const { el, state } = mount([{ id: 'a', preview: 'one' }, { id: 'b', preview: 'two' }])
    dots(el)[1].click()
    expect(state.jumps).toEqual(['b'])
  })

  it('previews a message beside its dot on hover and focus', async () => {
    const { el } = mount([{ id: 'a', preview: 'one' }, { id: 'b', preview: '' }])
    const nav = el.querySelector('nav')
    nav.getBoundingClientRect = () => ({ top: 50 })
    dots(el)[0].getBoundingClientRect = () => ({ top: 80, height: 16 })
    dots(el)[0].dispatchEvent(new Event('mouseenter'))
    await nextTick()
    expect(preview(el).textContent).toBe('one')
    expect(preview(el).style.top).toBe('38px')
    nav.dispatchEvent(new Event('mouseleave'))
    await nextTick()
    expect(preview(el)).toBeNull()
    dots(el)[1].getBoundingClientRect = () => ({ top: 96, height: 16 })
    dots(el)[1].dispatchEvent(new Event('focus'))
    await nextTick()
    expect(preview(el).textContent).toBe('(attachment)')
    dots(el)[1].dispatchEvent(new Event('blur'))
    await nextTick()
    expect(preview(el)).toBeNull()
  })

  it('offers to pause autoscroll, and shows when it is paused', async () => {
    const { el, state } = mount([{ id: 'a', preview: 'one' }])
    const button = el.querySelector('[data-testid="chat-minimap-autoscroll"]')
    expect(button.getAttribute('aria-pressed')).toBe('false')
    expect(button.getAttribute('aria-label')).toBe('Pause autoscroll')
    button.click()
    expect(state.toggles).toBe(1)
    state.paused = true
    await nextTick()
    expect(button.getAttribute('aria-pressed')).toBe('true')
    expect(button.title).toBe('Resume autoscroll')
  })

  it('only jumps on a tap: the mouse and focus events a tap brings open no preview', async () => {
    const now = vi.spyOn(Date, 'now').mockReturnValue(10_000)
    const { el, state } = mount([{ id: 'a', preview: 'one' }])
    el.querySelector('nav').getBoundingClientRect = () => ({ top: 0 })
    dots(el)[0].getBoundingClientRect = () => ({ top: 20, height: 24 })
    dots(el)[0].dispatchEvent(new Event('touchstart'))
    dots(el)[0].dispatchEvent(new Event('mouseenter'))
    dots(el)[0].dispatchEvent(new Event('focus'))
    dots(el)[0].click()
    await nextTick()
    expect(preview(el)).toBeNull()
    expect(state.jumps).toEqual(['a'])
    // A mouse a moment later previews again.
    now.mockReturnValue(11_001)
    dots(el)[0].dispatchEvent(new Event('mouseenter'))
    await nextTick()
    expect(preview(el).textContent).toBe('one')
    now.mockRestore()
  })
})

