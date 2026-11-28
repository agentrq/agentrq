<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/**
 * DateTimeInput: a date, a time or both; the value stays ISO 8601. A time is
 * picked on the account's clock, so not with the browser's own time pickers.
 */
import { computed } from 'vue'

import ClockInput from '../ClockInput.vue'
import DateTimeInput from '../DateTimeInput.vue'
import FormField from '../FormField.vue'
import { dateInput, useInput } from '../../a2ui/interact.js'
import { resolveValue } from '../../a2ui/resolve.js'
import { fieldClass } from '../../utils/formStyles.js'

const props = defineProps({ c: { type: Object, required: true }, ctx: { type: Object, required: true } })
const { model, label, error, readOnly, touch } = useInput(props)
const input = computed(() => dateInput(props.c, model.value))
const bound = (v) => dateInput(props.c, resolveValue(v, props.ctx)).value || undefined
function update(value) {
  model.value = value
  touch()
}
</script>

<template>
  <FormField data-test="a2ui-datetime" :label="label || 'Date'" :error="error" class="min-w-0">
    <template #default="{ id, describedBy, invalid }">
      <ClockInput v-if="input.type === 'time'" :id="id" :model-value="input.value" :label="label || 'Time'" :disabled="readOnly"
                  :aria-describedby="describedBy" :aria-invalid="invalid || undefined" :class="fieldClass(invalid, { answered: readOnly, size: 'chat' })"
                  @update:model-value="update" />
      <DateTimeInput v-else-if="input.type === 'datetime-local'" :id="id" :model-value="input.value" :min="bound(c.min)" :max="bound(c.max)" :disabled="readOnly"
                     :aria-describedby="describedBy" :aria-invalid="invalid || undefined" :class="fieldClass(invalid, { answered: readOnly, size: 'chat' })"
                     @update:model-value="update" />
      <input v-else :id="id" type="date" :value="input.value" :min="bound(c.min)" :max="bound(c.max)" :disabled="readOnly"
             :aria-describedby="describedBy" :aria-invalid="invalid || undefined" :class="fieldClass(invalid, { answered: readOnly, size: 'chat' })"
             @input="update($event.target.value)" @change="update($event.target.value)" />
    </template>
  </FormField>
</template>
