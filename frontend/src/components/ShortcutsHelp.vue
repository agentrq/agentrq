<script setup>
/**
 * The `?` sheet.
 *
 * Rendered straight from `SHORTCUTS`, so a binding added there shows up here
 * without anyone remembering to document it — a shortcut nobody can discover is
 * a shortcut nobody uses.
 */
import { computed } from 'vue';

import { SHORTCUTS, formatShortcut } from '../composables/useKeyboardShortcuts';

const props = defineProps({
  show: Boolean,
  /** Whether this keyboard's modifier is Command rather than Control. */
  mac: Boolean,
});
const emit = defineEmits(['close']);

const GROUPS = [
  { scope: 'global', title: 'Anywhere' },
  { scope: 'task', title: 'In a task' },
];

const groups = computed(() =>
  GROUPS.map((group) => ({
    ...group,
    items: SHORTCUTS.filter((s) => s.scope === group.scope).map((s) => ({
      ...s,
      combo: formatShortcut(s, { mac: props.mac }),
    })),
  }))
);
</script>

<template>
  <Transition name="fade">
    <div v-if="show" class="fixed inset-0 z-[150]" role="dialog" aria-modal="true" aria-label="Keyboard shortcuts">
      <div class="fixed inset-0 bg-gray-900/60 backdrop-blur-sm" @click="emit('close')"></div>

      <div class="relative mx-auto mt-[14vh] w-[92%] max-w-md">
        <div class="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl shadow-2xl overflow-hidden"
             tabindex="-1" @keydown.esc.prevent="emit('close')">
          <div class="flex items-center justify-between px-5 py-4 border-b border-gray-100 dark:border-zinc-800">
            <h2 class="text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-100">Keyboard Shortcuts</h2>
            <button type="button" @click="emit('close')" class="text-gray-400 hover:text-gray-600 dark:hover:text-zinc-300 transition-colors" aria-label="Close">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          </div>

          <div v-for="group in groups" :key="group.scope" class="px-5 py-4 border-b border-gray-100 dark:border-zinc-800 last:border-b-0">
            <p class="text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 mb-3">{{ group.title }}</p>
            <ul class="flex flex-col gap-2.5">
              <li v-for="item in group.items" :key="item.id" class="flex items-baseline justify-between gap-4">
                <span class="min-w-0">
                  <span class="block text-[13px] font-medium text-gray-900 dark:text-zinc-100">{{ item.label }}</span>
                  <span v-if="item.hint" class="block text-[11px] text-gray-400 dark:text-zinc-500">{{ item.hint }}</span>
                </span>
                <span class="shrink-0 flex items-center gap-1.5">
                  <kbd class="text-[10px] font-mono text-gray-700 dark:text-zinc-200 border border-gray-200 dark:border-zinc-700 rounded px-1.5 py-0.5">{{ item.combo }}</kbd>
                </span>
              </li>
            </ul>
          </div>

          <p class="px-5 py-3 text-[11px] text-gray-400 dark:text-zinc-500 bg-gray-50/50 dark:bg-zinc-800/30 border-t border-gray-100 dark:border-zinc-800">
            Single-key shortcuts pause while you are typing in a field.
          </p>
        </div>
      </div>
    </div>
  </Transition>
</template>

<style scoped>
.fade-enter-active, .fade-leave-active { transition: opacity 0.18s ease; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
</style>
