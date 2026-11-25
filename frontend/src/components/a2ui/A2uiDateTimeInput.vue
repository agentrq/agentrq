<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/** DateTimeInput: the browser's date, time or both picker; the value stays ISO 8601. */
import { computed } from 'vue'

import FormField from '../FormField.vue'
import { dateInput, useInput } from '../../a2ui/interact.js'
import { resolveValue } from '../../a2ui/resolve.js'
import { fieldClass } from '../../utils/formStyles.js'

const props = defineProps({ c: { type: Object, required: true }, ctx: { type: Object, required: true } })
const { model, label, error, readOnly, touch } = useInput(props)
const input = computed(() => dateInput(props.c, model.value))
const bound = (v) => dateInput(props.c, resolveValue(v, props.ctx)).value || undefined
function update(e) {
  model.value = e.target.value
  touch()
}
</script>

<template>
  <FormField data-test="a2ui-datetime" :label="label || 'Date'" :error="error" class="min-w-0">
    <template #default="{ id, describedBy, invalid }">
      <input :id="id" :type="input.type" :value="input.value" :min="bound(c.min)" :max="bound(c.max)" :disabled="readOnly"
             :aria-describedby="describedBy" :aria-invalid="invalid || undefined" :class="fieldClass(invalid, { answered: readOnly })"
             @input="update" @change="update" />
    </template>
  </FormField>
</template>
