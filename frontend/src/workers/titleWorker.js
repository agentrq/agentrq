import { pipeline, env } from '@huggingface/transformers';

// Skip local check, we'll fetch from HF Hub using WASM
env.allowLocalModels = false;

// We'll use a more capable model (248M parameters) for better instruction following
const MODEL_NAME = 'Xenova/LaMini-Flan-T5-248M';

class TitleGenerationPipeline {
  static task = 'text2text-generation';
  static model = MODEL_NAME;
  static instance = null;

  static async getInstance(progress_callback = null) {
    if (this.instance === null) {
      this.instance = await pipeline(this.task, this.model, {
        progress_callback,
        device: 'wasm', // We prefer WASM for broad compatibility, though webgpu is possible
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

      // Format the prompt for LaMini-Flan-T5
      const prompt = `Write a very short, concise task title (max 6 words) for the following task description:\n\n${data.text}`;

      const output = await generator(prompt, {
        max_new_tokens: 15,
        temperature: 0.3, // Low temperature for deterministic/focused titles
        repetition_penalty: 1.2,
      });

      let title = output[0].generated_text.trim();

      // Cleanup common artifacts if the model gets chatty
      title = title.replace(/^Title:\s*/i, '').replace(/^"|"$/g, '').trim();

      self.postMessage({ id, type: 'SUCCESS', data: { title } });
    } catch (error) {
      self.postMessage({ id, type: 'ERROR', error: error.message });
    }
  }
});
