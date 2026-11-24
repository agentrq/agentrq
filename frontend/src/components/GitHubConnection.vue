<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<!-- The GitHub accounts skills are imported from privately, through the
     server's GitHub App: shown beside an import only when the admin has set
     the app up. Connecting leaves for GitHub, which returns to
     /github/callback; that page comes back here. -->
<template>
  <div v-if="conn?.enabled" data-test="github-connection"
       class="flex flex-col sm:flex-row sm:items-center gap-2 sm:gap-3 pt-3 border-t border-gray-200/70 dark:border-zinc-700/60">
    <div class="flex items-center gap-2 min-w-0 flex-1 flex-wrap">
      <svg class="w-3.5 h-3.5 shrink-0 text-gray-900 dark:text-zinc-100" fill="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path :d="GITHUB_MARK" /></svg>
      <span class="text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Private repositories</span>
      <span v-if="!conn.installations.length" data-test="github-empty" class="text-[11px] text-gray-500 dark:text-zinc-400">
        Connect GitHub to import from repositories you choose.
      </span>
      <ul v-else class="flex flex-wrap items-center gap-1.5" aria-label="Connected GitHub accounts">
        <li v-for="i in conn.installations" :key="i.installationId" data-test="github-installation"
            class="inline-flex items-center gap-1.5 pl-2 pr-0.5 py-0.5 rounded-sm border border-gray-200 dark:border-zinc-700 bg-white dark:bg-zinc-900 text-[11px] text-gray-800 dark:text-zinc-200">
          <span class="w-1.5 h-1.5 rounded-full bg-green-500" aria-hidden="true"></span>
          <span class="font-mono font-semibold">{{ i.account }}</span>
          <span v-if="i.accountType === 'Organization'" class="text-[9px] font-bold uppercase tracking-wider text-gray-400 dark:text-zinc-500">Org</span>
          <button type="button" :data-test="`github-disconnect-${i.installationId}`" :aria-label="`Disconnect ${i.account}`" :title="`Disconnect ${i.account}`"
                  :disabled="busy" @click="confirming = i"
                  class="w-5 h-5 inline-flex items-center justify-center rounded-sm text-gray-400 hover:text-gray-900 hover:bg-gray-100 dark:hover:text-white dark:hover:bg-zinc-800 disabled:opacity-40 transition-colors">
            <svg class="w-3 h-3" fill="none" stroke="currentColor" stroke-width="2.5" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" d="M6 6l12 12M18 6L6 18" /></svg>
          </button>
        </li>
      </ul>
    </div>
    <a :href="conn.installUrl" data-test="github-connect" @click="rememberReturn"
       class="shrink-0 self-start sm:self-auto inline-flex items-center gap-2 px-3 py-1.5 rounded-lg border border-gray-200 dark:border-zinc-700 bg-white dark:bg-zinc-900 text-[11px] font-black uppercase tracking-widest text-gray-800 dark:text-zinc-100 hover:border-gray-400 dark:hover:border-zinc-500 transition-colors">
      <svg class="w-3.5 h-3.5" fill="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path :d="GITHUB_MARK" /></svg>
      {{ conn.installations.length ? 'Manage on GitHub' : 'Connect GitHub' }}
    </a>

    <DeleteModal :show="!!confirming" title="Disconnect GitHub" confirm-label="Disconnect"
                 :message="confirming ? `Private repositories of ${confirming.account} can no longer be imported. Skills already imported stay. The app stays installed on GitHub until you uninstall it there.` : ''"
                 @close="confirming = null" @confirm="disconnect" />
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';

import { disconnectGitHub, getGitHubConnection } from '../api';
import { useToasts } from '../composables/useToasts';
import { GITHUB_MARK, GITHUB_RETURN_KEY } from '../utils/githubConnect';
import DeleteModal from './DeleteModal.vue';

const route = useRoute();
const { notifySuccess, notifyError } = useToasts();
const conn = ref(null);
const busy = ref(false);
const confirming = ref(null);

async function load() {
  try {
    conn.value = await getGitHubConnection();
  } catch {
    // Importing public repositories needs none of this.
    conn.value = null;
  }
}

// GitHub's return comes back here, in this browser.
function rememberReturn() {
  try {
    sessionStorage.setItem(GITHUB_RETURN_KEY, route.fullPath);
  } catch {
    // Storage off: the callback page goes home instead.
  }
}

async function disconnect() {
  const i = confirming.value;
  confirming.value = null;
  busy.value = true;
  try {
    await disconnectGitHub(i.installationId);
    conn.value.installations = conn.value.installations.filter((x) => x.installationId !== i.installationId);
    notifySuccess(`Disconnected ${i.account}.`);
  } catch (e) {
    notifyError(e.message);
  } finally {
    busy.value = false;
  }
}

onMounted(load);
</script>
