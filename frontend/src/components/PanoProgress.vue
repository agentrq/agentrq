<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- How much of a Pano doc's checklist is done: a bar in the note's ink, and
     "4 of 6 done" under it. Nothing for a doc without a checklist. -->
<template>
  <div v-if="doc.checklistTotal" data-test="pano-progress">
    <div class="h-1.5 rounded-full bg-black/10 overflow-hidden" role="progressbar"
      :aria-valuenow="percent" aria-valuemin="0" aria-valuemax="100" :aria-label="`${doc.checklistDone} of ${doc.checklistTotal} done`">
      <div :class="color.inkBg" class="h-full rounded-full transition-[width] duration-300" :style="{ width: `${percent}%` }"></div>
    </div>
    <p class="mt-1.5 flex items-center gap-1.5 text-[10px] font-black uppercase tracking-widest text-gray-700">
      <span :class="color.inkBg" class="w-1.5 h-1.5 rounded-full" aria-hidden="true"></span>
      {{ doc.checklistDone }} of {{ doc.checklistTotal }} done
    </p>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { checklistPercent, panoColor } from '../composables/usePano'

const props = defineProps({ doc: { type: Object, required: true } })
const percent = computed(() => checklistPercent(props.doc))
const color = computed(() => panoColor(props.doc.color))
</script>
