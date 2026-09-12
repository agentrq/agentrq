<!-- Copyright 2026 Contextual, Inc. https://agentrq.com -->

<template>
  <figure class="my-2 border border-gray-200 dark:border-zinc-800 rounded-lg overflow-hidden bg-white dark:bg-zinc-900">

    <figcaption class="flex items-center justify-between gap-3 px-3 py-1.5 border-b border-gray-100 dark:border-zinc-800 bg-gray-50 dark:bg-zinc-950/40">
      <span class="text-[10px] font-bold uppercase tracking-wider text-gray-400 dark:text-zinc-500 truncate">
        {{ label || format }}
      </span>
      <!-- The toggle. It belongs to the block rather than to whoever drew it,
           so every diagram behaves the same and an extension gets it free. -->
      <button type="button" @click="showSource = !showSource"
              class="shrink-0 text-[10px] font-bold uppercase tracking-wider text-gray-500 dark:text-zinc-400 hover:text-black dark:hover:text-white transition-colors">
        {{ showSource ? 'Diagram' : 'Text' }}
      </button>
    </figcaption>

    <!-- The source, as written. Interpolated, never `v-html`: it is the thing
         the diagram was built from and is no safer for having been drawn. -->
    <pre v-if="showSource"
         class="m-0 px-3 py-2.5 text-[12px] leading-relaxed overflow-x-auto custom-scrollbar text-gray-800 dark:text-zinc-200"><code>{{ source }}</code></pre>

    <div v-else-if="error" class="px-3 py-2.5">
      <p class="text-[11px] text-amber-700 dark:text-amber-400">{{ error }}</p>
      <!-- Shown anyway when it will not draw: the text is what somebody needs
           in order to fix it, and hiding it leaves them with only a complaint. -->
      <pre class="mt-2 m-0 text-[12px] leading-relaxed overflow-x-auto custom-scrollbar text-gray-600 dark:text-zinc-400"><code>{{ source }}</code></pre>
    </div>

    <!-- The drawing, and only the drawing. Everything around it — this border,
         the caption, the label, the toggle — is out here, which is what keeps a
         diagram looking like every other block on the page. -->
    <div v-else-if="url" class="px-3 py-2.5 overflow-x-auto custom-scrollbar">
      <DrawerFrame :url="url" :source="source" @failed="onFailed" />
    </div>

    <p v-else class="px-3 py-2.5 text-[11px] text-gray-400 dark:text-zinc-500">Drawing…</p>

  </figure>
</template>

<script setup>
/**
 * A diagram, and the text it was drawn from.
 *
 * **Nothing is drawn here.** The extension that claimed the fence supplies the
 * code, and it runs in a sandboxed frame with an opaque origin — see
 * `useDrawerFrame.js`. This component decides what the block shows and never
 * touches the drawing itself.
 *
 * That is why there is no `v-html` in this file any more. There used to be: the
 * host drew mermaid itself, turned it into an `<svg>` string, and put it in the
 * page behind a sanitiser. The frame replaces that entirely — markup produced
 * from an extension's source now stays inside a document that can reach
 * nothing, so there is no sanitiser to get right and no third-party markup on a
 * privileged origin at all.
 */
import { ref, watch } from 'vue';

import DrawerFrame from './DrawerFrame.vue';
import { createDrawerSource } from '../composables/useDrawerFrame';

const props = defineProps({
  format: { type: String, required: true },
  source: { type: String, required: true },
  label: { type: String, default: '' },
});

const drawers = createDrawerSource();

/** Where the frame imports the drawer from. The page never holds the code. */
const url = ref('');
const error = ref('');
const showSource = ref(false);

/** Which lookup is the current one, so a slow answer cannot land on a new source. */
let finding = 0;

async function find() {
  const token = (finding += 1);
  url.value = '';
  error.value = '';

  const found = await drawers.find(props.format);
  if (token !== finding) return;

  if (found.ok) url.value = found.url;
  // Said rather than left blank: a format nothing draws is the commonest reason
  // a block does not become a picture, and the reader is owed the sentence.
  else error.value = found.reason || `Nothing installed draws ${props.format}.`;
}

/** A drawer that threw, or never answered. The source is still worth showing. */
function onFailed(reason) {
  url.value = '';
  error.value = reason || 'This could not be drawn.';
}

// The format is watched as well as the source: it decides which drawer runs.
watch(() => [props.format, props.source], find, { immediate: true });
</script>
