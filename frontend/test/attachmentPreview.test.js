// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The attachment preview the task page and the scheduled task page share:
 * what it draws for each kind of file, and its Download, Copy link and close.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h, ref, nextTick } from 'vue'

vi.mock('vue-router', () => ({
  useRoute: () => ({ path: '/workspaces/ws1/tasks/t1/instances', params: { id: 'ws1', taskId: 't1' } }),
}))

const recordTelemetry = vi.fn()
vi.mock('../src/api', async (importOriginal) => ({
  ...await importOriginal(),
  recordTelemetry: (...a) => recordTelemetry(...a),
}))

const downloadAttachment = vi.fn(() => Promise.resolve(true))
vi.mock('../src/composables/useAttachmentDownload', async (importOriginal) => ({
  ...await importOriginal(),
  downloadAttachment: (...a) => downloadAttachment(...a),
}))

const { default: AttachmentPreview } = await import('../src/components/AttachmentPreview.vue')
const { useToasts } = await import('../src/composables/useToasts')

const SRC = '/api/v1/workspaces/ws1/tasks/t1/attachments/a1'
const LINK = 'https://agentrq.example/storage/a1'
const file = (mimeType, extra = {}) => ({ id: 'a1', filename: 'f.bin', mimeType, ...extra })

let app
let el
let closed
const attachment = ref(null)
const publicLink = ref('')

function mount(att, link = LINK) {
  attachment.value = att
  publicLink.value = link
  closed = 0
  el = document.createElement('div')
  document.body.appendChild(el)
  app = createApp({
    render: () => h(AttachmentPreview, {
      attachment: attachment.value, src: SRC, publicLink: publicLink.value,
      onClose: () => { closed++; attachment.value = null },
    }),
  })
  app.mount(el)
}

const button = (label) => el.querySelector(`[aria-label="${label}"]`)
const escape = () => {
  const e = new KeyboardEvent('keydown', { key: 'Escape', cancelable: true })
  window.dispatchEvent(e)
  return e
}

beforeEach(() => {
  downloadAttachment.mockClear()
  recordTelemetry.mockClear()
  useToasts().toasts.value.splice(0)
})

afterEach(() => {
  vi.useRealTimers()
  app?.unmount()
  el?.remove()
  delete window.agentrq
})

describe('what it draws', () => {
  it('draws nothing without an attachment, and leaves Escape to others', () => {
    mount(null)
    expect(el.innerHTML).toBe('<!--v-if-->')
    expect(escape().defaultPrevented).toBe(false)
    expect(closed).toBe(0)
  })

  it('shows an image from the signed-in route', () => {
    mount(file('image/png'))
    expect(el.querySelector('img').getAttribute('src')).toBe(SRC)
  })

  it.each([
    ['video/mp4', 'video'],
    ['audio/mpeg', 'audio'],
    ['application/pdf', 'iframe'],
  ])('plays a %s in a <%s>', (mime, tag) => {
    mount(file(mime))
    expect(el.querySelector(tag).getAttribute('src')).toBe(SRC)
  })

  it('names any other file, with its type', () => {
    mount(file('text/csv', { filename: 'owners.csv' }))
    expect(el.querySelector('p.break-all').textContent).toBe('owners.csv')
    expect(el.textContent).toContain('text/csv')
  })

  it('offers Open in browser and Copy link only for a public link', () => {
    mount(file('text/csv'), '')
    expect(button('Download').getAttribute('href')).toBe(SRC)
    expect(button('Download').hasAttribute('target')).toBe(false)
    expect(button('Open in browser')).toBeNull()
    expect(button('Copy link')).toBeNull()
  })

  it('opens the public link in a new tab', () => {
    mount(file('text/csv'))
    expect(button('Download').getAttribute('href')).toBe(LINK)
    expect(button('Download').getAttribute('target')).toBe('_blank')
    expect(button('Open in browser').getAttribute('href')).toBe(LINK)
  })
})

describe('Download', () => {
  it('saves from the signed-in route, falling back to the public link', () => {
    const att = file('text/csv')
    mount(att)
    button('Download').click()
    expect(downloadAttachment).toHaveBeenCalledWith(att, { source: SRC, fallback: LINK })
  })

  it('falls back to the signed-in route when there is no public link', () => {
    const att = file('text/csv')
    mount(att, '')
    button('Download').click()
    expect(downloadAttachment).toHaveBeenCalledWith(att, { source: SRC, fallback: SRC })
  })
})

describe('Copy link', () => {
  it('copies the public link, counts it, and says Copied for a moment', async () => {
    vi.useFakeTimers()
    const writeText = vi.fn(() => Promise.resolve())
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    mount(file('text/csv'))
    button('Copy link').click()
    await vi.advanceTimersByTimeAsync(0)
    expect(writeText).toHaveBeenCalledWith(LINK)
    expect(recordTelemetry).toHaveBeenCalledWith('ui_copy_link', 'ws1')
    expect(button('Copied')).not.toBeNull()
    await vi.advanceTimersByTimeAsync(1500)
    expect(button('Copy link')).not.toBeNull()
  })

  it('uses the desktop shell clipboard when there is one', async () => {
    const write = vi.fn(() => Promise.resolve())
    window.agentrq = { clipboard: { write } }
    mount(file('text/csv'))
    button('Copy link').click()
    await nextTick(); await nextTick()
    expect(write).toHaveBeenCalledWith(LINK)
  })

  it('keeps the tick on a newer copy when an older one expires', async () => {
    vi.useFakeTimers()
    Object.defineProperty(navigator, 'clipboard', { value: { writeText: () => Promise.resolve() }, configurable: true })
    mount(file('text/csv'))
    button('Copy link').click()
    await vi.advanceTimersByTimeAsync(1000)
    attachment.value = file('text/csv', { id: 'a2' })
    await nextTick()
    button('Copy link').click()
    await vi.advanceTimersByTimeAsync(600)
    expect(button('Copied')).not.toBeNull()
  })

  it('says so when the clipboard refuses, and counts nothing', async () => {
    Object.defineProperty(navigator, 'clipboard', { value: { writeText: () => Promise.reject(new Error('no focus')) }, configurable: true })
    mount(file('text/csv'))
    button('Copy link').click()
    await new Promise((r) => setTimeout(r, 0))
    expect(useToasts().toasts.value.at(-1).message).toBe('Could not copy to the clipboard.')
    expect(recordTelemetry).not.toHaveBeenCalled()
    expect(button('Copy link')).not.toBeNull()
  })
})

describe('closing', () => {
  it('closes on Escape, taking the key', async () => {
    mount(file('text/csv'))
    expect(escape().defaultPrevented).toBe(true)
    expect(closed).toBe(1)
  })

  it('closes from the backdrop and the close button', async () => {
    mount(file('text/csv'))
    el.querySelector('.backdrop-blur-sm').click()
    expect(closed).toBe(1)
    attachment.value = file('text/csv')
    await nextTick()
    el.querySelector('button.rounded-full').click()
    expect(closed).toBe(2)
  })
})
