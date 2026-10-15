<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!--
  The effort a native ACP agent starts on, as one slider with Claude Code's
  steps, so both launch forms offer the same levels the same way. It starts at
  Default, the blank step, which leaves the level to the agent; an agent that
  does not offer the chosen level starts on its own default too.

  The value is the launch's `effort`: blank, or one of the levels.
-->
<script setup>
import { CLAUDE_CODE_EFFORTS, stepIndex } from '../composables/useAgentLaunch'

const props = defineProps({
  modelValue: { type: String, default: '' },
  idPrefix: { type: String, required: true },
})

const emit = defineEmits(['update:modelValue'])

function stepName() {
  return CLAUDE_CODE_EFFORTS[stepIndex(CLAUDE_CODE_EFFORTS, props.modelValue)].name
}

function set(id) {
  emit('update:modelValue', id)
}
</script>

<template>
  <div>
    <div class="flex items-baseline justify-between mb-1">
      <label
        :for="`${idPrefix}-acp-effort`"
        class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500"
        >Effort</label
      >
      <span :data-test="`${idPrefix}-acp-effort-value`" class="text-[11px] font-bold text-gray-900 dark:text-zinc-100">
        {{ stepName() }}
      </span>
    </div>
    <input
      :id="`${idPrefix}-acp-effort`"
      type="range"
      min="0"
      :max="CLAUDE_CODE_EFFORTS.length - 1"
      step="1"
      :value="stepIndex(CLAUDE_CODE_EFFORTS, modelValue)"
      :aria-valuetext="stepName()"
      @input="set(CLAUDE_CODE_EFFORTS[Number($event.target.value)].id)"
      class="w-full accent-black dark:accent-white cursor-pointer"
    />
    <!-- Each step is named under the track, and naming one picks it. -->
    <div class="flex justify-between">
      <button
        v-for="(step, i) in CLAUDE_CODE_EFFORTS"
        :key="step.id"
        type="button"
        tabindex="-1"
        @click="set(step.id)"
        :class="
          i === stepIndex(CLAUDE_CODE_EFFORTS, modelValue)
            ? 'text-gray-900 dark:text-zinc-100 font-bold'
            : 'text-gray-400 dark:text-zinc-500 hover:text-gray-700 dark:hover:text-zinc-300'
        "
        class="text-[10px] transition-colors"
      >
        {{ step.name }}
      </button>
    </div>
  </div>
</template>
