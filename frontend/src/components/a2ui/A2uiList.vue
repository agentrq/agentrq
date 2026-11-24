<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/** List: a static list of children, or a template drawn once per item. */
import { computed } from 'vue'

import A2uiNode from './A2uiNode.vue'
import { listClasses } from '../../a2ui/render.js'
import { childEntries } from '../../a2ui/surface.js'

const props = defineProps({ c: { type: Object, required: true }, ctx: { type: Object, required: true } })

const children = computed(() => childEntries(props.ctx.surface, props.c.children, props.ctx))
</script>

<template>
  <div data-test="a2ui-list" role="list" class="flex gap-2 min-w-0 custom-scrollbar" :class="listClasses(c)">
    <div v-for="child in children" :key="child.key" role="listitem" class="flex flex-col min-w-0" :class="c.direction === 'horizontal' && 'shrink-0'">
      <A2uiNode :id="child.id" :ctx="child.ctx" />
    </div>
  </div>
</template>
