<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/** TextField: a line, a paragraph, a number or a secret, in the app's field; its value always a string. */
import FormField from '../FormField.vue'
import { useInput } from '../../a2ui/interact.js'
import { fieldClass } from '../../utils/formStyles.js'
import { displayText } from '../../a2ui/render.js'
import { resolveValue } from '../../a2ui/resolve.js'

const props = defineProps({ c: { type: Object, required: true }, ctx: { type: Object, required: true } })
const { model, label, error, readOnly, touch } = useInput(props)

const TYPES = { number: 'number', obscured: 'password' }
</script>

<template>
  <FormField data-test="a2ui-textfield" :label="label" :error="error" class="min-w-0">
    <template #default="{ id, describedBy, invalid }">
      <textarea v-if="c.variant === 'longText'" :id="id" v-model="model" rows="3" :aria-describedby="describedBy" :aria-invalid="invalid || undefined"
                :placeholder="displayText(resolveValue(c.placeholder, ctx))" :disabled="readOnly" :class="fieldClass(invalid)" class="text-[13px] py-2 resize-y" @blur="touch" />
      <input v-else :id="id" :value="model" :type="TYPES[c.variant] ?? 'text'" :aria-describedby="describedBy" :aria-invalid="invalid || undefined"
             :inputmode="c.variant === 'number' ? 'decimal' : undefined" :autocomplete="c.variant === 'obscured' ? 'off' : undefined"
             :placeholder="displayText(resolveValue(c.placeholder, ctx))" :disabled="readOnly" :class="fieldClass(invalid)" class="text-[13px] py-2" @input="model = $event.target.value" @blur="touch" />
    </template>
  </FormField>
</template>
