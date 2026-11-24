<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/** Text: markdown without images or links; caption or body. */
import { computed } from 'vue'

import { displayText, textHtml } from '../../a2ui/render.js'
import { resolveValue } from '../../a2ui/resolve.js'

const props = defineProps({ c: { type: Object, required: true }, ctx: { type: Object, required: true } })

const html = computed(() => textHtml(displayText(resolveValue(props.c.text, props.ctx))))
</script>

<template>
  <div data-test="a2ui-text" class="md-body min-w-0 break-words"
       :class="c.variant === 'caption' ? 'text-[11px] text-gray-500 dark:text-zinc-400' : 'text-[13px] text-gray-800 dark:text-zinc-200'"
       v-html="html"></div>
</template>
