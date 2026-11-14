<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- Managing a team, over whatever page is on screen: its members, the
     invitations not yet answered, the log of every change, and which of your
     workspaces it sees. Every member may change all of it. Open while the
     route says ?manage=<tab>, so a link or the back button opens and shuts it.
     Each change names the version of the team it was made against; one made
     over someone else's change is refused, and the team is read again. -->
<template>
  <Teleport to="body">
  <div v-if="tab" class="fixed inset-0 z-[90] flex items-end sm:items-center justify-center sm:p-4" role="dialog" aria-modal="true" aria-labelledby="team-manage-title" data-test="team-manage">
    <div class="absolute inset-0 bg-gray-900/60 backdrop-blur-sm" @click="close"></div>
    <div class="relative w-full sm:max-w-2xl max-h-[90vh] flex flex-col bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-t-xl sm:rounded-xl shadow-2xl">
      <header class="flex items-start gap-3 px-5 sm:px-6 pt-5 pb-3 border-b border-gray-100 dark:border-zinc-800">
        <div class="flex-1 min-w-0">
          <h2 id="team-manage-title" class="flex text-lg font-black text-gray-900 dark:text-zinc-50 leading-tight min-w-0">
            <TeamNameEditor :team="team" input-class="text-lg h-8 font-black" />
          </h2>
          <p class="text-[11px] text-gray-500 dark:text-zinc-400 mt-0.5">Manage team · {{ members.length }} {{ members.length === 1 ? 'member' : 'members' }}</p>
        </div>
        <button type="button" data-test="team-manage-close" @click="close" aria-label="Close" class="shrink-0 p-1 text-gray-400 hover:text-gray-900 dark:hover:text-white">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
        </button>
      </header>

      <nav class="flex gap-1 px-4 sm:px-5 pt-2 border-b border-gray-100 dark:border-zinc-800 overflow-x-auto no-scrollbar" role="tablist">
        <button v-for="t in TABS" :key="t.id" type="button" role="tab" :aria-selected="tab === t.id" :data-test="`manage-tab-${t.id}`" @click="select(t.id)"
                :class="tab === t.id ? 'border-gray-900 dark:border-white text-gray-900 dark:text-white' : 'border-transparent text-gray-500 dark:text-zinc-400 hover:text-gray-800 dark:hover:text-zinc-200'"
                class="shrink-0 px-2.5 pb-2 border-b-2 text-[11px] font-bold uppercase tracking-widest transition-colors">
          {{ t.label }}<span v-if="t.id === 'invitations' && invitations.length" class="ml-1 text-gray-400 dark:text-zinc-500">{{ invitations.length }}</span>
        </button>
      </nav>

      <div class="flex-1 min-h-0 overflow-y-auto custom-scrollbar px-5 sm:px-6 py-4">
        <!-- Members: anyone removes anyone else, and leaves. -->
        <section v-if="tab === 'members'" class="space-y-5">
          <ul class="divide-y divide-gray-100 dark:divide-zinc-800">
            <li v-for="m in members" :key="m.userId" data-test="member-row" class="py-2.5 flex items-center gap-3">
              <OwnerAvatar :owner="m" size="md" />
              <div class="flex-1 min-w-0">
                <p class="text-sm font-bold text-gray-900 dark:text-zinc-100 truncate">{{ m.name || m.email }}<span v-if="isMe(m)" class="ml-1 text-[11px] font-medium text-gray-400">(you)</span></p>
                <p v-if="m.name" class="text-[11px] text-gray-500 dark:text-zinc-400 truncate">{{ m.email }}</p>
              </div>
              <button v-if="!isMe(m)" type="button" data-test="member-remove" @click="removing = m" :disabled="busy"
                      class="text-[10px] font-bold uppercase tracking-widest text-red-600 dark:text-red-400 hover:underline disabled:opacity-40">Remove</button>
            </li>
          </ul>
          <div class="flex flex-wrap gap-2 pt-3 border-t border-gray-100 dark:border-zinc-800">
            <button type="button" data-test="leave" @click="confirming = 'leave'" :disabled="busy"
                    class="px-4 py-2 bg-white dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700 text-[10px] font-bold text-gray-900 dark:text-zinc-100 hover:border-black dark:hover:border-white rounded-lg uppercase tracking-widest disabled:opacity-40">Leave team</button>
            <button type="button" data-test="delete" @click="confirming = 'delete'" :disabled="busy"
                    class="px-4 py-2 bg-red-600 text-white border border-red-700 text-[10px] font-bold hover:bg-red-700 rounded-lg uppercase tracking-widest disabled:opacity-40">Delete team</button>
          </div>
        </section>

        <!-- Invitations: the form, then those not yet answered. -->
        <section v-else-if="tab === 'invitations'" class="space-y-5">
          <form @submit.prevent="invite" data-test="invite-form" class="space-y-2">
            <label class="block text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest" for="invite-email">Invite by email</label>
            <div class="flex flex-col sm:flex-row gap-2">
              <input id="invite-email" ref="emailRef" v-model="inviteEmail" type="email" required placeholder="name@company.com" autocomplete="off"
                     class="min-w-0 flex-1 bg-gray-50 dark:bg-zinc-800/50 border border-gray-200 dark:border-zinc-800 rounded-lg px-3 py-2 text-sm focus:border-gray-900 dark:focus:border-white focus:ring-0 outline-none font-medium text-gray-900 dark:text-zinc-100" />
              <button type="submit" :disabled="inviting"
                      class="bg-gray-900 dark:bg-white text-white dark:text-zinc-900 px-5 py-2 rounded-lg text-[10px] font-bold uppercase tracking-widest disabled:opacity-40">
                {{ inviting ? 'Sending…' : 'Send' }}
              </button>
            </div>
            <p v-if="inviteErr" data-test="invite-error" role="alert" class="text-[11px] font-semibold text-red-600 dark:text-red-400">{{ inviteErr }}</p>
          </form>
          <p v-if="invitations.length === 0" data-test="invitations-empty" class="text-[11px] text-gray-500 dark:text-zinc-400">No invitation waiting for an answer.</p>
          <ul v-else class="divide-y divide-gray-100 dark:divide-zinc-800">
            <li v-for="i in invitations" :key="i.id" data-test="invitation-row" class="py-2.5 flex flex-col sm:flex-row sm:items-center gap-2">
              <div class="flex-1 min-w-0">
                <p class="text-sm font-bold text-gray-900 dark:text-zinc-100 truncate">{{ i.email }}</p>
                <p class="text-[11px] text-gray-500 dark:text-zinc-400"><template v-if="inviterName(i, members)">Sent by {{ inviterName(i, members) }} · </template>expires {{ expiresIn(i.expiresAt) }}</p>
              </div>
              <div class="flex gap-3 shrink-0">
                <button type="button" data-test="invitation-resend" @click="resend(i)" :disabled="inviting || busy" class="text-[10px] font-bold uppercase tracking-widest text-gray-700 dark:text-zinc-200 hover:underline disabled:opacity-40">Resend</button>
                <button type="button" data-test="invitation-revoke" @click="revoke(i)" :disabled="busy" class="text-[10px] font-bold uppercase tracking-widest text-red-600 dark:text-red-400 hover:underline disabled:opacity-40">Revoke</button>
              </div>
            </li>
          </ul>
        </section>

        <!-- Activity: every change to the team, newest first. -->
        <section v-else-if="tab === 'activity'">
          <p v-if="activities === null" class="text-[11px] text-gray-400 dark:text-zinc-500">Loading…</p>
          <p v-else-if="activities.length === 0" data-test="activity-empty" class="text-[11px] text-gray-500 dark:text-zinc-400">Nothing yet.</p>
          <ol v-else class="space-y-3">
            <li v-for="a in activities" :key="a.id" data-test="activity-row" class="flex items-start gap-2.5">
              <OwnerAvatar :owner="a.actor" />
              <div class="flex-1 min-w-0">
                <p class="text-[12px] text-gray-800 dark:text-zinc-100 break-words">{{ describeActivity(a) }}</p>
                <p class="text-[10px] text-gray-400 dark:text-zinc-500" :title="a.createdAt">{{ timeAgo(a.createdAt) }}</p>
              </div>
            </li>
          </ol>
          <button v-if="moreActivity" type="button" data-test="activity-more" @click="loadActivity(true)" :disabled="loadingActivity"
                  class="mt-4 text-[10px] font-bold uppercase tracking-widest text-gray-600 dark:text-zinc-300 hover:underline disabled:opacity-40">
            {{ loadingActivity ? 'Loading…' : 'Show older' }}
          </button>
        </section>

        <!-- Your workspaces, each with a switch for this team. -->
        <section v-else class="space-y-3">
          <p class="text-[11px] text-gray-500 dark:text-zinc-400 font-medium">Teammates in {{ team?.name }} see the workspaces you share here, and their tasks, read-only.</p>
          <WorkspaceSharing :team-id="teamId" />
        </section>
      </div>
    </div>
  </div>

  <!-- Beside the dialog, not in it: each is a dialog of its own. -->
  <DeleteModal :show="!!removing" title="Remove member" confirm-label="Remove"
               :message="removing ? `Remove ${removing.name || removing.email} from ${team?.name}? Their shared workspaces leave ${team?.name} too.` : ''"
               @close="removing = null" @confirm="removeMember" />
  <DeleteModal :show="confirming === 'leave'" title="Leave team" confirm-label="Leave"
               :message="members.length <= 1 ? `You are the last member: leaving deletes ${team?.name}.` : `Leave ${team?.name}? Your shares to it are removed.`"
               @close="confirming = ''" @confirm="leave" />
  <DeleteModal :show="confirming === 'delete'" title="Delete team" confirm-label="Delete team" :confirm-text="team?.name || ''"
               :message="`Delete ${team?.name} for everyone in it? This cannot be undone.`"
               @close="confirming = ''" @confirm="destroy" />
  <InviteLinkModal :link="inviteLink.link" :email="inviteLink.email" @close="inviteLink = { link: '', email: '' }" />
  </Teleport>
</template>

<script setup>
import { computed, nextTick, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import {
  createTeamInvitation, deleteTeam, fetchTeamActivities, fetchUser, leaveTeam as apiLeaveTeam, listTeamInvitations,
  listTeamMembers, removeTeamMember, revokeTeamInvitation,
} from '../api';
import { rememberTeam, useTeamStore } from '../stores/teamStore';
import { useToasts } from '../composables/useToasts';
import { useEscapeKey } from '../composables/useEscapeKey';
import {
  describeActivity, expiresIn, inviteError, inviterName, isVersionConflict, manageTabOf, timeAgo, useTeamMode, withManage,
} from '../composables/useTeamMode';
import DeleteModal from './DeleteModal.vue';
import InviteLinkModal from './InviteLinkModal.vue';
import OwnerAvatar from './OwnerAvatar.vue';
import TeamNameEditor from './TeamNameEditor.vue';
import WorkspaceSharing from './WorkspaceSharing.vue';

const TABS = [
  { id: 'members', label: 'Members' },
  { id: 'invitations', label: 'Invitations' },
  { id: 'activity', label: 'Activity' },
  { id: 'workspaces', label: 'Your workspaces' },
];
// An activity page; a full one may have more behind it.
const ACTIVITY_PAGE = 20;

const route = useRoute();
const router = useRouter();
const store = useTeamStore();
const { notifySuccess } = useToasts();
const { teamId, team, changeFailed } = useTeamMode();

const tab = computed(() => (teamId.value ? manageTabOf(route) : ''));
const version = () => store.getTeam(teamId.value)?.version;

const me = ref(null);
const members = ref([]);
const invitations = ref([]);
const activities = ref(null);
const moreActivity = ref(false);
const loadingActivity = ref(false);
const busy = ref(false);
const removing = ref(null);
const confirming = ref('');

const inviteEmail = ref('');
const inviting = ref(false);
const inviteErr = ref('');
const inviteLink = ref({ link: '', email: '' });
const emailRef = ref(null);

const isMe = (m) => String(m.userId) === String(me.value?.id);

const select = (id) => router.replace(withManage(route, id));
const close = () => router.replace(withManage(route, ''));

// The lists, read together, each also saying the version the team is at.
async function load() {
  const id = teamId.value;
  try {
    me.value ??= await fetchUser();
    const [m, i] = await Promise.all([listTeamMembers(id), listTeamInvitations(id)]);
    members.value = m?.members || [];
    invitations.value = i?.invitations || [];
    store.setVersion(id, i?.version ?? m?.version);
  } catch (err) {
    await changeFailed(err, id);
  }
  if (tab.value === 'activity') await loadActivity();
}

async function loadActivity(more = false) {
  loadingActivity.value = true;
  try {
    const before = more ? activities.value?.at(-1)?.id || '' : '';
    const res = await fetchTeamActivities(teamId.value, { limit: ACTIVITY_PAGE, before });
    const page = res?.activities || [];
    activities.value = more ? [...activities.value, ...page] : page;
    moreActivity.value = page.length === ACTIVITY_PAGE;
  } catch (err) {
    if (!more) activities.value = [];
    await changeFailed(err);
  } finally {
    loadingActivity.value = false;
  }
}

/**
 * A change: done against the team's version, which its answer moves on. A
 * conflict reads the team and the lists again; the rest is said.
 */
async function change(work, done) {
  busy.value = true;
  try {
    const res = await work(version());
    store.setVersion(teamId.value, res?.version);
    await done?.(res);
    return true;
  } catch (err) {
    if (await changeFailed(err) === 'conflict') await load();
    return false;
  } finally {
    busy.value = false;
  }
}

async function send(email) {
  inviting.value = true;
  inviteErr.value = '';
  try {
    const res = await createTeamInvitation(teamId.value, email, version());
    store.setVersion(teamId.value, res?.version);
    // Always the same words, whether or not that address has an account.
    notifySuccess('Invitation sent');
    if (res?.invitation) invitations.value = [res.invitation, ...invitations.value.filter((i) => i.email !== email)];
    if (!res?.emailSent && res?.link) inviteLink.value = { link: res.link, email };
    return true;
  } catch (err) {
    if (isVersionConflict(err)) {
      await changeFailed(err);
      await load();
    } else {
      inviteErr.value = inviteError(err);
    }
    return false;
  } finally {
    inviting.value = false;
  }
}

async function invite() {
  if (await send(inviteEmail.value.trim())) inviteEmail.value = '';
}

// A new invitation in place of the old one: one pending per address.
async function resend(i) {
  const revoked = await change((v) => revokeTeamInvitation(teamId.value, i.id, v), () => {
    invitations.value = invitations.value.filter((x) => x.id !== i.id);
  });
  if (revoked) await send(i.email);
}

const revoke = (i) => change((v) => revokeTeamInvitation(teamId.value, i.id, v), () => {
  invitations.value = invitations.value.filter((x) => x.id !== i.id);
  notifySuccess(`Invitation to ${i.email} revoked`);
});

function removeMember() {
  const m = removing.value;
  removing.value = null;
  return change((v) => removeTeamMember(teamId.value, m.userId, v), () => {
    members.value = members.value.filter((x) => x.userId !== m.userId);
    notifySuccess(`${m.name || m.email} removed`);
  });
}

// Leaving or deleting: the team goes from the switch, and you go back to Me.
function goodbye(work, message) {
  confirming.value = '';
  const id = teamId.value;
  return change(work, () => {
    store.drop(id);
    rememberTeam('');
    notifySuccess(message);
    return router.push('/');
  });
}

const leave = () => goodbye((v) => apiLeaveTeam(teamId.value, v), `You left ${team.value?.name}`);
const destroy = () => goodbye((v) => deleteTeam(teamId.value, v), `${team.value?.name} deleted`);

// Opened, or moved to another team: read it. Each tab's own list when shown.
watch(() => (tab.value ? teamId.value : ''), (id) => {
  if (!id) return;
  activities.value = null;
  load();
}, { immediate: true });
watch(tab, (t, before) => {
  if (t === 'activity' && before && before !== 'activity') loadActivity();
  if (t === 'invitations') nextTick(() => emailRef.value?.focus());
});

useEscapeKey(() => {
  if (!tab.value || removing.value || confirming.value || inviteLink.value.link) return false;
  close();
});
</script>
