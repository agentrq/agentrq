<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
import { nextTick, onMounted, onUnmounted, ref, watch } from 'vue';

const props = defineProps({
  show: Boolean,
  x: { type: Number, default: 0 },
  y: { type: Number, default: 0 },
  // [{ key, label, disabled?, detail? }] or { key, divider: true }. A disabled
  // item stays listed with its `detail` saying why, rather than vanishing.
  items: { type: Array, default: () => [] },
  // Opened from the keyboard: the first item takes the focus, so the menu can
  // be used without a pointer.
  autofocus: Boolean
});

const emit = defineEmits(['close', 'select']);
const root = ref(null);

function onSelect(item) {
  if (item.disabled) return;
  emit('select', item.key);
  emit('close');
}

const buttons = () => Array.from(root.value?.querySelectorAll('button:not([disabled])') ?? []);

watch(() => props.show, async (open) => {
  if (!open || !props.autofocus) return;
  await nextTick();
  buttons()[0]?.focus();
});

function onKeydown(e) {
  if (e.key === 'Escape') {
    e.preventDefault();
    emit('close');
    return;
  }
  if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
  e.preventDefault();
  const list = buttons();
  const i = list.indexOf(document.activeElement);
  const next = e.key === 'ArrowDown' ? (i + 1) % list.length : (i - 1 + list.length) % list.length;
  list[next]?.focus();
}

function onClickOutside() {
  emit('close');
}

onMounted(() => {
  window.addEventListener('click', onClickOutside);
  window.addEventListener('contextmenu', onClickOutside);
  window.addEventListener('scroll', onClickOutside, true);
});

onUnmounted(() => {
  window.removeEventListener('click', onClickOutside);
  window.removeEventListener('contextmenu', onClickOutside);
  window.removeEventListener('scroll', onClickOutside, true);
});
</script>

<template>
  <Teleport to="body">
    <div v-if="show" ref="root" role="menu"
         class="fixed z-[150] min-w-[160px] max-w-[280px] py-1 bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-lg shadow-2xl"
         :style="{ top: y + 'px', left: x + 'px' }"
         @click.stop
         @keydown="onKeydown"
         @contextmenu.prevent.stop>
      <template v-for="item in items" :key="item.key">
        <!-- Extension entries sit below this, so the app's own items never move
             when something is installed and it stays visible where a row came
             from. -->
        <div v-if="item.divider" class="my-1 border-t border-gray-100 dark:border-zinc-800"></div>
        <button v-else role="menuitem" @click="onSelect(item)" :disabled="item.disabled"
                class="w-full text-left px-3 py-2 text-[11px] font-semibold text-gray-700 dark:text-zinc-300 hover:bg-gray-50 dark:hover:bg-zinc-800 hover:text-black dark:hover:text-white focus:bg-gray-50 dark:focus:bg-zinc-800 focus:outline-none transition-colors disabled:opacity-50 disabled:cursor-not-allowed disabled:hover:bg-transparent disabled:hover:text-gray-700 dark:disabled:hover:text-zinc-300">
          <span class="block">{{ item.label }}</span>
          <span v-if="item.detail" class="block mt-0.5 text-[10px] font-medium text-gray-500 dark:text-zinc-400">{{ item.detail }}</span>
        </button>
      </template>
    </div>
  </Teleport>
</template>
