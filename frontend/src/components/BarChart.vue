<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<template>
  <div class="w-full h-full flex flex-col">
    <div class="flex-1 relative min-h-0">
      <svg class="absolute inset-0 w-full h-full" viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">
        <line v-for="y in [25, 50, 75]" :key="y" x1="0" :y1="y" x2="100" :y2="y" stroke="currentColor"
              class="text-gray-200 dark:text-zinc-800/40" stroke-width="1" vector-effect="non-scaling-stroke" stroke-dasharray="2,3" />
      </svg>

      <!-- Bars: value 0 at 82%, the largest at 18%, as the line chart maps them -->
      <div v-if="bars.length > 0" class="absolute left-6 right-0 flex gap-[3px]" style="top: 18%; bottom: 18%;">
        <div
          v-for="(b, i) in bars"
          :key="'bar-' + i"
          data-test="bar"
          class="relative flex-1 min-w-0 h-full flex items-end justify-center cursor-pointer"
          @mouseenter="hovered = b"
          @mouseleave="hovered = null"
        >
          <div class="w-3/5 max-w-8 rounded-t-[2px] transition-opacity" :class="hovered && hovered !== b ? 'opacity-60' : ''"
               :style="{ height: `${b.height}%`, minHeight: b.count > 0 ? '1px' : '0', backgroundColor: color }" />
        </div>
      </div>

      <!-- Y-axis labels -->
      <div v-if="maxValue > 0" class="absolute left-0 pointer-events-none flex flex-col justify-between" style="top: 18%; bottom: 18%;">
        <span class="text-[9px] font-black text-gray-400 dark:text-zinc-500 tabular-nums leading-none -translate-y-1/2">{{ formatValue(maxValue) }}</span>
        <span class="text-[9px] font-black text-gray-400 dark:text-zinc-500 tabular-nums leading-none translate-y-1/2">0</span>
      </div>

      <!-- Tooltip -->
      <div
        v-if="hovered"
        class="absolute z-20 bg-gray-900 dark:bg-zinc-900 text-white dark:text-zinc-100 border border-gray-700 dark:border-zinc-700 px-2.5 py-1 text-[10px] font-black uppercase tracking-widest pointer-events-none rounded shadow-md whitespace-nowrap"
        :style="{
          left: `calc(1.5rem + (100% - 1.5rem) * ${hovered.x / 100})`,
          top: `${82 - hovered.height * 0.64}%`,
          transform: `translate(${hovered.x > 80 ? '-100%' : hovered.x < 20 ? '0%' : '-50%'}, -110%)`
        }"
      >
        {{ hovered.label }}: {{ hovered.count }}
      </div>

      <!-- Empty State -->
      <div v-if="bars.length === 0" class="absolute inset-0 flex items-center justify-center">
        <span class="text-[10px] font-black text-gray-300 dark:text-zinc-500 uppercase tracking-widest italic">No data points</span>
      </div>
    </div>

    <!-- X-axis labels: each in its own slot, under its bars, cut short rather than overlapping -->
    <div v-if="labelGroups.length > 0" class="h-5 mt-0.5 ml-6 flex gap-[3px] items-end flex-shrink-0">
      <span
        v-for="(g, i) in labelGroups"
        :key="'lbl-' + i"
        class="min-w-0 truncate text-[9px] font-black text-gray-400 dark:text-zinc-500 tabular-nums leading-none uppercase"
        :class="g.size === 1 ? 'text-center' : 'text-left'"
        :style="{ flex: `${g.size} 1 0%` }"
        :title="g.label"
      >{{ g.label }}</span>
    </div>
  </div>
</template>

<script setup>
/**
 * Values side by side, drawn as the analytics line chart is: its grid, axis
 * labels, tooltip and colour. Data is [{ label, count }].
 */
import { computed, ref } from 'vue';

const props = defineProps({
  data: { type: Array, default: () => [] },
  color: { type: String, default: '#27272a' },
});

const hovered = ref(null);

const maxValue = computed(() => Math.max(0, ...props.data.map((d) => d.count)));

const bars = computed(() => {
  const len = props.data.length;
  return props.data.map((d, i) => ({
    label: d.label,
    count: d.count,
    x: ((i + 0.5) / len) * 100,
    height: maxValue.value > 0 ? (d.count / maxValue.value) * 100 : 0,
  }));
});

// Every bar's label when they are few; else one label per group of bars, the
// first's, over the group's width.
const labelGroups = computed(() => {
  const step = bars.value.length <= 7 ? 1 : Math.ceil(bars.value.length / 6);
  const groups = [];
  for (let i = 0; i < bars.value.length; i += step) {
    groups.push({ label: bars.value[i].label, size: Math.min(step, bars.value.length - i) });
  }
  return groups;
});

function formatValue(v) {
  if (v >= 1000000) return (v / 1000000).toFixed(1) + 'M';
  if (v >= 1000) return (v / 1000).toFixed(1) + 'k';
  return String(v);
}
</script>
