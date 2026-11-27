<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<!-- The GitLab account skills are imported from privately, through the
     server's GitLab application: shown beside an import only when the admin
     has set it up. Connecting leaves for GitLab, which returns to
     /gitlab/callback; that page comes back here. -->
<template>
  <div v-if="conn?.enabled" data-test="gitlab-connection"
       class="flex flex-col sm:flex-row sm:items-center gap-2 sm:gap-3 pt-3 border-t border-gray-200/70 dark:border-zinc-700/60">
    <div class="flex items-center gap-2 min-w-0 flex-1 flex-wrap">
      <svg class="w-3.5 h-3.5 shrink-0 text-gray-900 dark:text-zinc-100" fill="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path :d="GITLAB_MARK" /></svg>
      <span class="text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Private projects</span>
      <span v-if="!conn.username" data-test="gitlab-empty" class="text-[11px] text-gray-500 dark:text-zinc-400">
        Connect GitLab to import from your projects on {{ conn.host }}.
      </span>
      <span v-else data-test="gitlab-account"
            class="inline-flex items-center gap-1.5 pl-2 pr-0.5 py-0.5 rounded-sm border border-gray-200 dark:border-zinc-700 bg-white dark:bg-zinc-900 text-[11px] text-gray-800 dark:text-zinc-200">
        <span class="w-1.5 h-1.5 rounded-full bg-green-500" aria-hidden="true"></span>
        <span class="font-mono font-semibold">{{ conn.username }}</span>
        <span class="text-[9px] font-bold text-gray-400 dark:text-zinc-500">{{ conn.host }}</span>
        <button type="button" data-test="gitlab-disconnect" :aria-label="`Disconnect ${conn.username}`" :title="`Disconnect ${conn.username}`"
                :disabled="busy" @click="confirming = true"
                class="w-5 h-5 inline-flex items-center justify-center rounded-sm text-gray-400 hover:text-gray-900 hover:bg-gray-100 dark:hover:text-white dark:hover:bg-zinc-800 disabled:opacity-40 transition-colors">
          <svg class="w-3 h-3" fill="none" stroke="currentColor" stroke-width="2.5" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" d="M6 6l12 12M18 6L6 18" /></svg>
        </button>
      </span>
    </div>
    <a v-if="!conn.username" :href="conn.authorizeUrl" data-test="gitlab-connect" @click="rememberReturn"
       class="shrink-0 self-start sm:self-auto inline-flex items-center gap-2 px-3 py-1.5 rounded-lg border border-gray-200 dark:border-zinc-700 bg-white dark:bg-zinc-900 text-[11px] font-black uppercase tracking-widest text-gray-800 dark:text-zinc-100 hover:border-gray-400 dark:hover:border-zinc-500 transition-colors">
      <svg class="w-3.5 h-3.5" fill="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path :d="GITLAB_MARK" /></svg>
      Connect GitLab
    </a>

    <DeleteModal :show="confirming" title="Disconnect GitLab" confirm-label="Disconnect"
                 :message="`Private projects of ${conn.username} can no longer be imported. Skills already imported stay. AgentRQ asks GitLab to revoke its access.`"
                 @close="confirming = false" @confirm="disconnect" />
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';

import { disconnectGitLab, getGitLabConnection } from '../api';
import { useToasts } from '../composables/useToasts';
import { GITLAB_MARK, GITLAB_RETURN_KEY } from '../utils/gitlabConnect';
import DeleteModal from './DeleteModal.vue';

const route = useRoute();
const { notifySuccess, notifyError } = useToasts();
const conn = ref(null);
const busy = ref(false);
const confirming = ref(false);

async function load() {
  try {
    conn.value = await getGitLabConnection();
  } catch {
    // Importing public projects needs none of this.
    conn.value = null;
  }
}

// GitLab's return comes back here, in this browser.
function rememberReturn() {
  try {
    sessionStorage.setItem(GITLAB_RETURN_KEY, route.fullPath);
  } catch {
    // Storage off: the callback page goes home instead.
  }
}

async function disconnect() {
  confirming.value = false;
  busy.value = true;
  const name = conn.value.username;
  try {
    await disconnectGitLab();
    await load();
    notifySuccess(`Disconnected ${name}.`);
  } catch (e) {
    notifyError(e.message);
  } finally {
    busy.value = false;
  }
}

onMounted(load);
</script>
