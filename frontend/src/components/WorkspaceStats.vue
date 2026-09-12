<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
-->

<template>
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
</template>

<script setup>
/**
 * One workspace's analytics. The account-wide counterpart is `AccountStats.vue`
 * — both are thin: the panels come from `StatsPanels`, the range behaviour from
 * `useStatsRange`, and all either adds is which endpoint to call.
 */
import { computed, onMounted, watch } from 'vue';
import { fetchWorkspaceStats } from '../api';
import StatsPanels from './StatsPanels.vue';
import { useStatsRange, statsPalette } from '../composables/useStatsRange';
import { useThemeStore } from '../stores/themeStore';

const props = defineProps({
  workspaceId: { type: [String, Number], required: true },
});

const themeStore = useThemeStore();
const palette = computed(() => statsPalette(themeStore.isDark));

const {
  stats,
  loading,
  activeRange,
  customFrom,
  customTo,
  load,
  setRange,
  apply,
  rangeOptions,
  chartFixedLength,
  chartEndDate,
  heatmapGranularity,
} = useStatsRange({
  fetchStats: (range, from, to) => fetchWorkspaceStats(props.workspaceId, range, from, to),
});

// Setting either end of a custom range loads as soon as both are present, so
// the common case needs no extra click; the tick button re-runs a range that is
// already complete.
function onCustomFrom(value) {
  customFrom.value = value;
  apply();
}
function onCustomTo(value) {
  customTo.value = value;
  apply();
}

// The analytics tab is reached by changing a route parameter, so switching
// workspace does not remount this — without the watch, the previous
// workspace's numbers would stay on screen under the new workspace's name.
watch(() => props.workspaceId, load);

onMounted(load);
</script>
