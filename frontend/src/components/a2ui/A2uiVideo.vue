<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/**
 * Video: only a file of ours, and a poster only when that is one too. In a
 * Pano doc, a frame taped to the note, its play button in the note's ink
 * until it first plays.
 */
import { computed, ref } from 'vue'

import { panoPieceTilt } from '../../a2ui/pano.js'
import { isAllowedMedia } from '../../a2ui/render.js'
import { resolveValue } from '../../a2ui/resolve.js'

const props = defineProps({ c: { type: Object, required: true }, ctx: { type: Object, required: true } })

const url = computed(() => resolveValue(props.c.url, props.ctx))
const allowed = computed(() => isAllowedMedia(url.value, props.ctx.attachments))
const poster = computed(() => {
  const p = resolveValue(props.c.posterUrl, props.ctx)
  return isAllowedMedia(p, props.ctx.attachments) ? p : undefined
})
const video = ref(null)
const started = ref(false)
</script>

<template>
  <figure v-if="ctx.pano && allowed" data-pano="true"
          class="relative m-0 p-1.5 bg-white rounded-[3px] shadow-[0_1px_2px_rgba(0,0,0,0.08),0_10px_20px_-12px_rgba(0,0,0,0.35)] w-full max-w-xl"
          :style="{ transform: `rotate(${panoPieceTilt(ctx.surface.surfaceId + c.id)}deg)` }">
    <span aria-hidden="true" :class="ctx.pano.tape" class="absolute -top-2.5 left-8 -rotate-3 w-14 h-5 rounded-[2px] z-10"></span>
    <video ref="video" data-test="a2ui-video" :src="url" :poster="poster" :controls="started" preload="metadata" playsinline
           class="block w-full rounded-[2px] bg-black" :style="{ accentColor: ctx.pano.inkHex }" @play="started = true"></video>
    <button v-if="!started" type="button" data-test="a2ui-video-play" aria-label="Play video" @click="started = true; video.play()"
            class="absolute inset-0 m-auto w-14 h-14 rounded-full flex items-center justify-center text-white shadow-lg hover:scale-105 transition-transform"
            :class="ctx.pano.inkBg">
      <svg class="w-6 h-6 ml-1" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M8 5.14v13.72a1 1 0 0 0 1.5.86l11-6.86a1 1 0 0 0 0-1.72l-11-6.86A1 1 0 0 0 8 5.14Z" /></svg>
    </button>
  </figure>
  <video v-else-if="allowed" data-test="a2ui-video" :src="url" :poster="poster" controls preload="metadata" playsinline
         class="block w-full max-w-xl rounded-lg bg-black"></video>
  <div v-else data-test="a2ui-media-refused"
       class="flex items-center justify-center rounded-lg border border-dashed border-gray-300 dark:border-zinc-600 text-[10px] text-gray-400 dark:text-zinc-500 min-h-10 px-2 w-full max-w-xl">Video not shown</div>
</template>
