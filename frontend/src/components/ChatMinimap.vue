<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<template>
  <nav aria-label="Your messages" data-testid="chat-minimap"
       class="relative w-6 flex-col justify-center py-4" @mouseleave="hovered = null">
    <button type="button" data-testid="chat-minimap-autoscroll" :aria-pressed="paused ? 'true' : 'false'"
            :title="paused ? 'Resume autoscroll' : 'Pause autoscroll'" :aria-label="paused ? 'Resume autoscroll' : 'Pause autoscroll'"
            class="shrink-0 mb-2 mx-auto w-6 h-6 flex items-center justify-center rounded-md outline-none transition-colors focus-visible:ring-2 focus-visible:ring-gray-300 dark:focus-visible:ring-zinc-600"
            :class="paused
              ? 'bg-gray-900 text-white dark:bg-zinc-100 dark:text-zinc-900'
              : 'text-gray-400 hover:text-gray-700 hover:bg-gray-100 dark:text-zinc-500 dark:hover:text-zinc-200 dark:hover:bg-zinc-800'"
            @click="emit('toggle-autoscroll')">
      <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.25" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <path v-if="paused" d="M12 4v12M6 11l6 6 6-6M5 20h14" />
        <path v-else d="M9 5v14M15 5v14" />
      </svg>
    </button>
    <ol class="flex flex-col items-center max-h-full overflow-y-auto [scrollbar-width:none]">
      <li v-for="e in entries" :key="e.id" class="shrink-0">
        <button type="button" :data-minimap-dot="e.id" :aria-current="e.id === activeId ? 'true' : null"
                :aria-label="`Message ${e.n}: ${e.preview}`"
                class="group w-6 h-4 flex items-center justify-center outline-none"
                @click="emit('jump', e.id)" @mouseenter="hover($event, e)" @focus="hover($event, e)" @blur="hovered = null">
          <span class="block rounded-full transition-all duration-150"
                :class="e.id === activeId
                  ? 'w-2 h-2 bg-gray-700 dark:bg-zinc-200'
                  : 'w-1.5 h-1.5 bg-gray-300 dark:bg-zinc-600 group-hover:bg-gray-500 dark:group-hover:bg-zinc-400 group-focus-visible:bg-gray-500 dark:group-focus-visible:bg-zinc-400'"></span>
        </button>
      </li>
    </ol>
    <!-- Up to two lines of the message, beside its dot. Drawn here rather than
         inside the list, whose scrolling would clip it. -->
    <div v-if="hovered" data-testid="chat-minimap-preview" aria-hidden="true"
         class="absolute right-full mr-1 w-max max-w-72 -translate-y-1/2 pointer-events-none z-20 px-3.5 py-2.5 rounded-2xl rounded-br-md bg-gray-100 dark:bg-zinc-800 shadow-lg text-[13px] font-medium leading-relaxed text-gray-800 dark:text-zinc-200 line-clamp-2 break-words"
         :style="{ top: `${hovered.top}px` }">{{ hovered.preview || '(attachment)' }}</div>
  </nav>
</template>

<script setup>
/**
 * One dot per message the person sent, down the side of the chat: the dot of
 * the message being read stands out, hovering shows the start of a message,
 * and a click scrolls to it. The button above them pauses autoscroll for the
 * task. The tracking lives in useChatMinimap.
 */
import { ref } from 'vue';

defineProps({
  entries: { type: Array, required: true },
  activeId: { type: String, default: null },
  paused: { type: Boolean, default: false },
});
const emit = defineEmits(['jump', 'toggle-autoscroll']);

const hovered = ref(null);

function hover(event, entry) {
  const dot = event.currentTarget.getBoundingClientRect();
  const nav = event.currentTarget.closest('nav').getBoundingClientRect();
  hovered.value = { id: entry.id, preview: entry.preview, top: dot.top - nav.top + dot.height / 2 };
}
</script>
