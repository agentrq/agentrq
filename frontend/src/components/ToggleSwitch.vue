<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/**
 * The small on/off switch of the hook and event pages. Name it with an
 * `aria-label` or `aria-labelledby`, passed through to the button.
 */
const on = defineModel({ type: Boolean, default: false })
defineProps({ disabled: Boolean })
</script>

<template>
  <button type="button" role="switch" :aria-checked="on" :disabled="disabled" @click="on = !on"
    class="relative inline-flex w-9 h-5 shrink-0 rounded-full transition-colors disabled:opacity-50 disabled:cursor-not-allowed outline-none focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:ring-gray-900 dark:focus-visible:ring-zinc-300 dark:focus-visible:ring-offset-zinc-900"
    :class="on ? 'bg-black dark:bg-white' : 'bg-gray-200 dark:bg-zinc-700'">
    <span class="absolute top-0.5 w-4 h-4 rounded-full bg-white transition-all"
      :class="on ? 'left-[18px] dark:bg-black' : 'left-0.5 dark:bg-zinc-400'"></span>
  </button>
</template>
