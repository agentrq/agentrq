// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { pipeline, env } from '@huggingface/transformers';
import { titleMessages, cleanTitle, TITLE_MAX_NEW_TOKENS } from '../utils/titlePrompt.js';

// Skip local check, we'll fetch from HF Hub using WASM
env.allowLocalModels = false;

// AgentRQ's own title model: IBM Granite 4.0 350M (Apache-2.0) fine-tuned for
// task titles, Apache-2.0. Its 4-bit export uses only operators both
// onnxruntime-web backends have, so one file runs on WebGPU and on WASM.
const MODEL_NAME = 'agentrq/task-title-350m-ONNX';
// A tag, never 'main': the browser caches the model by its URL, so a new
// upload to 'main' would never reach anyone who already has the old one.
const MODEL_REVISION = 'v2';

class TitleGenerationPipeline {
  static task = 'text-generation';
  static model = MODEL_NAME;
  static instance = null;

  static async getInstance(progress_callback = null) {
    if (this.instance === null) {
      this.instance = await pipeline(this.task, this.model, {
        progress_callback,
        // WebGPU where the browser has a GPU adapter, else WASM; same file.
        device: (await self.navigator?.gpu?.requestAdapter?.()) ? 'webgpu' : 'wasm',
        dtype: 'q4',
        revision: MODEL_REVISION,
      });
    }
    return this.instance;
  }
}

self.addEventListener('message', async (e) => {
  const { id, type, data } = e.data;

  if (type === 'GENERATE_TITLE') {
    try {
      const generator = await TitleGenerationPipeline.getInstance((x) => {
        self.postMessage({ id, type: 'PROGRESS', data: x });
      });

      const output = await generator(titleMessages(data.text), {
        max_new_tokens: TITLE_MAX_NEW_TOKENS,
        do_sample: false,
      });

      const title = cleanTitle(output[0].generated_text.at(-1).content);

      self.postMessage({ id, type: 'SUCCESS', data: { title } });
    } catch (error) {
      self.postMessage({ id, type: 'ERROR', error: error.message });
    }
  }
});
