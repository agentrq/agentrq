<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/** Tabs: the chat's toggle over the picked tab's child; in a Pano doc, the note's tag pills. */
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
  <div v-if="ctx.pano" data-test="a2ui-tabs" data-pano="true" class="flex flex-col gap-3 min-w-0">
    <div role="tablist" class="flex flex-wrap gap-1.5">
      <button v-for="o in options" :key="o.value" type="button" role="tab" :aria-selected="picked === o.value" :data-test="o.test"
              class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[10px] font-black uppercase tracking-widest transition-opacity"
              :class="picked === o.value ? ctx.pano.tag : `bg-transparent ${ctx.pano.muted} opacity-60 hover:opacity-100`"
              @click="picked = o.value">
        <span v-if="picked === o.value" :class="ctx.pano.inkBg" class="w-1.5 h-1.5 rounded-full" aria-hidden="true"></span>
        {{ o.label }}
      </button>
    </div>
    <A2uiNode :id="current.child" :key="current.child" :ctx="ctx" />
  </div>
  <div v-else data-test="a2ui-tabs" class="flex flex-col gap-2 min-w-0">
    <div class="max-w-full">
      <SegmentedControl v-model="picked" :options="options" chat />
    </div>
    <A2uiNode :id="current.child" :key="current.child" :ctx="ctx" />
  </div>
</template>
