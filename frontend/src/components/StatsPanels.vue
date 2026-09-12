<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
-->

<template>
  <div class="flex flex-col gap-8">
    <!-- Filters -->
    <div class="flex flex-wrap items-center gap-1.5 bg-gray-100 dark:bg-zinc-900/90 p-1 border border-gray-200 dark:border-zinc-800 rounded-sm max-w-max shadow-sm">
      <button
        v-for="opt in rangeOptions"
        :key="opt.id"
        @click="$emit('update:activeRange', opt.id)"
        class="px-3 py-1.5 text-[10px] font-black uppercase tracking-widest rounded-sm transition-all"
        :class="activeRange === opt.id
          ? 'bg-white dark:bg-zinc-800 text-black dark:text-zinc-50 shadow-sm border border-gray-200 dark:border-zinc-700'
          : 'text-gray-500 hover:text-gray-900 dark:text-zinc-400 dark:hover:text-zinc-50'"
      >
        {{ opt.label }}
      </button>

      <!-- Custom Date Inputs (only if custom is selected) -->
      <div v-if="activeRange === 'custom'" class="flex items-center gap-2 px-2 border-l border-gray-200 dark:border-zinc-800 ml-1">
        <input type="date" :value="customFrom" @input="$emit('update:customFrom', $event.target.value)" class="text-[10px] bg-transparent border-none focus:ring-0 text-gray-900 dark:text-zinc-50 font-black p-0 w-24" />
        <span class="text-[10px] font-black text-gray-500 dark:text-zinc-500">–</span>
        <input type="date" :value="customTo" @input="$emit('update:customTo', $event.target.value)" class="text-[10px] bg-transparent border-none focus:ring-0 text-gray-900 dark:text-zinc-50 font-black p-0 w-24" />
        <button @click="$emit('apply')" class="p-1 text-gray-500 hover:text-black dark:hover:text-white transition-all">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M5 13l4 4L19 7" /></svg>
        </button>
      </div>
    </div>

    <!-- Summary Cards -->
    <div v-if="stats && stats.summary" class="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-4">
      <!-- Tasks Completed -->
      <div class="bg-white dark:bg-zinc-900/90 border border-gray-200 dark:border-zinc-800 hover:border-gray-300 dark:hover:border-[#aec477]/40 rounded-sm p-5 flex flex-col gap-1.5 shadow-sm hover:shadow-md transition-all duration-200 group">
        <div class="flex items-center gap-1.5">
          <span class="w-1.5 h-1.5 rounded-full bg-zinc-800 dark:bg-[#aec477]"></span>
          <span class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-400 dark:group-hover:text-[#aec477] transition-colors">Completed</span>
        </div>
        <span class="text-3xl font-black text-gray-900 dark:text-zinc-50 tabular-nums leading-none">{{ stats.summary.tasksCompleted.toLocaleString() }}</span>
      </div>

      <!-- Tasks Scheduled -->
      <div class="bg-white dark:bg-zinc-900/90 border border-gray-200 dark:border-zinc-800 hover:border-gray-300 dark:hover:border-zinc-700/80 rounded-sm p-5 flex flex-col gap-1.5 shadow-sm hover:shadow-md transition-all duration-200 group">
        <div class="flex items-center gap-1.5">
          <span class="w-1.5 h-1.5 rounded-full bg-gray-400 dark:bg-zinc-500"></span>
          <span class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-400">Scheduled</span>
        </div>
        <span class="text-3xl font-black text-gray-900 dark:text-zinc-50 tabular-nums leading-none">{{ stats.summary.tasksScheduled.toLocaleString() }}</span>
      </div>

      <!-- Messages -->
      <div class="bg-white dark:bg-zinc-900/90 border border-gray-200 dark:border-zinc-800 hover:border-gray-300 dark:hover:border-[#a8a3d9]/40 rounded-sm p-5 flex flex-col gap-1.5 shadow-sm hover:shadow-md transition-all duration-200 group">
        <div class="flex items-center gap-1.5">
          <span class="w-1.5 h-1.5 rounded-full bg-zinc-800 dark:bg-[#a8a3d9]"></span>
          <span class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-400 dark:group-hover:text-[#a8a3d9] transition-colors">Messages</span>
        </div>
        <span class="text-3xl font-black text-gray-900 dark:text-zinc-50 tabular-nums leading-none">{{ stats.summary.messages.toLocaleString() }}</span>
      </div>

      <!-- Manual Approvals -->
      <div class="bg-white dark:bg-zinc-900/90 border border-gray-200 dark:border-zinc-800 hover:border-gray-300 dark:hover:border-zinc-700/80 rounded-sm p-5 flex flex-col gap-1.5 shadow-sm hover:shadow-md transition-all duration-200 group">
        <div class="flex items-center gap-1.5">
          <span class="w-1.5 h-1.5 rounded-full bg-gray-400 dark:bg-zinc-500"></span>
          <span class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-400">Manual</span>
        </div>
        <span class="text-3xl font-black text-gray-900 dark:text-zinc-50 tabular-nums leading-none">{{ stats.summary.manualApprovals.toLocaleString() }}</span>
      </div>

      <!-- Auto Approvals -->
      <div class="bg-white dark:bg-zinc-900/90 border border-gray-200 dark:border-zinc-800 hover:border-gray-300 dark:hover:border-zinc-700/80 rounded-sm p-5 flex flex-col gap-1.5 shadow-sm hover:shadow-md transition-all duration-200 group">
        <div class="flex items-center gap-1.5">
          <span class="w-1.5 h-1.5 rounded-full bg-gray-400 dark:bg-zinc-500"></span>
          <span class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-400">Auto</span>
        </div>
        <span class="text-3xl font-black text-gray-900 dark:text-zinc-50 tabular-nums leading-none">{{ stats.summary.autoApprovals.toLocaleString() }}</span>
      </div>

      <!-- Denies -->
      <div class="bg-white dark:bg-zinc-900/90 border border-gray-200 dark:border-zinc-800 hover:border-gray-300 dark:hover:border-zinc-700/80 rounded-sm p-5 flex flex-col gap-1.5 shadow-sm hover:shadow-md transition-all duration-200 group">
        <div class="flex items-center gap-1.5">
          <span class="w-1.5 h-1.5 rounded-full bg-gray-400 dark:bg-zinc-500"></span>
          <span class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-400">Denies</span>
        </div>
        <span class="text-3xl font-black text-gray-900 dark:text-zinc-50 tabular-nums leading-none">{{ stats.summary.denies.toLocaleString() }}</span>
      </div>
    </div>

    <!-- Anything the host wants between the cards and the charts. The account
         dashboard puts its per-workspace breakdown here. -->
    <slot name="after-summary" />

    <!-- Heatmaps Section -->
    <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
      <!-- Task Heatmap -->
      <div class="bg-white dark:bg-zinc-900/90 border border-gray-200 dark:border-zinc-800 hover:border-gray-300 dark:hover:border-zinc-700/80 rounded-sm p-6 shadow-sm transition-all">
        <div class="flex items-center justify-between mb-6">
          <div class="flex items-center gap-2">
            <span class="w-2 h-2 rounded-full bg-zinc-800 dark:bg-[#aec477]"></span>
            <h3 class="text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-50">Task Activity Heatmap</h3>
          </div>
          <div class="text-gray-400 dark:text-zinc-500 text-[9px] font-black uppercase tracking-widest">{{ heatmapGranularity === 'hour' ? 'By Hour' : 'By Day' }}</div>
        </div>
        <div class="h-56 w-full relative">
          <div v-if="loading" class="absolute inset-0 flex items-center justify-center z-10">
            <div class="text-[10px] font-black text-gray-300 dark:text-zinc-400 animate-pulse">Computing...</div>
          </div>
          <HeatmapChart
            v-if="stats && stats.heatmap"
            :data="stats.heatmap.tasksCompleted || []"
            :granularity="heatmapGranularity"
            :range-start="stats.heatmap.rangeStart"
            :range-end="stats.heatmap.rangeEnd"
            :weekday-column-labels="activeRange === 'week'"
            metric-label="Tasks"
            :color="palette.taskHeatmap"
          />
        </div>
      </div>

      <!-- Message Heatmap -->
      <div class="bg-white dark:bg-zinc-900/90 border border-gray-200 dark:border-zinc-800 hover:border-gray-300 dark:hover:border-zinc-700/80 rounded-sm p-6 shadow-sm transition-all">
        <div class="flex items-center justify-between mb-6">
          <div class="flex items-center gap-2">
            <span class="w-2 h-2 rounded-full bg-zinc-800 dark:bg-[#a8a3d9]"></span>
            <h3 class="text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-50">Message Activity Heatmap</h3>
          </div>
          <div class="text-gray-400 dark:text-zinc-500 text-[9px] font-black uppercase tracking-widest">{{ heatmapGranularity === 'hour' ? 'By Hour' : 'By Day' }}</div>
        </div>
        <div class="h-56 w-full relative">
          <div v-if="loading" class="absolute inset-0 flex items-center justify-center z-10">
            <div class="text-[10px] font-black text-gray-300 dark:text-zinc-400 animate-pulse">Computing...</div>
          </div>
          <HeatmapChart
            v-if="stats && stats.heatmap"
            :data="stats.heatmap.messages || []"
            :granularity="heatmapGranularity"
            :range-start="stats.heatmap.rangeStart"
            :range-end="stats.heatmap.rangeEnd"
            :weekday-column-labels="activeRange === 'week'"
            metric-label="Messages"
            :color="palette.messageHeatmap"
          />
        </div>
      </div>
    </div>

    <!-- Charts Section -->
    <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
      <!-- Task Chart -->
      <div class="bg-white dark:bg-zinc-900/90 border border-gray-200 dark:border-zinc-800 hover:border-gray-300 dark:hover:border-zinc-700/80 rounded-sm p-6 shadow-sm transition-all">
        <div class="flex items-center justify-between mb-6">
          <div class="flex items-center gap-2">
            <span class="w-2 h-2 rounded-full bg-zinc-800 dark:bg-[#aec477]"></span>
            <h3 class="text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-50">Task Completion Velocity</h3>
          </div>
          <div class="text-gray-400 dark:text-zinc-500 text-[9px] font-black uppercase tracking-widest">Daily Trend</div>
        </div>
        <div class="h-56 w-full relative">
          <div v-if="loading" class="absolute inset-0 flex items-center justify-center z-10">
            <div class="text-[10px] font-black text-gray-300 dark:text-zinc-400 animate-pulse">Computing...</div>
          </div>
          <ChartSVG
            v-if="stats && stats.timeseries"
            :data="stats.timeseries.tasksCompleted || []"
            :color="palette.taskChart"
            :fixed-length="chartFixedLength"
            :last-date="chartEndDate"
          />
        </div>
      </div>

      <!-- Message Chart -->
      <div class="bg-white dark:bg-zinc-900/90 border border-gray-200 dark:border-zinc-800 hover:border-gray-300 dark:hover:border-zinc-700/80 rounded-sm p-6 shadow-sm transition-all">
        <div class="flex items-center justify-between mb-6">
          <div class="flex items-center gap-2">
            <span class="w-2 h-2 rounded-full bg-zinc-800 dark:bg-[#a8a3d9]"></span>
            <h3 class="text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-50">Communication Volume</h3>
          </div>
          <div class="text-gray-400 dark:text-zinc-500 text-[9px] font-black uppercase tracking-widest">Total Messages</div>
        </div>
        <div class="h-56 w-full relative">
          <div v-if="loading" class="absolute inset-0 flex items-center justify-center z-10">
            <div class="text-[10px] font-black text-gray-300 dark:text-zinc-400 animate-pulse">Computing...</div>
          </div>
          <ChartSVG
            v-if="stats && stats.timeseries"
            :data="stats.timeseries.messages || []"
            :color="palette.messageChart"
            :fixed-length="chartFixedLength"
            :last-date="chartEndDate"
          />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
/**
 * The analytics panels, given data — the range control, the six summary cards,
 * the two heatmaps and the two line charts.
 *
 * Purely presentational: it holds no range state and fetches nothing, so the
 * per-workspace and account-wide dashboards can render the identical set of
 * panels from their own endpoint. `useStatsRange` is the other half.
 */
import ChartSVG from './ChartSVG.vue';
import HeatmapChart from './HeatmapChart.vue';

defineProps({
  /** The stats response, or null while loading / after a failure. */
  stats: { type: Object, default: null },
  loading: { type: Boolean, default: false },
  activeRange: { type: String, required: true },
  rangeOptions: { type: Array, required: true },
  customFrom: { type: String, default: '' },
  customTo: { type: String, default: '' },
  /** Day-columns the line charts reserve; 0 means "as many as the data has". */
  chartFixedLength: { type: Number, default: 0 },
  chartEndDate: { type: String, required: true },
  heatmapGranularity: { type: String, default: 'day' },
  /** From `statsPalette(isDark)`; the SVGs take colours as props. */
  palette: { type: Object, required: true },
});

defineEmits(['update:activeRange', 'update:customFrom', 'update:customTo', 'apply']);
</script>
