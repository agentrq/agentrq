<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- The app's own dropdown, in place of a native <select>: each item may carry
     a person's avatar (`owner`) or the avatars of everyone it belongs to
     (`owners`), the chosen one is ticked, and a long list gets a search box.
     Arrows, Home/End, Enter, Escape and typing a letter work as in a select. -->
<template>
  <div ref="root" class="relative" :class="block ? 'w-full' : 'inline-block min-w-0 max-w-[12rem]'" @keydown="onKeydown">
    <button ref="trigger" type="button" :data-test="dataTest" :aria-label="ariaLabel" aria-haspopup="listbox" :aria-expanded="open"
            :disabled="disabled" @click="toggle"
            :class="block ? 'h-9' : 'h-8'"
            class="w-full flex items-center gap-1.5 min-w-0 px-2 bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-lg text-xs text-gray-700 dark:text-zinc-200 hover:border-gray-300 dark:hover:border-zinc-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-gray-900/20 dark:focus-visible:ring-white/20 transition-colors disabled:opacity-50">
      <span v-if="selected?.owner" class="shrink-0 flex"><OwnerAvatar :owner="selected.owner" /></span>
      <span v-else-if="selected?.owners?.length" class="shrink-0 flex -space-x-1"><OwnerAvatar v-for="o in selected.owners.slice(0, 3)" :key="o.userId ?? o.name" :owner="o" /></span>
      <span class="flex-1 min-w-0 truncate text-left" :class="selected ? '' : 'text-gray-400 dark:text-zinc-500'">{{ selected?.label || placeholder }}</span>
      <svg class="w-3 h-3 shrink-0 text-gray-400 dark:text-zinc-500 transition-transform" :class="open ? 'rotate-180' : ''" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M19 9l-7 7-7-7" /></svg>
    </button>

    <div v-if="open" :data-test="`${dataTest}-menu`"
         :class="align === 'right' ? 'right-0' : 'left-0'"
         class="absolute top-full mt-1 z-[130] min-w-full w-64 max-w-[calc(100vw-2rem)] bg-white dark:bg-zinc-900 border border-gray-200/80 dark:border-zinc-700/70 rounded-xl shadow-[0_12px_32px_-8px_rgba(0,0,0,0.18)] dark:shadow-[0_12px_32px_-8px_rgba(0,0,0,0.6)] p-1">
      <input v-if="searchable" ref="search" v-model="query" type="text" :data-test="`${dataTest}-search`" placeholder="Search" aria-label="Search" autocomplete="off"
             class="w-full mb-1 px-2 py-1.5 text-xs bg-gray-50 dark:bg-zinc-800/60 border border-gray-200 dark:border-zinc-700 rounded-md text-gray-900 dark:text-zinc-100 placeholder-gray-400 dark:placeholder-zinc-500 outline-none focus:border-gray-400 dark:focus:border-zinc-500" />
      <ul ref="list" role="listbox" :aria-label="ariaLabel" class="max-h-64 overflow-y-auto custom-scrollbar">
        <li v-for="(it, i) in shown" :key="it.value" role="option" :aria-selected="it.value === modelValue"
            :data-test="`${dataTest}-option`" :data-value="it.value" :data-active="i === active"
            @click="pick(it)" @mousemove="active = i"
            :class="i === active ? 'bg-gray-100 dark:bg-zinc-800' : ''"
            class="flex items-center gap-2 px-2 py-1.5 rounded-md text-xs text-gray-800 dark:text-zinc-100 cursor-pointer select-none">
          <span class="w-3.5 shrink-0 text-gray-900 dark:text-white">
            <svg v-if="it.value === modelValue" data-test="picker-check" class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" /></svg>
          </span>
          <span v-if="it.owner" class="shrink-0 flex"><OwnerAvatar :owner="it.owner" /></span>
          <span class="flex-1 min-w-0 truncate" :class="it.value === modelValue ? 'font-semibold' : 'font-medium'">{{ it.label }}</span>
          <span v-if="it.owners?.length" class="shrink-0 flex -space-x-1">
            <OwnerAvatar v-for="o in it.owners.slice(0, 3)" :key="o.userId ?? o.name" :owner="o" />
            <span v-if="it.owners.length > 3" class="text-[9px] text-gray-400 dark:text-zinc-500 pl-1.5">+{{ it.owners.length - 3 }}</span>
          </span>
        </li>
        <li v-if="shown.length === 0" data-test="picker-empty" class="px-2 py-1.5 text-xs text-gray-400 dark:text-zinc-500">No match</li>
      </ul>
    </div>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue';
import OwnerAvatar from './OwnerAvatar.vue';
import { SEARCH_THRESHOLD, filterPickerItems, isTypeaheadKey, stepIndex, typeaheadIndex } from '../composables/usePicker';

const props = defineProps({
  modelValue: { type: String, default: '' },
  // [{ value, label, owner?, owners? }]
  items: { type: Array, default: () => [] },
  ariaLabel: { type: String, default: '' },
  placeholder: { type: String, default: 'Choose…' },
  dataTest: { type: String, default: 'picker' },
  // As wide as its container: in a form, rather than a toolbar.
  block: { type: Boolean, default: false },
  // The menu's edge: 'right' for a picker near the right of the screen.
  align: { type: String, default: 'left' },
  disabled: { type: Boolean, default: false },
});
const emit = defineEmits(['update:modelValue']);

const root = ref(null);
const trigger = ref(null);
const search = ref(null);
const list = ref(null);
const open = ref(false);
const query = ref('');
const active = ref(-1);

const selected = computed(() => props.items.find((it) => it.value === props.modelValue) || null);
const searchable = computed(() => props.items.length > SEARCH_THRESHOLD);
const shown = computed(() => filterPickerItems(props.items, query.value));

// Typing a letter in a list with no search box: the letters typed within a
// moment, as a select reads them.
let typed = '';
let typedAt = 0;

function onOutside(e) {
  if (!root.value?.contains(e.target)) close(false);
}

async function show() {
  if (open.value || props.disabled) return;
  query.value = '';
  open.value = true;
  active.value = Math.max(0, shown.value.findIndex((it) => it.value === props.modelValue));
  document.addEventListener('mousedown', onOutside);
  await nextTick();
  if (searchable.value) search.value?.focus();
  reveal();
}

function close(refocus = true) {
  if (!open.value) return;
  open.value = false;
  document.removeEventListener('mousedown', onOutside);
  if (refocus) trigger.value?.focus();
}

const toggle = () => (open.value ? close() : show());

function pick(it) {
  emit('update:modelValue', it.value);
  close();
}

// The active row kept in sight as the arrows move it.
function reveal() {
  const el = list.value?.children?.[active.value];
  el?.scrollIntoView?.({ block: 'nearest' });
}

function move(i) {
  active.value = i;
  nextTick(reveal);
}

function onKeydown(e) {
  if (!open.value) {
    if (['ArrowDown', 'ArrowUp', 'Enter', ' '].includes(e.key) && e.target === trigger.value) {
      e.preventDefault();
      show();
    }
    return;
  }
  const n = shown.value.length;
  switch (e.key) {
    case 'Escape':
      // One Escape closes the menu, never the dialog around it.
      e.preventDefault();
      e.stopPropagation();
      close();
      return;
    case 'ArrowDown':
    case 'ArrowUp':
      e.preventDefault();
      move(stepIndex(active.value, e.key === 'ArrowDown' ? 1 : -1, n));
      return;
    case 'Home':
    case 'End':
      if (e.target === search.value) return;
      e.preventDefault();
      move(e.key === 'Home' ? 0 : n - 1);
      return;
    case 'Enter':
      e.preventDefault();
      if (shown.value[active.value]) pick(shown.value[active.value]);
      return;
    case 'Tab':
      close(false);
      return;
    default:
      if (e.target === search.value || !isTypeaheadKey(e)) return;
      typed = Date.now() - typedAt > 600 ? e.key : typed + e.key;
      typedAt = Date.now();
      {
        // A word typed out stays on its item; one letter again moves on.
        const from = typed.length > 1 ? active.value - 1 : active.value;
        const i = typeaheadIndex(shown.value, typed, from);
        if (i >= 0) move(i);
      }
  }
}

// The search narrows the list: the first match is the one Enter picks.
watch(query, () => { active.value = shown.value.length ? 0 : -1; });

onBeforeUnmount(() => document.removeEventListener('mousedown', onOutside));
</script>
