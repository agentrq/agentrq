<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- The Me | Team switch at the top of the sidebar. Team mode is the route's
     ?team=, so every choice here is a navigation; the last one is remembered
     in this browser. The badge counts invitations waiting for you. -->
<template>
  <div class="relative" data-test="team-switcher">
    <!-- Folded sidebar: one button, the menu beside it. -->
    <button v-if="collapsed" type="button" @click="open = !open"
            :aria-label="isTeamMode ? `Team: ${teamName}` : 'Me'"
            class="relative w-full flex items-center justify-center py-1.5 rounded-md transition-colors"
            :class="isTeamMode ? 'bg-gray-900 dark:bg-white text-white dark:text-zinc-900' : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-200 dark:hover:bg-zinc-700'">
      <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" :d="TEAM_ICON" /></svg>
      <span v-if="pendingCount" data-test="team-badge" class="absolute -top-0.5 right-1 min-w-[14px] h-[14px] px-1 rounded-full bg-red-500 text-white text-[8px] font-black inline-flex items-center justify-center leading-none">{{ pendingCount }}</span>
    </button>

    <div v-else class="flex items-center gap-1.5">
      <div class="flex-1 min-w-0 flex p-0.5 bg-gray-200/70 dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700/50 rounded-lg h-8">
        <button type="button" data-test="switch-me" @click="chooseMe"
                :class="!isTeamMode ? 'bg-white dark:bg-zinc-700 text-black dark:text-white shadow-sm' : 'text-gray-500 dark:text-zinc-400 hover:text-gray-800 dark:hover:text-zinc-200'"
                class="px-3 rounded-md text-[11px] font-semibold transition-all shrink-0">Me</button>
        <button type="button" data-test="switch-team" @click="chooseTeamSide"
                :class="isTeamMode ? 'bg-white dark:bg-zinc-700 text-black dark:text-white shadow-sm' : 'text-gray-500 dark:text-zinc-400 hover:text-gray-800 dark:hover:text-zinc-200'"
                class="flex-1 min-w-0 px-2 rounded-md text-[11px] font-semibold transition-all flex items-center gap-1">
          <span class="truncate">{{ teamLabel }}</span>
        </button>
        <button type="button" data-test="switch-menu" @click.stop="open = !open" aria-label="Teams menu" :aria-expanded="open"
                class="px-1 rounded-md text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-white transition-colors shrink-0">
          <svg class="w-3 h-3 transition-transform" :class="open ? 'rotate-180' : ''" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M19 9l-7 7-7-7" /></svg>
        </button>
      </div>
      <router-link v-if="pendingCount" to="/invitations" data-test="team-badge"
                   :title="`${pendingCount} pending invitation${pendingCount === 1 ? '' : 's'}`"
                   class="shrink-0 min-w-[18px] h-[18px] px-1 rounded-full bg-red-500 text-white text-[9px] font-black inline-flex items-center justify-center leading-none">{{ pendingCount }}</router-link>
    </div>

    <div v-if="open" v-click-outside="close" role="menu" data-test="team-menu"
         :class="[
           'absolute bg-white dark:bg-zinc-900 border border-gray-200/80 dark:border-zinc-700/70 rounded-xl shadow-[0_12px_32px_-8px_rgba(0,0,0,0.18)] dark:shadow-[0_12px_32px_-8px_rgba(0,0,0,0.6)] p-1 z-[110]',
           collapsed ? 'left-full top-0 ml-2 w-60' : 'left-0 right-0 top-full mt-1'
         ]">
      <p v-if="teams.length" class="px-2 pt-1.5 pb-1 text-[10px] font-medium text-gray-400 dark:text-zinc-500">Your teams</p>
      <button v-for="t in teams" :key="t.id" type="button" role="menuitem" @click="chooseTeam(t.id)"
              class="w-full flex items-center gap-2 px-2 py-1.5 rounded-md text-left text-xs hover:bg-gray-100 dark:hover:bg-zinc-800 transition-colors">
        <span class="w-3.5 shrink-0 text-gray-900 dark:text-white">
          <svg v-if="String(t.id) === teamId" class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" /></svg>
        </span>
        <span class="flex-1 min-w-0 truncate text-gray-800 dark:text-zinc-100 font-medium">{{ t.name }}</span>
        <span class="shrink-0 text-[10px] text-gray-400 dark:text-zinc-500 capitalize">{{ t.role }}</span>
      </button>
      <div v-if="teams.length" class="h-px bg-gray-100 dark:bg-zinc-800 mx-1 my-1"></div>
      <button type="button" role="menuitem" @click="go('/team/new')"
              class="w-full flex items-center gap-2 px-2 py-1.5 rounded-md text-left text-xs text-gray-700 dark:text-zinc-200 hover:bg-gray-100 dark:hover:bg-zinc-800 transition-colors">
        <svg class="w-3.5 h-3.5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M12 4v16m8-8H4" /></svg>
        Create team
      </button>
      <button type="button" role="menuitem" @click="go('/invitations')"
              class="w-full flex items-center gap-2 px-2 py-1.5 rounded-md text-left text-xs text-gray-700 dark:text-zinc-200 hover:bg-gray-100 dark:hover:bg-zinc-800 transition-colors">
        <svg class="w-3.5 h-3.5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M3 8l7.89 5.26a2 2 0 002.22 0L21 8M5 19h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" /></svg>
        <span class="flex-1">Invitations ({{ pendingCount }})</span>
      </button>
      <button v-if="settingsTeamId" type="button" role="menuitem" @click="go({ path: '/team/settings', query: { team: settingsTeamId } })"
              class="w-full flex items-center gap-2 px-2 py-1.5 rounded-md text-left text-xs text-gray-700 dark:text-zinc-200 hover:bg-gray-100 dark:hover:bg-zinc-800 transition-colors">
        <svg class="w-3.5 h-3.5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z M15 12a3 3 0 11-6 0 3 3 0 016 0z" /></svg>
        Team settings
      </button>
      <template v-if="collapsed">
        <div class="h-px bg-gray-100 dark:bg-zinc-800 mx-1 my-1"></div>
        <button type="button" role="menuitem" @click="chooseMe"
                class="w-full flex items-center gap-2 px-2 py-1.5 rounded-md text-left text-xs text-gray-700 dark:text-zinc-200 hover:bg-gray-100 dark:hover:bg-zinc-800 transition-colors">
          <span class="w-3.5 shrink-0">
            <svg v-if="!isTeamMode" class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" /></svg>
          </span>
          Me
        </button>
      </template>
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue';
import { storeToRefs } from 'pinia';
import { useRouter } from 'vue-router';
import { useTeamStore, rememberTeam, rememberedTeam } from '../stores/teamStore';
import { teamHome, useTeamMode } from '../composables/useTeamMode';

defineProps({ collapsed: { type: Boolean, default: false } });

// Two heads and a third behind them.
const TEAM_ICON = 'M18 18.72a9.094 9.094 0 003.741-.479 3 3 0 00-4.682-2.72m.94 3.198l.001.031c0 .225-.012.447-.037.666A11.944 11.944 0 0112 21c-2.17 0-4.207-.576-5.963-1.584A6.062 6.062 0 016 18.719m12 0a5.971 5.971 0 00-.941-3.197m0 0A5.995 5.995 0 0012 12.75a5.995 5.995 0 00-5.058 2.772m0 0a3 3 0 00-4.681 2.72 8.986 8.986 0 003.74.477m.94-3.197a5.971 5.971 0 00-.94 3.197M15 6.75a3 3 0 11-6 0 3 3 0 016 0zm6 3a2.25 2.25 0 11-4.5 0 2.25 2.25 0 014.5 0zm-13.5 0a2.25 2.25 0 11-4.5 0 2.25 2.25 0 014.5 0z';

const router = useRouter();
const store = useTeamStore();
const { teams, pendingCount } = storeToRefs(store);
const { teamId, isTeamMode, team } = useTeamMode();

const open = ref(false);
const close = () => { open.value = false; };

// The team the Team side opens: the one on screen, the last chosen, or the
// first you are in.
const nextTeam = computed(() => team.value || store.getTeam(rememberedTeam()) || teams.value[0] || null);
const teamName = computed(() => team.value?.name || '');
const teamLabel = computed(() => (nextTeam.value ? `Team: ${nextTeam.value.name}` : 'Team'));
const settingsTeamId = computed(() => (nextTeam.value ? String(nextTeam.value.id) : ''));

function go(to) {
  close();
  return router.push(to);
}

function chooseMe() {
  rememberTeam('');
  close();
  if (isTeamMode.value) return router.push('/');
}

function chooseTeam(id) {
  rememberTeam(String(id));
  return go(teamHome(id));
}

// No team yet: the Team side is where you make one (§1 of the flows).
function chooseTeamSide() {
  if (!nextTeam.value) return go('/team/new');
  return chooseTeam(nextTeam.value.id);
}
</script>
