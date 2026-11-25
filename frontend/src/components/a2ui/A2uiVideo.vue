<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/** Video: only a file of ours, and a poster only when that is one too. */
import { computed } from 'vue'

import { isAllowedMedia } from '../../a2ui/render.js'
import { resolveValue } from '../../a2ui/resolve.js'

const props = defineProps({ c: { type: Object, required: true }, ctx: { type: Object, required: true } })

const url = computed(() => resolveValue(props.c.url, props.ctx))
const allowed = computed(() => isAllowedMedia(url.value, props.ctx.attachments))
const poster = computed(() => {
  const p = resolveValue(props.c.posterUrl, props.ctx)
  return isAllowedMedia(p, props.ctx.attachments) ? p : undefined
})
</script>

<template>
  <video v-if="allowed" data-test="a2ui-video" :src="url" :poster="poster" controls preload="metadata" playsinline
         class="block w-full max-w-xl rounded-lg bg-black"></video>
  <div v-else data-test="a2ui-media-refused"
       class="flex items-center justify-center rounded-lg border border-dashed border-gray-300 dark:border-zinc-600 text-[10px] text-gray-400 dark:text-zinc-500 min-h-10 px-2 w-full max-w-xl">Video not shown</div>
</template>
