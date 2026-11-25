<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/**
 * Button: its child as the label, drawn as the app's own form buttons. A
 * click sends its event as the person's answer, runs its function, or opens
 * the Modal it triggers. Held back while a check fails, with the check's
 * message under it; while its answer is on its way it spins; once the card
 * is answered, the one that answered it is marked and the others fade.
 */
import { computed, useId } from 'vue'

import A2uiNode from './A2uiNode.vue'
import { buttonEnabled, clickButton, failedChecks, isChosen, isSending } from '../../a2ui/interact.js'
import { buttonClass } from '../../a2ui/render.js'
import { HELP } from '../../utils/formStyles.js'

const props = defineProps({ c: { type: Object, required: true }, ctx: { type: Object, required: true } })

const trigger = computed(() => props.ctx.modal?.trigger === props.c.id)
const enabled = computed(() => trigger.value || buttonEnabled(props.c, props.ctx))
const chosen = computed(() => isChosen(props.c, props.ctx.ui))
const sending = computed(() => isSending(props.c, props.ctx.ui))
const problems = computed(() => (props.ctx.ui.readOnly ? [] : failedChecks(props.c, props.ctx)))
const checkId = `a2ui-check-${useId()}`

function click() {
  if (trigger.value) props.ctx.modal.open()
  else clickButton(props.c, props.ctx)
}
</script>

<template>
  <div class="inline-flex flex-col items-start min-w-0 max-w-full">
    <button type="button" data-test="a2ui-button" :data-chosen="chosen ? 'true' : undefined" :disabled="!enabled"
            :aria-pressed="ctx.ui.answered && c.action.event ? String(chosen) : undefined" :aria-busy="sending || undefined"
            :aria-describedby="problems.length ? checkId : undefined"
            class="a2ui-button max-w-full [&_.md-body]:text-inherit [&_.md-body]:text-[10px] [&_.md-body]:leading-[15px]! [&_.md-body_p]:m-0"
            :class="buttonClass(c.variant, chosen)"
            @click="click">
      <svg v-if="sending" data-test="a2ui-sending" class="w-3 h-3 shrink-0 animate-spin" fill="none" viewBox="0 0 24 24" aria-hidden="true">
        <circle cx="12" cy="12" r="9" stroke="currentColor" stroke-opacity="0.25" stroke-width="3" />
        <path d="M21 12a9 9 0 0 0-9-9" stroke="currentColor" stroke-width="3" stroke-linecap="round" />
      </svg>
      <svg v-else-if="chosen" data-test="a2ui-chosen" class="w-3 h-3 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.75" aria-hidden="true">
        <path stroke-linecap="round" stroke-linejoin="round" d="M5 12.5l4.5 4.5L19 7.5" />
      </svg>
      <A2uiNode :id="c.child" :ctx="ctx" />
    </button>
    <p v-if="problems.length" :id="checkId" class="mt-1" :class="HELP">
      <span v-for="(message, i) in problems" :key="i" data-test="a2ui-check" class="block">{{ message }}</span>
    </p>
  </div>
</template>
