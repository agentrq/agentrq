<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/** Row: children side by side; on a narrow card they wrap rather than overflow. */
import { computed } from 'vue'

import A2uiNode from './A2uiNode.vue'
import { flexClasses } from '../../a2ui/render.js'
import { childEntries } from '../../a2ui/surface.js'

const props = defineProps({ c: { type: Object, required: true }, ctx: { type: Object, required: true } })

const children = computed(() => childEntries(props.ctx.surface, props.c.children, props.ctx))
</script>

<template>
  <div data-test="a2ui-row" class="flex flex-row flex-wrap @min-[24rem]/a2ui:flex-nowrap gap-x-3 gap-y-2 min-w-0" :class="flexClasses(c)">
    <A2uiNode v-for="child in children" :id="child.id" :key="child.key" :ctx="child.ctx" />
  </div>
</template>
