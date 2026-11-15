<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- The controls at the right of a team page's title, like a workspace's own:
     the find-task hint, the task lists by status, then analytics and skills,
     the current one pressed. All for the team workspace in ?workspace=, or
     the whole team. Skills only with a workspace: a team has none of its own. -->
<template>
  <div class="flex items-center gap-2 shrink-0">
    <span class="hidden @min-[64rem]:flex items-center gap-1.5 mr-1 text-gray-400 dark:text-zinc-500 select-none">
      <kbd class="font-sans text-[9px] font-black uppercase tracking-widest border border-gray-200 dark:border-zinc-700 rounded px-1.5 py-[5px] [text-box:trim-both_cap_alphabetic] bg-white dark:bg-zinc-900">{{ findTaskHint.keys }}</kbd>
      <span class="text-[10px] font-medium">{{ findTaskHint.label }}</span>
    </span>

    <div class="relative flex items-center">
      <button data-test="team-actions-search" @click="openSearch"
              class="@min-[48rem]:hidden h-8 w-8 text-gray-500 hover:bg-gray-100 dark:hover:bg-zinc-800 bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-lg transition-all shadow-sm flex items-center justify-center shrink-0 mr-1"
              title="Search Tasks">
        <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" /></svg>
      </button>
      <button data-test="team-actions-filters" @click="showFilters = !showFilters"
              class="@min-[48rem]:hidden h-8 w-8 text-gray-500 hover:bg-gray-100 dark:hover:bg-zinc-800 bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-lg transition-all shadow-sm flex items-center justify-center shrink-0"
              :class="{ 'bg-gray-100 dark:bg-zinc-800 text-black dark:text-white border-black dark:border-white': showFilters }"
              title="Filters">
        <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M3 4h13M3 8h9m-9 4h6m4 0l4-4m0 0l4 4m-4-4v12" /></svg>
      </button>
      <div data-test="team-actions-statuses"
           class="h-8 p-0.5 bg-gray-100 dark:bg-zinc-800 rounded-md border border-gray-200 dark:border-zinc-700/50 overflow-x-auto no-scrollbar"
           :class="showFilters ? 'absolute top-10 right-0 z-50 flex shadow-2xl border-gray-900 dark:border-white w-max' : 'hidden @min-[48rem]:flex items-center'">
        <button v-for="f in STATUSES" :key="f.id" :data-test="`team-actions-status-${f.id}`"
                @click="go(`/tasks/${f.id}`)"
                @mouseenter="tooltipStore.show($event, f.label, 'bottom')"
                @mouseleave="tooltipStore.hide()"
                :aria-label="f.label"
                class="h-7 px-2 rounded-sm transition-all duration-200 flex items-center justify-center text-gray-500 dark:text-zinc-400 hover:text-gray-700 dark:hover:text-zinc-300">
          <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" :d="f.icon" /></svg>
        </button>
      </div>
    </div>

    <button v-for="b in buttons" :key="b.id" :data-test="b.test" :title="b.title" :aria-label="b.title"
            @click="go(b.path)"
            class="h-8 w-8 hover:text-black dark:hover:text-white hover:bg-gray-100 dark:hover:bg-zinc-800 border rounded-lg transition-all shadow-sm flex items-center justify-center"
            :class="b.id === current ? 'bg-gray-100 dark:bg-zinc-800 text-black dark:text-white border-black dark:border-white' : 'text-gray-500 dark:text-zinc-400 bg-white dark:bg-zinc-900 border-gray-100 dark:border-zinc-800'">
      <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" :d="b.icon" /></svg>
    </button>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useTeamMode } from '../composables/useTeamMode';
import { shortcutHint, usesCommandKey } from '../composables/useKeyboardShortcuts';
import { usePlatformStore } from '../stores/platformStore';
import { useTooltipStore } from '../stores/tooltipStore';

defineProps({ current: { type: String, default: '' } });

const route = useRoute();
const router = useRouter();
const { link } = useTeamMode();
const tooltipStore = useTooltipStore();
const platformStore = usePlatformStore();
const findTaskHint = computed(() => shortcutHint('find-task', { mac: usesCommandKey(platformStore.$state) }));
const workspace = computed(() => (typeof route.query.workspace === 'string' ? route.query.workspace : ''));
const showFilters = ref(false);

// The team task list's statuses: "Pending on Me" is about you, so a team has none.
const STATUSES = [
  { id: 'active', label: 'Active', icon: 'M4 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2V6zM14 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2zM14 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z' },
  { id: 'notstarted', label: 'Not Started', icon: 'M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2' },
  { id: 'ongoing', label: 'Ongoing', icon: 'M13 10V3L4 14h7v7l9-11h-7z' },
  { id: 'completed', label: 'Completed', icon: 'M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z' },
  { id: 'scheduled', label: 'Scheduled', icon: 'M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z' },
];
const ALL = [
  { id: 'analytics', test: 'team-analytics', title: 'Analytics', path: '/analytics', icon: 'M7 12l3-3 3 3 4-4M8 21l4-4 4 4M3 4h18M4 4h16v12a1 1 0 01-1 1H5a1 1 0 01-1-1V4z' },
  { id: 'skills', test: 'team-skills-link', title: 'Skills', path: '/skills', icon: 'M12 11.5v9M12 11.5c-2-1.3-4.5-2-7.5-2V19c3 0 5.5.7 7.5 2M12 11.5c2-1.3 4.5-2 7.5-2V19c-3 0-5.5.7-7.5 2M15.5 1.5c.3 1.6 1.4 2.7 3 3-1.6.3-2.7 1.4-3 3-.3-1.6-1.4-2.7-3-3 1.6-.3 2.7-1.4 3-3zM8 3.5c.2.9.6 1.3 1.5 1.5-.9.2-1.3.6-1.5 1.5-.2-.9-.6-1.3-1.5-1.5.9-.2 1.3-.6 1.5-1.5z' },
];
const buttons = computed(() => ALL.filter((b) => b.id !== 'skills' || workspace.value));

function go(path) {
  showFilters.value = false;
  tooltipStore.hide();
  router.push(link({ path, query: workspace.value ? { workspace: workspace.value } : {} }));
}

function openSearch() {
  window.dispatchEvent(new CustomEvent('open-command-palette'));
}
</script>
