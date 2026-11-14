<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- The sidebar in team mode: the team's pages, then its workspaces.
     Members' shared workspaces of the same name are one team workspace, shown
     with each owner's avatar. The same rows and classes as the Me sidebar, so
     the two read as one app. -->
<template>
  <div class="space-y-0.5" data-test="team-sidebar">
    <div v-if="expanded" class="p-2">
      <span class="text-[11px] font-medium text-gray-500 dark:text-zinc-400">Team</span>
    </div>

    <router-link v-for="item in items" :key="item.label" :to="link(item.to)"
                 @mouseenter="showTooltip($event, item.label)" @mouseleave="hideTooltip"
                 class="items-center gap-2.5 px-2 py-1.5 text-xs transition-all duration-150 rounded-md"
                 :class="[item.desktopOnly ? 'hidden md:flex' : 'flex', expanded ? '' : 'justify-center', rowClass(item.active())]">
      <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
        <path stroke-linecap="round" stroke-linejoin="round" :d="item.icon" />
      </svg>
      <span v-if="expanded">{{ item.label }}</span>
    </router-link>

    <template v-if="expanded">
      <div class="p-2">
        <span class="text-[11px] font-medium text-gray-500 dark:text-zinc-400">Workspaces</span>
      </div>
      <p v-if="loaded && workspaces.length === 0" data-test="team-sidebar-empty" class="px-2 py-1 text-[11px] text-gray-400 dark:text-zinc-500">
        Nothing shared yet.
      </p>
      <router-link v-for="g in workspaces" :key="g.name" :to="link({ path: '/tasks/active', query: { workspace: g.name } })"
                   data-test="team-workspace"
                   @mouseenter="showTooltip($event, g.name)" @mouseleave="hideTooltip"
                   class="flex items-center gap-2.5 px-2 py-1.5 text-xs transition-all duration-150 rounded-md"
                   :class="rowClass(route.query.workspace === g.name)">
        <span class="truncate flex-1">{{ g.name }}</span>
        <span class="flex -space-x-1 shrink-0">
          <OwnerAvatar v-for="o in g.owners.slice(0, 3)" :key="o.userId ?? o.id" :owner="o" />
          <span v-if="g.owners.length > 3" class="text-[9px] text-gray-400 dark:text-zinc-500 pl-1.5">+{{ g.owners.length - 3 }}</span>
        </span>
      </router-link>
    </template>
  </div>
</template>

<script setup>
import { computed, watch } from 'vue';
import { useRoute } from 'vue-router';
import { useTeamStore } from '../stores/teamStore';
import { lostTeam, teamWorkspaceList, useTeamMode } from '../composables/useTeamMode';
import OwnerAvatar from './OwnerAvatar.vue';

defineProps({
  expanded: { type: Boolean, default: true },
  showTooltip: { type: Function, default: () => {} },
  hideTooltip: { type: Function, default: () => {} },
});

const route = useRoute();
const store = useTeamStore();
const { teamId, link, leaveTeam } = useTeamMode();

const rowClass = (active) => (active
  ? 'bg-gray-200 dark:bg-zinc-800 text-black dark:text-white'
  : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-200 dark:hover:bg-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50');

const items = [
  { label: 'Tasks', to: '/tasks/active', icon: 'M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2', active: () => route.path.startsWith('/tasks/') && !route.query.workspace },
  { label: 'Kanban', to: '/kanban', desktopOnly: true, icon: 'M4 5a1 1 0 011-1h3a1 1 0 011 1v14a1 1 0 01-1 1H5a1 1 0 01-1-1V5zM15 5a1 1 0 011-1h3a1 1 0 011 1v9a1 1 0 01-1 1h-3a1 1 0 01-1-1V5z', active: () => route.path === '/kanban' },
  { label: 'Skills', to: '/skills', icon: 'M8 6h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2zM10 3v3M14 3v3M10 18v3M14 18v3M3 10h3M3 14h3M18 10h3M18 14h3M9.5 12.2l1.8 1.8 3.4-3.6', active: () => route.path.startsWith('/skills') },
];

const loaded = computed(() => teamId.value in store.workspacesByTeam);
const workspaces = computed(() => teamWorkspaceList(store.workspacesOf(teamId.value)));

watch(teamId, async (id) => {
  if (!id) return;
  try {
    await store.loadWorkspaces(id);
  } catch (err) {
    // Removed from the team, or it is gone: back to Me, saying so (§9).
    if (lostTeam(err)) leaveTeam();
  }
}, { immediate: true });
</script>
