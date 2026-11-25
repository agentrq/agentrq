<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/**
 * Chart, from our catalog: the analytics pages' line chart and heatmap, or a
 * bar chart drawn as they are, in their colours unless the agent picked one.
 */
import { computed } from 'vue'

import BarChart from '../BarChart.vue'
import ChartSVG from '../ChartSVG.vue'
import HeatmapChart from '../HeatmapChart.vue'
import { chartColor, chartPoints, chartSummary, heatmapProps } from '../../a2ui/chart.js'
import { displayText } from '../../a2ui/render.js'
import { resolveValue } from '../../a2ui/resolve.js'
import { useThemeStore } from '../../stores/themeStore'

const props = defineProps({ c: { type: Object, required: true }, ctx: { type: Object, required: true } })

const theme = useThemeStore()
const variant = computed(() => props.c.variant ?? 'line')
const label = computed(() => displayText(resolveValue(props.c.label, props.ctx)))
const points = computed(() => chartPoints(resolveValue(props.c.data, props.ctx)))
const color = computed(() => chartColor(props.c.color, variant.value, theme.isDark))
// The heading's dot is the line chart's colour, a heatmap's tone too.
const dot = computed(() => chartColor(props.c.color, 'line', theme.isDark))
const bars = computed(() => points.value.map((p) => ({ label: p.label, count: p.value })))
const line = computed(() => points.value.map((p) => ({ date: p.label, count: p.value })))
const heatmap = computed(() => (variant.value === 'heatmap' ? heatmapProps(points.value) : null))
const summary = computed(() => chartSummary(label.value, points.value))
const height = computed(() => (heatmap.value?.granularity === 'hour' ? 'h-56' : 'h-40'))
</script>

<template>
  <!-- A narrow line chart shows every other date, so none overlap -->
  <figure data-test="a2ui-chart" :data-variant="variant" class="@container w-full min-w-0 m-0"
          :class="{ '@max-xs:[&_.h-5>span:nth-child(even)]:hidden': variant === 'line' }">
    <figcaption v-if="label" class="flex items-center gap-2 mb-3">
      <span class="w-2 h-2 rounded-full shrink-0" :style="{ backgroundColor: dot }" />
      <span class="text-[10px] leading-[15px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-50 truncate">{{ label }}</span>
    </figcaption>
    <div role="img" :aria-label="summary || 'No data points'" class="w-full relative" :class="height">
      <HeatmapChart v-if="heatmap" :data="heatmap.data" :granularity="heatmap.granularity" :range-start="heatmap.rangeStart"
                    :range-end="heatmap.rangeEnd" :metric-label="label || 'Count'" :color="color" />
      <BarChart v-else-if="variant === 'bar'" :data="bars" :color="color" />
      <ChartSVG v-else :data="line" :color="color" />
    </div>
  </figure>
</template>
