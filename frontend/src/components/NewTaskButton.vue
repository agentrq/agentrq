<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- New Task on a page that spans every workspace. The task form belongs to
     one, so it does what N does there: the shell asks which, in the
     workspace switcher, unless there is only one. -->
<template>
  <button type="button" data-test="new-task" title="New Task" @click="openNewTask"
    class="flex items-center gap-2 bg-gray-900 dark:bg-white text-white dark:text-zinc-900 px-3 h-8 rounded-sm text-[10px] font-bold shadow-sm transition-all hover:bg-gray-800 dark:hover:bg-zinc-100 uppercase tracking-widest">
    <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path stroke-linecap="round" stroke-linejoin="round" d="M12 4v16m8-8H4"/></svg>
    <span class="hidden @min-[40rem]:inline">New Task</span>
  </button>
</template>

<script setup>
function openNewTask() {
  window.dispatchEvent(new CustomEvent('open-new-task'))
}
</script>
