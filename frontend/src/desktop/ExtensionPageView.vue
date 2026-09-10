<template>
  <div class="flex-1 overflow-y-auto px-4 pb-10 custom-scrollbar">
    <div class="max-w-3xl mx-auto pt-8">

      <div class="flex items-baseline justify-between gap-4 mb-1">
        <h1 class="text-2xl font-black text-gray-900 dark:text-white truncate">
          {{ view?.title || entry?.label || pageId }}
        </h1>
        <button type="button" @click="open()" :disabled="busy"
                class="shrink-0 px-4 py-2 rounded-sm border border-gray-200 dark:border-zinc-800 text-[10px] font-black uppercase tracking-widest text-gray-700 dark:text-zinc-300 hover:bg-gray-50 dark:hover:bg-zinc-800 disabled:opacity-40 transition-colors">
          {{ busy ? 'Loading…' : 'Refresh' }}
        </button>
      </div>

      <!-- Named, always. A page drawn by somebody else's code says whose. -->
      <p class="text-[11px] text-gray-500 dark:text-zinc-400 mb-6">
        From the <strong class="font-semibold text-gray-700 dark:text-zinc-300">{{ name }}</strong> extension.
      </p>

      <div v-if="error"
           class="mb-6 px-3 py-2 rounded-sm border border-amber-200 dark:border-amber-900/60 bg-amber-50 dark:bg-amber-950/30 text-[11px] text-amber-800 dark:text-amber-300">
        {{ error }}
      </div>

      <div v-if="view" class="flex flex-col gap-3">
        <ExtensionNode v-for="(node, i) in view.nodes" :key="i" :node="node" @action="onAction" />
      </div>

      <!-- An extension that was uninstalled, or a link somebody kept. Said in
           words rather than left as an empty page that looks like a failure. -->
      <div v-else-if="!error && !busy"
           class="border border-dashed border-gray-200 dark:border-zinc-800 rounded-sm px-6 py-10 text-center">
        <p class="text-[11px] text-gray-500 dark:text-zinc-400">
          Nothing here. <strong class="font-semibold text-gray-700 dark:text-zinc-300">{{ name }}</strong>
          may have been uninstalled, or no longer offers this page.
        </p>
      </div>

    </div>
  </div>
</template>

<script setup>
/**
 * A page an extension contributed.
 *
 * Desktop only, and it lives here in `frontend/src/desktop/` rather than under
 * `desktop/` because Tailwind scans from the build root: a class used only in a
 * file outside this tree is silently dropped from the stylesheet, and the page
 * renders unstyled with no error to explain it.
 *
 * The extension's code never runs here. This asks the main process which entry
 * the route names, asks it to run that entry, and draws the description that
 * comes back with the application's own components — which is what keeps
 * third-party code away from a privileged origin that has a bridge to files,
 * the clipboard and the shell.
 */
import { computed, ref, watch } from 'vue';
import { useRoute } from 'vue-router';

import ExtensionNode from '../components/ExtensionNode.vue';
import { findEntry, useExtensionPages } from '../composables/useExtensionPages';

const route = useRoute();
const name = computed(() => String(route.params.name ?? ''));
const pageId = computed(() => String(route.params.pageId ?? ''));

const { pages, load, surfaces } = useExtensionPages();
const entry = ref(null);
const view = surfaces.panel;
const error = surfaces.error;
const busy = surfaces.busy;

async function open() {
  await load();
  entry.value = findEntry(pages.value, { name: name.value, pageId: pageId.value });
  if (!entry.value) {
    // Not an error: an extension can be uninstalled between somebody following
    // a link and the page opening. The empty state below says so.
    surfaces.dismiss();
    return;
  }
  await surfaces.invoke({ owner: entry.value.owner, id: entry.value.id, surface: 'page' }, {});
}

/**
 * A button inside the page, handed back to the entry that drew it.
 *
 * The action is never interpreted here — it is a string the extension chose and
 * this passes it back verbatim, which is the contract `normaliseNode` documents.
 */
function onAction(action) {
  if (!entry.value) return;
  surfaces.invoke({ owner: entry.value.owner, id: entry.value.id, surface: 'page' }, { action });
}

// Immediate, and re-run when the route changes: two extension pages share this
// component, so navigating between them would otherwise keep the first drawing.
watch([name, pageId], open, { immediate: true });
</script>
