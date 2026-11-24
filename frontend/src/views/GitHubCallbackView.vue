<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<!-- Where GitHub returns after the GitHub App is installed or changed. It
     posts GitHub's code and state with the session cookie, so the connect is
     finished by the signed-in account it was started for, then goes back to
     where it started. -->
<template>
  <div class="grow flex items-center justify-center p-4 bg-zinc-50 dark:bg-zinc-950">
    <div class="w-full max-w-md bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl p-6 sm:p-8 space-y-5" data-test="github-callback" :data-state="state" aria-live="polite">
      <div class="flex items-center gap-2.5">
        <svg viewBox="0 0 24 24" class="w-6 h-6 text-gray-700 dark:text-zinc-100" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" />
          <path d="M12 7l-3.5 8M12 7l3.5 8M9.5 12h5" />
        </svg>
        <span class="text-sm font-bold text-gray-800 dark:text-zinc-200">AgentRQ</span>
        <span class="text-gray-300 dark:text-zinc-600" aria-hidden="true">+</span>
        <svg class="w-5 h-5 text-gray-900 dark:text-zinc-100" fill="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path :d="GITHUB_MARK" /></svg>
      </div>
      <div class="flex items-center gap-3">
        <svg v-if="state === 'connecting'" class="w-5 h-5 shrink-0 animate-spin text-gray-400" fill="none" viewBox="0 0 24 24" aria-hidden="true">
          <circle cx="12" cy="12" r="10" stroke="currentColor" stroke-width="3" class="opacity-25" />
          <path d="M22 12a10 10 0 0 0-10-10" stroke="currentColor" stroke-width="3" stroke-linecap="round" />
        </svg>
        <h1 class="text-xl font-black text-gray-900 dark:text-white leading-tight">{{ title }}</h1>
      </div>
      <p v-if="message" data-test="github-callback-message" class="text-sm text-gray-500 dark:text-zinc-400">{{ message }}</p>
      <router-link v-if="state !== 'connecting'" :to="back" data-test="github-callback-back"
                   class="inline-flex px-6 py-2.5 rounded-lg bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest hover:opacity-90">Back to AgentRQ</router-link>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';

import { connectGitHub } from '../api';
import { useToasts } from '../composables/useToasts';
import { GITHUB_MARK, takeGitHubReturn } from '../utils/githubConnect';

const route = useRoute();
const router = useRouter();
const { notifySuccess } = useToasts();
const state = ref('connecting');
const message = ref('');
const back = ref('/');

const title = computed(() => ({
  connecting: 'Connecting GitHub…',
  requested: 'Waiting for approval',
  failed: 'GitHub was not connected',
}[state.value]));

const first = (v) => (Array.isArray(v) ? v[0] : v) || '';

onMounted(async () => {
  back.value = takeGitHubReturn();
  const code = first(route.query.code);
  const st = first(route.query.state);
  // An organization member asked its owners to install the app.
  if (first(route.query.setup_action) === 'request') {
    state.value = 'requested';
    message.value = "GitHub sent your request to the organization's owners. Connect again once one of them has approved it.";
    return;
  }
  if (!code || !st) {
    state.value = 'failed';
    message.value = 'GitHub did not send back what AgentRQ needs. Ask your administrator to turn on "Request user authorization (OAuth) during installation" in the GitHub App, then choose Connect GitHub beside a skill import to start again.';
    return;
  }
  try {
    const conn = await connectGitHub(code, st);
    const accounts = conn.installations.map((i) => i.account).join(', ');
    notifySuccess(accounts ? `GitHub connected: ${accounts}.` : 'GitHub connected, but the app is not installed on any account you can reach.');
    router.replace(back.value);
  } catch (e) {
    state.value = 'failed';
    // The server's reasons start in lower case, to follow on from others.
    message.value = e.message.charAt(0).toUpperCase() + e.message.slice(1);
  }
});
</script>
