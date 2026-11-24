<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/**
 * An agent message's A2UI card, drawn from its root component. A card that
 * fails the check (an old or hand-edited row) shows the message's text,
 * which every A2UI reply carries for that reason.
 */
import { computed } from 'vue'

import A2uiNode from './A2uiNode.vue'
import MarkdownBody from '../MarkdownBody.vue'
import { loadSurface, surfaceContext } from '../../a2ui/surface.js'

const props = defineProps({
  metadata: { type: Object, default: null },
  text: { type: String, default: '' },
  /** The message's attachments: a bucket link is drawn only when it is one of them. */
  attachments: { type: Array, default: () => [] },
  workspaceId: { type: String, default: '' },
})

const surface = computed(() => loadSurface(props.metadata))
const ctx = computed(() => surface.value && surfaceContext(surface.value, {
  locale: globalThis.navigator?.language,
  attachments: props.attachments,
}))
</script>

<template>
  <div v-if="surface" class="a2ui-surface @container/a2ui min-w-0 max-w-full" data-test="a2ui-surface" :data-surface-id="surface.surfaceId">
    <A2uiNode :id="surface.rootId" :ctx="ctx" />
  </div>
  <MarkdownBody v-else :text="text" :workspace-id="workspaceId" />
</template>
