<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- The workspaces a skill is turned on in as it arrives in your account, by
     import or from a teammate: a chip each, ticked or not. Which start ticked
     is the caller's choice. -->
<template>
  <fieldset v-if="workspaces.length" :data-test="dataTest" class="min-w-0">
    <legend class="mb-1.5 text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">{{ legend }}</legend>
    <div class="flex flex-wrap gap-1.5">
      <label v-for="w in workspaces" :key="w.id" :data-test="itemTest"
             :class="modelValue.includes(String(w.id)) ? 'border-gray-900 dark:border-zinc-100 text-gray-900 dark:text-zinc-100' : 'border-gray-200 dark:border-zinc-700 text-gray-500 dark:text-zinc-400 hover:border-gray-300 dark:hover:border-zinc-600'"
             class="inline-flex items-center gap-1.5 max-w-full min-w-0 px-2 py-1 border rounded-sm text-[11px] cursor-pointer transition-colors">
        <input v-model="ticked" type="checkbox" :value="String(w.id)" class="shrink-0 rounded-sm border-gray-300 dark:border-zinc-600" />
        <span class="min-w-0 truncate">{{ w.name }}</span>
      </label>
    </div>
  </fieldset>
</template>

<script setup>
import { computed } from 'vue';

const props = defineProps({
  workspaces: { type: Array, default: () => [] },
  modelValue: { type: Array, default: () => [] },
  legend: { type: String, default: 'Turn on in' },
  dataTest: { type: String, default: 'skill-workspaces' },
  itemTest: { type: String, default: 'skill-workspace' },
});
const emit = defineEmits(['update:modelValue']);

const ticked = computed({
  get: () => props.modelValue,
  set: (ids) => emit('update:modelValue', ids),
});
</script>
