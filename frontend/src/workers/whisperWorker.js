/**
 * Whisper Web Worker — matches whisperweb.dev approach.
 *
 * Uses the pipeline API from @huggingface/transformers.
 *
 * Tries WebGPU first (fast), falls back to WASM (universal).
 */
import { pipeline } from '@huggingface/transformers';

const isMobile = /Android|webOS|iPhone|iPad|iPod|BlackBerry|IEMobile|Opera Mini/i.test(navigator.userAgent);
const MODEL_ID = isMobile ? 'onnx-community/whisper-tiny.en' : 'onnx-community/whisper-base';

let pipe = null;
let isLoading = false;

async function loadModel(progressCb) {
  if (pipe) return;
  if (isLoading) return; // prevent double-load
  isLoading = true;

  // Detect WebGPU support
  let device = 'wasm';
  let dtype = { encoder_model: 'fp32', decoder_model_merged: 'q4' };

  if (typeof navigator !== 'undefined' && navigator.gpu) {
    try {
      const adapter = await navigator.gpu.requestAdapter();
      if (adapter) {
        device = 'webgpu';
      }
    } catch {
      // WebGPU not available, stay on WASM
    }
  }

  // For WASM fallback, use q8 quantization across the board
  if (device === 'wasm') {
    dtype = 'q8';
  }

  pipe = await pipeline('automatic-speech-recognition', MODEL_ID, {
    device,
    dtype,
    progress_callback: progressCb,
  });

  isLoading = false;
}

async function transcribe(audio, language) {
  const isMultilingual = !MODEL_ID.endsWith('.en') && !MODEL_ID.includes('moonshine');
  
  let outputs;
  if (isMultilingual) {
    const options = {
      language: language || 'en',
      task: 'transcribe',
    };
    outputs = await pipe(audio, options);
  } else {
    outputs = await pipe(audio);
  }
  
  return outputs.text ? outputs.text.trim() : '';
}

self.onmessage = async (event) => {
  const { type, audio, language } = event.data;

  if (type === 'transcribe') {
    try {
      // Load model if needed
      if (!pipe) {
        self.postMessage({ status: 'loading', progress: 0 });

        await loadModel((progress) => {
          if (progress.status === 'progress') {
            self.postMessage({
              status: 'loading',
              progress: Math.round(progress.progress || 0),
              file: progress.file,
            });
          } else if (progress.status === 'done') {
            self.postMessage({ status: 'loading', progress: 100 });
          }
        });

        self.postMessage({ status: 'ready' });
      }

      self.postMessage({ status: 'transcribing' });

      const text = await transcribe(audio, language);

      self.postMessage({
        status: 'complete',
        text,
      });
    } catch (err) {
      self.postMessage({
        status: 'error',
        error: err.message || 'Transcription failed',
      });
    }
  }
};
