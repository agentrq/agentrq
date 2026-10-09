<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!--
  Claude Code's model and effort, as two sliders.

  Sliders rather than dropdowns or a field: both are short, ordered lists, and
  every step is on screen at once. Each starts at Default, which leaves the
  choice to Claude Code. One component for all three launch forms, so they
  cannot disagree about the steps.

  The model steps are the ones the machine's Claude reported (`models`), and
  the fixed list until it has; effort is always the fixed list, which no
  lookup reports.

  The value is the launch's params object, `{ model, effort }`, replaced whole
  on every change so the form's own ref sees it.
-->
<script setup>
import { computed, ref, watch } from 'vue'
import { CLAUDE_CODE_EFFORTS, CLAUDE_CODE_MODELS, stepIndex } from '../composables/useAgentLaunch'

const props = defineProps({
  modelValue: { type: Object, default: () => ({}) },
  idPrefix: { type: String, required: true },
  models: { type: Array, default: () => CLAUDE_CODE_MODELS },
})

const emit = defineEmits(['update:modelValue'])

const sliders = computed(() => [
  { field: 'model', label: 'Model', steps: props.models },
  { field: 'effort', label: 'Effort', steps: CLAUDE_CODE_EFFORTS },
])

// What was last sent, not only what the props say: the props catch up on the
// next render, so a second change before then would undo the first.
const current = ref({ ...props.modelValue })
watch(
  () => props.modelValue,
  (value) => {
    current.value = { ...value }
  }
)

function set(field, id) {
  current.value = { ...current.value, [field]: id }
  emit('update:modelValue', current.value)
}

// A model this machine's Claude does not offer goes back to the default, so
// the launch never sends something the slider is not showing.
watch(
  () => props.models,
  (steps) => {
    const model = current.value.model
    if (model && !steps.some((step) => step.id === model)) set('model', '')
  },
  { immediate: true }
)
</script>

<template>
  <div class="space-y-3">
    <div v-for="s in sliders" :key="s.field">
      <div class="flex items-baseline justify-between mb-1">
        <label
          :for="`${idPrefix}-${s.field}`"
          class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500"
          >{{ s.label }}</label
        >
        <span :data-test="`${idPrefix}-${s.field}-value`" class="text-[11px] font-bold text-gray-900 dark:text-zinc-100">
          {{ s.steps[stepIndex(s.steps, modelValue?.[s.field])].name }}
        </span>
      </div>
      <input
        :id="`${idPrefix}-${s.field}`"
        type="range"
        min="0"
        :max="s.steps.length - 1"
        step="1"
        :value="stepIndex(s.steps, modelValue?.[s.field])"
        :aria-valuetext="s.steps[stepIndex(s.steps, modelValue?.[s.field])].name"
        @input="set(s.field, s.steps[Number($event.target.value)].id)"
        class="w-full accent-black dark:accent-white cursor-pointer"
      />
      <!-- Each step is named under the track, and naming one picks it. -->
      <div class="flex justify-between">
        <button
          v-for="(step, i) in s.steps"
          :key="step.id"
          type="button"
          tabindex="-1"
          @click="set(s.field, step.id)"
          :class="
            i === stepIndex(s.steps, modelValue?.[s.field])
              ? 'text-gray-900 dark:text-zinc-100 font-bold'
              : 'text-gray-400 dark:text-zinc-500 hover:text-gray-700 dark:hover:text-zinc-300'
          "
          class="text-[10px] transition-colors"
        >
          {{ step.name }}
        </button>
      </div>
    </div>
  </div>
</template>
