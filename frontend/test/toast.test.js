// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * A notification you can't act on is a dead end: the person has to go find
 * the task themselves. This is the other half of the fix in
 * useStreamToasts.toastFor — that decides *whether* a toast links to a task,
 * this is where clicking one actually goes there.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h } from 'vue'

const push = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ push }),
}))

const { default: Toast } = await import('../src/components/Toast.vue')
const { useToasts } = await import('../src/composables/useToasts')

// Unmounted after each test: a toast's own timer can fire after the file's
// DOM is torn down, and a still-mounted list re-rendering then throws.
const mounted = []

async function mount() {
  const el = document.createElement('div')
  document.body.appendChild(el)
  const app = createApp({ render: () => h(Toast) })
  app.mount(el)
  mounted.push(app)
  await new Promise((resolve) => setTimeout(resolve, 0))
  return el
}

describe('Toast', () => {
  afterEach(() => {
    while (mounted.length) mounted.pop().unmount()
    useToasts().toasts.value = []
  })

  beforeEach(() => {
    push.mockClear()
    // The toast list is module-level state shared with useStreamToasts'
    // caller — clear it so one test's toast doesn't linger into the next.
    const { toasts } = useToasts()
    toasts.value = []
  })

  it('is not clickable with nowhere to link to', async () => {
    useToasts().notifyInfo('New reply on "Ship it"')
    const el = await mount()

    const toast = el.querySelector('.toast')
    expect(toast.classList.contains('clickable')).toBe(false)

    toast.click()
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(push).not.toHaveBeenCalled()
  })

  it('navigates to the task on click, and dismisses itself', async () => {
    useToasts().notifyInfo('New reply on "Ship it"', 'Notice', { taskId: 't1', workspaceId: 'w1' })
    const el = await mount()

    const toast = el.querySelector('.toast')
    expect(toast.classList.contains('clickable')).toBe(true)

    toast.click()
    await new Promise((resolve) => setTimeout(resolve, 0))

    expect(push).toHaveBeenCalledWith('/workspaces/w1/tasks/t1')
    expect(useToasts().toasts.value).toHaveLength(0)
  })

  it('stays put when only the workspace is known', async () => {
    useToasts().notifyInfo('Agent could not start', 'Error', { taskId: '', workspaceId: 'w1' })
    const el = await mount()

    expect(el.querySelector('.toast').classList.contains('clickable')).toBe(false)
  })

  it('dismisses without navigating when the close button is used', async () => {
    useToasts().notifyInfo('New reply on "Ship it"', 'Notice', { taskId: 't1', workspaceId: 'w1' })
    const el = await mount()

    el.querySelector('.toast-close').click()
    await new Promise((resolve) => setTimeout(resolve, 0))

    expect(push).not.toHaveBeenCalled()
    expect(useToasts().toasts.value).toHaveLength(0)
  })
  it('offers "View task" only when the toast links to one', async () => {
    const { addToast } = useToasts()
    addToast('no link', 'info', 'A')
    addToast('linked', 'info', 'B', 4000, { taskId: 't1', workspaceId: 'w1' })
    const el = await mount()
    const links = [...el.querySelectorAll('.toast')].map((t) => t.querySelector('.toast-link')?.textContent ?? null)
    expect(links).toEqual([null, 'View task'])
  })

  it('navigates to a path, under its own label, when the link is one', async () => {
    useToasts().addToast('Browser agent ran: restart daemon', 'info', null, 0, { path: '/machines/m1', label: 'Show' })
    const el = await mount()

    expect(el.querySelector('.toast-link').textContent).toBe('Show')
    el.querySelector('.toast').click()
    await new Promise((resolve) => setTimeout(resolve, 0))

    expect(push).toHaveBeenCalledWith('/machines/m1')
    expect(useToasts().toasts.value).toHaveLength(0)
  })

  it('keeps the whole message on hover, since only two lines show', async () => {
    const long = 'word '.repeat(80).trim()
    useToasts().addToast(long, 'error', 'Error')
    const el = await mount()
    expect(el.querySelector('.toast-message').getAttribute('title')).toBe(long)
  })

  it('counts down for as long as the toast stays', async () => {
    const { addToast } = useToasts()
    addToast('short', 'success', 'Saved', 2500)
    addToast('sticky', 'info', 'Wait', 0)
    const el = await mount()
    const [short, sticky] = el.querySelectorAll('.toast')
    expect(short.querySelector('.toast-progress').style.animationDuration).toBe('2500ms')
    expect(sticky.querySelector('.toast-progress')).toBeNull()
  })
  it('shows no title unless given one, since the icon names the type', async () => {
    const { notifyError, notifySuccess, notifyInfo } = useToasts()
    notifyError('Could not save')
    notifySuccess('Saved')
    notifyInfo('Heads up')
    notifyInfo('Agent is waiting', 'Action Needed')
    const el = await mount()
    const titles = [...el.querySelectorAll('.toast')].map((t) => t.querySelector('.toast-title')?.textContent ?? null)
    expect(titles).toEqual([null, null, null, 'Action Needed'])
    expect(el.querySelectorAll('.toast-message.solo')).toHaveLength(3)
  })
  it('keeps an error up for 20 seconds and anything else for 4', async () => {
    vi.useFakeTimers()
    try {
      const { notifyError, notifySuccess, addToast, toasts } = useToasts()
      notifyError('Could not move the task')
      addToast('Model not available', 'error')
      notifySuccess('Saved')
      expect(toasts.value.map((t) => t.duration)).toEqual([20000, 20000, 4000])

      vi.advanceTimersByTime(4000)
      expect(toasts.value.map((t) => t.message)).toEqual(['Could not move the task', 'Model not available'])
      vi.advanceTimersByTime(16000)
      expect(toasts.value).toHaveLength(0)
    } finally {
      vi.useRealTimers()
    }
  })

  it('opens the task from anywhere on the card, not only "View task"', async () => {
    useToasts().notifyError('Agent crashed', 'Agent could not start', { taskId: 't9', workspaceId: 'w2' })
    const el = await mount()
    el.querySelector('.toast-message').click()
    expect(push).toHaveBeenCalledWith('/workspaces/w2/tasks/t9')
  })

  // The homepage's task row: workspace above, status icon, title, and the
  // move from one status to the other.
  it('shows a status change as a task row', async () => {
    useToasts().notifyEvent({
      tone: 'error', kind: 'status', title: 'Approve DB migration script', message: 'The agent needs your input.',
      from: 'ongoing', to: 'blocked', taskId: 't1', workspaceId: 'w1',
    }, 'Backend Agent v2')
    const el = await mount()
    const toast = el.querySelector('.toast')

    expect(toast.classList.contains('card')).toBe(true)
    expect(toast.classList.contains('clickable')).toBe(true)
    expect(toast.querySelector('.toast-eyebrow').textContent).toBe('Backend Agent v2')
    expect(toast.querySelector('.toast-title').textContent).toBe('Approve DB migration script')
    expect(toast.querySelector('.toast-dot').className).toContain('alert')
    const pills = [...toast.querySelectorAll('.toast-pill')]
    expect(pills.map((p) => p.textContent)).toEqual(['Ongoing', 'Blocked'])
    expect(pills.map((p) => p.classList.contains('was'))).toEqual([true, false])
    expect(toast.querySelector('.toast-arrow')).not.toBeNull()
  })

  it('shows one pill, and a stand-in eyebrow, for the rest of the stream', async () => {
    const { notifyEvent } = useToasts()
    notifyEvent({ tone: 'info', kind: 'reply', title: 'Ship it', message: 'Done.', taskId: 't1', workspaceId: 'w1' })
    notifyEvent({ tone: 'success', kind: 'connected', title: 'Agent connected', message: 'Ready.', taskId: '', workspaceId: 'w1' }, 'Ops')
    notifyEvent({ tone: 'info', kind: 'status', title: 'Ship it', message: 'Now.', to: 'completed', taskId: '', workspaceId: '' })
    notifyEvent({ tone: 'info', kind: 'status', title: 'Odd', message: 'Now.', from: 'x', to: 'y', taskId: '', workspaceId: '' })
    const el = await mount()
    const [reply, live, done, odd] = el.querySelectorAll('.toast')

    expect(reply.querySelector('.toast-eyebrow').textContent).toBe('AgentRQ')
    expect([...reply.querySelectorAll('.toast-pill')].map((p) => p.textContent)).toEqual(['Reply'])
    expect(live.querySelector('.toast-dot').className).toContain('live')
    expect(live.classList.contains('clickable')).toBe(false)
    expect([...done.querySelectorAll('.toast-pill')].map((p) => p.textContent)).toEqual(['Done'])
    expect(done.querySelector('.toast-dot').textContent).toBe('✓')
    expect([...odd.querySelectorAll('.toast-pill')].map((p) => p.textContent)).toEqual(['x', 'y'])
    expect(odd.querySelector('.toast-dot').className).toContain('info')
  })

  it('shows a notice with its pill, and a warning with its own dot', async () => {
    const { notifyEvent } = useToasts()
    notifyEvent({ tone: 'warning', kind: 'notice', title: 'Ship it', message: 'MCP server unavailable', taskId: 't1', workspaceId: 'w1' })
    notifyEvent({ tone: 'error', kind: 'notice', title: 'Ship it', message: 'Quota exceeded', taskId: 't1', workspaceId: 'w1' })
    const el = await mount()
    const [warning, error] = el.querySelectorAll('.toast')

    expect([...warning.querySelectorAll('.toast-pill')].map((p) => p.textContent)).toEqual(['Notice'])
    expect(warning.querySelector('.toast-dot').className).toContain('warn')
    expect(warning.querySelector('.toast-dot').textContent).toBe('!')
    expect(error.querySelector('.toast-dot').className).toContain('alert')
  })

  it('shows no pill for news it has no pill for', async () => {
    useToasts().notifyEvent({ tone: 'info', kind: 'mystery', title: 'Odd', message: 'Now.', taskId: '', workspaceId: '' })
    const el = await mount()
    expect(el.querySelectorAll('.toast-pill')).toHaveLength(0)
    expect(el.querySelector('.toast-dot').className).toContain('info')
  })

  it('keeps a plain toast plain', async () => {
    useToasts().notifySuccess('Saved')
    const el = await mount()
    const toast = el.querySelector('.toast')
    expect(toast.classList.contains('card')).toBe(false)
    expect(toast.querySelector('.toast-eyebrow')).toBeNull()
    expect(toast.querySelector('.toast-line')).toBeNull()
    expect(toast.querySelector('.toast-dot').textContent).toBe('✓')
  })

  it('gives a stream toast six seconds, and an error still twenty', () => {
    const { notifyEvent, toasts } = useToasts()
    notifyEvent({ tone: 'info', kind: 'reply', title: 'A', message: 'm', taskId: '', workspaceId: '' })
    notifyEvent({ tone: 'error', kind: 'permission', title: 'B', message: 'm', taskId: '', workspaceId: '' })
    expect(toasts.value.map((t) => t.duration)).toEqual([6000, 20000])
  })

  // A task with no title still has a message, which then leads.
  it('leads with the message when the task has no title', () => {
    const { notifyEvent, toasts } = useToasts()
    notifyEvent({ tone: 'info', kind: 'status', title: '', message: 'Now.', to: 'ongoing', taskId: '', workspaceId: '' })
    expect(toasts.value[0].title).toBeNull()
  })

  // A burst of status changes must not bury the page.
  it('shows four at most, dropping the oldest', () => {
    const { notifyInfo, toasts } = useToasts()
    for (const n of [1, 2, 3, 4, 5, 6]) notifyInfo(`n${n}`)
    expect(toasts.value.map((t) => t.message)).toEqual(['n3', 'n4', 'n5', 'n6'])
  })
})
