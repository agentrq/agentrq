<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<!-- The one "load more" every task list uses, so they all look and behave alike. -->
<template>
  <button @click.stop="$emit('click')" :disabled="loading"
          class="w-full py-1.5 rounded-sm border border-dashed border-gray-200 dark:border-zinc-800 text-gray-500 dark:text-zinc-400 text-[10px] font-semibold hover:border-gray-300 dark:hover:border-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-100 dark:hover:bg-zinc-800/50 transition-all flex items-center justify-center gap-1.5">
    <svg v-if="loading" class="w-3 h-3 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 12a8 8 0 018-8v8H4z" /></svg>
    {{ loading ? 'Loading...' : label }}
  </button>
</template>

<script setup>
defineProps({
  label: { type: String, default: 'Load More' },
  loading: { type: Boolean, default: false }
});
defineEmits(['click']);
</script>
