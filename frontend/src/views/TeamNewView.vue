<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- Creating a team, and the Team side's empty state when you are in none
     (§1 of the flows). Creating one makes you its owner and opens its
     settings, with the invite box ready. -->
<template>
  <div class="flex flex-col h-full w-full overflow-y-auto custom-scrollbar">
    <div class="w-full px-4 py-2 mb-6 shrink-0 flex flex-row items-start justify-between gap-4">
      <div class="flex flex-col min-w-0 flex-1">
        <h1 class="text-lg md:text-2xl font-black text-gray-800 dark:text-zinc-200 truncate leading-tight">{{ store.teams.length ? 'Create a team' : 'Teams' }}</h1>
        <p class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5">Teams let you see each other's workspaces and tasks, read-only. You choose which of yours to share.</p>
      </div>
    </div>

    <div class="px-4 pb-10 max-w-xl">
      <form @submit.prevent="create" class="bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-sm shadow-sm p-4 sm:p-8 space-y-4">
        <h2 v-if="!store.teams.length" class="text-sm font-bold text-gray-900 dark:text-zinc-100">Create your first team</h2>
        <div class="space-y-2">
          <label for="team-name" class="block text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest ml-1">Team name</label>
          <input id="team-name" ref="nameRef" v-model="name" type="text" maxlength="100" required placeholder="e.g. Core"
                 class="w-full bg-gray-50 dark:bg-zinc-800/50 border border-gray-200 dark:border-zinc-800 rounded-sm px-4 py-3 text-sm focus:border-gray-900 dark:focus:border-white focus:ring-0 outline-none font-bold text-gray-900 dark:text-zinc-100 transition-all shadow-sm" />
        </div>
        <p v-if="error" data-test="create-error" class="text-[11px] font-semibold text-red-600 dark:text-red-400">{{ error }}</p>
        <div class="flex justify-end">
          <button type="submit" :disabled="busy || !name.trim()"
                  class="bg-gray-900 dark:bg-white text-white dark:text-zinc-900 px-8 py-2.5 rounded-sm text-[10px] font-bold hover:bg-black dark:hover:bg-zinc-100 transition-all uppercase tracking-widest disabled:opacity-40 disabled:cursor-not-allowed">
            {{ busy ? 'Creating…' : 'Create' }}
          </button>
        </div>
      </form>

      <p class="mt-4 text-[11px] text-gray-500 dark:text-zinc-400 px-1" data-test="invitation-hint">
        <router-link v-if="store.pendingCount" to="/invitations" class="font-semibold underline text-gray-900 dark:text-white">
          You have {{ store.pendingCount }} invitation{{ store.pendingCount === 1 ? '' : 's' }}
        </router-link>
        <template v-else>You have no invitations.</template>
      </p>
    </div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { createTeam } from '../api';
import { useTeamStore, rememberTeam } from '../stores/teamStore';

const router = useRouter();
const store = useTeamStore();

const name = ref('');
const busy = ref(false);
const error = ref('');
const nameRef = ref(null);

onMounted(() => nameRef.value?.focus());

async function create() {
  busy.value = true;
  error.value = '';
  try {
    const team = await createTeam(name.value.trim());
    store.upsert(team);
    rememberTeam(String(team.id));
    // Members, with the invite box focused: the next thing to do.
    await router.push({ path: '/team/settings', query: { team: String(team.id), tab: 'members', welcome: '1' } });
  } catch (err) {
    error.value = err.message;
  } finally {
    busy.value = false;
  }
}
</script>
