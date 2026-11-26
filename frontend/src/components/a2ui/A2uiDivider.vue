<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
/** Divider: a hairline across, or down beside its siblings in a Row; in a Pano doc, dashed in the note's ink. */
defineProps({ c: { type: Object, required: true }, ctx: { type: Object, required: true } })
</script>

<template>
  <div v-if="ctx.pano && c.axis === 'vertical'" data-test="a2ui-divider" data-pano="true" role="separator" aria-orientation="vertical"
       class="self-stretch w-0 min-h-4 border-l border-dashed border-current opacity-25"></div>
  <hr v-else-if="ctx.pano" data-test="a2ui-divider" data-pano="true" class="w-full border-0 border-t border-dashed border-current opacity-25 my-1" />
  <div v-else-if="c.axis === 'vertical'" data-test="a2ui-divider" role="separator" aria-orientation="vertical"
       class="self-stretch w-px min-h-4 bg-gray-200 dark:bg-zinc-800"></div>
  <hr v-else data-test="a2ui-divider" class="w-full border-0 border-t border-gray-200 dark:border-zinc-800 my-1" />
</template>
