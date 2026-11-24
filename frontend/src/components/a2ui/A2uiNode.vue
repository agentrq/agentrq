<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/**
 * One component of a card, by id, drawn by the component for its entry. An
 * entry this renderer has no component for draws a small placeholder.
 */
import { computed } from 'vue'

import A2uiAudioPlayer from './A2uiAudioPlayer.vue'
import A2uiCard from './A2uiCard.vue'
import A2uiColumn from './A2uiColumn.vue'
import A2uiDivider from './A2uiDivider.vue'
import A2uiIcon from './A2uiIcon.vue'
import A2uiImage from './A2uiImage.vue'
import A2uiList from './A2uiList.vue'
import A2uiRow from './A2uiRow.vue'
import A2uiTabs from './A2uiTabs.vue'
import A2uiText from './A2uiText.vue'
import A2uiVideo from './A2uiVideo.vue'
import { accessibilityAttrs, weightStyle } from '../../a2ui/render.js'

const props = defineProps({
  id: { type: String, required: true },
  ctx: { type: Object, required: true },
})

// Looked up when drawn, not built at load: the containers import this file,
// so a map made at load could meet one of them not yet defined.
function viewFor(name) {
  switch (name) {
    case 'Text': return A2uiText
    case 'Image': return A2uiImage
    case 'Icon': return A2uiIcon
    case 'Video': return A2uiVideo
    case 'AudioPlayer': return A2uiAudioPlayer
    case 'Row': return A2uiRow
    case 'Column': return A2uiColumn
    case 'List': return A2uiList
    case 'Card': return A2uiCard
    case 'Tabs': return A2uiTabs
    case 'Divider': return A2uiDivider
    default: return null
  }
}

const c = computed(() => props.ctx.surface.components.get(props.id))
const view = computed(() => viewFor(c.value?.component))
</script>

<template>
  <component :is="view" v-if="view" :c="c" :ctx="ctx" :style="weightStyle(c)" v-bind="accessibilityAttrs(c, ctx)" />
  <span v-else data-test="a2ui-unknown" :style="weightStyle(c)"
        class="inline-flex items-center gap-1 px-1.5 py-0.5 rounded border border-dashed border-gray-300 dark:border-zinc-600 text-[10px] text-gray-400 dark:text-zinc-500">
    {{ c?.component ?? id }} is not shown here
  </span>
</template>
