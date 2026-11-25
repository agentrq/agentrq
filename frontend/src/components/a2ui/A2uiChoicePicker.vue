<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/**
 * ChoicePicker: one or several of its options, as chips or as a list of
 * boxes, with a search over them when filterable. Its value is the list of
 * the picked options' values.
 */
import { computed, ref, useId } from 'vue'

import FieldError from '../FieldError.vue'
import { choiceOptions, pickChoice, useInput } from '../../a2ui/interact.js'
import { FIELD, LABEL } from '../../utils/formStyles.js'

const props = defineProps({ c: { type: Object, required: true }, ctx: { type: Object, required: true } })
const { model, label, error, readOnly, touch } = useInput(props)
const id = `a2ui-choice-${useId()}`
const query = ref('')
const multiple = computed(() => props.c.variant === 'multipleSelection')
const options = computed(() => choiceOptions(props.c, props.ctx, query.value))
const selected = computed(() => (Array.isArray(model.value) ? model.value : []))

function pick(value) {
  if (readOnly.value) return
  model.value = pickChoice(props.c, selected.value, value)
  touch()
}
</script>

<template>
  <fieldset data-test="a2ui-choicepicker" class="min-w-0" :aria-describedby="error ? `${id}-error` : undefined" :disabled="readOnly">
    <legend v-if="label" :class="LABEL" class="mb-1.5">{{ label }}</legend>
    <input v-if="c.filterable" v-model="query" type="search" data-test="a2ui-choice-filter" placeholder="Filter"
           :aria-label="label ? `Filter ${label}` : 'Filter'" :class="FIELD" class="text-[12px] py-1.5 mb-2" />
    <div v-if="c.displayStyle === 'chips'" class="flex flex-wrap gap-1.5" :role="multiple ? 'group' : 'radiogroup'">
      <button v-for="o in options" :key="o.value" type="button" data-test="a2ui-chip" :data-value="o.value"
              :role="multiple ? undefined : 'radio'" :aria-checked="multiple ? undefined : String(selected.includes(o.value))"
              :aria-pressed="multiple ? String(selected.includes(o.value)) : undefined"
              class="inline-flex items-center gap-1 px-3 py-1 rounded-full border text-[12px] font-medium transition-colors disabled:cursor-not-allowed"
              :class="selected.includes(o.value)
                ? 'bg-gray-900 dark:bg-white text-white dark:text-zinc-900 border-gray-900 dark:border-white'
                : 'bg-white dark:bg-zinc-900 text-gray-700 dark:text-zinc-300 border-gray-200 dark:border-zinc-700 hover:border-gray-300 dark:hover:border-zinc-600 disabled:opacity-50'"
              @click="pick(o.value)">
        <svg v-if="multiple && selected.includes(o.value)" class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" /></svg>
        {{ o.label }}
      </button>
    </div>
    <div v-else class="flex flex-col gap-1.5">
      <label v-for="o in options" :key="o.value" data-test="a2ui-choice" :data-value="o.value"
             class="flex items-center gap-2 px-2.5 py-1.5 rounded-lg border text-[12px] transition-colors"
             :class="[selected.includes(o.value)
               ? 'border-gray-900 dark:border-white bg-gray-50 dark:bg-zinc-800 text-gray-900 dark:text-white'
               : 'border-gray-200 dark:border-zinc-700 text-gray-600 dark:text-zinc-400 hover:border-gray-300 dark:hover:border-zinc-600',
               readOnly ? 'cursor-not-allowed' : 'cursor-pointer']">
        <input :type="multiple ? 'checkbox' : 'radio'" :name="id" :value="o.value" :checked="selected.includes(o.value)"
               class="w-3.5 h-3.5 accent-gray-900 dark:accent-white" @change="pick(o.value)" />
        <span class="font-medium break-words min-w-0">{{ o.label }}</span>
      </label>
    </div>
    <p v-if="!options.length" data-test="a2ui-choice-none" class="text-[11px] text-gray-400 dark:text-zinc-500">No option matches</p>
    <FieldError v-if="error" :id="`${id}-error`" :message="error" />
  </fieldset>
</template>
