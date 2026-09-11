<template>
  <div class="md-body">
    <template v-for="(segment, i) in segments" :key="i">
      <!-- Markdown, exactly as before: marked, then DOMPurify, then the DOM. -->
      <div v-if="segment.type === 'markdown'" v-html="renderMarkdown(segment.text)"></div>

      <!-- A fence somebody claimed. Drawn from what the extension answered,
           which is a view spec in the closed vocabulary — never its markup. -->
      <ExtensionNode v-else-if="drawn.get(segment.key)" :node="drawn.get(segment.key)" />

      <!-- Until the extension answers, and if it never usefully does, the
           fence stays what it was. A block that vanishes while something is
           being asked about it is worse than one that simply renders. -->
      <div v-else v-html="renderMarkdown(segment.fence)"></div>
    </template>
  </div>
</template>

<script setup>
/**
 * A message body, with the parts an extension claimed drawn by it.
 *
 * Every message used to go straight from `renderMarkdown` into `v-html` and
 * nothing was consulted on the way, so a ```mermaid fence was a code block and
 * no extension could make it anything else. This is the seam.
 *
 * ## What an extension is asked, and what it may answer
 *
 * It is handed the fence's *source* and answers with a view spec — the same
 * closed vocabulary every other surface uses. It cannot return markup, and the
 * spec is validated by `normaliseView` before anything is drawn, so a renderer
 * has exactly the reach every other contribution has.
 *
 * ## The fence stays a fence until something better exists
 *
 * Before the answer arrives, and if the answer is unusable, the original block
 * renders as markdown. A diagram appearing a moment later is a small surprise;
 * a code block disappearing and leaving nothing is a message that looks broken.
 *
 * ## Asked once per body, not per render
 *
 * `renderMarkdown` is called in a template and runs on every re-render. Asking
 * the main process there would be a bridge call per keystroke in the message
 * box, so the asking is a watcher keyed on the text.
 */
import { ref, shallowRef, watch } from 'vue';

import ExtensionNode from './ExtensionNode.vue';
import { renderMarkdown } from '../utils/markdown';
import { mayHaveBlocks, splitFences } from '../utils/markdownBlocks';
import { normaliseView } from '../composables/useExtensionView';
import { useExtensionRenderers } from '../composables/useExtensionRenderers';

const props = defineProps({
  text: { type: String, default: '' },
});

const renderers = useExtensionRenderers();

/** The body, cut into runs of markdown and the fences somebody claimed. */
const segments = shallowRef([{ type: 'markdown', text: props.text }]);

/** What each claimed fence became, by key. Empty until an extension answers. */
const drawn = ref(new Map());

/** Which pass is current, so a slow answer cannot land on a body that moved on. */
let pass = 0;

async function build() {
  const token = (pass += 1);
  const text = props.text ?? '';
  const languages = renderers.languages.value;

  // The common case, and it has to stay cheap: no claimed language, or no
  // fence at all, means one run and nothing asked of anybody.
  if (!mayHaveBlocks(text, languages)) {
    segments.value = text ? [{ type: 'markdown', text }] : [];
    drawn.value = new Map();
    return;
  }

  const parts = splitFences(text, languages).map((segment, i) => ({ ...segment, key: `${i}` }));
  segments.value = parts;
  drawn.value = new Map();

  for (const segment of parts) {
    if (segment.type !== 'block') continue;

    const answer = await renderers.render(segment.language, segment.source);
    if (token !== pass) return;
    if (!answer?.ok) continue;

    // Validated here rather than trusted: a renderer is an extension like any
    // other, and this is the one place a spec can be refused.
    const view = normaliseView(answer.view);
    if (!view.ok || view.view.nodes.length === 0) continue;

    drawn.value = new Map(drawn.value).set(
      segment.key,
      view.view.nodes.length === 1 ? view.view.nodes[0] : { type: 'group', label: '', children: view.view.nodes },
    );
  }
}

watch(() => [props.text, renderers.languages.value], build, { immediate: true });
</script>
