<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
/** The files chosen for a task not yet sent, each removable. Draws nothing when there are none. */
defineProps({
  attachments: { type: Array, default: () => [] },
})
defineEmits(['remove'])
</script>

<template>
  <div v-if="attachments.length > 0" class="flex flex-wrap gap-2">
    <div v-for="(att, i) in attachments" :key="i" data-test="attachment-chip"
         class="flex items-center text-[10px] bg-gray-50 dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 border border-gray-200 dark:border-zinc-700 rounded-md px-2.5 py-1 font-bold shadow-sm">
      <span class="truncate max-w-[150px]">{{ att.filename }}</span>
      <button type="button" data-test="attachment-remove" @click.prevent="$emit('remove', i)" class="ml-2 text-gray-500 hover:text-red-500 transition-colors">
        <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2.5"><path d="M6 18L18 6M6 6l12 12"></path></svg>
      </button>
    </div>
  </div>
</template>
