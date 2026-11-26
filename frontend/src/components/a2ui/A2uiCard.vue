<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/**
 * Card: its one child in the app's card frame, or, in a Pano doc, on a
 * smaller note taped to the doc, leaning as notes do.
 */
import A2uiNode from './A2uiNode.vue'
import { panoPieceTilt } from '../../a2ui/pano.js'

defineProps({ c: { type: Object, required: true }, ctx: { type: Object, required: true } })
</script>

<template>
  <div v-if="ctx.pano" data-test="a2ui-card" data-pano="true"
       class="relative min-w-0 rounded-md border border-black/[0.06] bg-white/60 px-4 pt-6 pb-4 shadow-[0_1px_2px_rgba(0,0,0,0.06),0_10px_20px_-12px_rgba(0,0,0,0.3)]"
       :style="{ transform: `rotate(${panoPieceTilt(ctx.surface.surfaceId + c.id)}deg)` }">
    <span aria-hidden="true" :class="ctx.pano.tape" class="absolute -top-2.5 left-1/2 -translate-x-1/2 -rotate-2 w-16 h-5 rounded-[2px]"></span>
    <A2uiNode :id="c.child" :ctx="ctx" />
  </div>
  <div v-else data-test="a2ui-card" class="min-w-0 rounded-xl border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 p-3 @min-[32rem]/a2ui:p-4">
    <A2uiNode :id="c.child" :ctx="ctx" />
  </div>
</template>
