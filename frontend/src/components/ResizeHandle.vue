<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
/**
 * The edge a column is resized by, shared by the desktop side panel and the
 * Pano chat. The whole gap beside the column is grabbable, and a grip in the
 * middle of it always shows, so that the edge can be dragged is visible
 * before anyone hovers it. A double-click asks for the column's default back.
 *
 * The caller places it (`-left-4`, `right-0`) and says when it shows (`flex`,
 * `hidden lg:flex`), and keeps the width: this only reports where a drag
 * would put it.
 */
import { onUnmounted } from 'vue'

const props = defineProps({
  /** The column's width as drawn, which a drag starts from. */
  width: { type: Number, required: true },
  /** The column's edge it sits on: dragging away from the column widens it. */
  edge: { type: String, default: 'left' },
})
const emit = defineEmits(['resize', 'reset'])
/** Whether a drag is under way, for a caller whose page must ignore the pointer meanwhile. */
const dragging = defineModel('dragging', { type: Boolean, default: false })

let start = null

function startDrag(event) {
  if (event.button !== 0) return
  event.preventDefault()
  start = { x: event.clientX, width: props.width }
  dragging.value = true
  event.currentTarget.setPointerCapture?.(event.pointerId)
  globalThis.addEventListener('pointermove', onDrag)
  globalThis.addEventListener('pointerup', endDrag)
}

function onDrag(event) {
  const moved = event.clientX - start.x
  emit('resize', start.width + (props.edge === 'left' ? -moved : moved))
}

function endDrag() {
  dragging.value = false
  start = null
  globalThis.removeEventListener('pointermove', onDrag)
  globalThis.removeEventListener('pointerup', endDrag)
}

onUnmounted(endDrag)
</script>

<template>
  <div role="separator" aria-orientation="vertical" title="Drag to resize · double-click to reset"
       class="group absolute inset-y-4 w-4 cursor-col-resize items-center justify-center touch-none"
       @pointerdown="startDrag" @dblclick="emit('reset')">
    <div :class="['absolute inset-y-0 w-px transition-colors', dragging ? 'bg-gray-400 dark:bg-zinc-500' : 'bg-transparent group-hover:bg-gray-300 dark:group-hover:bg-zinc-700']"></div>
    <div data-resize-grip
         :class="['relative w-1 h-10 rounded-full transition-colors', dragging ? 'bg-gray-500 dark:bg-zinc-400' : 'bg-gray-300 dark:bg-zinc-700 group-hover:bg-gray-400 dark:group-hover:bg-zinc-500']"></div>
  </div>
</template>
