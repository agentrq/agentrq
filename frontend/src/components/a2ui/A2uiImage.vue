<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/** Image: only a file of ours; anything else draws an empty frame. */
import { computed } from 'vue'

import { displayText, imageClasses, isAllowedMedia } from '../../a2ui/render.js'
import { resolveValue } from '../../a2ui/resolve.js'

const props = defineProps({ c: { type: Object, required: true }, ctx: { type: Object, required: true } })

const url = computed(() => resolveValue(props.c.url, props.ctx))
const allowed = computed(() => isAllowedMedia(url.value, props.ctx.attachments))
const alt = computed(() => displayText(resolveValue(props.c.description, props.ctx)))
</script>

<template>
  <img v-if="allowed" data-test="a2ui-image" :src="url" :alt="alt" loading="lazy"
       class="block h-auto rounded-lg bg-gray-50 dark:bg-zinc-800" :class="imageClasses(c)" />
  <div v-else data-test="a2ui-media-refused" role="img" :aria-label="alt || 'Image not shown'"
       class="flex items-center justify-center rounded-lg border border-dashed border-gray-300 dark:border-zinc-600 text-[10px] text-gray-400 dark:text-zinc-500 min-h-10 px-2"
       :class="imageClasses(c)">Image not shown</div>
</template>
