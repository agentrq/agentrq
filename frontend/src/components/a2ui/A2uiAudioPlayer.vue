<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/**
 * AudioPlayer: its description above the player; only a file of ours. In a
 * Pano doc, a slim pill in the note's colours: a play button in its ink, and
 * the time on a bar like its checklist's.
 */
import { computed, ref } from 'vue'

import { panoTime } from '../../a2ui/pano.js'
import { displayText, isAllowedMedia } from '../../a2ui/render.js'
import { resolveValue } from '../../a2ui/resolve.js'

const props = defineProps({ c: { type: Object, required: true }, ctx: { type: Object, required: true } })

const url = computed(() => resolveValue(props.c.url, props.ctx))
const allowed = computed(() => isAllowedMedia(url.value, props.ctx.attachments))
const description = computed(() => displayText(resolveValue(props.c.description, props.ctx)))

const audio = ref(null)
const playing = ref(false)
const at = ref(0)
const duration = ref(0)
const percent = computed(() => (duration.value > 0 ? Math.min(100, (at.value / duration.value) * 100) : 0))
function toggle() {
  if (audio.value.paused) audio.value.play()
  else audio.value.pause()
}
// A click on the bar moves to that point.
function seek(event) {
  if (!(duration.value > 0)) return
  const box = event.currentTarget.getBoundingClientRect()
  audio.value.currentTime = Math.max(0, Math.min(1, (event.clientX - box.left) / box.width)) * duration.value
}
</script>

<template>
  <div v-if="ctx.pano && allowed" data-pano="true" class="flex flex-col gap-1.5 min-w-0 w-full max-w-md">
    <span v-if="description" :class="ctx.pano.muted" class="text-[10px] font-black uppercase tracking-widest truncate">{{ description }}</span>
    <div :class="ctx.pano.tag" class="flex items-center gap-3 rounded-full pl-1 pr-4 py-1">
      <button type="button" data-test="a2ui-audio-play" :aria-label="playing ? 'Pause' : 'Play'" @click="toggle"
              :class="ctx.pano.inkBg" class="w-8 h-8 shrink-0 rounded-full flex items-center justify-center text-white">
        <svg v-if="playing" class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M7 5h3.5v14H7zM13.5 5H17v14h-3.5z" /></svg>
        <svg v-else class="w-3.5 h-3.5 ml-0.5" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M8 5.14v13.72a1 1 0 0 0 1.5.86l11-6.86a1 1 0 0 0 0-1.72l-11-6.86A1 1 0 0 0 8 5.14Z" /></svg>
      </button>
      <div data-test="a2ui-audio-bar" role="slider" aria-label="Position" aria-valuemin="0" :aria-valuemax="Math.floor(duration)" :aria-valuenow="Math.floor(at)"
           class="flex-1 h-1.5 rounded-full bg-white/70 overflow-hidden cursor-pointer" @click="seek">
        <div :class="ctx.pano.inkBg" class="h-full rounded-full" :style="{ width: `${percent}%` }"></div>
      </div>
      <span data-test="a2ui-audio-time" class="text-[10px] font-black tabular-nums shrink-0">{{ panoTime(at) }} / {{ panoTime(duration) }}</span>
    </div>
    <audio ref="audio" data-test="a2ui-audio" :src="url" preload="metadata" class="hidden"
           @play="playing = true" @pause="playing = false" @ended="playing = false"
           @timeupdate="at = $event.target.currentTime" @loadedmetadata="duration = $event.target.duration"></audio>
  </div>
  <div v-else class="flex flex-col gap-1 min-w-0 w-full max-w-xl">
    <span v-if="description" class="text-[11px] text-gray-500 dark:text-zinc-400 truncate">{{ description }}</span>
    <audio v-if="allowed" data-test="a2ui-audio" :src="url" controls preload="metadata" class="w-full"></audio>
    <div v-else data-test="a2ui-media-refused"
         class="flex items-center justify-center rounded-lg border border-dashed border-gray-300 dark:border-zinc-600 text-[10px] text-gray-400 dark:text-zinc-500 min-h-10 px-2">Audio not shown</div>
  </div>
</template>
