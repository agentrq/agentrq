<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<!-- A time of day, picked on the account's clock. Not <input type="time">,
     which draws the browser language's clock whatever the account picked. The
     value is "HH:MM" on the 24-hour clock either way, or "" for none. -->
<template>
  <div role="group" :aria-label="label" class="inline-flex items-center gap-0.5">
    <select :value="hourValue" :disabled="disabled" aria-label="Hour" data-test="clock-hour" :class="SELECT"
            @change="setHour(Number($event.target.value))">
      <option v-if="!parsed" value="" disabled>--</option>
      <option v-for="h in hours" :key="h" :value="h">{{ pad(h) }}</option>
    </select>
    <span aria-hidden="true">:</span>
    <select :value="parsed ? parsed.m : ''" :disabled="disabled" aria-label="Minute" data-test="clock-minute" :class="SELECT"
            @change="emitTime(parsed ? parsed.h : 0, Number($event.target.value))">
      <option v-if="!parsed" value="" disabled>--</option>
      <option v-for="m in MINUTES" :key="m" :value="m">{{ pad(m) }}</option>
    </select>
    <select v-if="twelve" :value="parsed && parsed.h >= 12 ? 'PM' : 'AM'" :disabled="disabled" aria-label="AM or PM" data-test="clock-period"
            :class="[SELECT, 'ml-1']" @change="setPeriod($event.target.value)">
      <option value="AM">AM</option>
      <option value="PM">PM</option>
    </select>
  </div>
</template>

<script setup>
import { computed } from 'vue';
import { withClock } from '../composables/useTimeFormat';

const props = defineProps({
  modelValue: { type: String, default: '' },
  disabled: { type: Boolean, default: false },
  label: { type: String, default: 'Time' },
});
const emit = defineEmits(['update:modelValue']);

const SELECT = 'appearance-none bg-transparent border-none outline-none p-0 cursor-pointer disabled:cursor-default rounded-sm focus-visible:ring-1 focus-visible:ring-gray-400 dark:focus-visible:ring-zinc-500';
const MINUTES = Array.from({ length: 60 }, (_, i) => i);
const pad = (n) => String(n).padStart(2, '0');

const twelve = computed(() => withClock({}).hourCycle === 'h12');
const hours = computed(() => (twelve.value ? [12, ...Array.from({ length: 11 }, (_, i) => i + 1)] : Array.from({ length: 24 }, (_, i) => i)));

const parsed = computed(() => {
  const m = /^(\d{2}):(\d{2})/.exec(props.modelValue);
  return m && Number(m[1]) < 24 && Number(m[2]) < 60 ? { h: Number(m[1]), m: Number(m[2]) } : null;
});
const hourValue = computed(() => {
  if (!parsed.value) return '';
  return twelve.value ? parsed.value.h % 12 || 12 : parsed.value.h;
});

const emitTime = (h, m) => emit('update:modelValue', `${pad(h)}:${pad(m)}`);

// On the 12-hour clock the hour keeps the half of the day it is in.
function setHour(h) {
  const minute = parsed.value ? parsed.value.m : 0;
  if (!twelve.value) return emitTime(h, minute);
  const pm = !!parsed.value && parsed.value.h >= 12;
  emitTime((h % 12) + (pm ? 12 : 0), minute);
}

function setPeriod(period) {
  const h = parsed.value ? parsed.value.h % 12 : 0;
  emitTime(h + (period === 'PM' ? 12 : 0), parsed.value ? parsed.value.m : 0);
}
</script>
