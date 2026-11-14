<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- Across the top of every page in team mode: whose work this is, so
     nobody mistakes a teammate's workspace for their own. The team's name
     renames it in place. Right after joining (?joined=1), once, the offer to
     share yours. -->
<template>
  <div data-test="team-mode-bar" class="shrink-0">
    <div class="flex items-center gap-2 px-4 py-1.5 text-[11px] text-gray-600 dark:text-zinc-300 border-b border-gray-100 dark:border-zinc-800">
      <span class="flex items-center gap-1 min-w-0">
        <span class="shrink-0">Viewing</span>
        <span class="min-w-0 flex font-semibold text-gray-900 dark:text-white"><TeamNameEditor :team="team" :fallback="name" input-class="text-[11px] h-5" /></span>
        <span class="shrink-0">(read-only)</span>
      </span>
      <router-link :to="withManage(route, 'members')" data-test="team-bar-manage" class="ml-auto shrink-0 font-semibold text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-white">Manage</router-link>
      <router-link to="/" @click="rememberTeam('')" class="shrink-0 font-semibold text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-white">Back to Me</router-link>
    </div>
    <div v-if="joined" data-test="joined-banner" class="flex items-center gap-3 px-4 py-2 text-[12px] bg-gray-50 dark:bg-zinc-900 text-gray-800 dark:text-zinc-100 border-b border-gray-100 dark:border-zinc-800">
      <span class="flex-1 min-w-0">You joined <span class="font-semibold">{{ team?.name || name }}</span>. Share your workspaces?</span>
      <router-link :to="shareLink" class="shrink-0 font-bold underline">Share</router-link>
      <button type="button" @click="dismiss" aria-label="Dismiss" class="shrink-0 text-gray-500 hover:text-gray-900 dark:hover:text-white">
        <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
      </button>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { rememberTeam } from '../stores/teamStore';
import { useTeamMode, withManage } from '../composables/useTeamMode';
import TeamNameEditor from './TeamNameEditor.vue';

defineProps({ name: { type: String, default: 'Team' } });

const route = useRoute();
const router = useRouter();
const { team } = useTeamMode();
const joined = computed(() => route.query.joined === '1');

// The welcome's offer: the Manage team dialog on your workspaces, the
// welcome done with.
const shareLink = computed(() => {
  const { joined: _shown, ...query } = route.query;
  return withManage({ path: route.path, query }, 'workspaces');
});

function dismiss() {
  const { joined: _shown, ...query } = route.query;
  return router.replace({ path: route.path, query });
}
</script>
