<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/**
 * Progress, from our catalog: a bar filled to value of max, its label and
 * percent above it. Bound to the data model, it moves as the agent updates
 * the card in place.
 */
import { computed } from 'vue'

import { displayText, progressPercent } from '../../a2ui/render.js'
import { resolveValue } from '../../a2ui/resolve.js'
import { LABEL } from '../../utils/formStyles.js'

const props = defineProps({ c: { type: Object, required: true }, ctx: { type: Object, required: true } })

const label = computed(() => displayText(resolveValue(props.c.label, props.ctx)))
const percent = computed(() => progressPercent(resolveValue(props.c.value, props.ctx), resolveValue(props.c.max, props.ctx)))
</script>

<template>
  <div data-test="a2ui-progress" class="min-w-0">
    <div v-if="label || percent !== null" class="flex items-baseline justify-between gap-3 mb-1.5">
      <span :class="LABEL" class="truncate">{{ label }}</span>
      <span v-if="percent !== null" data-test="a2ui-progress-value" class="text-[13px] leading-[18px] font-semibold tabular-nums text-gray-900 dark:text-zinc-100">{{ percent }}%</span>
    </div>
    <div role="progressbar" :aria-label="label || 'Progress'" aria-valuemin="0" aria-valuemax="100" :aria-valuenow="percent ?? undefined"
         class="h-1.5 w-full rounded-full bg-gray-100 dark:bg-zinc-800 overflow-hidden">
      <div data-test="a2ui-progress-bar" class="h-full rounded-full bg-gray-900 dark:bg-zinc-100 transition-[width] duration-500 ease-out motion-reduce:transition-none"
           :style="{ width: `${percent ?? 0}%` }" />
    </div>
  </div>
</template>
