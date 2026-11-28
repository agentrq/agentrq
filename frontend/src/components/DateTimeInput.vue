<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<!-- A date and a time, the time on the account's clock: the browser's date
     picker, which draws no clock, beside a ClockInput. Not
     <input type="datetime-local">, which draws the browser language's clock.
     The value is "YYYY-MM-DDTHH:MM", as datetime-local's is, or "" for none. -->
<template>
  <div class="flex items-center gap-2 min-w-0">
    <input type="date" :value="date" :min="minDate" :max="maxDate" :disabled="disabled" aria-label="Date" data-test="datetime-date"
           class="bg-transparent border-none outline-none p-0 min-w-0 flex-1" @input="setDate($event.target.value)" @change="setDate($event.target.value)" />
    <ClockInput :model-value="time" :disabled="disabled" @update:model-value="setTime" />
  </div>
</template>

<script setup>
import { computed } from 'vue';
import ClockInput from './ClockInput.vue';

const props = defineProps({
  modelValue: { type: String, default: '' },
  disabled: { type: Boolean, default: false },
  min: { type: String, default: undefined },
  max: { type: String, default: undefined },
});
const emit = defineEmits(['update:modelValue']);

const DATE = /^\d{4}-\d{2}-\d{2}/;
const dateOf = (s) => DATE.exec(s || '')?.[0] ?? '';
const date = computed(() => dateOf(props.modelValue));
const time = computed(() => /T(\d{2}:\d{2})/.exec(props.modelValue)?.[1] ?? '');
const minDate = computed(() => dateOf(props.min) || undefined);
const maxDate = computed(() => dateOf(props.max) || undefined);

// A date picked before a time starts at midnight; a time before a date, today.
function setDate(d) {
  emit('update:modelValue', d ? `${d}T${time.value || '00:00'}` : '');
}
function setTime(t) {
  const now = new Date();
  const today = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`;
  emit('update:modelValue', `${date.value || today}T${t}`);
}
</script>
