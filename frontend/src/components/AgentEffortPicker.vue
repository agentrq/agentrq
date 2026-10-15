<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<template>
  <!-- Nothing unless the agent offers reasoning levels for the model it runs
       now: a switch to a model without them takes the picker away. -->
  <div v-if="picker.available.value" class="relative">
    <button ref="trigger"
            type="button"
            title="Reasoning effort"
            @click.stop="toggle"
            :disabled="picker.sending.value"
            :class="compact
              ? 'text-[9px] font-medium text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-100 max-w-[100px]'
              : 'w-7 h-7 sm:w-auto sm:px-2 gap-1 justify-center rounded-md text-[10px] font-semibold bg-gray-100 dark:bg-zinc-800 text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-200 dark:hover:bg-zinc-700 border border-gray-200 dark:border-zinc-700'"
            class="flex items-center transition-all disabled:opacity-40 truncate">
      <svg v-if="!compact" class="w-3.5 h-3.5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
        <path stroke-linecap="round" stroke-linejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z" />
      </svg>
      <span :class="compact ? 'truncate' : 'hidden sm:inline truncate'">{{ picker.selectedName.value || 'Effort' }}</span>
      <span v-if="picker.isPending.value" class="ml-1 shrink-0 opacity-60">…</span>
    </button>

    <Teleport to="body">
      <div v-if="open"
           ref="menu"
           :style="{ top: `${position.top}px`, left: `${position.left}px`, maxHeight: `${position.maxHeight}px` }"
           class="fixed w-[200px] overflow-y-auto bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl shadow-2xl z-50 p-2"
           @click.stop>
        <h3 class="text-[10px] font-bold uppercase tracking-wider text-gray-500 dark:text-zinc-400 px-2 py-1.5">Effort</h3>
        <button v-for="level in picker.efforts.value" :key="level.id"
                type="button"
                :title="level.description || undefined"
                @click="pick(level.id)"
                :class="level.id === picker.selectedId.value
                  ? 'bg-gray-100 dark:bg-zinc-800 text-gray-900 dark:text-white'
                  : 'text-gray-600 dark:text-zinc-400 hover:bg-gray-50 dark:hover:bg-zinc-800/60'"
                class="w-full text-left px-2 py-1.5 rounded-md text-[11px] font-semibold transition-colors flex items-center gap-2">
          <span class="truncate grow">{{ level.name || level.id }}</span>
          <span v-if="level.id === picker.selectedId.value" class="shrink-0 text-[9px]">●</span>
        </button>
      </div>
    </Teleport>
  </div>
</template>

<script setup>
/**
 * Choosing the reasoning level the workspace's agent runs at. The same menu as
 * AgentModelPicker, beside it; the logic is in useAgentEffortPicker.
 */
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';

import { MAX_POPOVER_HEIGHT, popoverPosition } from '../composables/useAgentModelPicker';
import { useAgentEffortPicker } from '../composables/useAgentEffortPicker';
import { useToasts } from '../composables/useToasts';
import { setAgentEffort } from '../api';

const props = defineProps({
  workspace: { type: Object, default: null },
  compact: { type: Boolean, default: false },
});

const open = ref(false);
const trigger = ref(null);
const menu = ref(null);
const position = ref({ top: 0, left: 0, maxHeight: MAX_POPOVER_HEIGHT });
const { addToast } = useToasts();

const picker = useAgentEffortPicker({
  workspace: () => props.workspace,
  selectEffort: (effort) => setAgentEffort(props.workspace?.id, effort),
});

async function toggle() {
  open.value = !open.value;
  if (!open.value) return;
  const anchor = trigger.value?.getBoundingClientRect();
  await nextTick();
  const box = menu.value?.getBoundingClientRect();
  position.value = popoverPosition(
    anchor,
    { width: box?.width || 200, height: box?.height || 0 },
    { width: window.innerWidth, height: window.innerHeight },
  );
}

function close() {
  open.value = false;
}

function onDocumentClick(event) {
  if (!open.value || trigger.value?.contains(event.target) || menu.value?.contains(event.target)) return;
  open.value = false;
}

watch(picker.error, (message) => {
  if (message) addToast(message, 'error');
});

async function pick(id) {
  open.value = false;
  await picker.choose(id);
}

let tick;
onMounted(() => {
  tick = setInterval(() => picker.expirePending(), 1000);
  document.addEventListener('click', onDocumentClick);
  window.addEventListener('scroll', close, true);
  window.addEventListener('resize', close);
});
onBeforeUnmount(() => {
  clearInterval(tick);
  document.removeEventListener('click', onDocumentClick);
  window.removeEventListener('scroll', close, true);
  window.removeEventListener('resize', close);
});
</script>
