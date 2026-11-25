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
    <label :for="id" class="inline-flex items-center gap-2 min-h-5 pointer-coarse:min-h-10 text-[13px] leading-[18px] text-gray-900 dark:text-zinc-100" :class="readOnly ? 'cursor-default' : 'cursor-pointer'">
      <input :id="id" v-model="checked" type="checkbox" :disabled="readOnly" :aria-describedby="error ? `${id}-error` : undefined"
             class="w-3.5 h-3.5 shrink-0 rounded accent-black dark:accent-white outline-none focus-visible:ring-2 focus-visible:ring-gray-900/20 focus-visible:ring-offset-2 dark:focus-visible:ring-white/25 dark:focus-visible:ring-offset-zinc-900" />
      <span class="break-words">{{ label }}</span>
    </label>
    <FieldError v-if="error" :id="`${id}-error`" :message="error" />
  </div>
</template>
