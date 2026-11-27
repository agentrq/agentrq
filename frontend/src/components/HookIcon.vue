<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/**
 * A hook's icon from its manifest, or its name's first two letters when it
 * has none. The icon is a data: URI the server checked; it goes in an <img>,
 * never v-html, so an SVG's scripts never run. The tile is white in both
 * themes, as most service marks are drawn for a light background.
 */
defineProps({
  name: { type: String, default: '' },
  icon: { type: String, default: '' },
})
</script>

<template>
  <span aria-hidden="true"
    class="shrink-0 w-8 h-8 flex items-center justify-center rounded-lg border border-gray-100 dark:border-zinc-700 overflow-hidden"
    :class="icon ? 'bg-white p-1.5' : 'bg-gray-50 dark:bg-zinc-800 text-[10px] font-black uppercase text-gray-500 dark:text-zinc-400'">
    <img v-if="icon" data-test="hook-icon" :src="icon" alt="" class="w-full h-full object-contain" />
    <span v-else>{{ name.slice(0, 2) }}</span>
  </span>
</template>
