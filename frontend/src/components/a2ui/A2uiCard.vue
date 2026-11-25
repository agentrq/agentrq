<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/** Card: its one child in the app's card frame. */
import A2uiNode from './A2uiNode.vue'

defineProps({ c: { type: Object, required: true }, ctx: { type: Object, required: true } })
</script>

<template>
  <div data-test="a2ui-card" class="min-w-0 rounded-xl border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 p-4 @min-[32rem]/a2ui:p-5">
    <A2uiNode :id="c.child" :ctx="ctx" />
  </div>
</template>
