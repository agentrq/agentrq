<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<!-- A team's analytics: a workspace's panels, summed over the team's
     workspaces, or one team workspace's (?workspace=). -->
<template>
  <div class="@container flex-1 min-w-0 flex flex-col h-full page-surface overflow-hidden">
    <div class="w-full px-4 py-2 mb-6 shrink-0 flex flex-row items-start justify-between gap-4">
      <div class="flex flex-col min-w-0 flex-1">
        <h1 class="text-lg @min-[48rem]:text-2xl font-black text-gray-800 dark:text-zinc-200 tracking-tight truncate leading-tight">
          <!-- Spans, not <template>s: see WorkspaceDetailView's heading. -->
          <span v-if="workspace">
            <span data-test="team-analytics-scope" class="opacity-50 cursor-pointer hover:opacity-100" @click="openTasks">{{ workspace }}</span>
            <span class="mx-1.5 text-gray-300 dark:text-zinc-700 font-medium">/</span>
            <span class="text-gray-400 dark:text-zinc-500">Analytics</span>
          </span>
          <span v-else>Analytics</span>
        </h1>
        <p v-if="!workspace" data-test="team-analytics-scope" class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5 truncate">All workspaces</p>
      </div>
      <TeamWorkspaceActions current="analytics" />
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
import TeamWorkspaceActions from '../components/TeamWorkspaceActions.vue';
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

function openTasks() {
  router.push(link({ path: '/tasks/active', query: { workspace: workspace.value } }));
}
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
