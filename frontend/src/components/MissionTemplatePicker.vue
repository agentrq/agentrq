<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<!-- Optional mission templates in one row: the categories, or once one is
     chosen, its specialities with a way back. A speciality pre-fills the
     mission, which stays editable. -->
<template>
  <div data-mission-picker>
    <div class="flex items-baseline justify-between gap-2 mb-2">
      <span class="text-xs font-bold text-gray-800 dark:text-zinc-200">
        Start from a template
        <span class="ml-1 text-[11px] font-medium text-gray-400 dark:text-zinc-500">Optional</span>
      </span>
      <button v-if="undoText !== null" type="button" @click="undo"
              class="text-[11px] font-semibold text-gray-500 hover:text-gray-900 dark:text-zinc-400 dark:hover:text-zinc-100 underline-offset-2 hover:underline">
        Undo
      </button>
    </div>
    <div v-if="openCategory" class="flex flex-wrap gap-1.5" role="group" :aria-label="`${openCategory.label} templates`">
      <button type="button" @click="back" :title="'Back to all categories'"
              class="inline-flex items-center gap-1 px-2 py-1 rounded-md text-[11px] font-bold text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-100 hover:bg-gray-100 dark:hover:bg-zinc-800 transition-colors">
        <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M15 19l-7-7 7-7" /></svg>
        {{ openCategory.label }}
      </button>
      <button v-for="sub in openCategory.subcategories" :key="sub.id" type="button"
              :aria-pressed="isSelected(openCategory, sub)"
              @click="pick(sub)"
              :class="[CHIP, isSelected(openCategory, sub) ? CHIP_ON : CHIP_OFF]">
        {{ sub.label }}
      </button>
    </div>
    <div v-else class="flex flex-wrap gap-1.5" role="group" aria-label="Template categories">
      <button v-for="category in categories" :key="category.id" type="button"
              :aria-pressed="holdsSelection(category)"
              @click="chooseCategory(category)"
              :class="[CHIP, holdsSelection(category) ? CHIP_ON : CHIP_OFF]">
        {{ category.label }}
      </button>
    </div>
  </div>
</template>

<script setup>
import { useMissionPicker } from '../composables/useMissionPicker';

const CHIP = 'px-2.5 py-1 rounded-md border text-[11px] font-semibold transition-colors';
const CHIP_ON = 'bg-black dark:bg-white border-black dark:border-white text-white dark:text-zinc-900';
const CHIP_OFF =
  'bg-white dark:bg-zinc-900 border-gray-200 dark:border-zinc-700 text-gray-700 dark:text-zinc-300 ' +
  'hover:border-gray-400 dark:hover:border-zinc-500';

const mission = defineModel({ type: String, default: '' });
const { categories, openCategory, isSelected, holdsSelection, undoText, chooseCategory, back, pick, undo } =
  useMissionPicker(mission);
</script>
