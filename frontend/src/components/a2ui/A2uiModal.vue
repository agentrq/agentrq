<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/**
 * Modal: its trigger, drawn in place, opens its content in a dialog over the
 * page. The trigger's click only opens it: it answers nothing.
 */
import { computed, nextTick, ref } from 'vue'

import A2uiNode from './A2uiNode.vue'

const props = defineProps({ c: { type: Object, required: true }, ctx: { type: Object, required: true } })

const dialog = ref(null)
const isOpen = ref(false)
async function open() {
  isOpen.value = true
  await nextTick()
  // Modal where the browser has it, so focus stays inside and Esc closes it.
  if (typeof dialog.value.showModal === 'function') dialog.value.showModal()
  else dialog.value.setAttribute('open', '')
}
function close() {
  isOpen.value = false
}
const triggerCtx = computed(() => ({ ...props.ctx, modal: { trigger: props.c.trigger, open } }))
</script>

<template>
  <div data-test="a2ui-modal" class="min-w-0">
    <A2uiNode :id="c.trigger" :ctx="triggerCtx" />
    <dialog v-if="isOpen" ref="dialog" data-test="a2ui-modal-dialog"
            class="fixed inset-0 m-auto w-[min(32rem,calc(100vw-2rem))] max-h-[calc(100dvh-4rem)] overflow-auto rounded-xl border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 text-gray-800 dark:text-zinc-200 p-4 backdrop:bg-black/40"
            @close="close">
      <div class="flex justify-end -mt-1 -mr-1 mb-1">
        <button type="button" data-test="a2ui-modal-close" aria-label="Close" class="p-1 rounded-md text-gray-400 hover:text-gray-700 dark:hover:text-zinc-200 hover:bg-gray-100 dark:hover:bg-zinc-800" @click="close">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
        </button>
      </div>
      <A2uiNode :id="c.content" :ctx="ctx" />
    </dialog>
  </div>
</template>
