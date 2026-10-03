// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { ref, watch, onUnmounted } from 'vue';
import TitleWorker from '../workers/titleWorker.js?worker';
import { recordTelemetry, TELEMETRY_LOCAL_AI_TITLE_GENERATE } from '../api';

// Whether this browser can run the local title model at all. Shared with the
// background naming of untitled tasks, so the two agree.
export function localTitleModelSupported() {
  return typeof window !== 'undefined' &&
    typeof Worker !== 'undefined' &&
    typeof WebAssembly === 'object' &&
    typeof WebAssembly.instantiate === 'function';
}

// workspaceId is optional and only used to scope usage telemetry; it may be a
// plain string, a ref or a getter, matching however the caller's route exposes
// it. Without one the generation still runs, it just goes uncounted.
export function useAutoTitle(descriptionRef, titleRef, workspaceId) {
  const isGenerating = ref(false);
  const isModelLoading = ref(false);
  const modelProgress = ref(0);
  const isOverridden = ref(false);
  
  let worker = null;
  let debounceTimer = null;
  const currentMessageId = ref(0);

  // Initialize Worker on demand
  const getWorker = () => {
    if (!worker) {
      worker = new TitleWorker();
      worker.addEventListener('message', onWorkerMessage);
    }
    return worker;
  };

  const onWorkerMessage = (e) => {
    const { id, type, data, error } = e.data;
    
    // Ignore old messages if we dispatched a newer request
    if (id !== currentMessageId.value && type !== 'PROGRESS') return;

    switch (type) {
      case 'PROGRESS':
        if (data.status === 'initiate') {
          isModelLoading.value = true;
          modelProgress.value = 0;
        } else if (data.status === 'progress') {
          isModelLoading.value = true;
          modelProgress.value = Math.round(data.progress);
        } else if (data.status === 'done' || data.status === 'ready') {
          isModelLoading.value = false;
        }
        break;
      
      case 'SUCCESS':
        if (!isOverridden.value) {
          titleRef.value = data.title;
        }
        isGenerating.value = false;
        break;
        
      case 'ERROR':
        console.error('Title generation error:', error);
        isGenerating.value = false;
        break;
    }
  };

  const isSupported = localTitleModelSupported();

  const generateTitle = () => {
    if (!isSupported) return;
    const text = descriptionRef.value || '';
    if (text.trim().length < 5) return;

    // Counted after the guards above, so a click that generates nothing —
    // unsupported browser, or too little text to work from — is not recorded
    // as a use of the model.
    recordTelemetry(
      TELEMETRY_LOCAL_AI_TITLE_GENERATE,
      typeof workspaceId === 'function' ? workspaceId() : (workspaceId?.value ?? workspaceId)
    );

    // Asking for a title takes back a title typed earlier, or the answer would
    // be dropped as overridden. Typing again while it runs still keeps theirs.
    isOverridden.value = false;
    isGenerating.value = true;
    const w = getWorker();
    currentMessageId.value = Date.now();

    w.postMessage({
      id: currentMessageId.value,
      type: 'GENERATE_TITLE',
      data: { text: text.trim() }
    });
  };

  const markOverridden = () => {
    isOverridden.value = true;
  };

  onUnmounted(() => {
    if (worker) {
      worker.terminate();
    }
  });

  return {
    isSupported,
    isGenerating,
    isModelLoading,
    modelProgress,
    isOverridden,
    markOverridden,
    generateTitle
  };
}
