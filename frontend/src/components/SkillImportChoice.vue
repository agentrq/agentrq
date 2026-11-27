<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<!-- The skills a repository offers before an import, each with the skill
     cards' on/off switch: the ones on are imported. One that cannot be
     imported says why, its switch off and disabled. -->
<template>
  <div :data-test="`${testPrefix}-import-choice`" class="space-y-2 text-[11px] text-gray-700 dark:text-zinc-300">
    <p class="font-bold break-words">
      <span v-if="choice.tooLarge">{{ choice.repo }} is too large to import whole. Choose the skills to import:</span>
      <span v-else>Choose the skills to import from {{ choice.repo }}:</span>
    </p>
    <!-- One scrolling box: the header row shares the rows' width, so its
         switch for them all stays in line with theirs, scrollbar or not, and
         stays in sight while the list scrolls. -->
    <div class="max-h-72 overflow-y-auto bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700 rounded-sm">
    <div class="sticky top-0 z-10 flex items-center gap-3 px-3 py-2 bg-gray-50 dark:bg-zinc-800 border-b border-gray-200 dark:border-zinc-700">
      <span class="text-[10px] text-gray-500 dark:text-zinc-400">{{ chosen.length }} of {{ choosable.length }} on</span>
      <span class="ml-auto text-[9px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400">All</span>
      <button type="button" role="switch" :data-test="`${testPrefix}-choice-all`"
              :aria-checked="allOn" aria-label="Import all" :disabled="!choosable.length" @click="chosen = allOn ? [] : choosable"
              :class="switchClass(allOn)">
        <span :class="knobClass(allOn)"></span>
      </button>
    </div>
    <ul class="divide-y divide-gray-100 dark:divide-zinc-800">
      <li v-for="c in choice.candidates" :key="c.path" :data-test="`${testPrefix}-candidate`"
          class="flex items-center gap-3 min-w-0 px-3 py-2" :class="c.reason ? 'opacity-50' : ''">
        <span class="min-w-0 flex-1">
          <span class="flex flex-wrap items-baseline gap-x-2">
            <span :data-test="`${testPrefix}-candidate-name`" class="font-mono font-bold break-all">{{ c.name }}</span>
            <span v-if="c.sizeBytes" class="text-[10px] text-gray-400 dark:text-zinc-500 tabular-nums">{{ formatSkillSize(c.sizeBytes) }}</span>
            <span v-if="c.path && c.path !== c.name" class="text-[10px] font-mono text-gray-400 dark:text-zinc-500 break-all">{{ c.path }}</span>
          </span>
          <span v-if="c.reason" class="block break-words text-amber-600 dark:text-amber-400">{{ c.reason }}</span>
        </span>
        <button type="button" role="switch" :data-test="`${testPrefix}-candidate-switch`"
                :aria-checked="isOn(c)" :aria-label="`Import ${c.name}`" :disabled="!!c.reason" @click="flip(c)"
                :class="switchClass(isOn(c))">
          <span :class="knobClass(isOn(c))"></span>
        </button>
      </li>
    </ul>
    </div>
    <!-- What else the import is asked, such as overwriting, on the left. -->
    <div class="flex flex-wrap items-center gap-2">
      <slot name="options" />
      <span class="ml-auto flex items-center gap-2">
      <button type="button" @click="$emit('cancel')"
              class="px-3 py-2 text-[10px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400 hover:text-gray-800 dark:hover:text-zinc-100">Cancel</button>
      <button type="button" :data-test="`${testPrefix}-import-chosen`" :disabled="importing || !chosen.length" @click="$emit('import')"
              class="px-4 py-2 bg-black dark:bg-white text-white dark:text-black rounded-lg text-[11px] font-black uppercase tracking-widest disabled:opacity-40">
        {{ importing ? 'Importing…' : `Import ${chosen.length}` }}
      </button>
      </span>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue';

import { choosablePaths, formatSkillSize } from '../composables/useSkills';

// choice: { repo, candidates, tooLarge }; the model is the paths switched on.
const props = defineProps({
  choice: { type: Object, required: true },
  importing: { type: Boolean, default: false },
  testPrefix: { type: String, default: 'skill' },
});
defineEmits(['import', 'cancel']);
const chosen = defineModel({ type: Array, default: () => [] });

const choosable = computed(() => choosablePaths(props.choice.candidates));
const allOn = computed(() => choosable.value.length > 0 && choosable.value.every((p) => chosen.value.includes(p)));

// The skill cards' switch, as it looks there.
function switchClass(on) {
  return ['relative block shrink-0 w-11 h-6 rounded-full border transition-colors disabled:opacity-50',
    on ? 'bg-gray-900 dark:bg-white border-gray-900 dark:border-white' : 'bg-gray-200 dark:bg-zinc-700 border-gray-300 dark:border-zinc-600'];
}

function knobClass(on) {
  return ['absolute top-[3px] w-4 h-4 rounded-full transition-all', on ? 'left-[23px] bg-white dark:bg-zinc-900' : 'left-[3px] bg-white dark:bg-zinc-400'];
}

function isOn(c) {
  return !c.reason && chosen.value.includes(c.path);
}

function flip(c) {
  if (c.reason) return;
  chosen.value = isOn(c) ? chosen.value.filter((p) => p !== c.path) : [...chosen.value, c.path];
}
</script>
