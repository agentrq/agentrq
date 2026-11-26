<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/**
 * One a2ui block of a Pano doc, drawn in the note's colours: every
 * component reads ctx.pano, the doc's colour, for its look. Nothing in it
 * answers, so it is read-only.
 */
import { computed, reactive } from 'vue'

import A2uiNode from './A2uiNode.vue'
import { surfaceContext } from '../../a2ui/surface.js'

const props = defineProps({
  surface: { type: Object, required: true },
  /** The doc's files, the only media a block draws. */
  files: { type: Array, default: () => [] },
  /** The doc's colour, from panoColor. */
  color: { type: Object, required: true },
})

const ui = reactive({ answered: null, readOnly: true, busy: false, pressed: null, changes: null, send: () => {} })
const ctx = computed(() => surfaceContext(props.surface, { attachments: props.files, ui, pano: props.color }))
</script>

<template>
  <div class="a2ui-surface pano-a2ui @container/a2ui min-w-0 max-w-full my-4" data-test="pano-a2ui" :data-surface-id="surface.surfaceId"
       :style="{ '--pano-accent': color.inkHex }">
    <A2uiNode :id="surface.rootId" :ctx="ctx" />
  </div>
</template>
