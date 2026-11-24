<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/**
 * A set of radio buttons drawn as cards, each with a line saying what picking
 * it means: a fieldset, so the legend is read with every option. An option's
 * look can be replaced through the `option` slot.
 */
import { computed, useId } from 'vue'
import { LABEL } from '../utils/formStyles'
import FieldError from './FieldError.vue'

const picked = defineModel({ type: [String, Number], default: '' })
const props = defineProps({
  legend: { type: String, required: true },
  // { value, label, description, test? }; `test` becomes the card's data-test.
  options: { type: Array, required: true },
  error: { type: String, default: '' },
  required: Boolean,
  columns: { type: String, default: 'sm:grid-cols-2' },
})

const made = useId()
const name = `choice-${made}`
const errorId = `${name}-error`
const invalid = computed(() => !!props.error)
</script>

<template>
  <fieldset :aria-describedby="invalid ? errorId : undefined">
    <legend :class="LABEL" class="mb-1.5">
      {{ legend }}<span v-if="required" class="text-red-500" aria-hidden="true"> *</span>
    </legend>
    <slot name="empty" />
    <div class="grid gap-2" :class="columns">
      <label v-for="option in options" :key="option.value" :data-test="option.test"
        class="relative flex items-start gap-3 p-3 rounded-lg border cursor-pointer transition-colors has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-gray-900/20 dark:has-[:focus-visible]:ring-white/20"
        :class="option.value === picked
          ? 'border-gray-900 dark:border-zinc-200 bg-gray-50 dark:bg-zinc-800/60'
          : invalid
            ? 'border-red-500 dark:border-red-500/70'
            : 'border-gray-200 dark:border-zinc-700 hover:border-gray-300 dark:hover:border-zinc-600'">
        <input v-model="picked" type="radio" :name="name" :value="option.value" :required="required"
          class="mt-0.5 shrink-0 accent-black dark:accent-white" />
        <span class="min-w-0">
          <slot name="option" :option="option">
            <span class="block text-sm font-bold text-gray-900 dark:text-zinc-50">{{ option.label }}</span>
            <span v-if="option.description" class="block text-xs text-gray-500 dark:text-zinc-400 mt-0.5">{{ option.description }}</span>
          </slot>
        </span>
      </label>
    </div>
    <FieldError v-if="invalid" :id="errorId" :message="error" />
  </fieldset>
</template>
