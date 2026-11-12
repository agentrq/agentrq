<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
// Events and Hooks: one page, two tabs. A hook only exists to fire events.
defineProps({ active: { type: String, required: true } })

const tabs = [
  { key: 'events', label: 'Events', to: '/events' },
  { key: 'hooks', label: 'Hooks', to: '/events/hooks' },
]
</script>

<template>
  <nav class="px-4 mb-4 flex gap-1 border-b border-gray-100 dark:border-zinc-800" aria-label="Events">
    <RouterLink v-for="tab in tabs" :key="tab.key" :to="tab.to" :data-test="`tab-${tab.key}`"
      :aria-current="tab.key === active ? 'page' : undefined"
      class="px-3 py-2 -mb-px text-[11px] font-black uppercase tracking-widest border-b-2 transition-colors"
      :class="tab.key === active
        ? 'border-black dark:border-white text-gray-900 dark:text-zinc-50'
        : 'border-transparent text-gray-400 dark:text-zinc-500 hover:text-gray-700 dark:hover:text-zinc-300'">
      {{ tab.label }}
    </RouterLink>
  </nav>
</template>
