<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<template>
  <div class="bg-white dark:bg-zinc-900/90 border border-gray-200 dark:border-zinc-800 hover:border-gray-300 dark:hover:border-zinc-700/80 rounded-sm p-6 shadow-sm transition-all">
    <div class="flex flex-wrap items-center justify-between gap-3 mb-6">
      <div class="flex items-center gap-2">
        <span class="w-2 h-2 rounded-full bg-zinc-800 dark:bg-[#aec477]"></span>
        <h3 class="text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-50">Task Latency</h3>
        <span class="text-gray-400 dark:text-zinc-500 text-[9px] font-black uppercase tracking-widest">Minutes · {{ closed.toLocaleString() }} closed</span>
      </div>
      <div class="flex items-center gap-1 bg-gray-100 dark:bg-zinc-900 p-1 border border-gray-200 dark:border-zinc-800 rounded-sm" role="group" aria-label="Aggregate">
        <button
          v-for="opt in aggregates"
          :key="opt.id"
          type="button"
          :aria-pressed="aggregate === opt.id"
          @click="setAggregate(opt.id)"
          class="px-3 py-1 text-[10px] font-black uppercase tracking-widest rounded-sm transition-all"
          :class="aggregate === opt.id
            ? 'bg-white dark:bg-zinc-800 text-black dark:text-zinc-50 shadow-sm border border-gray-200 dark:border-zinc-700'
            : 'text-gray-500 hover:text-gray-900 dark:text-zinc-400 dark:hover:text-zinc-50'"
        >{{ opt.label }}</button>
      </div>
    </div>

    <div class="grid grid-cols-1 lg:grid-cols-[minmax(0,1fr)_minmax(0,2.2fr)] gap-6">
      <!-- Summary cards: the whole range's value per line. -->
      <div class="grid grid-cols-2 gap-3 content-start">
        <div
          v-for="card in cards"
          :key="card.key"
          class="border border-gray-200 dark:border-zinc-800 rounded-sm p-4 flex flex-col gap-1.5"
          :data-card="card.key"
        >
          <div class="flex items-start gap-1.5 min-w-0">
            <span class="w-2 h-2 mt-0.5 rounded-full shrink-0" :style="{ background: card.color }"></span>
            <span class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-400 leading-tight">{{ card.label }}</span>
          </div>
          <div class="flex items-baseline gap-1">
            <span class="text-2xl font-black text-gray-900 dark:text-zinc-50 leading-none">{{ card.value }}</span>
            <span v-if="card.value !== '—'" class="text-[10px] font-black text-gray-400 dark:text-zinc-500">min</span>
          </div>
          <span class="text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">{{ card.count.toLocaleString() }} {{ card.count === 1 ? 'task' : 'tasks' }}</span>
        </div>
      </div>

      <!-- The chart -->
      <div class="flex flex-col min-w-0">
        <!-- Legend, always shown: identity never rests on colour alone. -->
        <div class="flex flex-wrap gap-x-4 gap-y-1 mb-3">
          <span v-for="s in series" :key="s.key" class="flex items-center gap-1.5 text-[10px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400">
            <svg class="w-4 h-2" viewBox="0 0 16 2" aria-hidden="true"><line x1="0" y1="1" x2="16" y2="1" :stroke="colors[s.key]" stroke-width="2" :stroke-dasharray="s.dashed ? '4 3' : null" /></svg>{{ s.label }}
          </span>
        </div>

        <div class="flex gap-2 h-56">
          <!-- Y axis, in minutes -->
          <div class="relative w-10 shrink-0" aria-hidden="true">
            <span
              v-for="t in chart.ticks"
              :key="t.y"
              class="absolute right-0 text-[9px] font-black text-gray-400 dark:text-zinc-500 tabular-nums leading-none -translate-y-1/2"
              :style="{ top: `${t.y}%` }"
            >{{ t.label }}</span>
          </div>

          <div class="relative flex-1 min-w-0">
            <svg
              class="absolute inset-0 w-full h-full overflow-visible"
              viewBox="0 0 100 100"
              preserveAspectRatio="none"
              role="img"
              :aria-label="`Task latency in minutes, ${aggregate}`"
              @mousemove="onMove"
              @mouseleave="leave"
            >
              <line
                v-for="t in chart.ticks"
                :key="'g' + t.y"
                x1="0" :y1="t.y" x2="100" :y2="t.y"
                stroke="currentColor"
                :class="t.value === 0 ? 'text-gray-300 dark:text-zinc-700' : 'text-gray-100 dark:text-zinc-800'"
                stroke-width="1"
                vector-effect="non-scaling-stroke"
              />
              <line
                v-if="hovered >= 0"
                :x1="chart.xs[hovered]" y1="0" :x2="chart.xs[hovered]" y2="100"
                stroke="currentColor"
                class="text-gray-300 dark:text-zinc-600"
                stroke-width="1"
                vector-effect="non-scaling-stroke"
              />
              <path
                v-for="s in chart.series"
                :key="s.key"
                :d="s.path"
                fill="none"
                :stroke="colors[s.key]"
                :stroke-dasharray="s.dash"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
                vector-effect="non-scaling-stroke"
              />
            </svg>

            <!-- Dots are HTML so the stretched SVG does not squash them:
                 lone points (a bucket between two gaps) and the hovered one. -->
            <template v-for="s in chart.series" :key="'d' + s.key">
              <span
                v-for="p in s.dots"
                :key="s.key + p.index"
                class="absolute w-2 h-2 rounded-full -translate-x-1/2 -translate-y-1/2 pointer-events-none ring-2 ring-white dark:ring-zinc-900"
                :style="{ left: `${p.x}%`, top: `${p.y}%`, background: colors[s.key] }"
              ></span>
            </template>

            <!-- Direct labels at each line's last value, spread apart. -->
            <span
              v-for="s in chart.series.filter((x) => x.end)"
              :key="'l' + s.key"
              class="absolute left-full ml-2 text-[9px] font-black text-gray-600 dark:text-zinc-300 tabular-nums whitespace-nowrap -translate-y-1/2 pointer-events-none hidden sm:block"
              :style="{ top: `${s.end.labelY}%` }"
            >{{ formatMinutes(s.end.value) }}</span>

            <div v-if="tooltip" class="absolute z-20 top-0 pointer-events-none bg-gray-900 dark:bg-zinc-950 text-white dark:text-zinc-100 border border-gray-700 dark:border-zinc-700 px-2.5 py-2 rounded shadow-md text-[10px] min-w-[10rem]"
              :style="{ left: `${chart.xs[hovered]}%`, transform: `translateX(${chart.xs[hovered] > 60 ? 'calc(-100% - 8px)' : '8px'})` }">
              <div class="font-black uppercase tracking-widest mb-1">{{ tooltip.title }} · {{ tooltip.closed }} closed</div>
              <div v-for="r in tooltip.rows" :key="r.key" class="flex items-center gap-2">
                <span class="w-2 h-2 rounded-full shrink-0 ring-1 ring-white/50" :style="{ background: r.color }"></span>
                <span class="flex-1 text-gray-300 dark:text-zinc-400">{{ r.label }}</span>
                <span class="font-black tabular-nums">{{ r.value }}{{ r.value === '—' ? '' : ' min' }}</span>
              </div>
            </div>

            <div v-if="loading && !chart.hasData" class="absolute inset-0 flex items-center justify-center">
              <div class="text-[10px] font-black text-gray-300 dark:text-zinc-400 animate-pulse">Computing...</div>
            </div>
            <div v-else-if="!chart.hasData" class="absolute inset-0 flex items-center justify-center">
              <span class="text-[10px] font-black text-gray-300 dark:text-zinc-500 uppercase tracking-widest italic">No closed tasks in this range</span>
            </div>
          </div>
          <div class="w-8 shrink-0 hidden sm:block" aria-hidden="true"></div>
        </div>

        <!-- X axis -->
        <div class="flex gap-2 mt-1">
          <div class="w-10 shrink-0"></div>
          <div class="relative flex-1 h-4 min-w-0">
            <span
              v-for="l in chart.xLabels"
              :key="l.i"
              class="absolute text-[9px] font-black text-gray-400 dark:text-zinc-500 tabular-nums uppercase whitespace-nowrap -translate-x-1/2"
              :style="{ left: `${l.x}%` }"
            >{{ l.label }}</span>
          </div>
          <div class="w-8 shrink-0 hidden sm:block"></div>
        </div>

        <!-- The same numbers as a table, for screen readers. -->
        <table class="sr-only">
          <caption>Task latency in minutes ({{ aggregate }})</caption>
          <thead>
            <tr><th>Period</th><th v-for="s in series" :key="s.key">{{ s.label }}</th><th>Closed</th></tr>
          </thead>
          <tbody>
            <tr v-for="p in data?.points || []" :key="p.periodStart">
              <td>{{ bucketLabel(p.periodStart, granularity) }}</td>
              <td v-for="s in series" :key="s.key">{{ formatMinutes(toMinutes(p[s.key]?.seconds)) }}</td>
              <td>{{ p.closed }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<script setup>
/**
 * How long the range's closed tasks took. The logic is `useTaskLatency`;
 * this is its markup, shared by the workspace and account dashboards, which
 * differ only in the fetch they pass.
 */
import { onMounted, toRef } from 'vue';
import { useTaskLatency, formatMinutes, toMinutes, bucketLabel } from '../composables/useTaskLatency';
import { useThemeStore } from '../stores/themeStore';

const props = defineProps({
  fetchLatency: { type: Function, required: true },
  activeRange: { type: String, required: true },
  customFrom: { type: String, default: '' },
  customTo: { type: String, default: '' },
  /** What the numbers are about (a workspace id); a change reloads them. */
  scope: { type: [String, Number], default: '' },
});

const themeStore = useThemeStore();

const {
  data, loading, aggregate, aggregates, series, setAggregate, load,
  colors, granularity, chart, cards, closed, hovered, tooltip, hover, leave,
} = useTaskLatency({
  fetchLatency: (...args) => props.fetchLatency(...args),
  activeRange: toRef(props, 'activeRange'),
  customFrom: toRef(props, 'customFrom'),
  customTo: toRef(props, 'customTo'),
  scope: toRef(props, 'scope'),
  isDark: toRef(themeStore, 'isDark'),
});

function onMove(e) {
  const rect = e.currentTarget.getBoundingClientRect();
  hover(rect.width ? (e.clientX - rect.left) / rect.width : 0);
}

onMounted(load);
</script>
