<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/** Slider: a range from min to max, snapping to its steps, its value beside its label. */
import { computed, useId } from 'vue'

import FieldError from '../FieldError.vue'
import { sliderRange, useInput } from '../../a2ui/interact.js'
import { displayText } from '../../a2ui/render.js'
import { LABEL } from '../../utils/formStyles.js'

const props = defineProps({ c: { type: Object, required: true }, ctx: { type: Object, required: true } })
const { model, label, error, readOnly, touch } = useInput(props)
const id = `a2ui-slider-${useId()}`
const range = computed(() => sliderRange(props.c))
const value = computed({
  get: () => (typeof model.value === 'number' ? model.value : range.value.min),
  set: (v) => { model.value = Number(v); touch() },
})
</script>

<template>
  <div data-test="a2ui-slider" class="min-w-0">
    <div class="flex items-baseline justify-between gap-3 mb-1.5">
      <label :for="id" :class="LABEL">{{ label }}</label>
      <output :for="id" data-test="a2ui-slider-value" class="text-[13px] leading-[18px] font-semibold tabular-nums text-gray-900 dark:text-zinc-100">{{ displayText(value) }}</output>
    </div>
    <input :id="id" v-model.number="value" type="range" :min="range.min" :max="range.max" :step="range.step" :disabled="readOnly"
           :aria-describedby="error ? `${id}-error` : undefined" class="block w-full h-4 pointer-coarse:h-10 accent-black dark:accent-white cursor-pointer disabled:cursor-default outline-none focus-visible:ring-2 focus-visible:ring-gray-900/20 dark:focus-visible:ring-white/25 rounded-full" />
    <FieldError v-if="error" :id="`${id}-error`" :message="error" />
  </div>
</template>
