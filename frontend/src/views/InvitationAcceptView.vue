<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- An invitation link, /invite#<token> (§3 of the flows). Public, since it
     may be opened signed out: then it offers the sign-in and comes back here
     after it. An invitation for another email, or one no longer valid, says
     so and offers no Accept. -->
<template>
  <div class="grow flex items-center justify-center p-4 bg-zinc-50 dark:bg-zinc-950">
    <div class="w-full max-w-md bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl p-6 sm:p-8 space-y-5" data-test="accept-card" :data-state="state">
      <div class="flex items-center gap-2.5">
        <svg viewBox="0 0 24 24" class="w-6 h-6 text-gray-700 dark:text-zinc-100" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" />
          <path d="M12 7l-3.5 8M12 7l3.5 8M9.5 12h5" />
        </svg>
        <span class="text-sm font-bold text-gray-800 dark:text-zinc-200">AgentRQ</span>
      </div>

      <template v-if="state === 'signedOut'">
        <h1 class="text-xl font-black text-gray-900 dark:text-white leading-tight">You have been invited to a team</h1>
        <p class="text-sm text-gray-500 dark:text-zinc-400">Sign in with the email the invitation was sent to, and you come back here to accept it.</p>
        <a :href="signInHref" data-test="accept-sign-in"
           class="inline-flex px-6 py-2.5 rounded-lg bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest hover:opacity-90">Sign in</a>
      </template>

      <template v-else-if="state === 'ready'">
        <h1 class="text-xl font-black text-gray-900 dark:text-white leading-tight">
          {{ preview.invitedByName || 'Someone' }} invited you to {{ preview.teamName }}
        </h1>
        <p class="text-sm text-gray-500 dark:text-zinc-400">As {{ preview.role || 'member' }}. You will see what its members share with it, read-only, and choose what of yours to share.</p>
        <div class="flex gap-2">
          <router-link to="/" class="px-4 py-2 rounded-lg border border-gray-200 dark:border-zinc-700 text-[11px] font-black uppercase tracking-widest text-gray-700 dark:text-zinc-200">Not now</router-link>
          <button type="button" data-test="accept-button" :disabled="busy" @click="accept"
                  class="px-6 py-2 rounded-lg bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest hover:opacity-90 disabled:opacity-40">{{ busy ? 'Joining…' : 'Accept' }}</button>
        </div>
      </template>

      <template v-else-if="state === 'wrongEmail'">
        <h1 class="text-xl font-black text-gray-900 dark:text-white leading-tight">This invitation is for another account</h1>
        <p data-test="accept-wrong-email" class="text-sm text-gray-500 dark:text-zinc-400">
          <template v-if="err">{{ err.message }}</template>
          <template v-else>This invitation is for <span class="font-semibold text-gray-900 dark:text-white">{{ preview.email || 'another email' }}</span>. Sign in with that account.</template>
        </p>
        <router-link to="/" class="inline-flex text-[11px] font-black uppercase tracking-widest text-gray-700 dark:text-zinc-200 underline">Go to AgentRQ</router-link>
      </template>

      <template v-else-if="state === 'invalid'">
        <h1 class="text-xl font-black text-gray-900 dark:text-white leading-tight">Invitation not valid</h1>
        <p data-test="accept-invalid" class="text-sm text-gray-500 dark:text-zinc-400">
          <template v-if="err?.status === 410">{{ err.message }}</template>
          <template v-else>This invitation is no longer valid. Ask {{ preview?.invitedByName || 'whoever invited you' }} for a new one.</template>
        </p>
        <router-link to="/" class="inline-flex text-[11px] font-black uppercase tracking-widest text-gray-700 dark:text-zinc-200 underline">Go to AgentRQ</router-link>
      </template>

      <template v-else-if="state === 'error'">
        <p data-test="accept-error" class="text-sm text-red-600 dark:text-red-400">{{ err.message }}</p>
        <button type="button" @click="load" class="text-[11px] font-black uppercase tracking-widest text-gray-700 dark:text-zinc-200 underline">Try again</button>
      </template>

      <p v-else class="text-sm text-gray-500 dark:text-zinc-400">Reading the invitation…</p>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { acceptInvitation, fetchUser, previewInvitation } from '../api';
import { useTeamStore, rememberTeam } from '../stores/teamStore';
import { invitationState, joinedHome } from '../composables/useTeamMode';

const route = useRoute();
const router = useRouter();
const store = useTeamStore();

// The token is the link's #fragment, which no server is sent. It is kept in
// this tab's sessionStorage, not the address bar, so it survives the sign-in
// without going through its redirect_url, which the server does see.
const TOKEN_KEY = 'agentrq:invite-token';
const token = ref('');

function storage(fn) {
  try {
    return fn(window.sessionStorage);
  } catch {
    return null;
  }
}

function takeToken() {
  const fromLink = route.hash.slice(1);
  if (!fromLink) return storage((s) => s.getItem(TOKEN_KEY)) || '';
  storage((s) => s.setItem(TOKEN_KEY, fromLink));
  router.replace({ path: route.path, query: route.query });
  return fromLink;
}
const signedIn = ref(null);
const preview = ref(null);
const err = ref(null);
const busy = ref(false);

const state = computed(() => {
  if (signedIn.value === false) return 'signedOut';
  if (!token.value) return 'invalid';
  return invitationState(preview.value, err.value);
});

// The login page sends you back to a same-origin redirect_url once signed in.
// Both through the router, so a server under a base path keeps it.
const signInHref = computed(() => router.resolve({ path: '/login', query: { redirect_url: router.resolve('/invite').href } }).href);

async function load() {
  err.value = null;
  preview.value = null;
  try {
    signedIn.value = !!(await fetchUser());
  } catch {
    signedIn.value = false;
  }
  if (!signedIn.value || !token.value) return;
  try {
    preview.value = await previewInvitation(token.value);
  } catch (e) {
    err.value = e;
  }
}

async function accept() {
  busy.value = true;
  try {
    const team = await acceptInvitation(token.value);
    storage((s) => s.removeItem(TOKEN_KEY));
    await store.load();
    store.upsert(team);
    rememberTeam(String(team.id));
    await router.replace(joinedHome(team.id));
  } catch (e) {
    err.value = e;
  } finally {
    busy.value = false;
  }
}

function open() {
  token.value = takeToken();
  return load();
}

onMounted(open);
// Another link pasted into this tab changes only the fragment.
watch(() => route.hash, (hash) => hash && open());
</script>
