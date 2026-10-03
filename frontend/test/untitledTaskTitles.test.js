// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

const workers = []

class FakeTitleWorker {
  constructor() {
    this.listeners = []
    this.posted = []
    this.terminated = false
    workers.push(this)
  }
  addEventListener(_type, fn) { this.listeners.push(fn) }
  removeEventListener(_type, fn) { this.listeners = this.listeners.filter((l) => l !== fn) }
  postMessage(msg) { this.posted.push(msg) }
  terminate() { this.terminated = true }
  emit(data) { [...this.listeners].forEach((fn) => fn({ data })) }
}

vi.mock('../src/workers/titleWorker.js?worker', () => ({ default: FakeTitleWorker }))

const api = {
  getTask: vi.fn(),
  updateTaskTitle: vi.fn(),
  recordTelemetry: vi.fn(),
}
vi.mock('../src/api', () => ({
  getTask: (...a) => api.getTask(...a),
  updateTaskTitle: (...a) => api.updateTaskTitle(...a),
  recordTelemetry: (...a) => api.recordTelemetry(...a),
  TELEMETRY_LOCAL_AI_TITLE_GENERATE: 'local_ai_title_generate',
}))

const {
  UNTITLED_TASK_TITLE,
  autoTitleKey,
  isAutoTitleOn,
  setAutoTitleOn,
  createUntitledTaskTitler,
  nameUntitledTask,
} = await import('../src/composables/useUntitledTaskTitles')

const flush = () => new Promise((r) => setTimeout(r, 0))

// The worker answers the latest request it was sent.
async function answer(type, extra) {
  await flush()
  const w = workers.at(-1)
  w.emit({ id: w.posted.at(-1).id, type, ...extra })
}

const memoryStorage = (initial = {}) => {
  const data = { ...initial }
  return {
    getItem: (k) => (k in data ? data[k] : null),
    setItem: (k, v) => { data[k] = String(v) },
    data,
  }
}

beforeEach(() => {
  workers.length = 0
  api.getTask.mockReset().mockResolvedValue({ task: { title: UNTITLED_TASK_TITLE } })
  api.updateTaskTitle.mockReset().mockResolvedValue({ task: {} })
  api.recordTelemetry.mockReset()
  localStorage.clear()
})

afterEach(() => vi.restoreAllMocks())

describe('the per-workspace switch', () => {
  it('is on by default on a computer and off on a phone', () => {
    expect(isAutoTitleOn('ws1', { storage: memoryStorage(), mobile: false })).toBe(true)
    expect(isAutoTitleOn('ws1', { storage: memoryStorage(), mobile: true })).toBe(false)
  })

  it('keeps either answer, whatever the device', () => {
    const storage = memoryStorage()
    setAutoTitleOn('ws1', true, storage)
    expect(storage.data[autoTitleKey('ws1')]).toBe('on')
    expect(isAutoTitleOn('ws1', { storage, mobile: true })).toBe(true)

    setAutoTitleOn('ws1', false, storage)
    expect(isAutoTitleOn('ws1', { storage, mobile: false })).toBe(false)
    expect(isAutoTitleOn('ws2', { storage, mobile: false })).toBe(true)
  })

  it('falls back to the default when storage cannot be read, and reports a failed write', () => {
    const broken = {
      getItem: () => { throw new Error('SecurityError: storage disabled') },
      setItem: () => { throw new Error('QuotaExceededError: storage full') },
    }
    expect(isAutoTitleOn('ws1', { storage: broken, mobile: false })).toBe(true)
    expect(isAutoTitleOn('ws1', { storage: null, mobile: true })).toBe(false)
    expect(setAutoTitleOn('ws1', true, broken)).toBe(false)
  })

  it('reads this device by default', () => {
    expect(setAutoTitleOn('ws1', false)).toBe(true)
    expect(isAutoTitleOn('ws1')).toBe(false)
  })
})

describe('naming an untitled task', () => {
  it('names it from its description and lets the model go afterwards', async () => {
    const titler = createUntitledTaskTitler({ supported: () => true })

    expect(titler.enqueue('ws1', 't1', '  Fix the login page redirect  ')).toBe(true)
    expect(workers[0].posted[0]).toMatchObject({ type: 'GENERATE_TITLE', data: { text: 'Fix the login page redirect' } })
    expect(api.recordTelemetry).toHaveBeenCalledWith('local_ai_title_generate', 'ws1')

    await answer('PROGRESS', { data: { status: 'progress', progress: 40 } })
    await answer('SUCCESS', { data: { title: ' Fix login redirect ' } })
    await titler.idle()

    expect(api.getTask).toHaveBeenCalledWith('ws1', 't1')
    expect(api.updateTaskTitle).toHaveBeenCalledWith('ws1', 't1', 'Fix login redirect')
    expect(workers[0].terminated).toBe(true)
  })

  it('leaves a task alone that was renamed while the model ran', async () => {
    api.getTask.mockResolvedValue({ task: { title: 'Named by hand' } })
    const titler = createUntitledTaskTitler({ supported: () => true })

    titler.enqueue('ws1', 't1', 'Fix the login page redirect')
    await answer('SUCCESS', { data: { title: 'Fix login redirect' } })
    await titler.idle()

    expect(api.updateTaskTitle).not.toHaveBeenCalled()
  })

  it('writes nothing when the model has nothing to say', async () => {
    const titler = createUntitledTaskTitler({ supported: () => true })

    titler.enqueue('ws1', 't1', 'Fix the login page redirect')
    await answer('SUCCESS', { data: {} })
    await titler.idle()

    expect(api.getTask).not.toHaveBeenCalled()
    expect(api.updateTaskTitle).not.toHaveBeenCalled()
  })

  it('names queued tasks one at a time with one model, and goes on past a failure', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const titler = createUntitledTaskTitler({ supported: () => true })

    titler.enqueue('ws1', 't1', 'First task description')
    titler.enqueue('ws1', 't2', 'Second task description')
    expect(workers[0].posted).toHaveLength(1)

    // An answer to another request is not this one's.
    await flush()
    workers[0].emit({ id: -1, type: 'SUCCESS', data: { title: 'Stray' } })
    await answer('ERROR', { error: 'model failed to load' })
    await answer('SUCCESS', { data: { title: 'Second task' } })
    await titler.idle()

    expect(workers).toHaveLength(1)
    expect(api.updateTaskTitle.mock.calls).toEqual([['ws1', 't2', 'Second task']])
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('t1'), expect.any(Error))
  })

  it('starts a fresh model for work that arrives after the queue emptied', async () => {
    const titler = createUntitledTaskTitler({ supported: () => true })
    titler.enqueue('ws1', 't1', 'First task description')
    await answer('SUCCESS', { data: { title: 'First' } })
    await titler.idle()

    titler.enqueue('ws1', 't2', 'Second task description')
    await answer('SUCCESS', { data: { title: 'Second' } })
    await titler.idle()

    expect(workers).toHaveLength(2)
    expect(workers.every((w) => w.terminated)).toBe(true)
  })

  it('queues nothing it could not name', async () => {
    const titler = createUntitledTaskTitler({ supported: () => true })
    expect(titler.enqueue('ws1', 't1', 'tiny')).toBe(false)
    expect(titler.enqueue('ws1', 't1', null)).toBe(false)
    expect(titler.enqueue('ws1', undefined, 'Fix the login page redirect')).toBe(false)
    expect(createUntitledTaskTitler({ supported: () => false }).enqueue('ws1', 't1', 'Fix the login page redirect')).toBe(false)
    await titler.idle()
    expect(workers).toHaveLength(0)
  })
})

describe('nameUntitledTask', () => {
  it('does nothing in a workspace switched off on this device', () => {
    setAutoTitleOn('ws1', false)
    expect(nameUntitledTask('ws1', 't1', 'Fix the login page redirect')).toBe(false)
    expect(workers).toHaveLength(0)
  })

  it('queues the task in a workspace switched on', async () => {
    vi.stubGlobal('Worker', class {})
    setAutoTitleOn('ws1', true)
    expect(nameUntitledTask('ws1', 't1', 'Fix the login page redirect')).toBe(true)
    await answer('SUCCESS', { data: { title: 'Fix login redirect' } })
    await flush()
    await flush()
    expect(api.updateTaskTitle).toHaveBeenCalledWith('ws1', 't1', 'Fix login redirect')
    vi.unstubAllGlobals()
  })
})
