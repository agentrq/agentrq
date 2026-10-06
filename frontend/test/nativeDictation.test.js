// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ref, nextTick } from 'vue'
import { dictationPlatform, isMobileDevice, dictationLanguage, browserLanguageName, useNativeDictation } from '../src/composables/useNativeDictation'

const IPHONE = 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1'

const ANDROID = 'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Mobile Safari/537.36'

const recognitions = []

class FakeRecognition {
  constructor() { recognitions.push(this) }
  start() { this.started = true }
  stop() { this.onend() }
  abort() { this.aborted = true }
}

// A result list in the shape Safari hands to onresult.
const heard = (...parts) => ({ results: parts.map((transcript) => [{ transcript }]) })

const setup = (body, input, over = {}) => {
  const opts = { language: () => 'en-US', onError: vi.fn(), onEnd: vi.fn(), onUnavailable: vi.fn(), ...over }
  return { opts, d: useNativeDictation(body, input, opts) }
}

beforeEach(() => {
  recognitions.length = 0
  vi.stubGlobal('navigator', { ...navigator, userAgent: IPHONE })
  vi.stubGlobal('webkitSpeechRecognition', FakeRecognition)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('dictationPlatform', () => {
  it('knows an iPhone by its user agent', () => {
    expect(dictationPlatform({ userAgent: IPHONE })).toBe('apple')
  })

  it('knows an iPad asking for the desktop site by its touch screen', () => {
    expect(dictationPlatform({ userAgent: 'Macintosh', platform: 'MacIntel', maxTouchPoints: 5 })).toBe('apple')
  })

  it('knows an Android phone', () => {
    expect(dictationPlatform({ userAgent: ANDROID, platform: 'Linux armv8l' })).toBe('android')
  })

  it('does not take a computer for one', () => {
    expect(dictationPlatform({ userAgent: 'Macintosh', platform: 'MacIntel', maxTouchPoints: 0 })).toBe(null)
    expect(dictationPlatform({ userAgent: 'Mozilla/5.0 (X11; Linux x86_64) Chrome/130', platform: 'Linux x86_64' })).toBe(null)
    expect(dictationPlatform({})).toBe(null)
  })

  it('reads the real navigator when given none', () => {
    expect(dictationPlatform()).toBe('apple')
  })

  it('is null with no navigator at all', () => {
    vi.stubGlobal('navigator', undefined)
    expect(dictationPlatform()).toBe(null)
  })
})

describe('isMobileDevice', () => {
  it('counts the phones with dictation', () => {
    expect(isMobileDevice({ userAgent: IPHONE })).toBe(true)
    expect(isMobileDevice({ userAgent: ANDROID })).toBe(true)
  })

  it('counts an older phone with none', () => {
    expect(isMobileDevice({ userAgent: 'Opera/9.80 (J2ME/MIDP; Opera Mini/9.80)' })).toBe(true)
  })

  it('does not count a computer', () => {
    expect(isMobileDevice({ userAgent: 'Mozilla/5.0 (X11; Linux x86_64) Chrome/130' })).toBe(false)
  })

  it('reads the real navigator, and copes with none', () => {
    expect(isMobileDevice()).toBe(true)
    vi.stubGlobal('navigator', undefined)
    expect(isMobileDevice()).toBe(false)
  })
})

describe('dictationLanguage', () => {
  it('uses the browser language when the workspace has no setting', () => {
    expect(dictationLanguage(null, 'de-AT')).toBe('de-AT')
    expect(dictationLanguage('auto', 'de-AT')).toBe('de-AT')
  })

  it('keeps the browser region when the setting is the same language', () => {
    expect(dictationLanguage('de', 'de-AT')).toBe('de-AT')
  })

  it('uses the setting when it names another language', () => {
    expect(dictationLanguage('tr', 'en-GB')).toBe('tr')
  })

  it('falls back to US English with no browser language', () => {
    expect(dictationLanguage(null, undefined)).toBe('en-US')
  })
})

describe('browserLanguageName', () => {
  const MAC = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15'

  it('names the full tag on a phone, which dictates in it', () => {
    expect(browserLanguageName({ userAgent: IPHONE, language: 'de-AT' })).toBe('German (Austria)')
    expect(browserLanguageName({ userAgent: ANDROID, language: 'fil-PH' })).toBe('Filipino (Philippines)')
  })

  it('names US English on a phone with no browser language', () => {
    expect(browserLanguageName({ userAgent: ANDROID })).toBe('English (United States)')
  })

  it('shows the tag itself on a phone when it cannot be named', () => {
    expect(browserLanguageName({ userAgent: IPHONE, language: 'not a tag!' })).toBe('not a tag!')
  })

  it("reads the browser's own navigator when given none", () => {
    vi.stubGlobal('navigator', { userAgent: IPHONE, language: 'tr-TR' })
    expect(browserLanguageName()).toBe('Turkish (Türkiye)')
    vi.stubGlobal('navigator', undefined)
    expect(browserLanguageName()).toBe('English')
  })

  it("names Whisper's language on a computer", () => {
    expect(browserLanguageName({ userAgent: MAC, language: 'de-AT' })).toBe('German')
  })

  it("falls back to English on a computer when Whisper lacks the language", () => {
    expect(browserLanguageName({ userAgent: MAC, language: 'fil-PH' })).toBe('English')
    expect(browserLanguageName({ userAgent: MAC })).toBe('English')
  })
})

describe('useNativeDictation', () => {
  it('is offered on an iPhone with speech recognition', () => {
    expect(setup(ref('')).d.isSupported).toBe(true)
  })

  it('takes the unprefixed recogniser too', () => {
    vi.stubGlobal('webkitSpeechRecognition', undefined)
    vi.stubGlobal('SpeechRecognition', FakeRecognition)
    expect(setup(ref('')).d.isSupported).toBe(true)
  })

  it('is offered on Android, one phrase per start', () => {
    vi.stubGlobal('navigator', { ...navigator, userAgent: ANDROID })
    const { d } = setup(ref(''))
    expect(d.isSupported).toBe(true)
    d.start()
    expect(recognitions[0]).toMatchObject({ continuous: false, interimResults: true })
  })

  it('is not offered on a computer, which keeps the local model', () => {
    vi.stubGlobal('navigator', { ...navigator, userAgent: 'Mozilla/5.0 (X11; Linux x86_64) Chrome/130' })
    expect(setup(ref('')).d.isSupported).toBe(false)
  })

  it('is not offered on an iPhone without speech recognition', () => {
    vi.stubGlobal('webkitSpeechRecognition', undefined)
    expect(setup(ref('')).d.isSupported).toBe(false)
  })

  it('starts continuous, live recognition in the asked-for language', () => {
    const { d } = setup(ref(''), undefined, { language: () => 'tr-TR' })
    d.start()
    const [r] = recognitions
    expect(r).toMatchObject({ lang: 'tr-TR', continuous: true, interimResults: true, started: true })
    expect(d.isListening.value).toBe(true)
  })

  it('writes the words at the cursor as they are heard, and leaves the cursor after them', async () => {
    const body = ref('fix login page')
    const el = document.createElement('textarea')
    el.value = body.value
    el.setSelectionRange(3, 3)
    const { d } = setup(body, ref(el))
    d.start()
    const [r] = recognitions

    r.onresult(heard('the'))
    expect(body.value).toBe('fix the login page')
    // Stands in for v-model, whose update sends the cursor to the end.
    el.value = body.value
    await nextTick()
    expect([el.selectionStart, el.selectionEnd]).toEqual([7, 7])

    // Each result is the whole of what was heard, replacing the last one.
    r.onresult(heard('the', ' broken'))
    expect(body.value).toBe('fix the broken login page')
  })

  it('appends when it is given no field', () => {
    const body = ref('fix')
    const { d } = setup(body)
    d.start()
    recognitions[0].onresult(heard('login'))
    expect(body.value).toBe('fix login')
  })

  it('appends when the field shows something else', () => {
    const body = ref('fix')
    const el = document.createElement('textarea')
    el.value = 'stale'
    el.setSelectionRange(0, 0)
    const { d } = setup(body, ref(el))
    d.start()
    recognitions[0].onresult(heard('login'))
    expect(body.value).toBe('fix login')
  })

  it('leaves the text alone until something is heard', () => {
    const body = ref('fix')
    const { d } = setup(body)
    d.start()
    recognitions[0].onresult(heard('  '))
    expect(body.value).toBe('fix')
  })

  it('starts on an empty box', () => {
    const body = ref(undefined)
    const { d } = setup(body)
    d.start()
    recognitions[0].onresult(heard('hello'))
    expect(body.value).toBe('hello')
  })

  it('ends once, on stop, keeping what was heard', () => {
    const body = ref('')
    const { d, opts } = setup(body)
    d.start()
    recognitions[0].onresult(heard('hello'))
    d.stop()
    expect(d.isListening.value).toBe(false)
    expect(body.value).toBe('hello')
    expect(opts.onEnd).toHaveBeenCalledTimes(1)
    // A late end from the same recogniser is not a second dictation.
    recognitions[0].onend()
    expect(opts.onEnd).toHaveBeenCalledTimes(1)
  })

  it('ends when the device stops listening by itself', () => {
    const { d, opts } = setup(ref(''))
    d.start()
    recognitions[0].onend()
    expect(d.isListening.value).toBe(false)
    expect(opts.onEnd).toHaveBeenCalledTimes(1)
  })

  it('does nothing on stop or abort when not listening', () => {
    const { d, opts } = setup(ref(''))
    d.stop()
    d.abort()
    expect(opts.onEnd).not.toHaveBeenCalled()
  })

  it('aborts on leaving, and still counts the dictation', () => {
    const { d, opts } = setup(ref(''))
    d.start()
    d.abort()
    expect(recognitions[0].aborted).toBe(true)
    expect(d.isListening.value).toBe(false)
    expect(opts.onEnd).toHaveBeenCalledTimes(1)
    recognitions[0].onend()
    expect(opts.onEnd).toHaveBeenCalledTimes(1)
  })

  it.each([
    ['not-allowed', 'Microphone permission denied'],
    ['audio-capture', 'No microphone found on this device'],
    ['no-speech', 'No speech detected'],
    ['network', 'Dictation needs a network connection'],
    ['language-not-supported', 'Dictation failed: language-not-supported'],
  ])('says what went wrong on %s', (code, message) => {
    const { d, opts } = setup(ref(''))
    d.start()
    recognitions[0].onerror({ error: code })
    expect(opts.onError).toHaveBeenCalledWith(message)
  })

  it('says nothing when it was aborted on purpose', () => {
    const { d, opts } = setup(ref(''))
    d.start()
    recognitions[0].onerror({ error: 'aborted' })
    expect(opts.onError).not.toHaveBeenCalled()
  })

  it('reports the service refused rather than an error', () => {
    const { d, opts } = setup(ref(''))
    d.start()
    recognitions[0].onerror({ error: 'service-not-allowed' })
    expect(opts.onUnavailable).toHaveBeenCalledTimes(1)
    expect(opts.onError).not.toHaveBeenCalled()
  })

  it('recovers when the recogniser will not start', () => {
    vi.stubGlobal('webkitSpeechRecognition', class extends FakeRecognition {
      start() { throw new Error('already started') }
    })
    const { d, opts } = setup(ref(''))
    d.start()
    expect(d.isListening.value).toBe(false)
    expect(opts.onError).toHaveBeenCalledWith('Dictation failed: already started')
    d.stop()
    expect(opts.onEnd).not.toHaveBeenCalled()
  })
})
