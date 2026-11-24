<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/**
 * One form field: its label, the control, an error and a description, with
 * the ids that tie them together. The control comes from the default slot,
 * which is handed `id`, `describedBy` and `invalid` to bind on it, so a
 * screen reader reads the label, the error and the description with it.
 */
import { computed, useId } from 'vue'
import { HELP, LABEL } from '../utils/formStyles'
import FieldError from './FieldError.vue'

const props = defineProps({
  label: { type: String, required: true },
  description: { type: String, default: '' },
  error: { type: String, default: '' },
  required: Boolean,
  // Says "Optional" beside the label.
  optional: Boolean,
  // The control's id, when the page needs to know it; one is made otherwise.
  id: { type: String, default: '' },
})

const slots = defineSlots()
const made = useId()
const id = computed(() => props.id || `field-${made}`)
const errorId = computed(() => `${id.value}-error`)
const descriptionId = computed(() => `${id.value}-description`)
const hasDescription = computed(() => !!(props.description || slots.description))
const describedBy = computed(() => [
  props.error && errorId.value,
  hasDescription.value && descriptionId.value,
].filter(Boolean).join(' ') || undefined)
</script>

<template>
  <div>
    <div class="flex flex-wrap items-end justify-between gap-x-3 gap-y-1 mb-1.5">
      <label :for="id" :class="LABEL">
        {{ label }}<span v-if="required" class="text-red-500" aria-hidden="true"> *</span>
      </label>
      <slot name="aside">
        <span v-if="optional" class="text-[11px] font-medium text-gray-400 dark:text-zinc-500">Optional</span>
      </slot>
    </div>
    <slot :id="id" :describedBy="describedBy" :invalid="!!error" />
    <FieldError v-if="error" :id="errorId" :message="error" />
    <p v-if="hasDescription" :id="descriptionId" :class="HELP" class="mt-1.5">
      <slot name="description">{{ description }}</slot>
    </p>
  </div>
</template>
