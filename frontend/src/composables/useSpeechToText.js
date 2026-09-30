// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { ref, computed, onUnmounted, watch, nextTick } from 'vue';
import { insertAtCursor } from '../utils/insertAtCursor';
import { WHISPER_LANGUAGES } from '../utils/whisperLanguages';
import WhisperWorker from '../workers/whisperWorker.js?worker';
import { useToasts } from './useToasts';
import { useNativeDictation, dictationLanguage, isMobileDevice } from './useNativeDictation';
import { recordTelemetry, TELEMETRY_LOCAL_AI_RECORDING_END, TELEMETRY_UI_DICTATION_END } from '../api';

/**
 * Composable for browser-based speech-to-text using local Whisper AI.
 * Audio is recorded via MediaRecorder, resampled to 16kHz mono Float32Array,
 * and transcribed in a Web Worker using @huggingface/transformers.
 *
 * Uses the same model as whisperweb.dev (onnx-community/whisper-base).
 *
 * On a phone or tablet its own dictation is used instead; see
 * useNativeDictation. Whisper is never run there — it crashes a phone — so a
 * phone without dictation, or one that refuses it, has no voice input at all.
 *
 * @param {import('vue').Ref<string>} targetRef - Reactive string ref the transcribed text goes into
 * @param {string|import('vue').ComputedRef<string>} workspaceId - Workspace identifier for settings lookup
 * @param {import('vue').Ref<HTMLTextAreaElement|null>} [inputRef] - The field bound to targetRef; the text
 *   lands at its cursor, and is appended without it
 */
export function useSpeechToText(targetRef, workspaceId, inputRef) {
  const recording = ref(false);
  const isTranscribing = ref(false);
  const isModelLoading = ref(false);
  const modelProgress = ref(0);
  const error = ref('');
  const { notifyError } = useToasts();


  watch(error, (newVal) => {
    if (newVal) {
      notifyError(newVal, 'Speech to Text');
    }
  });

  const SUPPORTED_LANGUAGES = Object.keys(WHISPER_LANGUAGES);

  // Callers pass the workspace id as a plain string, a ref or a getter,
  // depending on whether their route makes it reactive.
  function resolveWorkspaceId() {
    return typeof workspaceId === 'function' ? workspaceId() : (workspaceId?.value ?? workspaceId);
  }

  // Set once the phone has refused its own dictation, for as long as this view
  // lives; the mic is then hidden.
  const nativeUnavailable = ref(false);
  const native = useNativeDictation(targetRef, inputRef, {
    language: () => {
      const wsId = resolveWorkspaceId();
      return dictationLanguage(wsId ? localStorage.getItem(`stt_lang_${wsId}`) : null, navigator.language);
    },
    onError: (message) => { error.value = message; },
    onEnd: () => recordTelemetry(TELEMETRY_UI_DICTATION_END, resolveWorkspaceId()),
    onUnavailable: () => {
      nativeUnavailable.value = true;
      error.value = "Dictation isn't available here. Check it is turned on in your phone's keyboard settings.";
    },
  });

  const isRecording = computed(() => recording.value || native.isListening.value);

  const whisperSupported = !isMobileDevice()
    && !!navigator.mediaDevices?.getUserMedia
    && typeof Worker !== 'undefined'
    && typeof MediaRecorder !== 'undefined';

  const isSupported = computed(() => native.isSupported ? !nativeUnavailable.value : whisperSupported);

  function getResolvedLanguage() {
    if (typeof window === 'undefined') return 'en';

    // 1. Check workspace settings override
    const wsId = resolveWorkspaceId();
    if (wsId) {
      const saved = localStorage.getItem(`stt_lang_${wsId}`);
      if (saved && saved !== 'auto') return saved;
    }

    // 2. Check browser language (navigator.language)
    const browserLang = (navigator.language || '').split('-')[0].toLowerCase();
    if (SUPPORTED_LANGUAGES.includes(browserLang)) {
      return browserLang;
    }

    // 3. Fallback to English
    return 'en';
  }

  let worker = null;
  let mediaRecorder = null;
  let audioChunks = [];
  let stream = null;

  function getWorker() {
    if (!worker) {
      worker = new WhisperWorker();

      worker.onmessage = (event) => {
        const { status, text, progress, error: errMsg } = event.data;

        switch (status) {
          case 'loading':
            isModelLoading.value = true;
            isTranscribing.value = true;
            modelProgress.value = progress ?? 0;
            break;

          case 'ready':
            isModelLoading.value = false;
            modelProgress.value = 100;
            break;

          case 'transcribing':
            isTranscribing.value = true;
            isModelLoading.value = false;
            break;

          case 'complete':
            isTranscribing.value = false;
            isModelLoading.value = false;
            if (text) {
              // Read when the text arrives, not when recording started, so
              // anything typed while it was transcribing moves the cursor too.
              const el = inputRef?.value;
              const { value, caret } = insertAtCursor(targetRef.value, text, el);
              targetRef.value = value;
              // Vue's update puts the cursor at the end; put it back after the
              // inserted text so the next dictation or keystroke follows on.
              if (el) nextTick(() => el.setSelectionRange(caret, caret));
            }
            break;

          case 'error':
            isTranscribing.value = false;
            isModelLoading.value = false;
            error.value = errMsg || 'Transcription failed';
            console.error('[STT] Worker error:', errMsg);
            break;
        }
      };

      worker.onerror = (e) => {
        isTranscribing.value = false;
        isModelLoading.value = false;
        error.value = e.message || 'Worker error';
        console.error('[STT] Worker exception:', e);
      };
    }
    return worker;
  }

  let sharedAudioCtx = null;

  function initAudioContext() {
    try {
      if (!sharedAudioCtx) {
        sharedAudioCtx = new (window.AudioContext || window.webkitAudioContext)();
      }
      if (sharedAudioCtx.state === 'suspended') {
        sharedAudioCtx.resume();
      }
    } catch (e) {
      console.error('[STT] Failed to initialize AudioContext:', e);
    }
  }

  /**
   * Convert an audio Blob (webm/opus) to 16kHz mono Float32Array.
   * Uses OfflineAudioContext for reliable resampling.
   */
  async function audioToFloat32(blob) {
    const arrayBuffer = await blob.arrayBuffer();

    if (!arrayBuffer || arrayBuffer.byteLength === 0) {
      throw new Error('Audio data is empty');
    }

    // First decode at native sample rate using the pre-authorized shared context
    if (!sharedAudioCtx) {
      sharedAudioCtx = new (window.AudioContext || window.webkitAudioContext)();
    }
    const audioBuffer = await sharedAudioCtx.decodeAudioData(arrayBuffer);

    // Use OfflineAudioContext to resample to 16kHz mono
    const targetSampleRate = 16000;
    const targetLength = Math.round(audioBuffer.duration * targetSampleRate);

    if (targetLength === 0) {
      throw new Error('Audio recording is empty');
    }

    const offlineCtx = new OfflineAudioContext(1, targetLength, targetSampleRate);
    const source = offlineCtx.createBufferSource();
    source.buffer = audioBuffer;
    source.connect(offlineCtx.destination);
    source.start(0);

    const resampled = await offlineCtx.startRendering();
    // Copy the data to avoid detached buffer issues
    return new Float32Array(resampled.getChannelData(0));
  }

  async function startRecording() {
    error.value = '';
    audioChunks = [];

    try {
      stream = await navigator.mediaDevices.getUserMedia({
        audio: {
          channelCount: 1,
          echoCancellation: true,
          noiseSuppression: true,
        }
      });

      // Prefer webm for best decode support
      const mimeType = MediaRecorder.isTypeSupported('audio/webm;codecs=opus')
        ? 'audio/webm;codecs=opus'
        : MediaRecorder.isTypeSupported('audio/webm')
          ? 'audio/webm'
          : '';

      mediaRecorder = new MediaRecorder(stream, mimeType ? { mimeType } : undefined);

      mediaRecorder.ondataavailable = (e) => {
        if (e.data.size > 0) {
          audioChunks.push(e.data);
        }
      };

      mediaRecorder.onstop = async () => {
        // Stop mic stream
        stream?.getTracks().forEach(t => t.stop());
        stream = null;

        if (audioChunks.length === 0) {
          error.value = 'No audio captured';
          return;
        }

        const blob = new Blob(audioChunks, { type: mediaRecorder.mimeType || 'audio/webm' });
        audioChunks = [];

        try {
          isTranscribing.value = true;
          const float32Audio = await audioToFloat32(blob);
          const w = getWorker();
          const lang = getResolvedLanguage();
          w.postMessage(
            { type: 'transcribe', audio: float32Audio, language: lang },
            [float32Audio.buffer] // Transfer ownership for zero-copy
          );
        } catch (e) {
          isTranscribing.value = false;
          error.value = 'Failed to process audio: ' + e.message;
          console.error('[STT] Audio processing error:', e);
        }
      };

      mediaRecorder.start(250); // Collect data every 250ms
      recording.value = true;
    } catch (e) {
      if (e.name === 'NotAllowedError') {
        error.value = 'Microphone permission denied';
      } else if (e.name === 'NotFoundError') {
        error.value = 'No microphone found on this device';
      } else {
        error.value = 'Failed to access microphone: ' + e.message;
      }
      console.error('[STT] Mic access error:', e);
    }
  }

  function stopRecording() {
    // Captured before the flag is cleared, and the count is gated on it:
    // onUnmounted calls this unconditionally, so without the guard every
    // teardown of a view holding this composable would report a recording that
    // never happened. It also keeps a second stop from counting twice.
    const wasRecording = recording.value;

    if (mediaRecorder && mediaRecorder.state !== 'inactive') {
      mediaRecorder.stop();
    }
    recording.value = false;

    if (wasRecording) {
      recordTelemetry(TELEMETRY_LOCAL_AI_RECORDING_END, resolveWorkspaceId());
    }
  }

  function toggleRecording() {
    if (native.isSupported) {
      if (nativeUnavailable.value) return;
      if (native.isListening.value) {
        native.stop();
      } else {
        error.value = '';
        native.start();
      }
      return;
    }

    // Initialize AudioContext directly on the user click gesture thread
    initAudioContext();

    if (isRecording.value) {
      stopRecording();
    } else if (!isTranscribing.value) {
      startRecording();
    }
  }

  // Cleanup on unmount
  onUnmounted(() => {
    native.abort();
    stopRecording();
    stream?.getTracks().forEach(t => t.stop());
    if (sharedAudioCtx) {
      sharedAudioCtx.close().catch(() => {});
    }
    // Don't terminate the worker — let it persist for reuse across views
  });

  return {
    isRecording,
    isTranscribing,
    isModelLoading,
    modelProgress,
    error,
    isSupported,
    toggleRecording,
  };
}
