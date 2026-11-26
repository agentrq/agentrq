<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<template>
  <section class="relative w-full min-w-0" data-test="pinned-card" :data-message-id="message.id" :data-collapsed="collapsed ? 'true' : undefined">
    <div class="min-w-0" :class="collapsed ? FOLDED_CARD : 'max-h-[45vh] overflow-y-auto'" data-test="pinned-card-body">
      <A2uiSurface :metadata="message.metadata" :text="message.text" :attachments="message.attachments" :workspace-id="workspaceId"
                   class="text-[13px] text-gray-800 dark:text-zinc-200" :read-only="readOnly" :busy="busy" @action="emit('action', $event)" />
    </div>
    <!-- Over the card's top-right corner, clear of its title. -->
    <div class="absolute top-1.5 right-1.5 flex items-center gap-0.5 rounded-md bg-white/90 dark:bg-zinc-900/90">
      <button type="button" @click="emit('toggle')" :aria-expanded="!collapsed" :title="collapsed ? 'Expand card' : 'Collapse card'"
              class="p-1 rounded text-gray-400 dark:text-zinc-500 hover:text-gray-800 dark:hover:text-zinc-200 transition-colors"
              data-test="pinned-card-toggle">
        <svg class="w-3 h-3 transition-transform" :class="collapsed ? '' : 'rotate-180'" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M19 9l-7 7-7-7" /></svg>
      </button>
      <button type="button" @click="emit('unpin')" title="Unpin card"
              class="p-1 rounded text-gray-700 dark:text-zinc-200 hover:text-gray-400 dark:hover:text-zinc-500 transition-colors"
              data-test="pinned-card-unpin">
        <svg class="w-3 h-3" fill="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path :d="PIN_ICON" /></svg>
      </button>
    </div>
  </section>
</template>

<script setup>
/**
 * The progress card pinned over the conversation, drawn as itself and still
 * updated in place as the agent works. Folded, it keeps its top (the title
 * and bar of a progress card) and fades out the rest.
 */
import A2uiSurface from './A2uiSurface.vue';
import { FOLDED_CARD, PIN_ICON } from '../../composables/useCardPins.js';

defineProps({
  message: { type: Object, required: true },
  collapsed: Boolean,
  workspaceId: { type: String, default: '' },
  readOnly: Boolean,
  busy: Boolean,
});
const emit = defineEmits(['toggle', 'unpin', 'action']);
</script>
