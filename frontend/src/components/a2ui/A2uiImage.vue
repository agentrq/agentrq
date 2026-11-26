<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/** Image: only a file of ours; anything else draws an empty frame. In a Pano doc, a photo taped to the note. */
import { computed } from 'vue'

import { panoPieceTilt } from '../../a2ui/pano.js'
import { displayText, imageClasses, isAllowedMedia } from '../../a2ui/render.js'
import { resolveValue } from '../../a2ui/resolve.js'

const props = defineProps({ c: { type: Object, required: true }, ctx: { type: Object, required: true } })

const url = computed(() => resolveValue(props.c.url, props.ctx))
const allowed = computed(() => isAllowedMedia(url.value, props.ctx.attachments))
const alt = computed(() => displayText(resolveValue(props.c.description, props.ctx)))
</script>

<template>
  <figure v-if="ctx.pano && allowed && !['icon', 'avatar'].includes(c.variant)" data-pano="true"
          class="relative m-0 p-1.5 pb-2 bg-white rounded-[3px] shadow-[0_1px_2px_rgba(0,0,0,0.08),0_10px_20px_-12px_rgba(0,0,0,0.35)] max-w-full w-fit"
          :style="{ transform: `rotate(${panoPieceTilt(ctx.surface.surfaceId + c.id)}deg)` }">
    <span aria-hidden="true" :class="ctx.pano.tape" class="absolute -top-2.5 left-1/2 -translate-x-1/2 rotate-2 w-14 h-5 rounded-[2px]"></span>
    <img data-test="a2ui-image" :src="url" :alt="alt" loading="lazy" class="block h-auto rounded-[2px]" :class="imageClasses(c)" />
  </figure>
  <img v-else-if="allowed" data-test="a2ui-image" :src="url" :alt="alt" loading="lazy"
       class="block h-auto rounded-lg bg-gray-50 dark:bg-zinc-800" :class="imageClasses(c)" />
  <div v-else data-test="a2ui-media-refused" role="img" :aria-label="alt || 'Image not shown'"
       class="flex items-center justify-center rounded-lg border border-dashed border-gray-300 dark:border-zinc-600 text-[10px] text-gray-400 dark:text-zinc-500 min-h-10 px-2"
       :class="imageClasses(c)">Image not shown</div>
</template>
