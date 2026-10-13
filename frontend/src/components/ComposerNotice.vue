<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
/**
 * The strip under a reply box that says why it will not send, or what will
 * happen when it does: offline (red), waiting its turn (gray), not started
 * (amber). A link or button for what to do about it goes in the slot.
 */
defineProps({
  tone: { type: String, default: 'gray' },
  /** Whether the dot pulses, for a state that is ongoing rather than settled. */
  pulse: { type: Boolean, default: false },
})

const TONES = {
  red: { box: 'bg-red-50 dark:bg-red-500/10 border-red-200 dark:border-red-500/20', dot: 'bg-red-500', text: 'text-red-700 dark:text-red-400' },
  gray: { box: 'bg-gray-50 dark:bg-zinc-800/50 border-gray-200 dark:border-zinc-700', dot: 'bg-gray-400 dark:bg-zinc-500', text: 'text-gray-600 dark:text-zinc-400' },
  amber: { box: 'bg-amber-50 dark:bg-amber-500/10 border-amber-200 dark:border-amber-500/20', dot: 'bg-amber-400', text: 'text-amber-700 dark:text-amber-400' },
}
</script>

<template>
  <div :class="TONES[tone].box" class="flex items-center gap-3 mt-2 px-3 py-2 border rounded-sm">
    <span :class="[TONES[tone].dot, { 'animate-pulse': pulse }]" class="w-2.5 h-2.5 rounded-full shrink-0"></span>
    <p :class="TONES[tone].text" class="text-[10px] font-bold"><slot /></p>
    <slot name="action" />
  </div>
</template>
