<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/** Icon: a catalog name in the app's outline style, or a custom svgPath. */
import { computed } from 'vue'

import { iconFor } from '../../a2ui/icons.js'

const props = defineProps({ c: { type: Object, required: true }, ctx: { type: Object, required: true } })

const drawing = computed(() => iconFor(props.c, props.ctx))
</script>

<template>
  <svg data-test="a2ui-icon" class="w-5 h-5 shrink-0 text-gray-600 dark:text-zinc-300" viewBox="0 0 24 24" aria-hidden="true">
    <path v-if="drawing?.stroke" :d="drawing.stroke" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
    <path v-else-if="drawing?.fill" :d="drawing.fill" fill="currentColor" />
  </svg>
</template>
