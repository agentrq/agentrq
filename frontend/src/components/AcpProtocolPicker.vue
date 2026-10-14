<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!--
  How the ACP gateway talks to AgentRQ, as one slider: Classic, where the
  gateway speaks ACP to the agent itself, or ACP v1, where it only bridges the
  agent and AgentRQ speaks ACP. The same slider as `ClaudeOptionsPicker`, so
  the launch forms read alike. It starts at Classic, the blank step.

  The value is the launch's `acpVersion`: blank, or "1".
-->
<script setup>
import { ACP_PROTOCOLS, stepIndex } from '../composables/useAgentLaunch'

const props = defineProps({
  modelValue: { type: String, default: '' },
  idPrefix: { type: String, required: true },
})

const emit = defineEmits(['update:modelValue'])

/** The step the slider is on, named in full above the track. */
function stepTitle() {
  const step = ACP_PROTOCOLS[stepIndex(ACP_PROTOCOLS, props.modelValue)]
  return step.title ?? step.name
}

// The value is a plain string, replaced whole, so unlike Claude's options
// there is no earlier change for a second one to undo.
function set(id) {
  emit('update:modelValue', id)
}
</script>

<template>
  <div>
    <div class="flex items-baseline justify-between mb-1">
      <label
        :for="`${idPrefix}-acp-version`"
        class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500"
        >Protocol</label
      >
      <span :data-test="`${idPrefix}-acp-version-value`" class="text-[11px] font-bold text-gray-900 dark:text-zinc-100">
        {{ stepTitle() }}
      </span>
    </div>
    <input
      :id="`${idPrefix}-acp-version`"
      type="range"
      min="0"
      :max="ACP_PROTOCOLS.length - 1"
      step="1"
      :value="stepIndex(ACP_PROTOCOLS, modelValue)"
      :aria-valuetext="stepTitle()"
      @input="set(ACP_PROTOCOLS[Number($event.target.value)].id)"
      class="w-full accent-black dark:accent-white cursor-pointer"
    />
    <!-- Each step is named under the track, and naming one picks it. -->
    <div class="flex justify-between">
      <button
        v-for="(step, i) in ACP_PROTOCOLS"
        :key="step.id"
        type="button"
        tabindex="-1"
        @click="set(step.id)"
        :class="
          i === stepIndex(ACP_PROTOCOLS, modelValue)
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
