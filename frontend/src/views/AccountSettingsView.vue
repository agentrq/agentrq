<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<!-- The account's own settings, which hold in every workspace, laid out like a
     workspace's: the clock times are shown with, and the presets every
     workspace offers. The tab is the route's ?tab=, so a link opens one. Each
     change applies at once; there is no Save. -->
<template>
  <div class="flex flex-col h-full w-full min-w-0 bg-transparent overflow-y-auto custom-scrollbar" data-test="account-settings">
    <div class="w-full px-4 py-2 mb-6 shrink-0 flex flex-col min-w-0">
      <h1 class="text-lg md:text-2xl font-black text-gray-800 dark:text-zinc-200 leading-tight">Settings</h1>
      <p class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5">Account settings · these hold in every workspace</p>
    </div>

    <div class="flex-1 px-4 pb-20 min-w-0">
      <div class="w-full min-w-0 flex flex-col md:flex-row gap-8">
        <div class="w-full md:w-48 shrink-0">
          <nav class="flex flex-col gap-1 sticky top-0" role="tablist">
            <button v-for="t in TABS" :key="t.id" type="button" role="tab" :aria-selected="tab === t.id" :data-test="`settings-tab-${t.id}`" @click="select(t.id)"
                    :class="tab === t.id ? 'bg-gray-900 text-white dark:bg-white dark:text-zinc-900 shadow-lg shadow-black/5' : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-100 dark:hover:bg-zinc-800'"
                    class="flex items-center gap-3 px-4 py-2.5 rounded-sm text-[10px] font-bold uppercase tracking-widest transition-all text-left">
              <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" :d="t.icon" /></svg>
              {{ t.label }}
            </button>
          </nav>
        </div>

        <div class="flex-1 min-w-0">
          <div class="bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-sm shadow-sm overflow-hidden min-h-[500px] p-4 sm:p-8">
            <div v-if="tab === 'general'" class="space-y-8 animate-in fade-in slide-in-from-bottom-2 duration-300">
              <fieldset data-test="time-format" class="space-y-2">
                <legend class="block text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest ml-1 mb-2">Time Format</legend>
                <SegmentedControl v-model="picked" :options="TIME_FORMAT_OPTIONS" class="ml-1" />
                <p class="text-[9px] text-gray-500 dark:text-zinc-500 font-bold uppercase tracking-wider ml-1 mt-2">
                  Every time in the app is shown this way. It is now <span data-test="time-format-example" class="text-gray-900 dark:text-zinc-100">{{ example }}</span>.
                </p>
              </fieldset>
            </div>

            <WorkspacePresetsPanel v-else-if="tab === 'presets'" />
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import SegmentedControl from '../components/SegmentedControl.vue';
import WorkspacePresetsPanel from '../components/WorkspacePresetsPanel.vue';
import { useToasts } from '../composables/useToasts';
import { onWebMCPChange } from '../composables/useWebMCPChanges';
import {
  TIME_FORMAT_12H, TIME_FORMAT_24H, TIME_FORMAT_BROWSER, loadTimeFormat, saveTimeFormat, timeFormat, withClock,
} from '../composables/useTimeFormat';

const TABS = [
  { id: 'general', label: 'General', icon: 'M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z' },
  { id: 'presets', label: 'Presets', icon: 'M7 8h10M7 12h4m1 8l-4-4H5a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v8a2 2 0 01-2 2h-3l-4 4z' },
];

const TIME_FORMAT_OPTIONS = [
  { value: TIME_FORMAT_BROWSER, label: 'Browser', test: 'time-format-browser' },
  { value: TIME_FORMAT_12H, label: '12h', test: 'time-format-12h' },
  { value: TIME_FORMAT_24H, label: '24h', test: 'time-format-24h' },
];

const route = useRoute();
const router = useRouter();
const { notifyError } = useToasts();

const tab = computed(() => (TABS.some((t) => t.id === route.query.tab) ? route.query.tab : 'general'));
const select = (id) => router.replace({ path: '/settings', query: { tab: id } });

// A pick is shown at once and saved; a refusal puts the last one back.
const picked = computed({
  get: () => timeFormat.value,
  set: (format) => saveTimeFormat(format).catch((e) => notifyError(e?.message || 'Failed to save account settings')),
});

// Read when drawn, so it follows the pick.
const example = computed(() => new Date().toLocaleTimeString('en-US', withClock({ hour: '2-digit', minute: '2-digit' })));

// The clock is read once, at sign-in, and kept by every save; only a browser
// agent's change needs it read again.
onWebMCPChange(() => loadTimeFormat());
</script>
