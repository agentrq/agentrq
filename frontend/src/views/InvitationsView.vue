<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- Invitations addressed to you (§3 of the flows). Accepting one switches
     to that team, with a one-time offer to share your workspaces. -->
<template>
  <div class="flex flex-col h-full w-full overflow-y-auto custom-scrollbar">
    <div class="w-full px-4 py-2 mb-6 shrink-0 flex flex-row items-start justify-between gap-4">
      <div class="flex flex-col min-w-0 flex-1">
        <h1 class="text-lg md:text-2xl font-black text-gray-800 dark:text-zinc-200 truncate leading-tight">Invitations</h1>
        <p class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5">Teams that invited you. Joining one lets you see what its members share, and share yours.</p>
      </div>
    </div>

    <div class="px-4 pb-10 max-w-2xl space-y-3">
      <p v-if="store.invitations.length === 0" data-test="invitations-empty"
         class="py-6 px-4 border border-dashed border-gray-200 dark:border-zinc-800 rounded-xl text-[11px] text-gray-500 dark:text-zinc-500 font-medium">
        You have no invitations.
      </p>
      <div v-for="i in store.invitations" :key="i.id" data-test="invitation-card"
           class="p-4 rounded-xl border border-gray-100 dark:border-zinc-800 bg-white dark:bg-zinc-900 flex flex-col sm:flex-row sm:items-center gap-3">
        <div class="flex-1 min-w-0">
          <p class="text-sm text-gray-700 dark:text-zinc-200">
            <span class="font-semibold">{{ i.invitedBy || 'Someone' }}</span> invited you to
            <span class="font-bold text-gray-900 dark:text-white">{{ i.teamName }}</span> as {{ i.role }}
          </p>
          <p class="text-[11px] text-gray-500 dark:text-zinc-400 mt-0.5">Expires {{ expiresIn(i.expiresAt) }}</p>
        </div>
        <div class="flex gap-2 shrink-0">
          <button type="button" data-test="invitation-decline" :disabled="busy === i.id" @click="decline(i)"
                  class="px-4 py-2 rounded-lg border border-gray-200 dark:border-zinc-700 bg-white dark:bg-zinc-900 text-[11px] font-black uppercase tracking-widest text-gray-700 dark:text-zinc-200 hover:border-gray-300 disabled:opacity-40">Decline</button>
          <button type="button" data-test="invitation-accept" :disabled="busy === i.id" @click="accept(i)"
                  class="px-4 py-2 rounded-lg bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest hover:opacity-90 disabled:opacity-40">Accept</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { acceptInvitation, declineInvitation } from '../api';
import { useTeamStore, rememberTeam } from '../stores/teamStore';
import { useToasts } from '../composables/useToasts';
import { expiresIn, joinedHome, teamOf } from '../composables/useTeamMode';

const router = useRouter();
const store = useTeamStore();
const { notifySuccess, notifyError } = useToasts();
const busy = ref('');

async function answer(i, work) {
  busy.value = i.id;
  try {
    return await work();
  } catch (err) {
    notifyError(err.message);
    // Answered elsewhere, or no longer valid: the list says what is left.
    await store.load();
    return null;
  } finally {
    busy.value = '';
  }
}

async function accept(i) {
  const res = await answer(i, async () => (await acceptInvitation({ invitationId: i.id })) ?? {});
  if (!res) return;
  const team = { id: i.teamId, name: i.teamName, role: i.role, ...teamOf(res) };
  store.dropInvitation(i.id);
  store.upsert(team);
  rememberTeam(String(team.id));
  return router.push(joinedHome(team.id));
}

async function decline(i) {
  const res = await answer(i, async () => { await declineInvitation(i.id); return true; });
  if (!res) return;
  store.dropInvitation(i.id);
  notifySuccess(`Declined ${i.teamName}`);
}

// Fresh on every visit: the badge may be older than the list.
onMounted(() => store.load());
</script>
