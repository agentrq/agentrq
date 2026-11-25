<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/** CheckBox: a box and its label, true when ticked. */
import { computed, useId } from 'vue'

import FieldError from '../FieldError.vue'
import { useInput } from '../../a2ui/interact.js'

const props = defineProps({ c: { type: Object, required: true }, ctx: { type: Object, required: true } })
const { model, label, error, readOnly, touch } = useInput(props)
const id = `a2ui-check-${useId()}`
const checked = computed({ get: () => model.value === true, set: (v) => { model.value = v; touch() } })
</script>

<template>
  <div data-test="a2ui-checkbox" class="min-w-0">
    <label :for="id" class="inline-flex items-center gap-2 text-[13px] text-gray-800 dark:text-zinc-200" :class="readOnly ? 'cursor-not-allowed opacity-70' : 'cursor-pointer'">
      <input :id="id" v-model="checked" type="checkbox" :disabled="readOnly" :aria-describedby="error ? `${id}-error` : undefined"
             class="w-4 h-4 rounded border-gray-300 dark:border-zinc-600 accent-gray-900 dark:accent-white" />
      <span class="break-words">{{ label }}</span>
    </label>
    <FieldError v-if="error" :id="`${id}-error`" :message="error" />
  </div>
</template>
