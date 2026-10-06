<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
/**
 * The account's skills, and one skill's page in place of the list when the
 * route names one. Thin on purpose: the list and the page are the same
 * components a workspace's Skills tab draws, given no workspace.
 */
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import SkillPage from '../components/SkillPage.vue'
import SkillsPanel from '../components/SkillsPanel.vue'

const route = useRoute()
const skillName = computed(() => route.params.name || '')
</script>

<template>
  <div class="flex flex-col h-full w-full overflow-y-auto custom-scrollbar">
    <div class="w-full px-4 py-2 mb-6 shrink-0 flex flex-row items-start justify-between gap-4">
      <div class="flex flex-col min-w-0 flex-1">
        <h1 class="text-lg md:text-2xl font-black text-gray-800 dark:text-zinc-200 truncate leading-tight">Skills</h1>
        <p class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5">
          Playbooks agents load when a task matches one. Turn one on in each workspace whose agents should see it.
        </p>
      </div>
    </div>

    <div class="px-4 pb-10">
      <SkillPage v-if="skillName" :name="skillName" />
      <SkillsPanel v-else />
    </div>
  </div>
</template>
