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

    <div v-else-if="drawn" class="px-3 py-2.5 overflow-x-auto custom-scrollbar agentrq-diagram" v-html="drawn"></div>

    <p v-else class="px-3 py-2.5 text-[11px] text-gray-400 dark:text-zinc-500">Drawing…</p>

  </figure>
</template>

<script setup>
/**
 * A diagram, and the text it was drawn from.
 *
 * The `v-html` here is the only one in anything an extension can reach, and it
 * is deliberate: mermaid turns text into an `<svg>` string, and there is no
 * other way to put that in the document. What makes it safe is that the string
 * is produced by *this application's* dependency from source that never
 * contained markup, and then passed through DOMPurify's SVG profile — which
 * removes `foreignObject`, the element that would otherwise let HTML back in
 * through a diagram label. See `useDiagram.js`.
 *
 * Redrawn when the theme changes, because mermaid bakes its colours into the
 * SVG rather than reading them from the page.
 */
import { ref, watch } from 'vue';
import { storeToRefs } from 'pinia';

import { renderDiagram } from '../composables/useDiagram';
import { useThemeStore } from '../stores/themeStore';

const props = defineProps({
  format: { type: String, required: true },
  source: { type: String, required: true },
  label: { type: String, default: '' },
});

const themeStore = useThemeStore();
const { isDark } = storeToRefs(themeStore);

/** What the drawer produced: mermaid's SVG, or KaTeX's HTML and MathML. */
const drawn = ref('');
const error = ref('');
const showSource = ref(false);

/** Which draw is the current one, so a slow answer cannot land on a new source. */
let drawing = 0;

async function draw() {
  const token = (drawing += 1);
  drawn.value = '';
  error.value = '';

  const result = await renderDiagram(props.format, props.source, { theme: isDark.value ? 'dark' : 'light' });
  if (token !== drawing) return;

  if (result.ok) drawn.value = result.html;
  else error.value = result.reason;
}

// The format is watched too: it decides which drawer runs, so a node that
// changes format has to be redrawn rather than keeping the old picture.
watch(() => [props.format, props.source, isDark.value], draw, { immediate: true });
</script>

<style>
/* Mermaid sizes to its content; this keeps a wide diagram inside the message
   rather than pushing the conversation sideways. */
.agentrq-diagram svg {
  max-width: 100%;
  height: auto;
}
</style>
