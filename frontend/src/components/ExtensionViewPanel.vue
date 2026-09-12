<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
-->

<template>
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 px-4" @click.self="$emit('close')">
    <div class="w-full max-w-md bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl shadow-sm max-h-[80vh] flex flex-col">

      <div class="flex items-baseline justify-between gap-3 px-5 pt-4 pb-3 border-b border-gray-100 dark:border-zinc-800">
        <h2 class="text-sm font-black text-gray-900 dark:text-white truncate">{{ view.title || view.owner }}</h2>
        <!-- Named, always. A panel that appeared from a right-click and does not
             say which extension drew it is one nobody can attribute or remove. -->
        <span class="shrink-0 text-[10px] font-bold uppercase tracking-wider text-gray-400 dark:text-zinc-500">
          {{ view.owner }}
        </span>
      </div>

      <div class="px-5 py-4 overflow-y-auto custom-scrollbar flex flex-col gap-3">
        <ExtensionNode v-for="(node, i) in view.nodes" :key="i" :node="node" @action="$emit('action', $event)" />
      </div>

      <div class="px-5 py-3 border-t border-gray-100 dark:border-zinc-800 flex justify-end">
        <button type="button" @click="$emit('close')"
                class="px-4 py-2 rounded-lg border border-gray-200 dark:border-zinc-800 text-[11px] font-black uppercase tracking-widest text-gray-700 dark:text-zinc-300 hover:bg-gray-50 dark:hover:bg-zinc-800 transition-colors">
          Close
        </button>
      </div>

    </div>
  </div>
</template>

<script setup>
/**
 * What an extension drew.
 *
 * The spec reaching this component has already been through `normaliseView`:
 * every node type is known, every string is clamped, every tone is one of ours
 * and every href is either http(s) or absent. So this draws without checking,
 * and there is exactly one place where a spec can be refused.
 *
 * There is no `v-html` here and there will never be one. Extension text often
 * did not originate in the extension — it came from an issue title, a commit
 * message, a webhook payload — and interpolation is what keeps that from being
 * anybody's problem to remember.
 */
import ExtensionNode from './ExtensionNode.vue';

defineProps({ view: { type: Object, required: true } });
defineEmits(['close', 'action']);
</script>
