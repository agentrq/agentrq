<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/** Column: children one above the other; a Button alone on its line sits on the right, a closing row of them below a dashed line. */
import { computed } from 'vue'

import A2uiNode from './A2uiNode.vue'
import { columnChildClass, flexClasses } from '../../a2ui/render.js'
import { childEntries } from '../../a2ui/surface.js'

const props = defineProps({ c: { type: Object, required: true }, ctx: { type: Object, required: true } })

const children = computed(() => childEntries(props.ctx.surface, props.c.children, props.ctx))
</script>

<template>
  <div data-test="a2ui-column" class="flex flex-col gap-3 min-w-0" :class="flexClasses(c)">
    <A2uiNode v-for="(child, i) in children" :id="child.id" :key="child.key" :ctx="child.ctx"
               :class="columnChildClass(c, ctx.surface.components, child.id, i === children.length - 1)" />
  </div>
</template>
