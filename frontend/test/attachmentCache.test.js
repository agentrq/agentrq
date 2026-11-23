// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi } from 'vitest'

import {
  DESKTOP_BUDGET_BYTES,
  MAX_ATTACHMENT_BYTES,
  WEB_BUDGET_BYTES,
  attachmentUrlsForTask,
  attachmentUrlsForWorkspace,
  budgetFor,
  evictionPlan,
  isAttachmentRequest,
  isCacheableApiRead,
  isEventStream,
  isTaskRead,
  touchCacheEntry,
  withinSizeCap,
} from '../src/composables/useAttachmentCache'

const attachment = '/api/v1/workspaces/ws1/tasks/0iCYTqxKOqv/attachments/a1'

describe('isAttachmentRequest', () => {
  it('recognises an attachment', () => {
    expect(isAttachmentRequest(attachment)).toBe(true)
  })

  it('does not mistake a task for one', () => {
    expect(isAttachmentRequest('/api/v1/workspaces/ws1/tasks/0iCYTqxKOqv')).toBe(false)
    expect(isAttachmentRequest('/api/v1/workspaces/ws1/tasks')).toBe(false)
    expect(isAttachmentRequest('')).toBe(false)
    expect(isAttachmentRequest(null)).toBe(false)
  })
})

describe('isTaskRead', () => {
  it('recognises the reads the local database now owns', () => {
    expect(isTaskRead('/api/v1/tasks')).toBe(true)
    expect(isTaskRead('/api/v1/workspaces/ws1/tasks')).toBe(true)
    expect(isTaskRead('/api/v1/workspaces/ws1/tasks/0iCYTqxKOqv')).toBe(true)
  })

  it('leaves alone the paths that only look like task reads', () => {
    // An attachment lives under /tasks/, and so do the action endpoints, which
    // are not reads at all.
    expect(isTaskRead(attachment)).toBe(false)
    expect(isTaskRead('/api/v1/workspaces/ws1/tasks/0iCYTqxKOqv/reply')).toBe(false)
    expect(isTaskRead('/api/v1/workspaces/ws1/tasks/0iCYTqxKOqv/status')).toBe(false)
    expect(isTaskRead('/api/v1/workspaces/ws1/tasks/counts')).toBe(true)
  })

  it('ignores anything outside the API', () => {
    expect(isTaskRead('/tasks/all')).toBe(false)
    expect(isTaskRead('')).toBe(false)
    expect(isTaskRead(null)).toBe(false)
  })
})

describe('isCacheableApiRead', () => {
  it('keeps what nothing else claims', () => {
    // These are what the shell needs to render at all.
    expect(isCacheableApiRead('/api/v1/workspaces')).toBe(true)
    expect(isCacheableApiRead('/api/v1/workspaces/ws1')).toBe(true)
    expect(isCacheableApiRead('/api/v1/auth/user')).toBe(true)
  })

  it('stops shadowing the task reads the database owns', () => {
    // Two caches with different lifetimes answering the same question disagree
    // in a way that looks like a bug in the database.
    expect(isCacheableApiRead('/api/v1/tasks')).toBe(false)
    expect(isCacheableApiRead('/api/v1/workspaces/ws1/tasks')).toBe(false)
    expect(isCacheableApiRead('/api/v1/workspaces/ws1/tasks/0iCYTqxKOqv')).toBe(false)
  })

  it('leaves attachments to their own cache', () => {
    expect(isCacheableApiRead(attachment)).toBe(false)
  })

  it('leaves the event streams to the network', () => {
    // An open stream held by the worker stops a new version activating.
    expect(isCacheableApiRead('/api/v1/events/stream')).toBe(false)
    expect(isCacheableApiRead('/api/v1/workspaces/ws1/events')).toBe(false)
  })

  it('ignores anything outside the API', () => {
    expect(isCacheableApiRead('/tasks/all')).toBe(false)
    expect(isCacheableApiRead('')).toBe(false)
    expect(isCacheableApiRead(null)).toBe(false)
    expect(isCacheableApiRead(undefined)).toBe(false)
  })
})

describe('isEventStream', () => {
  it('recognises both streams', () => {
    expect(isEventStream('/api/v1/events/stream')).toBe(true)
    expect(isEventStream('/base/api/v1/workspaces/ws1/events')).toBe(true)
  })

  it('does not mistake the events a workspace publishes for one', () => {
    expect(isEventStream('/api/v1/events')).toBe(false)
    expect(isEventStream('/api/v1/workspaces/ws1/events/e1')).toBe(false)
    expect(isEventStream('')).toBe(false)
    expect(isEventStream(null)).toBe(false)
  })
})

describe('budgetFor', () => {
  it('gives the desktop an order of magnitude more', () => {
    // It writes into a partition folder it owns, under no browser quota
    // pressure, unlike a web origin sharing a quota with everything else.
    expect(budgetFor('desktop')).toBe(DESKTOP_BUDGET_BYTES)
    expect(budgetFor('web')).toBe(WEB_BUDGET_BYTES)
    expect(budgetFor(undefined)).toBe(WEB_BUDGET_BYTES)
    expect(DESKTOP_BUDGET_BYTES).toBeGreaterThan(WEB_BUDGET_BYTES * 5)
  })
})

describe('withinSizeCap', () => {
  const withLength = (bytes) => ({ headers: { get: () => String(bytes) } })

  it('keeps a small file and refuses a large one', () => {
    expect(withinSizeCap(withLength(1024))).toBe(true)
    expect(withinSizeCap(withLength(MAX_ATTACHMENT_BYTES))).toBe(true)
    expect(withinSizeCap(withLength(MAX_ATTACHMENT_BYTES + 1))).toBe(false)
  })

  it('accepts an override for the cap', () => {
    expect(withinSizeCap(withLength(500), 100)).toBe(false)
    expect(withinSizeCap(withLength(50), 100)).toBe(true)
  })

  it('keeps a response whose size cannot be read', () => {
    // Refusing everything unmeasurable would mean caching almost nothing, and
    // buffering the body to measure it would cost the memory this is saving.
    expect(withinSizeCap({ headers: { get: () => null } })).toBe(true)
    expect(withinSizeCap({ headers: { get: () => 'unknown' } })).toBe(true)
    expect(withinSizeCap({})).toBe(true)
    expect(withinSizeCap(null)).toBe(true)
  })
})

describe('evictionPlan', () => {
  const entry = (url, size) => ({ url, size })

  it('drops nothing while the total fits', () => {
    expect(evictionPlan([entry('a', 10), entry('b', 20)], 100)).toEqual([])
    expect(evictionPlan([entry('a', 100)], 100)).toEqual([])
  })

  it('drops the least recently used first, and only as many as it must', () => {
    // Entries arrive oldest first, which is the order Cache Storage returns
    // keys in — and the worker re-inserts an entry when it serves it, so
    // insertion order is a recency list.
    const entries = [entry('oldest', 50), entry('middle', 50), entry('newest', 50)]

    expect(evictionPlan(entries, 100)).toEqual(['oldest'])
    expect(evictionPlan(entries, 40)).toEqual(['oldest', 'middle'])
  })

  it('never drops the newest entry to make room for itself', () => {
    // A single file larger than the whole budget would otherwise be written and
    // immediately deleted on every request for it.
    const entries = [entry('only', 500)]

    expect(evictionPlan(entries, 100)).toEqual([])
  })

  it('treats a missing or unreadable size as zero rather than throwing', () => {
    const entries = [entry('a', undefined), entry('b', 'huge'), entry('c', 200)]

    expect(evictionPlan(entries, 100)).toEqual(['a', 'b'])
  })

  it('has nothing to plan for an empty or missing list', () => {
    expect(evictionPlan([], 100)).toEqual([])
    expect(evictionPlan(null, 100)).toEqual([])
    expect(evictionPlan(undefined, 100)).toEqual([])
  })
})

describe('attachmentUrlsForTask', () => {
  const urls = [
    'https://x/api/v1/workspaces/ws1/tasks/t1/attachments/a1',
    'https://x/api/v1/workspaces/ws1/tasks/t1/attachments/a2',
    'https://x/api/v1/workspaces/ws1/tasks/t2/attachments/a3',
    'https://x/api/v1/workspaces/ws2/tasks/t1/attachments/a4',
  ]

  it('finds one task\'s attachments and no other task\'s', () => {
    expect(attachmentUrlsForTask(urls, 'ws1', 't1')).toEqual([urls[0], urls[1]])
  })

  it('does not confuse the same task id in another workspace', () => {
    expect(attachmentUrlsForTask(urls, 'ws2', 't1')).toEqual([urls[3]])
  })

  it('has nothing to find without both ids or a list', () => {
    expect(attachmentUrlsForTask(urls, 'ws1', '')).toEqual([])
    expect(attachmentUrlsForTask(urls, '', 't1')).toEqual([])
    expect(attachmentUrlsForTask(null, 'ws1', 't1')).toEqual([])
    expect(attachmentUrlsForTask(undefined, 'ws1', 't1')).toEqual([])
  })
})

describe('attachmentUrlsForWorkspace', () => {
  it('finds a workspace’s attachments among everything cached', () => {
    // Clearing a workspace has to reach the bytes as well as the rows, and the
    // workspace is already in the path.
    const urls = [
      'https://x/api/v1/workspaces/ws1/tasks/t1/attachments/a1',
      'https://x/api/v1/workspaces/ws2/tasks/t2/attachments/a2',
      'https://x/api/v1/workspaces/ws1/tasks/t1',
    ]

    expect(attachmentUrlsForWorkspace(urls, 'ws1')).toEqual([
      'https://x/api/v1/workspaces/ws1/tasks/t1/attachments/a1',
    ])
  })

  it('has nothing to find without a workspace or a list', () => {
    expect(attachmentUrlsForWorkspace(['x'], '')).toEqual([])
    expect(attachmentUrlsForWorkspace(null, 'ws1')).toEqual([])
    expect(attachmentUrlsForWorkspace(undefined, 'ws1')).toEqual([])
  })
})

describe('touchCacheEntry', () => {
  // The bug this replaced: Workbox's cachedResponseWillBeUsed carries a
  // cacheName and no cache, so reaching for one threw on every cache hit --
  // and CacheFirst does not guard its cache read, so an attachment that had
  // loaded once never loaded again.
  const fakeStorage = (cache) => ({ open: vi.fn().mockResolvedValue(cache) })

  it('re-inserts the entry under the name it was given', async () => {
    const cache = { put: vi.fn().mockResolvedValue(undefined), delete: vi.fn() }
    const storage = fakeStorage(cache)
    const request = new Request('https://x/api/v1/workspaces/ws1/tasks/t1/attachments/a1')
    const response = new Response('bytes')

    expect(await touchCacheEntry({ cacheName: 'attachment-cache', request, response, storage })).toBe(true)
    expect(storage.open).toHaveBeenCalledWith('attachment-cache')
    expect(cache.put).toHaveBeenCalledWith(request, response)
    // A put already removes the matching record and appends the new one, so the
    // delete that used to come first bought nothing and could lose the bytes.
    expect(cache.delete).not.toHaveBeenCalled()
  })

  it('serves the attachment anyway when the cache cannot be opened', async () => {
    const storage = { open: vi.fn().mockRejectedValue(new Error('no storage')) }
    await expect(touchCacheEntry({ cacheName: 'attachment-cache', request: 'r', response: 'x', storage })).resolves.toBe(false)
  })

  it('serves the attachment anyway when the re-insert fails', async () => {
    // Quota, most likely. A recency hint must never cost a served file.
    const cache = { put: vi.fn().mockRejectedValue(new Error('QuotaExceededError')) }
    await expect(
      touchCacheEntry({ cacheName: 'attachment-cache', request: 'r', response: 'x', storage: fakeStorage(cache) })
    ).resolves.toBe(false)
  })

  it('falls back to the worker\'s own caches when none is passed', async () => {
    const cache = { put: vi.fn().mockResolvedValue(undefined) }
    const caches = fakeStorage(cache)
    vi.stubGlobal('caches', caches)
    try {
      expect(await touchCacheEntry({ cacheName: 'attachment-cache', request: 'r', response: 'x' })).toBe(true)
      expect(caches.open).toHaveBeenCalledWith('attachment-cache')
    } finally {
      vi.unstubAllGlobals()
    }
  })
})
