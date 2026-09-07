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
  >
    <!-- The panel that only makes sense once several workspaces are in view:
         which of them actually produced the totals above. -->
    <template #after-summary>
      <div class="bg-white dark:bg-zinc-900/90 border border-gray-200 dark:border-zinc-800 hover:border-gray-300 dark:hover:border-zinc-700/80 rounded-sm p-6 shadow-sm transition-all">
        <div class="flex items-center justify-between mb-6">
          <div class="flex items-center gap-2">
            <span class="w-2 h-2 rounded-full bg-zinc-800 dark:bg-[#aec477]"></span>
            <h3 class="text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-50">By Workspace</h3>
          </div>
          <div class="text-gray-400 dark:text-zinc-500 text-[9px] font-black uppercase tracking-widest">Busiest First</div>
        </div>

        <div v-if="loading && !breakdown.length" class="py-8 text-center text-[10px] font-black text-gray-300 dark:text-zinc-400 animate-pulse">
          Computing...
        </div>

        <div v-else-if="!breakdown.length" class="py-8 text-center border border-dashed border-gray-200 dark:border-zinc-800 rounded-sm">
          <p class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">No activity in this range</p>
        </div>

        <div v-else class="flex flex-col">
          <!-- Column headings -->
          <div class="flex items-center gap-4 px-2 pb-2 border-b border-gray-100 dark:border-zinc-800">
            <span class="flex-1 text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Workspace</span>
            <span class="w-20 text-right text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Completed</span>
            <span class="w-20 text-right text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Messages</span>
            <span class="hidden sm:block w-28 text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Share</span>
            <span class="shrink-0 w-4" aria-hidden="true"></span>
          </div>

          <!-- A real anchor, not a button that pushes a route: these rows are
               links to another screen, so they have to behave like links —
               cmd-click and middle-click open a new tab, the URL shows in the
               status bar, and "Copy Link Address" works. Opening two
               workspaces' analytics side by side is most of why you would read
               this panel at all.

               `component is` rather than two v-for blocks: a deleted workspace
               has nothing to link to, and an anchor with no destination is
               worse than a plain row. -->
          <component
            :is="row.name ? 'router-link' : 'div'"
            v-for="row in breakdown"
            :key="row.workspaceId"
            :to="row.name ? analyticsRoute(row) : undefined"
            :title="row.name ? `Analytics for ${row.name}` : undefined"
            class="group flex items-center gap-4 px-2 py-3 border-b border-gray-100 dark:border-zinc-800/60 text-left transition-colors"
            :class="row.name ? 'hover:bg-gray-50 dark:hover:bg-zinc-800/40 cursor-pointer' : 'cursor-default'"
          >
            <span class="flex-1 min-w-0 flex items-center gap-2">
              <span class="w-1.5 h-1.5 rounded-full shrink-0" :class="row.name ? 'bg-zinc-800 dark:bg-[#aec477]' : 'bg-gray-300 dark:bg-zinc-700'"></span>
              <span v-if="row.name" class="text-[11px] font-black text-gray-800 dark:text-zinc-200 truncate group-hover:text-black dark:group-hover:text-white group-hover:underline decoration-1 underline-offset-2 transition-colors">{{ row.name }}</span>
              <!-- Telemetry outlives the workspace it was recorded in. The row
                   is kept rather than dropped so the breakdown still adds up to
                   the totals above, but there is nothing to navigate to. -->
              <span v-else class="text-[11px] font-black text-gray-400 dark:text-zinc-500 italic truncate">Deleted workspace</span>
            </span>
            <span class="w-20 text-right text-[11px] font-black text-gray-900 dark:text-zinc-50 tabular-nums">{{ row.tasksCompleted.toLocaleString() }}</span>
            <span class="w-20 text-right text-[11px] font-black text-gray-900 dark:text-zinc-50 tabular-nums">{{ row.messages.toLocaleString() }}</span>
            <span class="hidden sm:block w-28">
              <span class="block h-1.5 rounded-sm bg-gray-100 dark:bg-zinc-800 overflow-hidden">
                <span class="block h-full rounded-sm bg-zinc-800 dark:bg-[#aec477]" :style="{ width: shareOf(row) }"></span>
              </span>
            </span>
            <!-- Visible at rest, not only on hover: a row whose only clue is a
                 hover background is a link nobody finds. Faint until you reach
                 for it, then it commits. -->
            <span class="shrink-0 w-4 flex justify-end">
              <svg v-if="row.name" class="w-3.5 h-3.5 text-gray-300 dark:text-zinc-600 group-hover:text-gray-900 dark:group-hover:text-white transition-colors"
                   fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5" aria-hidden="true">
                <path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7" />
              </svg>
            </span>
          </component>
        </div>
      </div>
    </template>
  </StatsPanels>
</template>

<script setup>
/**
 * Account-wide analytics: the same panels as one workspace's dashboard, summed
 * across every workspace the signed-in user owns, plus a breakdown of which
 * workspaces drove the totals.
 *
 * Everything shared with `WorkspaceStats.vue` comes from `StatsPanels` and
 * `useStatsRange`; all this adds is the endpoint and the breakdown panel.
 */
import { computed } from 'vue';
import { fetchUserStats } from '../api';
import StatsPanels from './StatsPanels.vue';
import { useStatsRange, statsPalette } from '../composables/useStatsRange';
import { workspaceRoute } from '../composables/useWorkspaceSwitcher';
import { useThemeStore } from '../stores/themeStore';

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
} = useStatsRange({ fetchStats: fetchUserStats });

const breakdown = computed(() => stats.value?.workspaces ?? []);

// The share bar is relative to the busiest workspace rather than to the total:
// with a dozen workspaces every bar would otherwise be a sliver, and the
// question the panel answers is which ones are carrying the work.
const busiest = computed(() =>
  breakdown.value.reduce((max, row) => Math.max(max, row.tasksCompleted + row.messages), 0)
);

function shareOf(row) {
  if (!busiest.value) return '0%';
  return `${Math.round(((row.tasksCompleted + row.messages) / busiest.value) * 100)}%`;
}

/**
 * Straight to that workspace's own analytics, not its overview: the reader is
 * already looking at numbers and wants the same numbers for one workspace.
 */
const analyticsRoute = (row) => workspaceRoute(row.workspaceId, 'analytics');

function onCustomFrom(value) {
  customFrom.value = value;
  apply();
}
function onCustomTo(value) {
  customTo.value = value;
  apply();
}

defineExpose({ load });

load();
</script>
