<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/** AudioPlayer: its description above the player; only a file of ours. */
import { computed } from 'vue'

import { displayText, isAllowedMedia } from '../../a2ui/render.js'
import { resolveValue } from '../../a2ui/resolve.js'

const props = defineProps({ c: { type: Object, required: true }, ctx: { type: Object, required: true } })

const url = computed(() => resolveValue(props.c.url, props.ctx))
const allowed = computed(() => isAllowedMedia(url.value, props.ctx.attachments))
const description = computed(() => displayText(resolveValue(props.c.description, props.ctx)))
</script>

<template>
  <div class="flex flex-col gap-1 min-w-0 w-full max-w-xl">
    <span v-if="description" class="text-[11px] text-gray-500 dark:text-zinc-400 truncate">{{ description }}</span>
    <audio v-if="allowed" data-test="a2ui-audio" :src="url" controls preload="metadata" class="w-full"></audio>
    <div v-else data-test="a2ui-media-refused"
         class="flex items-center justify-center rounded-lg border border-dashed border-gray-300 dark:border-zinc-600 text-[10px] text-gray-400 dark:text-zinc-500 min-h-10 px-2">Audio not shown</div>
  </div>
</template>
