<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<!--
  What to run: Claude Code or the gateway.

  A segmented control rather than a dropdown, because there are exactly two of
  them and a dropdown hides one of the two behind a click. It lives in a
  component because both launch forms — the machine page's and the workspace
  panel's — have to offer the same choice, and a copy of the markup in each is
  how the two start disagreeing about what the options are called.

  `idPrefix` gives each option a stable id so the form around it, and the tests
  of it, can address a particular option rather than guessing at a position.
-->
<script setup>
import { computed } from 'vue'
import { KINDS } from '../composables/useAgentLaunch'

const props = defineProps({
  modelValue: { type: String, default: '' },
  idPrefix: { type: String, required: true },
  label: { type: String, default: 'What to run' },
})

defineEmits(['update:modelValue'])

/** The description under the control, which belongs to whichever is chosen. */
const description = computed(() => KINDS.find((k) => k.id === props.modelValue)?.description ?? '')
</script>

<template>
  <div>
    <p :id="`${idPrefix}-label`" class="block text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 mb-1">
      {{ label }}
    </p>
    <!-- `max-w-full` here and `min-w-0` on the options are load-bearing: an
         inline-flex is otherwise never narrower than its options want, so in a
         container too small for it — a narrow card, a phone — it does not
         shrink, it spills through whatever padding is around it. -->
    <div
      role="radiogroup"
      :aria-labelledby="`${idPrefix}-label`"
      class="inline-flex max-w-full p-0.5 bg-gray-100 dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700 rounded-lg"
    >
      <button
        v-for="k in KINDS"
        :key="k.id"
        :id="`${idPrefix}-${k.id}`"
        type="button"
        role="radio"
        :aria-checked="modelValue === k.id"
        @click="$emit('update:modelValue', k.id)"
        :class="
          modelValue === k.id
            ? 'bg-white dark:bg-zinc-700 text-black dark:text-white shadow-sm'
            : 'text-gray-500 dark:text-zinc-400 hover:text-gray-700 dark:hover:text-zinc-200'
        "
        class="min-w-0 px-6 py-1.5 rounded-md text-[10px] font-bold uppercase tracking-wider transition-all"
      >
        {{ k.label }}
      </button>
    </div>
    <p v-if="description" class="text-[11px] text-gray-500 dark:text-zinc-400 mt-1.5">{{ description }}</p>
  </div>
</template>
