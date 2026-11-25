<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/**
 * Button: its child as the label. A click sends its event as the person's
 * answer, runs its function, or opens the Modal it triggers. Held back while
 * a check fails, with the check's message under it; once the card is
 * answered, the one that answered it is marked.
 */
import { computed } from 'vue'

import A2uiNode from './A2uiNode.vue'
import { buttonEnabled, clickButton, failedChecks, isChosen } from '../../a2ui/interact.js'

const props = defineProps({ c: { type: Object, required: true }, ctx: { type: Object, required: true } })

const trigger = computed(() => props.ctx.modal?.trigger === props.c.id)
const enabled = computed(() => trigger.value || buttonEnabled(props.c, props.ctx))
const chosen = computed(() => isChosen(props.c, props.ctx.ui))
const problems = computed(() => (props.ctx.ui.readOnly ? [] : failedChecks(props.c, props.ctx)))

function click() {
  if (trigger.value) props.ctx.modal.open()
  else clickButton(props.c, props.ctx)
}

const VARIANT = {
  primary: 'bg-gray-900 dark:bg-white text-white dark:text-zinc-900 border-gray-900 dark:border-white hover:opacity-90',
  borderless: 'border-transparent text-gray-700 dark:text-zinc-200 hover:bg-gray-100 dark:hover:bg-zinc-800',
  default: 'bg-white dark:bg-zinc-900 text-gray-800 dark:text-zinc-100 border-gray-200 dark:border-zinc-700 hover:border-gray-300 dark:hover:border-zinc-600',
}
</script>

<template>
  <div class="inline-flex flex-col items-start min-w-0 max-w-full">
    <button type="button" data-test="a2ui-button" :data-chosen="chosen ? 'true' : undefined" :disabled="!enabled"
            :aria-pressed="ctx.ui.answered && c.action.event ? String(chosen) : undefined"
            class="a2ui-button inline-flex items-center gap-1.5 max-w-full px-3 py-1.5 rounded-lg border text-[12px] font-semibold transition-all disabled:cursor-not-allowed [&_.md-body]:text-inherit [&_.md-body]:text-[12px] [&_.md-body_p]:m-0"
            :class="[VARIANT[c.variant] ?? VARIANT.default,
                     chosen ? 'ring-2 ring-offset-1 ring-gray-900 dark:ring-white dark:ring-offset-zinc-900' : 'disabled:opacity-40']"
            @click="click">
      <svg v-if="chosen" data-test="a2ui-chosen" class="w-3.5 h-3.5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.75" aria-hidden="true">
        <path stroke-linecap="round" stroke-linejoin="round" d="M5 12.5l4.5 4.5L19 7.5" />
      </svg>
      <A2uiNode :id="c.child" :ctx="ctx" />
    </button>
    <p v-for="(message, i) in problems" :key="i" data-test="a2ui-check" class="mt-1 text-[11px] font-medium text-gray-500 dark:text-zinc-400">{{ message }}</p>
  </div>
</template>
