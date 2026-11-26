<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<!-- Shows or hides the board's Scheduled column, beside New Task. `tall`
     matches the team board's filter pickers it sits beside. -->
<template>
  <button type="button" data-test="scheduled-toggle" @click="toggleScheduled"
          :aria-pressed="showScheduled"
          :title="showScheduled ? 'Hide the Scheduled column' : 'Show the Scheduled column'"
          class="px-2.5 flex items-center gap-1.5 border rounded-lg text-[10px] font-bold uppercase tracking-wider transition-all shadow-sm hover:text-black dark:hover:text-white hover:bg-gray-100 dark:hover:bg-zinc-800"
          :class="[tall ? 'h-9.5' : 'h-8', showScheduled ? 'bg-gray-100 dark:bg-zinc-800 text-black dark:text-white border-black dark:border-white' : 'text-gray-500 dark:text-zinc-400 bg-white dark:bg-zinc-900 border-gray-100 dark:border-zinc-800']">
    <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>
    <span class="hidden @min-[40rem]:inline">Scheduled</span>
  </button>
</template>

<script setup>
import { useKanbanScheduled } from '../composables/useKanbanScheduled';

defineProps({ tall: Boolean });

const { showScheduled, toggleScheduled } = useKanbanScheduled();
</script>
