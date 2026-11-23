<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/**
 * The machine switcher, opened with `M`.
 *
 * The same box as the workspace switcher — same overlay, same row geometry,
 * same ↑↓/↵/Esc keys — because it answers the same question about a different
 * list. The matching and the fetch live in `useMachineSwitcher`.
 */
import { computed, nextTick, ref, watch } from 'vue';
import { useRouter } from 'vue-router';

import {
  machineLabel,
  machineRoute,
  matchMachines,
  useMachineSwitcher,
} from '../composables/useMachineSwitcher';

const props = defineProps({
  show: Boolean,
  /** The machine on screen, so its row can be marked rather than hidden. */
  currentMachineId: { type: String, default: '' },
});
const emit = defineEmits(['close']);

const router = useRouter();
const { machines, loading, error, load } = useMachineSwitcher();

const query = ref('');
const highlighted = ref(0);
const inputRef = ref(null);

const results = computed(() => matchMachines(machines.value, query.value, 10));
const hasMachines = computed(() => machines.value.length > 0);

watch(
  () => props.show,
  async (open) => {
    if (!open) return;
    query.value = '';
    highlighted.value = 0;
    load();
    await nextTick();
    inputRef.value?.focus();
  }
);

watch(results, (rows) => {
  if (highlighted.value >= rows.length) highlighted.value = 0;
});

function move(delta) {
  const count = results.value.length;
  if (count === 0) return;
  highlighted.value = (highlighted.value + delta + count) % count;
}

function open(machine) {
  emit('close');
  router.push(machineRoute(machine));
}

function openList() {
  emit('close');
  router.push('/machines');
}

function submit() {
  const hit = results.value[highlighted.value];
  if (hit) open(hit);
}

const isCurrent = (machine) => !!props.currentMachineId && String(machine.id) === props.currentMachineId;
</script>

<template>
  <Transition name="fade">
    <div v-if="show" class="fixed inset-0 z-[150]" role="dialog" aria-modal="true" aria-label="Switch machine">
      <div class="fixed inset-0 bg-gray-900/60 backdrop-blur-sm" @click="emit('close')"></div>

      <div class="relative mx-auto mt-[12vh] w-[92%] max-w-xl">
        <div class="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl shadow-2xl overflow-hidden">
          <!-- Query -->
          <div class="flex items-center gap-3 px-4 py-3 border-b border-gray-100 dark:border-zinc-800">
            <svg class="w-4 h-4 shrink-0 text-gray-400 dark:text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
            </svg>
            <input ref="inputRef" v-model="query" type="text" autocomplete="off" spellcheck="false"
                   placeholder="Search machines by name"
                   class="grow bg-transparent text-[14px] text-gray-900 dark:text-zinc-100 placeholder:text-gray-400 dark:placeholder:text-zinc-600 focus:outline-none"
                   @keydown.down.prevent="move(1)"
                   @keydown.up.prevent="move(-1)"
                   @keydown.enter.prevent="submit"
                   @keydown.esc.prevent="emit('close')" />
            <kbd class="shrink-0 text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 border border-gray-200 dark:border-zinc-700 rounded px-1.5 py-0.5">Esc</kbd>
          </div>

          <!-- Results -->
          <ul v-if="results.length" class="max-h-80 overflow-y-auto py-1">
            <li v-for="(m, i) in results" :key="m.id">
              <button type="button" @click="open(m)" @mouseenter="highlighted = i"
                      class="w-full text-left px-4 py-2.5 flex items-center gap-3 transition-colors"
                      :class="i === highlighted ? 'bg-gray-50 dark:bg-zinc-800' : 'hover:bg-gray-50/60 dark:hover:bg-zinc-800/60'">
                <span class="shrink-0 w-1.5 h-1.5 rounded-full"
                      :class="m.online ? 'bg-emerald-500' : 'bg-gray-300 dark:bg-zinc-600'"
                      :title="m.online ? 'Online' : 'Offline'"></span>
                <span class="grow min-w-0 truncate text-[13px] font-medium text-gray-900 dark:text-zinc-100">{{ machineLabel(m) }}</span>
                <span class="shrink-0 text-[11px] text-gray-400 dark:text-zinc-500 tabular-nums">
                  {{ m.sessions ?? 0 }} {{ (m.sessions ?? 0) === 1 ? 'agent' : 'agents' }}
                </span>
                <span v-if="!m.enabled" class="shrink-0 text-[9px] font-black uppercase tracking-widest text-amber-600/70 dark:text-amber-500/70">Off</span>
                <span v-else-if="isCurrent(m)" class="shrink-0 text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Current</span>
              </button>
            </li>
          </ul>

          <!-- Loading, errors and empty states -->
          <div v-else class="px-4 py-6 text-center">
            <p v-if="loading" class="text-[12px] text-gray-500 dark:text-zinc-400">Loading machines…</p>
            <p v-else-if="error" class="text-[12px] text-red-600 dark:text-red-400">{{ error }}</p>
            <p v-else-if="!hasMachines" class="text-[12px] text-gray-500 dark:text-zinc-400">
              No machines yet.
              <button type="button" @click="openList" class="block mx-auto mt-1 text-gray-400 dark:text-zinc-500 underline hover:text-gray-600 dark:hover:text-zinc-300">Set one up from the machines page.</button>
            </p>
            <p v-else class="text-[12px] text-gray-500 dark:text-zinc-400">
              No machine matches &ldquo;{{ query }}&rdquo;.
            </p>
          </div>

          <!-- Footer -->
          <div class="flex items-center justify-between gap-4 px-4 py-2 border-t border-gray-100 dark:border-zinc-800 bg-gray-50/50 dark:bg-zinc-800/30">
            <span class="flex items-center gap-4 shrink-0">
              <span class="text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">↑↓ Navigate</span>
              <span class="text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">↵ Open</span>
            </span>
            <button v-if="hasMachines" type="button" @click="openList"
                    class="text-[9px] text-right text-gray-400 dark:text-zinc-500 hover:text-gray-600 dark:hover:text-zinc-300 truncate">
              All {{ machines.length }} machine{{ machines.length === 1 ? '' : 's' }} →
            </button>
          </div>
        </div>
      </div>
    </div>
  </Transition>
</template>

<style scoped>
.fade-enter-active, .fade-leave-active { transition: opacity 0.18s ease; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
</style>
