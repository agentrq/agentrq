<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/** Tabs: the chat's toggle over the picked tab's child. */
import { computed, ref } from 'vue'

import A2uiNode from './A2uiNode.vue'
import SegmentedControl from '../SegmentedControl.vue'
import { displayText } from '../../a2ui/render.js'
import { resolveValue } from '../../a2ui/resolve.js'

const props = defineProps({ c: { type: Object, required: true }, ctx: { type: Object, required: true } })

const picked = ref(0)
const options = computed(() => props.c.tabs.map((t, i) => ({
  value: i,
  label: displayText(resolveValue(t.title, props.ctx)),
  test: `a2ui-tab-${i}`,
})))
const current = computed(() => props.c.tabs[picked.value] ?? props.c.tabs[0])
</script>

<template>
  <div data-test="a2ui-tabs" class="flex flex-col gap-2 min-w-0">
    <div class="max-w-full">
      <SegmentedControl v-model="picked" :options="options" chat />
    </div>
    <A2uiNode :id="current.child" :key="current.child" :ctx="ctx" />
  </div>
</template>
