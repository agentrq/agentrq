<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->
<template>
  <!-- A row of quick reactions, and optionally a way to the system's full
       emoji keyboard. Where it opens is the parent's to say: it is absolutely
       positioned inside whatever relative box it is dropped into. -->
  <div v-click-outside="() => $emit('close')" @keydown.esc="$emit('close')"
       role="menu" aria-label="Emoji"
       class="absolute z-50 flex items-center gap-0.5 p-1 bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-full shadow-xl animate-in fade-in">
    <button v-for="emoji in QUICK_REACTIONS" :key="emoji" type="button" role="menuitem"
            @click="$emit('pick', emoji)"
            :aria-label="`React ${emoji}`"
            class="h-7 w-7 rounded-full text-[16px] leading-none flex items-center justify-center hover:bg-gray-100 dark:hover:bg-zinc-800 transition-colors">{{ emoji }}</button>
    <button v-if="showMore" type="button" role="menuitem"
            @click="$emit('more')"
            title="All emoji"
            aria-label="All emoji"
            class="h-7 w-7 rounded-full text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-100 dark:hover:bg-zinc-800 transition-colors flex items-center justify-center">
      <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M12 5v14M5 12h14" /></svg>
    </button>
  </div>
</template>

<script setup>
import { QUICK_REACTIONS } from '../composables/useEmojiReactions';

defineProps({
  /** Offer the system's emoji keyboard after the quick reactions. */
  showMore: { type: Boolean, default: false },
});

defineEmits(['pick', 'more', 'close']);
</script>
