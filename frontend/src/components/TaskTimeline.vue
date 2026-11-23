<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<template>
  <div v-if="segments.length" class="flex min-w-0" :class="compact ? 'items-center gap-3' : 'flex-col-reverse gap-1'" data-testid="task-timeline">
    <!-- The dots: one per transition, the time spent in a state on the line to the next -->
    <ol class="flex items-start min-w-0 overflow-x-auto no-scrollbar" :class="compact ? 'flex-1 pr-1' : 'w-full pb-0.5'">
      <li v-for="(s, i) in shown" :key="i"
          class="flex flex-col min-w-0"
          :class="[i < shown.length - 1 ? 'flex-1' : 'shrink-0', compact ? 'min-w-5' : 'min-w-[4.5rem]']"
          :title="`${stateLabel(s)} · ${formatTransitionTime(s.start, now)}${s.closed ? '' : ` · ${formatDuration(s.seconds)}`}`">
        <div class="flex items-center h-4">
          <span class="relative flex items-center justify-center w-3 h-3 shrink-0">
            <span v-if="s.current && !s.closed" class="absolute inset-0 rounded-full animate-ping opacity-40" :class="dotClass(s)"></span>
            <span class="rounded-full" :class="[dotClass(s), s.current ? 'w-2.5 h-2.5 ring-2 ring-offset-1 ring-gray-300 dark:ring-zinc-600 ring-offset-white dark:ring-offset-zinc-900' : 'w-2 h-2']"></span>
          </span>
          <template v-if="i < shown.length - 1">
            <span class="flex-1 min-w-1" :class="lineClass(s)"></span>
            <template v-if="!compact">
              <span class="px-1 text-[9px] font-semibold tabular-nums shrink-0" :class="durationClass(s)">{{ formatDuration(s.seconds) }}</span>
              <span class="flex-1 min-w-1" :class="lineClass(s)"></span>
            </template>
          </template>
          <span v-else-if="!s.closed" class="pl-1 text-[9px] font-semibold tabular-nums shrink-0" :class="durationClass(s)">{{ formatDuration(s.seconds) }}</span>
        </div>
        <div v-if="!compact" class="pr-2 leading-tight whitespace-nowrap">
          <div class="text-[9px] font-black uppercase tracking-wider" :class="labelClass(s)">{{ stateLabel(s) }}</div>
          <div class="text-[9px] tabular-nums text-gray-400 dark:text-zinc-500">{{ formatTransitionTime(s.start, now) }}</div>
        </div>
      </li>
    </ol>

    <!-- The totals; a phone has room for the one -->
    <div v-if="compact" class="shrink-0 text-[9px] font-semibold uppercase tracking-wider text-gray-400 dark:text-zinc-500 whitespace-nowrap">
      Total <span class="tabular-nums text-gray-700 dark:text-zinc-200">{{ formatDuration(totals.totalSeconds) }}</span>
    </div>
    <div v-else class="flex items-center gap-2 text-[9px] font-semibold uppercase tracking-wider text-gray-400 dark:text-zinc-500 whitespace-nowrap">
      <span>{{ workedLabel }} <span class="tabular-nums text-gray-700 dark:text-zinc-200">{{ formatDuration(totals.workedSeconds) }}</span></span>
      <span v-if="totals.blockedSeconds">· Blocked <span class="tabular-nums text-red-600 dark:text-red-400">{{ formatDuration(totals.blockedSeconds) }}</span></span>
      <span v-if="totals.needsInputSeconds">· Needs input <span class="tabular-nums text-yellow-600 dark:text-yellow-400">{{ formatDuration(totals.needsInputSeconds) }}</span></span>
      <span v-if="totals.startToCloseSeconds !== null">· Start→close <span class="tabular-nums text-gray-700 dark:text-zinc-200">{{ formatDuration(totals.startToCloseSeconds) }}</span></span>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue';
import {
  formatDuration, formatTransitionTime, timelineDotClass, timelineLineClass, timelineSegments,
  timelineStateLabel, timelineTextClass, timelineTone, timelineTotals, timelineWorkedLabel,
} from '../composables/useTaskTimeline';

const COMPACT_DOTS = 6;

const props = defineProps({
  transitions: { type: Array, default: () => [] },
  // One line, for a phone: the dots and durations, no labels under them.
  compact: { type: Boolean, default: false },
  // The task's live tone (taskStatusTone), so the current dot turns yellow
  // the moment the task shows as waiting on the person.
  currentTone: { type: String, default: '' },
});

// The state the task is in now keeps growing, so the clock ticks while the
// timeline is on screen.
const now = ref(new Date());
let timer = null;
onMounted(() => { timer = setInterval(() => { now.value = new Date(); }, 15000); });
onUnmounted(() => clearInterval(timer));

const segments = computed(() => timelineSegments(props.transitions, now.value));
const totals = computed(() => timelineTotals(segments.value));
const workedLabel = computed(() => timelineWorkedLabel(props.transitions, segments.value));
// A phone has room for the latest few; the totals still count them all.
const shown = computed(() => (props.compact ? segments.value.slice(-COMPACT_DOTS) : segments.value));

const tone = (s) => timelineTone(s, props.currentTone);
const dotClass = (s) => timelineDotClass(tone(s));
const labelClass = (s) => timelineTextClass(tone(s), s.current);
const durationClass = (s) => timelineTextClass(tone(s), false);
const lineClass = (s) => timelineLineClass(tone(s));
const stateLabel = (s) => timelineStateLabel(tone(s) === 'pending' ? 'needsinput' : s.state);
</script>
