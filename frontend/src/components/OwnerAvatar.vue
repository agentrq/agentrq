<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- A teammate's picture, or their initial: in the team sidebar's groups, on
     task cards and in the members list. With `label`, their name beside it,
     as the owner chip a team-mode task card carries. -->
<template>
  <span class="inline-flex items-center gap-1.5 min-w-0 leading-none" data-test="owner-avatar" :title="owner?.name || owner?.email || ''">
    <span :class="[size === 'sm' ? 'w-4 h-4 text-[8px]' : 'w-6 h-6 text-[10px]']"
          class="shrink-0 rounded-full bg-gray-200 dark:bg-zinc-700 text-gray-600 dark:text-zinc-200 font-semibold inline-flex items-center justify-center overflow-hidden ring-1 ring-white dark:ring-zinc-900">
      <img v-if="owner?.picture" :src="owner.picture" alt="" class="w-full h-full object-cover" />
      <template v-else>{{ initial }}</template>
    </span>
    <span v-if="label" class="truncate text-[10px] font-medium text-gray-500 dark:text-zinc-400">{{ owner?.name || owner?.email }}</span>
  </span>
</template>

<script setup>
import { computed } from 'vue';

const props = defineProps({
  owner: { type: Object, default: null },
  size: { type: String, default: 'sm' },
  label: { type: Boolean, default: false },
});

const initial = computed(() => (props.owner?.name || props.owner?.email || '?').charAt(0).toUpperCase());
</script>
