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
 *
 * What the person enters stays in the card's own data model; a Button's
 * click is emitted as `action`, for the page to send. Once the card is
 * answered it is read-only, its chosen Button marked.
 */
import { computed, reactive, shallowRef, watch, watchEffect } from 'vue'

import A2uiNode from './A2uiNode.vue'
import MarkdownBody from '../MarkdownBody.vue'
import { cardAnswer } from '../../a2ui/interact.js'
import { loadSurface, surfaceContext } from '../../a2ui/surface.js'

const props = defineProps({
  metadata: { type: Object, default: null },
  text: { type: String, default: '' },
  /** The message's attachments: a bucket link is drawn only when it is one of them. */
  attachments: { type: Array, default: () => [] },
  workspaceId: { type: String, default: '' },
  /** Nothing may be answered here: a closed workspace, a teammate's task, a question nobody waits for. */
  readOnly: Boolean,
  /** A click is on its way. */
  busy: Boolean,
})
const emit = defineEmits(['action'])

// Loaded again only when the card itself changes: a task refreshed while the
// person types must not wipe what they entered.
const surface = shallowRef(null)
watch(
  () => JSON.stringify([props.metadata?.messages, props.metadata?.content?.dataModel, props.metadata?.type]),
  () => { surface.value = loadSurface(props.metadata, { interactive: true }) },
  { immediate: true },
)

const ui = reactive({ answered: null, readOnly: false, busy: false, send: (action) => emit('action', action) })
watchEffect(() => {
  ui.answered = cardAnswer(props.metadata)
  ui.readOnly = props.readOnly || !!ui.answered
  ui.busy = props.busy
})

const ctx = computed(() => surface.value && surfaceContext(surface.value, {
  locale: globalThis.navigator?.language,
  attachments: props.attachments,
  ui,
}))
</script>

<template>
  <div v-if="surface" class="a2ui-surface @container/a2ui min-w-0 max-w-full" data-test="a2ui-surface" :data-surface-id="surface.surfaceId"
       :data-answered="ui.answered ? 'true' : undefined">
    <A2uiNode :id="surface.rootId" :ctx="ctx" />
  </div>
  <MarkdownBody v-else :text="text" :workspace-id="workspaceId" />
</template>
