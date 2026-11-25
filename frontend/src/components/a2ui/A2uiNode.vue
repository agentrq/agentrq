<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/**
 * One component of a card, by id, drawn by the component for its entry. An
 * entry this renderer has no component for draws a small placeholder. One
 * the agent's last in-place update changed is marked for a moment.
 */
import { computed, onUnmounted, ref, watch } from 'vue'

import A2uiAudioPlayer from './A2uiAudioPlayer.vue'
import A2uiButton from './A2uiButton.vue'
import A2uiCard from './A2uiCard.vue'
import A2uiChart from './A2uiChart.vue'
import A2uiCheckBox from './A2uiCheckBox.vue'
import A2uiChoicePicker from './A2uiChoicePicker.vue'
import A2uiColumn from './A2uiColumn.vue'
import A2uiDateTimeInput from './A2uiDateTimeInput.vue'
import A2uiDivider from './A2uiDivider.vue'
import A2uiIcon from './A2uiIcon.vue'
import A2uiImage from './A2uiImage.vue'
import A2uiList from './A2uiList.vue'
import A2uiModal from './A2uiModal.vue'
import A2uiProgress from './A2uiProgress.vue'
import A2uiRow from './A2uiRow.vue'
import A2uiSlider from './A2uiSlider.vue'
import A2uiTabs from './A2uiTabs.vue'
import A2uiText from './A2uiText.vue'
import A2uiTextField from './A2uiTextField.vue'
import A2uiVideo from './A2uiVideo.vue'
import { accessibilityAttrs, weightStyle } from '../../a2ui/render.js'
import { shownValues } from '../../a2ui/live.js'

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
    case 'Button': return A2uiButton
    case 'TextField': return A2uiTextField
    case 'CheckBox': return A2uiCheckBox
    case 'ChoicePicker': return A2uiChoicePicker
    case 'Slider': return A2uiSlider
    case 'DateTimeInput': return A2uiDateTimeInput
    case 'Modal': return A2uiModal
    case 'Chart': return A2uiChart
    case 'Progress': return A2uiProgress
    default: return null
  }
}

const c = computed(() => props.ctx.surface.components.get(props.id))
const view = computed(() => viewFor(c.value?.component))

// Marked for a moment when what it shows changes with an agent's update to
// the card (ui.changes), never with what the person enters.
const FLASH_MS = 1600
const shown = computed(() => shownValues(c.value, props.ctx))
const changed = ref(false)
let timer = null
watch([shown, () => props.ctx.ui?.changes], ([now, changes], [before, was]) => {
  if (changes === was || now === before || !now) return
  clearTimeout(timer)
  changed.value = true
  timer = setTimeout(() => { changed.value = false }, FLASH_MS)
})
onUnmounted(() => clearTimeout(timer))
</script>

<template>
  <component :is="view" v-if="view" :c="c" :ctx="ctx" :style="weightStyle(c)" v-bind="accessibilityAttrs(c, ctx)"
             :class="{ 'a2ui-changed': changed }" :data-changed="changed ? 'true' : undefined" />
  <span v-else data-test="a2ui-unknown" :style="weightStyle(c)"
        class="inline-flex items-center gap-1 px-1.5 py-0.5 rounded border border-dashed border-gray-300 dark:border-zinc-600 text-[10px] text-gray-400 dark:text-zinc-500">
    {{ c?.component ?? id }} is not shown here
  </span>
</template>
