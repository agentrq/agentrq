<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
/**
 * The `/` menu's list, for a `useSlashCommands` menu: the agent's commands and
 * the workspace's skills. Keyboard-first — the caller hands its keydown to the
 * menu — and the pointer is a convenience. It keeps the highlighted row in
 * view as the arrow keys move it.
 */
import { nextTick, ref, watch } from 'vue'

const props = defineProps({
  menu: { type: Object, required: true },
})
const emit = defineEmits(['accept'])

const list = ref(null)

watch(() => props.menu.selected.value, (i) => {
  nextTick(() => list.value?.children?.[i]?.scrollIntoView?.({ block: 'nearest' }))
})
</script>

<template>
  <div v-if="menu.open.value" data-test="slash-menu"
       class="max-h-52 overflow-y-auto rounded-sm border border-gray-200 dark:border-zinc-700 bg-white dark:bg-zinc-800 shadow-sm custom-scrollbar">
    <ul ref="list" role="listbox" aria-label="Commands and skills">
      <li v-for="(command, i) in menu.matches.value" :key="command.name"
          role="option" :aria-selected="i === menu.selected.value"
          @mouseenter="menu.highlight(i)"
          @mousedown.prevent="emit('accept', command)"
          :class="i === menu.selected.value ? 'bg-gray-105 dark:bg-zinc-700/60' : ''"
          class="flex items-baseline gap-2 px-3 py-1.5 cursor-pointer transition-colors">
        <span class="text-[12px] font-bold text-gray-900 dark:text-zinc-100 shrink-0">/{{ command.name }}</span>
        <span v-if="command.description"
              class="text-[11px] font-medium text-gray-500 dark:text-zinc-400 truncate">{{ command.description }}</span>
        <!-- The argument it expects, or that it is a skill, kept to the right
             so the name and what it does read as one line. -->
        <span v-if="command.input?.hint || command.hint || command.skill"
              class="ml-auto pl-3 text-[10px] font-medium text-gray-400 dark:text-zinc-500 shrink-0">{{ command.input?.hint || command.hint || 'skill' }}</span>
      </li>
    </ul>
  </div>
</template>
