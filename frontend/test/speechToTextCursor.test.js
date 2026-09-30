// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ref, createApp, h, nextTick, watch } from 'vue'

const workers = []

vi.mock('../src/workers/whisperWorker.js?worker', () => ({
  default: class FakeWhisperWorker {
    constructor() { workers.push(this) }
    postMessage() {}
  },
}))

const telemetry = []

vi.mock('../src/api', () => ({
  recordTelemetry: (action) => { telemetry.push(action) },
  TELEMETRY_LOCAL_AI_RECORDING_END: 'local_ai_recording_end',
  TELEMETRY_UI_DICTATION_END: 'ui_dictation_end',
}))

const { useSpeechToText } = await import('../src/composables/useSpeechToText')

class FakeRecorder {
  static isTypeSupported() { return true }
  constructor() { this.state = 'inactive'; this.mimeType = 'audio/webm' }
  start() { this.state = 'recording' }
  stop() {
    this.state = 'inactive'
    this.ondataavailable({ data: new Blob(['x']) })
    this.onstop()
  }
}

class FakeAudioContext {
  constructor() { this.state = 'running' }
  decodeAudioData() { return Promise.resolve({ duration: 1 }) }
  close() { return Promise.resolve() }
}

class FakeOfflineContext {
  createBufferSource() { return { connect() {}, start() {} } }
  startRendering() { return Promise.resolve({ getChannelData: () => new Float32Array(4) }) }
}

const apps = []
// Records, stops and has the worker answer with `transcript`.
const dictate = async (body, input, transcript) => {
  let stt
  const app = createApp({ setup() { stt = useSpeechToText(body, 'ws1', input); return () => h('div') } })
  app.mount(document.createElement('div'))
  apps.push(app)
  stt.toggleRecording()
  await vi.waitFor(() => expect(stt.isRecording.value).toBe(true))
  stt.toggleRecording()
  await vi.waitFor(() => expect(workers.length).toBe(1))
  workers[0].onmessage({ data: { status: 'complete', text: transcript } })
  await nextTick()
}

beforeEach(() => {
  workers.length = 0
  telemetry.length = 0
  vi.stubGlobal('navigator', {
    ...navigator,
    mediaDevices: { getUserMedia: () => Promise.resolve({ getTracks: () => [] }) },
  })
  vi.stubGlobal('Worker', class {})
  vi.stubGlobal('MediaRecorder', FakeRecorder)
  vi.stubGlobal('AudioContext', FakeAudioContext)
  vi.stubGlobal('OfflineAudioContext', FakeOfflineContext)
})

afterEach(() => {
  apps.splice(0).forEach((app) => app.unmount())
  vi.unstubAllGlobals()
})

describe('useSpeechToText', () => {
  it('puts the transcript at the cursor and leaves the cursor after it', async () => {
    const body = ref('fix login page')
    const el = document.createElement('textarea')
    document.body.appendChild(el)
    el.value = body.value
    el.setSelectionRange(3, 3)
    // Stands in for v-model, whose update sends the cursor to the end.
    const stop = watch(body, (v) => { el.value = v })

    await dictate(body, ref(el), 'the')

    expect(body.value).toBe('fix the login page')
    expect(el.value).toBe('fix the login page')
    expect([el.selectionStart, el.selectionEnd]).toEqual([7, 7])
    stop()
    el.remove()
  })

  it('appends when it is given no field', async () => {
    const body = ref('fix')
    await dictate(body, undefined, 'login')
    expect(body.value).toBe('fix login')
  })
})

describe('useSpeechToText on an iPhone', () => {
  const recognitions = []
  class FakeRecognition {
    constructor() { recognitions.push(this) }
    start() {}
    stop() { this.onend() }
    abort() {}
  }

  const mount = (body) => {
    let stt
    const app = createApp({ setup() { stt = useSpeechToText(body, 'ws1'); return () => h('div') } })
    app.mount(document.createElement('div'))
    apps.push(app)
    return stt
  }

  beforeEach(() => {
    recognitions.length = 0
    vi.stubGlobal('navigator', {
      ...navigator,
      userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)',
      language: 'en-GB',
      mediaDevices: { getUserMedia: () => Promise.resolve({ getTracks: () => [] }) },
    })
    vi.stubGlobal('webkitSpeechRecognition', FakeRecognition)
    localStorage.removeItem('stt_lang_ws1')
  })

  it('dictates with the device, not Whisper, and counts it as a dictation', async () => {
    localStorage.setItem('stt_lang_ws1', 'tr')
    const body = ref('fix')
    const stt = mount(body)
    expect(stt.isSupported.value).toBe(true)

    stt.toggleRecording()
    expect(stt.isRecording.value).toBe(true)
    expect(recognitions[0].lang).toBe('tr')
    recognitions[0].onresult({ results: [[{ transcript: 'login' }]] })
    expect(body.value).toBe('fix login')

    stt.toggleRecording()
    expect(stt.isRecording.value).toBe(false)
    expect(workers).toHaveLength(0)
    expect(telemetry).toEqual(['ui_dictation_end'])
  })

  it('shows what went wrong', async () => {
    const stt = mount(ref(''))
    stt.toggleRecording()
    recognitions[0].onerror({ error: 'not-allowed' })
    expect(stt.error.value).toBe('Microphone permission denied')
    recognitions[0].onend()
    // A second go clears the last error, so the same one can show again.
    stt.toggleRecording()
    expect(stt.error.value).toBe('')
  })

  it('counts a dictation cut short by leaving the view', () => {
    const stt = mount(ref(''))
    stt.toggleRecording()
    apps.splice(0).forEach((app) => app.unmount())
    expect(telemetry).toEqual(['ui_dictation_end'])
  })

  it('hides the mic, and says why, when the phone refuses dictation', () => {
    const stt = mount(ref(''))
    stt.toggleRecording()
    recognitions[0].onerror({ error: 'service-not-allowed' })
    recognitions[0].onend()
    expect(stt.isSupported.value).toBe(false)
    expect(stt.error.value).toMatch(/Dictation isn't available here/)

    // Never Whisper on a phone, even if the button were somehow pressed.
    stt.toggleRecording()
    expect(recognitions).toHaveLength(1)
    expect(stt.isRecording.value).toBe(false)
  })

  it('offers no mic on a phone without dictation', () => {
    vi.stubGlobal('webkitSpeechRecognition', undefined)
    vi.stubGlobal('navigator', { ...navigator, userAgent: 'Mozilla/5.0 (Android 14; Mobile; rv:131.0) Gecko/131.0 Firefox/131.0' })
    expect(mount(ref('')).isSupported.value).toBe(false)
  })
})

describe('useSpeechToText on a computer', () => {
  it('offers Whisper', () => {
    let stt
    const app = createApp({ setup() { stt = useSpeechToText(ref(''), 'ws1'); return () => h('div') } })
    app.mount(document.createElement('div'))
    apps.push(app)
    expect(stt.isSupported.value).toBe(true)
  })
})
