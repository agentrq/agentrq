<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<template>
  <section v-if="links.total" class="border border-gray-200 dark:border-zinc-700 rounded-sm bg-white dark:bg-zinc-900 overflow-hidden min-w-0"
           :class="pinned ? 'flex-col' : 'w-full'" data-testid="task-links" :data-pinned="pinned || null">
    <button type="button" @click="expanded = !expanded" :aria-expanded="expanded"
            class="w-full shrink-0 flex items-center gap-2 px-3 py-1.5 text-left bg-gray-50 dark:bg-zinc-800/80 hover:bg-gray-100 dark:hover:bg-zinc-800 transition-colors min-w-0"
            data-testid="task-links-toggle">
      <svg class="w-3.5 h-3.5 shrink-0 text-gray-500 dark:text-zinc-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.899a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.1 1.1" /></svg>
      <span class="text-[10px] font-semibold text-gray-800 dark:text-zinc-200 shrink-0">Links</span>
      <span class="text-[9px] font-semibold text-gray-500 dark:text-zinc-400 shrink-0" data-testid="task-links-count">{{ links.total }}</span>
      <span v-if="!pinned" class="text-[11px] text-gray-600 dark:text-zinc-400 truncate min-w-0" data-testid="task-links-summary">· {{ summary }}</span>
      <span class="flex-1"></span>
      <svg class="w-3 h-3 shrink-0 text-gray-400 dark:text-zinc-500 transition-transform" :class="expanded ? 'rotate-180' : ''" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M19 9l-7 7-7-7" /></svg>
    </button>

    <div v-if="expanded" class="p-3 overflow-y-auto flex flex-col gap-2 min-w-0" :class="pinned ? 'min-h-0' : 'max-h-[45vh]'" data-testid="task-links-body">
      <div v-for="group in links.groups" :key="group.kind" class="min-w-0" :data-testid="`task-links-${group.kind}`">
        <span class="text-[9px] font-semibold uppercase tracking-wider text-gray-400 dark:text-zinc-500">{{ group.label }}</span>
        <ul class="mt-1 flex gap-1.5 min-w-0" :class="pinned ? 'flex-col' : 'flex-wrap'">
          <li v-for="link in group.links" :key="link.key" class="min-w-0 max-w-full">
            <RouterLink v-if="link.to" :to="link.to" :title="link.to" :class="chip" class="block truncate" @click="emit('follow')">{{ link.label }}</RouterLink>
            <a v-else-if="link.kind === 'pr'" :href="link.href" target="_blank" rel="noopener noreferrer" :title="link.href"
               :class="chip" class="flex items-center gap-1 font-mono" @click="emit('follow')">
              <svg class="w-3 h-3 shrink-0" fill="currentColor" viewBox="0 0 24 24" aria-hidden="true" data-testid="task-links-github"><path :d="GITHUB_MARK" /></svg><span class="truncate">{{ link.label }}</span></a>
            <a v-else :href="link.href" target="_blank" rel="noopener noreferrer" :title="link.href" :class="chip" class="block truncate" @click="emit('follow')">{{ link.label }}</a>
          </li>
        </ul>
      </div>
    </div>
  </section>
</template>

<script setup>
/**
 * Every link the task mentions: pull requests, Pano docs, other tasks and the
 * rest, so none has to be dug out of a long conversation.
 *
 * Pinned, it is a small note over the conversation's corner, open until folded.
 * Otherwise it is a one-line bar under the plan that opens to the list, for a
 * view too narrow to have room beside the conversation.
 */
import { computed, ref } from 'vue';
import { serverOrigin } from '../api';
import { taskLinks } from '../composables/useTaskLinks';

const props = defineProps({
  task: { type: Object, required: true },
  taskId: { type: String, default: '' },
  pinned: { type: Boolean, default: false },
});

const emit = defineEmits(['follow']);

// GitHub's mark, as the sign-in page draws it.
const GITHUB_MARK = 'M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0112 6.844c.85.004 1.705.115 2.504.337 1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.019 10.019 0 0022 12.017C22 6.484 17.522 2 12 2z';

const chip = 'max-w-full px-1.5 py-0.5 rounded border border-gray-200 dark:border-zinc-700 text-[11px] text-gray-700 dark:text-zinc-300 hover:border-gray-400 dark:hover:border-zinc-500 hover:text-gray-900 dark:hover:text-zinc-100 transition-colors';

const expanded = ref(props.pinned);

// Links to this server are what make a task or Pano link. In the desktop app
// the page's own origin is `app://`, so the server is asked for its address.
const origin = ref(window.location.origin);
Promise.resolve()
  .then(() => serverOrigin())
  .then((o) => { origin.value = o; })
  .catch(() => {});

const links = computed(() => taskLinks(props.task, { origin: origin.value, taskId: props.taskId }));
const summary = computed(() => links.value.groups.flatMap((g) => g.links.map((l) => l.label)).join(' · '));
</script>
