// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { ref, nextTick } from 'vue';
import { insertAtCursor } from '../utils/insertAtCursor';
import { WHISPER_LANGUAGES } from '../utils/whisperLanguages';

/**
 * Which phone or tablet this is, of those whose own dictation is used:
 * 'apple' for an iPhone, iPad or iPod, 'android' for Android, null for
 * anything else. An iPad asks for the desktop site and says it is a Mac, so a
 * Mac with a touch screen counts as one.
 */
export function dictationPlatform(nav = typeof navigator !== 'undefined' ? navigator : undefined) {
  if (!nav) return null;
  const ua = nav.userAgent || '';
  if (/iPhone|iPad|iPod/.test(ua)) return 'apple';
  if (nav.platform === 'MacIntel' && nav.maxTouchPoints > 1) return 'apple';
  if (/Android/.test(ua)) return 'android';
  return null;
}

/**
 * Whether this is a phone or tablet of any kind — one of dictationPlatform's,
 * or one of the older ones that has no dictation for a page to use.
 */
export function isMobileDevice(nav = typeof navigator !== 'undefined' ? navigator : undefined) {
  return !!dictationPlatform(nav) || /webOS|BlackBerry|IEMobile|Opera Mini/i.test(nav?.userAgent || '');
}

/**
 * The language to ask the recogniser for. The workspace setting holds a bare
 * code ("de"); the browser's tag keeps its region ("de-AT"), so it wins when
 * the two agree.
 *
 * @param {string|null|undefined} saved - the workspace's `stt_lang_*` setting
 * @param {string|undefined} browserLang - `navigator.language`
 */
export function dictationLanguage(saved, browserLang) {
  const browser = browserLang || 'en-US';
  if (!saved || saved === 'auto') return browser;
  return browser.split('-')[0].toLowerCase() === saved ? browser : saved;
}

/**
 * The name of the language "Use Browser Language" stands for, as the mic
 * will hear it. A phone dictates in the browser's own tag, region and all, so
 * it is named in full; Whisper knows only its own list, and falls back to
 * English for anything else.
 */
export function browserLanguageName(nav = typeof navigator !== 'undefined' ? navigator : undefined) {
  const tag = nav?.language || 'en-US';
  if (dictationPlatform(nav)) {
    try {
      return new Intl.DisplayNames(['en'], { type: 'language', languageDisplay: 'standard' }).of(tag);
    } catch {
      return tag;
    }
  }
  return WHISPER_LANGUAGES[tag.split('-')[0].toLowerCase()] || 'English';
}

const ERRORS = {
  'not-allowed': 'Microphone permission denied',
  'audio-capture': 'No microphone found on this device',
  'no-speech': 'No speech detected',
  'network': 'Dictation needs a network connection',
};

/**
 * Dictation with the phone's own speech recognition — the engine behind the
 * keyboard's dictation key — so there is no model to download and every
 * language the phone dictates in works.
 *
 * Offered on iPhones, iPads and Android only. A computer keeps the local
 * Whisper model, which it can afford to run, rather than send the audio to a
 * browser vendor; a phone never runs Whisper, which crashes it. See
 * useSpeechToText.
 *
 * The words appear as they are recognised, at the cursor as it was when
 * dictation started. On Android it is one phrase per start, ending at a
 * pause: Chrome there repeats earlier results when listening continuously,
 * which would write every word twice.
 *
 * @param {import('vue').Ref<string>} targetRef
 * @param {import('vue').Ref<HTMLTextAreaElement|null>} [inputRef]
 * @param {object} opts
 * @param {() => string} opts.language - asked on every start
 * @param {(message: string) => void} opts.onError
 * @param {() => void} opts.onEnd - once per dictation that started, however it ended
 * @param {() => void} opts.onUnavailable - the phone refused the service:
 *   dictation turned off, or a home-screen app on an iOS that withholds it
 */
export function useNativeDictation(targetRef, inputRef, { language, onError, onEnd, onUnavailable }) {
  const Recognition = window.SpeechRecognition || window.webkitSpeechRecognition;
  const platform = dictationPlatform();
  const isSupported = !!Recognition && !!platform;
  const isListening = ref(false);

  let recognition = null;

  function start() {
    // The cursor is read once: every result replaces the whole of what has
    // been heard so far, spliced in at the same place.
    const base = targetRef.value || '';
    const el = inputRef?.value;
    const anchor = el && el.value === base && typeof el.selectionStart === 'number'
      ? { value: base, selectionStart: el.selectionStart, selectionEnd: el.selectionEnd }
      : null;

    const r = new Recognition();
    r.lang = language();
    r.continuous = platform === 'apple';
    r.interimResults = true;

    r.onresult = (event) => {
      // Rebuilt from every result, not from resultIndex on: Safari has been
      // seen to leave isFinal unset and to re-send earlier results.
      let heard = '';
      for (let i = 0; i < event.results.length; i++) {
        heard += event.results[i][0].transcript;
      }
      heard = heard.trim();
      if (!heard) return;
      const { value, caret } = insertAtCursor(base, heard, anchor);
      targetRef.value = value;
      const field = inputRef?.value;
      if (field) nextTick(() => field.setSelectionRange(caret, caret));
    };

    r.onerror = (event) => {
      if (event.error === 'aborted') return;
      if (event.error === 'service-not-allowed') {
        onUnavailable();
        return;
      }
      onError(ERRORS[event.error] || `Dictation failed: ${event.error}`);
    };

    r.onend = () => {
      if (recognition !== r) return;
      recognition = null;
      isListening.value = false;
      onEnd();
    };

    recognition = r;
    isListening.value = true;
    try {
      r.start();
    } catch (e) {
      recognition = null;
      isListening.value = false;
      onError('Dictation failed: ' + e.message);
    }
  }

  // Stopping keeps what was heard; the recogniser ends, and onend follows.
  function stop() {
    recognition?.stop();
  }

  // Leaving the view throws away whatever was still being recognised, but the
  // dictation still happened.
  function abort() {
    const r = recognition;
    if (!r) return;
    recognition = null;
    isListening.value = false;
    r.abort();
    onEnd();
  }

  return { isSupported, isListening, start, stop, abort };
}
