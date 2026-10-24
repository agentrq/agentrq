// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ref, createApp, h } from 'vue'

const workers = []

vi.mock('../src/workers/titleWorker.js?worker', () => ({
  default: class FakeTitleWorker {
    constructor() {
      this.listeners = []
      this.posted = []
      this.terminated = false
      workers.push(this)
    }
    addEventListener(_type, fn) { this.listeners.push(fn) }
    postMessage(msg) { this.posted.push(msg) }
    terminate() { this.terminated = true }
    emit(data) { this.listeners.forEach((fn) => fn({ data })) }
  },
}))

const recordTelemetry = vi.fn()
vi.mock('../src/api', () => ({
  recordTelemetry: (...args) => recordTelemetry(...args),
  TELEMETRY_LOCAL_AI_TITLE_GENERATE: 'local_ai_title_generate',
}))

const { useAutoTitle } = await import('../src/composables/useAutoTitle')

// Mounted in a throwaway app so the composable's unmount hook runs.
const apps = []
const setup = (description = 'Fix the login page redirect', workspaceId) => {
  const body = ref(description)
  const title = ref('')
  let auto
  const app = createApp({ setup() { auto = useAutoTitle(body, title, workspaceId); return () => h('div') } })
  app.mount(document.createElement('div'))
  apps.push(app)
  return { body, title, auto, unmount: () => app.unmount() }
}

// The worker answers the latest request it was sent.
const answer = (type, extra) => {
  const w = workers.at(-1)
  w.emit({ id: w.posted.at(-1).id, type, ...extra })
}

beforeEach(() => {
  workers.length = 0
  recordTelemetry.mockClear()
  vi.stubGlobal('Worker', class {})
})

afterEach(() => {
  apps.splice(0).forEach((app) => app._container && app.unmount())
  vi.unstubAllGlobals()
})

describe('useAutoTitle', () => {
  it('fills the title with the generated one', () => {
    const { title, auto } = setup()
    auto.generateTitle()
    expect(auto.isGenerating.value).toBe(true)
    expect(workers[0].posted[0]).toMatchObject({
      type: 'GENERATE_TITLE',
      data: { text: 'Fix the login page redirect' },
    })
    answer('SUCCESS', { data: { title: 'Fix login redirect' } })
    expect(title.value).toBe('Fix login redirect')
    expect(auto.isGenerating.value).toBe(false)
  })

  it('generates again after a typed title was cleared', () => {
    const { title, auto } = setup()
    title.value = 'typed'
    auto.markOverridden()
    title.value = ''
    auto.generateTitle()
    expect(auto.isOverridden.value).toBe(false)
    answer('SUCCESS', { data: { title: 'Fix login redirect' } })
    expect(title.value).toBe('Fix login redirect')
  })

  it('keeps a title typed while generating', () => {
    const { title, auto } = setup()
    auto.generateTitle()
    title.value = 'mine'
    auto.markOverridden()
    answer('SUCCESS', { data: { title: 'Fix login redirect' } })
    expect(title.value).toBe('mine')
    expect(auto.isGenerating.value).toBe(false)
  })

  it('ignores an answer to an older request', () => {
    const { title, auto } = setup()
    auto.generateTitle()
    workers[0].emit({ id: -1, type: 'SUCCESS', data: { title: 'stale' } })
    expect(title.value).toBe('')
    expect(auto.isGenerating.value).toBe(true)
  })

  it('stops generating on an error', () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    const { title, auto } = setup()
    auto.generateTitle()
    answer('ERROR', { error: 'title model failed to load' })
    expect(auto.isGenerating.value).toBe(false)
    expect(title.value).toBe('')
    expect(error).toHaveBeenCalled()
    error.mockRestore()
  })

  it('ignores an unknown message', () => {
    const { auto } = setup()
    auto.generateTitle()
    answer('OTHER')
    expect(auto.isGenerating.value).toBe(true)
  })

  it('reports model download progress', () => {
    const { auto } = setup()
    auto.generateTitle()
    const w = workers[0]
    w.emit({ id: -1, type: 'PROGRESS', data: { status: 'initiate' } })
    expect(auto.isModelLoading.value).toBe(true)
    expect(auto.modelProgress.value).toBe(0)
    w.emit({ id: -1, type: 'PROGRESS', data: { status: 'progress', progress: 41.6 } })
    expect(auto.modelProgress.value).toBe(42)
    w.emit({ id: -1, type: 'PROGRESS', data: { status: 'download' } })
    expect(auto.isModelLoading.value).toBe(true)
    w.emit({ id: -1, type: 'PROGRESS', data: { status: 'done' } })
    expect(auto.isModelLoading.value).toBe(false)
    w.emit({ id: -1, type: 'PROGRESS', data: { status: 'initiate' } })
    w.emit({ id: -1, type: 'PROGRESS', data: { status: 'ready' } })
    expect(auto.isModelLoading.value).toBe(false)
  })

  it('reuses one worker and terminates it on unmount', () => {
    const { auto, unmount } = setup()
    auto.generateTitle()
    auto.generateTitle()
    expect(workers).toHaveLength(1)
    unmount()
    expect(workers[0].terminated).toBe(true)
  })

  it('unmounts cleanly without ever starting a worker', () => {
    setup().unmount()
    expect(workers).toHaveLength(0)
  })

  it('does nothing with too little text', () => {
    const { body, auto } = setup('abc')
    auto.generateTitle()
    body.value = undefined
    auto.generateTitle()
    expect(workers).toHaveLength(0)
    expect(recordTelemetry).not.toHaveBeenCalled()
  })

  it('does nothing when the browser lacks a worker or WebAssembly', () => {
    vi.stubGlobal('Worker', undefined)
    expect(setup().auto.isSupported).toBe(false)
    vi.stubGlobal('Worker', class {})
    vi.stubGlobal('WebAssembly', undefined)
    expect(setup().auto.isSupported).toBe(false)
    vi.stubGlobal('WebAssembly', { instantiate: 1 })
    const { auto } = setup()
    expect(auto.isSupported).toBe(false)
    auto.generateTitle()
    expect(workers).toHaveLength(0)
  })

  it('does nothing on a phone, where the model would crash the tab', () => {
    vi.stubGlobal('WebAssembly', { instantiate: () => {} })
    vi.stubGlobal('navigator', { userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)', maxTouchPoints: 5 })
    const { auto } = setup()
    expect(auto.isSupported).toBe(false)
    auto.generateTitle()
    expect(workers).toHaveLength(0)
    expect(recordTelemetry).not.toHaveBeenCalled()
  })

  it('counts the use against the workspace, however it is given', () => {
    setup('Fix the login page redirect', 'ws1').auto.generateTitle()
    setup('Fix the login page redirect', ref('ws2')).auto.generateTitle()
    setup('Fix the login page redirect', () => 'ws3').auto.generateTitle()
    setup().auto.generateTitle()
    expect(recordTelemetry.mock.calls).toEqual([
      ['local_ai_title_generate', 'ws1'],
      ['local_ai_title_generate', 'ws2'],
      ['local_ai_title_generate', 'ws3'],
      ['local_ai_title_generate', undefined],
    ])
  })
})
