// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { downloadAttachment, isInlineImage, REVOKE_DELAY_MS } from '../src/composables/useAttachmentDownload'

const links = { source: '/api/v1/workspaces/ws1/tasks/t1/attachments/a1', fallback: 'https://agentrq.example/storage/a1' }

let clicked
let urls
let schedule

beforeEach(() => {
  vi.restoreAllMocks()
  clicked = []
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function () {
    clicked.push({ href: this.getAttribute('href'), download: this.download, target: this.target, rel: this.rel, attached: this.isConnected })
  })
  urls = { createObjectURL: vi.fn(() => 'blob:app/1'), revokeObjectURL: vi.fn() }
  schedule = vi.fn()
})

const ok = (body, type) => vi.fn(() => Promise.resolve({ ok: true, blob: () => Promise.resolve(new Blob([body], { type })) }))

describe('downloadAttachment', () => {
  it('saves the bytes under the filename, typed with the mimeType', async () => {
    const fetchImpl = ok('%PDF', 'application/octet-stream')
    const saved = await downloadAttachment({ id: 'a1', filename: 'report.pdf', mimeType: 'application/pdf' }, links, { fetchImpl, urls, schedule })

    expect(saved).toBe(true)
    expect(fetchImpl).toHaveBeenCalledWith(links.source)
    const blob = urls.createObjectURL.mock.calls[0][0]
    expect(blob.type).toBe('application/pdf')
    expect(await blob.text()).toBe('%PDF')
    expect(clicked).toEqual([{ href: 'blob:app/1', download: 'report.pdf', target: '', rel: '', attached: true }])
    expect(document.querySelector('a')).toBeNull()
  })

  it('revokes the object URL once the browser has had time to read it', async () => {
    await downloadAttachment({ id: 'a1', filename: 'x.txt', mimeType: 'text/plain' }, links, { fetchImpl: ok('x', ''), urls, schedule })
    expect(schedule).toHaveBeenCalledWith(expect.any(Function), REVOKE_DELAY_MS)
    expect(urls.revokeObjectURL).not.toHaveBeenCalled()
    schedule.mock.calls[0][0]()
    expect(urls.revokeObjectURL).toHaveBeenCalledWith('blob:app/1')
  })

  it('keeps the served type when the attachment names none, and octet-stream when neither does', async () => {
    await downloadAttachment({ id: 'a1', filename: 'a.png' }, links, { fetchImpl: ok('x', 'image/png'), urls, schedule })
    await downloadAttachment({ id: 'a1', filename: 'a.bin' }, links, { fetchImpl: ok('x', ''), urls, schedule })
    expect(urls.createObjectURL.mock.calls.map(([b]) => b.type)).toEqual(['image/png', 'application/octet-stream'])
  })

  it('names the file by its id when it has no filename', async () => {
    await downloadAttachment({ id: 'a1', mimeType: 'text/plain' }, links, { fetchImpl: ok('x', ''), urls, schedule })
    expect(clicked[0].download).toBe('a1')
  })

  it('follows the public link in a new tab when the read is refused', async () => {
    const fetchImpl = vi.fn(() => Promise.resolve({ ok: false, status: 401 }))
    const saved = await downloadAttachment({ id: 'a1', filename: 'r.pdf', mimeType: 'application/pdf' }, links, { fetchImpl, urls, schedule })

    expect(saved).toBe(false)
    expect(urls.createObjectURL).not.toHaveBeenCalled()
    expect(schedule).not.toHaveBeenCalled()
    expect(clicked).toEqual([{ href: links.fallback, download: 'r.pdf', target: '_blank', rel: 'noopener noreferrer', attached: true }])
  })

  it('follows the signed-in route in place when that is all there is and the read fails', async () => {
    const fetchImpl = vi.fn(() => Promise.reject(new Error('offline')))
    await downloadAttachment({ id: 'a1', filename: 'r.pdf' }, { source: links.source, fallback: links.source }, { fetchImpl, urls, schedule })
    expect(clicked).toEqual([{ href: links.source, download: 'r.pdf', target: '', rel: '', attached: true }])
  })

  it('uses the real fetch, document and timers by default', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(() => Promise.resolve({ ok: false, status: 500 }))
    await downloadAttachment({ id: 'a1', filename: 'r.pdf' }, links)
    expect(fetchSpy).toHaveBeenCalledWith(links.source)
    expect(clicked[0].href).toBe(links.fallback)
    fetchSpy.mockRestore()
  })
})

describe('isInlineImage', () => {
  it('is true only for the image types the server sends as images', () => {
    for (const mimeType of ['image/png', 'IMAGE/JPEG', 'image/gif', 'image/webp', 'image/bmp']) expect(isInlineImage({ mimeType })).toBe(true)
    for (const mimeType of ['image/svg+xml', 'text/html', '', undefined]) expect(isInlineImage({ mimeType })).toBe(false)
    expect(isInlineImage(null)).toBe(false)
  })
})
