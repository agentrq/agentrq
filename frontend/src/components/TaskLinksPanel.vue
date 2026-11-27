<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<template>
  <section v-if="links.total" class="border border-gray-200 dark:border-zinc-700 rounded-sm bg-white dark:bg-zinc-900 overflow-hidden min-w-0"
           :class="pinned ? ['flex-col', folded ? '' : 'w-52'] : 'w-full'" data-testid="task-links" data-message-links :data-pinned="pinned || null">
    <button type="button" @click="expanded = !expanded" :aria-expanded="expanded" :title="folded ? 'Links' : null"
            class="w-full shrink-0 flex items-center gap-2 px-3 py-1.5 text-left bg-gray-50 dark:bg-zinc-800/80 hover:bg-gray-100 dark:hover:bg-zinc-800 transition-colors min-w-0"
            data-testid="task-links-toggle">
      <svg class="w-3.5 h-3.5 shrink-0 text-gray-500 dark:text-zinc-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.899a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.1 1.1" /></svg>
      <span v-if="!folded" class="text-[10px] font-semibold text-gray-800 dark:text-zinc-200 shrink-0">Links</span>
      <span class="text-[9px] font-semibold text-gray-500 dark:text-zinc-400 shrink-0" data-testid="task-links-count">{{ links.total }}</span>
      <span v-if="!pinned" class="text-[11px] text-gray-600 dark:text-zinc-400 truncate min-w-0" data-testid="task-links-summary">· {{ summary }}</span>
      <span v-if="!folded" class="flex-1"></span>
      <svg v-if="!folded" class="w-3 h-3 shrink-0 text-gray-400 dark:text-zinc-500 transition-transform" :class="expanded ? 'rotate-180' : ''" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M19 9l-7 7-7-7" /></svg>
    </button>

    <div v-if="expanded" class="p-3 overflow-y-auto flex flex-col gap-2 min-w-0" :class="pinned ? 'min-h-0' : 'max-h-[45vh]'" data-testid="task-links-body">
      <div v-for="group in links.groups" :key="group.kind" class="min-w-0" :data-testid="`task-links-${group.kind}`">
        <span class="text-[9px] font-semibold uppercase tracking-wider text-gray-400 dark:text-zinc-500">{{ group.label }}</span>
        <ul class="mt-1 flex gap-1.5 min-w-0" :class="pinned ? 'flex-col' : 'flex-wrap'">
          <li v-for="link in group.links" :key="link.key" class="min-w-0 max-w-full">
            <RouterLink v-if="link.to" :to="link.to" :title="link.to" :class="chip" class="block truncate" @click="emit('follow')">{{ link.label }}</RouterLink>
            <a v-else-if="link.number" :href="link.href" target="_blank" rel="noopener noreferrer" :title="link.href"
               :class="chip" class="flex items-center gap-1 h-[calc(1.5em+6px)] font-mono [&>span]:[text-box:trim-both_cap_alphabetic]" @click="emit('follow')">
              <svg class="w-3 h-3 shrink-0" fill="currentColor" viewBox="0 0 24 24" aria-hidden="true" :data-testid="`task-links-${link.mark}`"><path :d="MARKS[link.mark]" /></svg><span class="shrink-0" data-testid="task-links-number">{{ link.sigil }}{{ link.number }}</span><span class="ml-auto pl-1 py-1 -my-1 truncate min-w-0 text-gray-400 dark:text-zinc-500" data-testid="task-links-repo">{{ link.repo }}</span></a>
            <a v-else :href="link.href" target="_blank" rel="noopener noreferrer" :title="link.href" :class="chip" class="block truncate" @click="emit('follow')">{{ link.label }}</a>
          </li>
        </ul>
      </div>
    </div>
  </section>
</template>

<script setup>
/**
 * Every link the task mentions: pull and merge requests, issues, Actions runs
 * and GitLab pipelines, Pano
 * docs, other tasks and the rest, so none has to be dug out of a long
 * conversation. The Pano docs linked to the task are listed too, mentioned or
 * not.
 *
 * Pinned, it is a small note over the conversation's corner, open until folded
 * to just its icon and count.
 * Otherwise it is a one-line bar under the plan that opens to the list, for a
 * view too narrow to have room beside the conversation.
 */
import { computed, ref } from 'vue';
import { serverOrigin } from '../api';
import { taskLinks } from '../composables/useTaskLinks';
import { GITHUB_MARK } from '../utils/githubConnect';

// The GitLab mark, from Simple Icons (CC0).
const GITLAB_MARK = 'm23.6004 9.5927-.0337-.0862L20.3.9814a.851.851 0 0 0-.3362-.405.8748.8748 0 0 0-.9997.0539.8748.8748 0 0 0-.29.4399l-2.2055 6.748H7.5375l-2.2057-6.748a.8573.8573 0 0 0-.29-.4412.8748.8748 0 0 0-.9997-.0537.8585.8585 0 0 0-.3362.4049L.4332 9.5015l-.0325.0862a6.0657 6.0657 0 0 0 2.0119 7.0105l.0113.0087.03.0213 4.976 3.7264 2.462 1.8633 1.4995 1.1321a1.0085 1.0085 0 0 0 1.2197 0l1.4995-1.1321 2.4619-1.8633 5.006-3.7489.0125-.01a6.0682 6.0682 0 0 0 2.0094-7.003z';
const MARKS = { github: GITHUB_MARK, gitlab: GITLAB_MARK };

const props = defineProps({
  task: { type: Object, required: true },
  taskId: { type: String, default: '' },
  pinned: { type: Boolean, default: false },
  // The Pano docs linked to the task, which the conversation may never name.
  panoDocs: { type: Array, default: () => [] },
});

const emit = defineEmits(['follow']);

const chip = 'max-w-full px-1.5 py-0.5 rounded border border-gray-200 dark:border-zinc-700 text-[11px] text-gray-700 dark:text-zinc-300 hover:border-gray-400 dark:hover:border-zinc-500 hover:text-gray-900 dark:hover:text-zinc-100 transition-colors';

const expanded = ref(props.pinned);
const folded = computed(() => props.pinned && !expanded.value);

// Links to this server are what make a task or Pano link. In the desktop app
// the page's own origin is `app://`, so the server is asked for its address.
const origin = ref(window.location.origin);
Promise.resolve()
  .then(() => serverOrigin())
  .then((o) => { origin.value = o; })
  .catch(() => {});

const links = computed(() => taskLinks(props.task, { origin: origin.value, taskId: props.taskId, panoDocs: props.panoDocs }));
const summary = computed(() => links.value.groups.flatMap((g) => g.links.map((l) => l.label)).join(' · '));
</script>
