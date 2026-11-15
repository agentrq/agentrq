<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
/**
 * A team's Skills page, of one team workspace (?workspace=). Only a team has
 * one: your own skills are reached from each workspace, on its Skills tab.
 */
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import TeamWorkspaceSkills from '../components/TeamWorkspaceSkills.vue'
import TeamWorkspaceActions from '../components/TeamWorkspaceActions.vue'
import { useTeamMode } from '../composables/useTeamMode'

const route = useRoute()
const router = useRouter()
const { link } = useTeamMode()
const teamWorkspace = computed(() => (typeof route.query.workspace === 'string' ? route.query.workspace : ''))
</script>

<template>
  <div class="@container flex flex-col h-full w-full overflow-y-auto custom-scrollbar">
    <div class="w-full px-4 py-2 mb-6 shrink-0 flex flex-row items-start justify-between gap-4">
      <div class="flex flex-col min-w-0 flex-1">
        <h1 class="text-lg md:text-2xl font-black text-gray-800 dark:text-zinc-200 tracking-tight truncate leading-tight">
          <template v-if="teamWorkspace">
            <span data-test="team-skills-workspace" class="opacity-50 cursor-pointer hover:opacity-100" @click="router.push(link({ path: '/tasks/active', query: { workspace: teamWorkspace } }))">{{ teamWorkspace }}</span>
            <span class="mx-1.5 text-gray-300 dark:text-zinc-700 font-medium">/</span>
            <span class="text-gray-400 dark:text-zinc-500">Skills</span>
          </template>
          <template v-else>Skills</template>
        </h1>
        <p class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5">
          <template v-if="teamWorkspace">The skills it owns, which every teammate can change.</template>
          <template v-else>Each team workspace owns its own skills.</template>
        </p>
      </div>
      <TeamWorkspaceActions current="skills" />
    </div>

    <div class="px-4 pb-10">
      <TeamWorkspaceSkills />
    </div>
  </div>
</template>
