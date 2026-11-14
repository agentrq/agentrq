<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- A team's analytics: a workspace's panels, summed over the team's
     workspaces, or one team workspace's (?workspace=). -->
<template>
  <div class="@container flex-1 min-w-0 flex flex-col h-full page-surface overflow-hidden">
    <div class="w-full px-4 py-2 mb-6 shrink-0 flex flex-row items-start justify-between gap-4">
      <div class="flex flex-col min-w-0 flex-1">
        <h1 class="text-lg @min-[48rem]:text-2xl font-black text-gray-800 dark:text-zinc-200 truncate leading-tight">Analytics</h1>
        <p data-test="team-analytics-scope" class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5 truncate">{{ workspace || 'All workspaces' }}</p>
      </div>
      <button data-test="team-analytics-tasks" @click="router.push(link({ path: '/tasks/active', query: workspace ? { workspace } : {} }))"
              class="h-8 w-8 text-gray-500 dark:text-zinc-400 hover:text-black dark:hover:text-white hover:bg-gray-100 dark:hover:bg-zinc-800 bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-lg transition-all shadow-sm flex items-center justify-center shrink-0" title="Tasks">
        <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4 6h16M4 12h16M4 18h16" /></svg>
      </button>
    </div>
    <div class="flex-1 overflow-y-auto custom-scrollbar px-4">
      <StatsPanels
        :stats="stats"
        :loading="loading"
        :active-range="activeRange"
        :range-options="rangeOptions"
        :custom-from="customFrom"
        :custom-to="customTo"
        :chart-fixed-length="chartFixedLength"
        :chart-end-date="chartEndDate"
        :heatmap-granularity="heatmapGranularity"
        :palette="palette"
        @update:active-range="setRange"
        @update:custom-from="onCustomFrom"
        @update:custom-to="onCustomTo"
        @apply="apply"
      />
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { fetchTeamStats } from '../api';
import StatsPanels from '../components/StatsPanels.vue';
import { useStatsRange, statsPalette } from '../composables/useStatsRange';
import { useThemeStore } from '../stores/themeStore';
import { useToasts } from '../composables/useToasts';
import { lostTeam, useTeamMode } from '../composables/useTeamMode';

const route = useRoute();
const router = useRouter();
const themeStore = useThemeStore();
const { notifyError } = useToasts();
const { teamId, link, leaveTeam } = useTeamMode();

const palette = computed(() => statsPalette(themeStore.isDark));
const workspace = computed(() => (typeof route.query.workspace === 'string' ? route.query.workspace : ''));

const {
  stats, loading, activeRange, customFrom, customTo, load, setRange, apply,
  rangeOptions, chartFixedLength, chartEndDate, heatmapGranularity,
} = useStatsRange({
  fetchStats: (range, from, to) => fetchTeamStats(teamId.value, range, from, to, workspace.value),
  onError: (err) => (lostTeam(err) ? leaveTeam() : notifyError(err.message)),
});

function onCustomFrom(value) {
  customFrom.value = value;
  apply();
}
function onCustomTo(value) {
  customTo.value = value;
  apply();
}

// Picking another team or team workspace in the sidebar changes only the query.
watch(() => [teamId.value, workspace.value], load);

onMounted(load);
</script>
