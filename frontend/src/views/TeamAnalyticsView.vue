<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- A team's analytics: the workspace panels over every shared workspace, or
     one group of them (?group=), and who did what. -->
<template>
  <div class="flex-1 min-w-0 flex flex-col h-full page-surface overflow-hidden px-4">
    <div class="flex flex-wrap items-center gap-3 pb-4">
      <h1 class="text-lg font-black text-gray-900 dark:text-white">Analytics</h1>
      <select :value="group" data-test="team-analytics-group" aria-label="Group" @change="setGroup($event.target.value)"
              class="ml-auto bg-gray-50 dark:bg-zinc-800/50 border border-gray-200 dark:border-zinc-800 rounded-sm px-2 py-1 text-xs font-semibold text-gray-900 dark:text-zinc-100 outline-none">
        <option value="">All groups</option>
        <option v-for="g in groups" :key="g" :value="g">{{ g }}</option>
      </select>
    </div>
    <div class="flex-1 overflow-y-auto custom-scrollbar">
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
      >
        <template #after-summary>
          <div v-if="members.length" data-test="team-by-member" class="bg-white dark:bg-zinc-900/90 border border-gray-200 dark:border-zinc-800 rounded-sm p-5 shadow-sm">
            <h3 class="text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-50 mb-3">By member</h3>
            <table class="w-full text-xs">
              <thead>
                <tr class="text-left text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">
                  <th class="py-1.5 font-black">Member</th>
                  <th class="py-1.5 font-black text-right">Completed</th>
                  <th class="py-1.5 font-black text-right">Messages</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 dark:divide-zinc-800">
                <tr v-for="m in members" :key="m.key" data-test="team-member-row">
                  <td class="py-2"><OwnerAvatar :owner="m.owner" label /></td>
                  <td class="py-2 text-right tabular-nums font-bold text-gray-900 dark:text-zinc-100">{{ m.tasksCompleted.toLocaleString() }}</td>
                  <td class="py-2 text-right tabular-nums font-bold text-gray-900 dark:text-zinc-100">{{ m.messages.toLocaleString() }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </template>
      </StatsPanels>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { fetchTeamStats } from '../api';
import StatsPanels from '../components/StatsPanels.vue';
import OwnerAvatar from '../components/OwnerAvatar.vue';
import { useStatsRange, statsPalette } from '../composables/useStatsRange';
import { useThemeStore } from '../stores/themeStore';
import { useTeamStore } from '../stores/teamStore';
import { useToasts } from '../composables/useToasts';
import { lostTeam, teamGroups, useTeamMode } from '../composables/useTeamMode';

const route = useRoute();
const router = useRouter();
const themeStore = useThemeStore();
const teamStore = useTeamStore();
const { notifyError } = useToasts();
const { teamId, leaveTeam } = useTeamMode();

const palette = computed(() => statsPalette(themeStore.isDark));
const group = computed(() => (typeof route.query.group === 'string' ? route.query.group : ''));
const groups = computed(() => teamGroups(teamStore.workspacesOf(teamId.value)).map((g) => g.name));

const {
  stats, loading, activeRange, customFrom, customTo, load, setRange, apply,
  rangeOptions, chartFixedLength, chartEndDate, heatmapGranularity,
} = useStatsRange({
  fetchStats: (range, from, to) => fetchTeamStats(teamId.value, range, from, to, group.value),
  onError: (err) => (lostTeam(err) ? leaveTeam() : notifyError(err.message)),
});

const members = computed(() => (stats.value?.byMember || []).map((m) => ({
  key: String(m.owner?.userId ?? m.userId ?? m.owner?.name),
  owner: m.owner ?? { userId: m.userId, name: m.name, picture: m.picture },
  tasksCompleted: m.tasksCompleted ?? 0,
  messages: m.messages ?? 0,
})));

function onCustomFrom(value) {
  customFrom.value = value;
  apply();
}
function onCustomTo(value) {
  customTo.value = value;
  apply();
}

function setGroup(value) {
  const { group: _old, ...query } = route.query;
  return router.replace({ path: route.path, query: value ? { ...query, group: value } : query });
}

watch(group, load);

onMounted(() => {
  load();
  teamStore.loadWorkspaces(teamId.value).catch(() => {});
});
</script>
