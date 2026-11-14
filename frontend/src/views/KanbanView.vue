<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- Every workspace's tasks on one board. The board is the workspace one: with
     no workspace in the route it lists them all and names each card's. -->
<template>
  <div class="flex flex-col h-full w-full min-h-0">
    <div class="@container w-full px-4 py-2 shrink-0 flex items-start justify-between gap-4">
      <div class="flex flex-col min-w-0">
        <h1 class="text-lg md:text-2xl font-black text-gray-800 dark:text-zinc-200 truncate leading-tight">Kanban</h1>
        <p class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5">{{ isTeamMode ? 'Every shared workspace\'s tasks on one board.' : 'Every workspace\'s tasks on one board.' }}</p>
      </div>
      <TeamTaskFilters v-if="isTeamMode" />
      <NewTaskButton v-else />
    </div>
    <KanbanBoardView />
  </div>
</template>

<script setup>
import { watch } from 'vue';
import { useRouter } from 'vue-router';
import { useViewport } from '../composables/useViewport';
import KanbanBoardView from './KanbanBoardView.vue';
import NewTaskButton from '../components/NewTaskButton.vue';
import TeamTaskFilters from '../components/TeamTaskFilters.vue';
import { useTeamMode } from '../composables/useTeamMode';

const router = useRouter();
const { isMobile } = useViewport();
const { isTeamMode, teamId } = useTeamMode();

// Desktop and large screens only, like the workspace board: a narrow viewport
// that lands here (a direct link, a resize) gets the task list instead.
watch(isMobile, (mobile) => {
  if (mobile) router.replace(teamId.value ? `/tasks/active?team=${encodeURIComponent(teamId.value)}` : '/tasks/active');
}, { immediate: true });
</script>
