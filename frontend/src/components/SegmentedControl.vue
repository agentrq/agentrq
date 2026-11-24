<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/**
 * The app's segmented pills (Agent | Human, ON | OFF), built on radio
 * buttons so the arrow keys move between them and a screen reader reads a
 * group. Name it with a legend, or an `aria-labelledby` on the group.
 */
import { useId } from 'vue'

const picked = defineModel({ type: [String, Number, Boolean], default: '' })
defineProps({
  // { value, label, test? }; `test` becomes the pill's data-test.
  options: { type: Array, required: true },
  uppercase: Boolean,
})

const name = `segment-${useId()}`
</script>

<template>
  <div role="radiogroup" class="flex p-1 bg-gray-100 dark:bg-zinc-950 border border-gray-200 dark:border-zinc-800 rounded-lg w-fit">
    <label v-for="option in options" :key="String(option.value)" :data-test="option.test"
      class="px-5 py-1.5 rounded-md text-[10px] font-semibold border cursor-pointer select-none transition-all has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-gray-900/20 dark:has-[:focus-visible]:ring-white/20"
      :class="[uppercase && 'uppercase', option.value === picked
        ? 'bg-white dark:bg-zinc-800 text-gray-900 dark:text-white shadow-sm border-gray-200 dark:border-zinc-700'
        : 'text-gray-500 dark:text-zinc-500 border-transparent hover:text-gray-800 dark:hover:text-zinc-300']">
      <input v-model="picked" type="radio" :name="name" :value="option.value" class="sr-only" />
      {{ option.label }}
    </label>
  </div>
</template>
