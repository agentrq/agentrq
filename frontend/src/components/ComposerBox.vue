<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
/**
 * The reply box, shared by every place a message is written to an agent (a
 * task's reply, the Pano's chat): the text on top, growing with it up to a
 * limit, and a toolbar under it with the caller's tools on the left, its
 * actions on the right, and Send last.
 *
 * Attributes go to the text area, not the box (`placeholder`, `disabled`,
 * `@keydown` …), and `inputRef` hands the caller the element itself. Send is a
 * submit button: the caller wraps the box in its form.
 */
import { nextTick, ref, watch } from 'vue'

defineOptions({ inheritAttrs: false })

const props = defineProps({
  /** Called with the text area, as a function ref is. */
  inputRef: { type: Function, default: null },
  sendDisabled: { type: Boolean, default: false },
  sendTitle: { type: String, default: 'Send Message' },
})
const model = defineModel({ type: String, default: '' })

/** The height the text starts at, and the most it grows to before it scrolls. */
const MIN_HEIGHT = 46
const MAX_HEIGHT = 150

const textarea = ref(null)

function setInput(el) {
  textarea.value = el
  props.inputRef?.(el)
}

/** Fit the text: back to one line, then as tall as it needs, up to the limit. */
function fit() {
  const el = textarea.value
  if (!el) return
  el.style.height = `${MIN_HEIGHT}px`
  el.style.height = `${Math.min(el.scrollHeight, MAX_HEIGHT)}px`
}

// However the text changed — typed, sent and cleared, put back — it is fitted
// once it is drawn.
watch(model, () => nextTick(fit))
</script>

<template>
  <div class="flex flex-col bg-white dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700 rounded-sm focus-within:border-gray-900 dark:focus-within:border-white focus-within:ring-0 transition-all group relative min-w-0 shadow-sm">
    <textarea :ref="setInput" v-model="model" rows="1" v-bind="$attrs"
      class="w-full px-3.5 pt-3 pb-1.5 text-[13px] font-medium text-gray-800 dark:text-zinc-200 bg-transparent outline-none border-none focus:outline-none focus:ring-0 disabled:opacity-50 resize-none min-h-[46px] max-h-[150px] custom-scrollbar"></textarea>

    <div class="flex items-center justify-between gap-2 px-3 pb-2 pt-1">
      <div class="relative flex flex-wrap items-center gap-2 min-w-0">
        <slot name="tools" />
      </div>
      <div class="flex items-center gap-2 shrink-0">
        <slot name="actions" />
        <button type="submit" :title="sendTitle" :disabled="sendDisabled"
                class="h-6 w-6 rounded-full bg-black dark:bg-white text-white dark:text-zinc-900 hover:opacity-90 disabled:opacity-30 transition-all flex items-center justify-center shrink-0 shadow-sm">
          <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
            <path stroke-linecap="round" stroke-linejoin="round" d="M5 10l7-7m0 0l7 7m-7-7v18" />
          </svg>
        </button>
      </div>
    </div>
  </div>
</template>
