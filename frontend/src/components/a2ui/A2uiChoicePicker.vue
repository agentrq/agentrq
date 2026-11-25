<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/**
 * ChoicePicker: one or several of its options, as chips or as a list of
 * radio or check rows, with a search over them when filterable. Its value is
 * the list of the picked options' values. Chips are the chat's toggle (as
 * Agent | Human on the task page); rows are the chat's fields.
 */
import { computed, ref, useId } from 'vue'

import FieldError from '../FieldError.vue'
import { choiceOptions, pickChoice, useInput } from '../../a2ui/interact.js'
import { CHAT_TOGGLE_TRACK, HELP, LABEL, fieldClass } from '../../utils/formStyles.js'
import { chipClass, choiceRowClass } from '../../a2ui/render.js'

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
    <div v-if="c.filterable" class="relative mb-1.5">
      <svg class="absolute left-2 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-gray-400 dark:text-zinc-500 pointer-events-none" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true">
        <circle cx="11" cy="11" r="7" /><path stroke-linecap="round" d="m20 20-3.5-3.5" />
      </svg>
      <input v-model="query" type="search" data-test="a2ui-choice-filter" placeholder="Filter" autocomplete="off"
             :aria-label="label ? `Filter ${label}` : 'Filter'" :class="fieldClass(false, { answered: readOnly, size: 'chat' })" class="pl-7 [&::-webkit-search-cancel-button]:appearance-none" />
    </div>
    <div v-if="c.displayStyle === 'chips'" :class="CHAT_TOGGLE_TRACK" :role="multiple ? 'group' : 'radiogroup'">
      <button v-for="o in options" :key="o.value" type="button" data-test="a2ui-chip" :data-value="o.value"
              :role="multiple ? undefined : 'radio'" :aria-checked="multiple ? undefined : String(selected.includes(o.value))"
              :aria-pressed="multiple ? String(selected.includes(o.value)) : undefined"
              :class="chipClass(selected.includes(o.value))" @click="pick(o.value)">
        {{ o.label }}
      </button>
    </div>
    <div v-else class="flex flex-col gap-1.5">
      <label v-for="o in options" :key="o.value" data-test="a2ui-choice" :data-value="o.value"
             :class="choiceRowClass(selected.includes(o.value), readOnly)">
        <input :type="multiple ? 'checkbox' : 'radio'" :name="id" :value="o.value" :checked="selected.includes(o.value)"
               class="w-3.5 h-3.5 shrink-0 accent-black dark:accent-white" @change="pick(o.value)" />
        <span class="min-w-0 break-words">{{ o.label }}</span>
      </label>
    </div>
    <p v-if="!options.length" data-test="a2ui-choice-none" :class="HELP" class="py-2">No option matches</p>
    <FieldError v-if="error" :id="`${id}-error`" :message="error" />
  </fieldset>
</template>
