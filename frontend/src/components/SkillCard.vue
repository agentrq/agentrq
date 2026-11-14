<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- One skill in a grid of them: its name and description, a line about it
     (the `meta` slot), and a control in its corner (the `action` slot). The
     card opens the skill's page; the control sits beside the link, not in it,
     so using it never navigates. Your skills and a team's look the same. -->
<template>
  <div class="relative min-w-0">
    <RouterLink :to="to" :data-test="linkTest" :data-enabled="enabled"
                class="group h-full flex flex-col gap-1.5 min-w-0 py-3 sm:p-3 sm:bg-gray-50 sm:dark:bg-zinc-800/50 sm:rounded-sm sm:border border-gray-100 dark:border-zinc-800 hover:border-gray-300 dark:hover:border-zinc-600 hover:bg-gray-100 dark:hover:bg-zinc-800 transition-colors">
      <!-- Dimmed when no agent sees it, here or anywhere. -->
      <span data-test="skill-card-name" class="truncate text-xs leading-6 font-bold font-mono" :class="[actionWidth, muted ? 'text-gray-400 dark:text-zinc-500' : 'text-gray-800 dark:text-zinc-100']">{{ name }}</span>
      <span class="line-clamp-2 min-h-[2lh] text-[11px] leading-snug break-words"
            :class="muted ? 'text-gray-400 dark:text-zinc-500' : 'text-gray-500 dark:text-zinc-400'">{{ description }}</span>
      <span class="mt-auto flex items-center gap-1.5 min-w-0 text-[10px] text-gray-400 dark:text-zinc-500 tabular-nums">
        <slot name="meta" />
      </span>
    </RouterLink>
    <div class="absolute top-3 right-0 sm:right-3 shrink-0">
      <slot name="action" />
    </div>
  </div>
</template>

<script setup>
defineProps({
  to: { type: [String, Object], required: true },
  name: { type: String, default: '' },
  description: { type: String, default: '' },
  muted: { type: Boolean, default: false },
  linkTest: { type: String, default: 'skill-card' },
  // The switch's state, for the page's tests and styles; none on a team's.
  enabled: { type: Boolean, default: undefined },
  // Room the name leaves for the corner's control.
  actionWidth: { type: String, default: 'pr-12' },
});
</script>
