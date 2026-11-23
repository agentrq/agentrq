<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<!-- A team's settings, laid out like a workspace's: its members, the
     invitations not yet answered, the log of every change, which of your
     workspaces it sees, and leaving or deleting it. Every member may change
     all of it. The tab is the route's ?tab=, so a link opens one. Each change
     names the version of the team it was made against; one made over someone
     else's change is refused, and the team is read again. -->
<template>
  <div class="flex flex-col h-full w-full min-w-0 bg-transparent overflow-y-auto custom-scrollbar" data-test="team-settings">
    <div class="w-full px-4 py-2 mb-6 shrink-0 flex flex-col min-w-0">
      <h1 class="flex text-lg md:text-2xl font-black text-gray-800 dark:text-zinc-200 leading-tight min-w-0">
        <TeamNameEditor :team="team" input-class="text-lg md:text-2xl h-9 font-black" />
      </h1>
      <p class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5">Team settings · {{ members.length }} {{ members.length === 1 ? 'member' : 'members' }}</p>
    </div>

    <div class="flex-1 px-4 pb-20 min-w-0">
      <div class="w-full min-w-0 flex flex-col md:flex-row gap-8">
        <div class="w-full md:w-48 shrink-0">
          <nav class="flex flex-col gap-1 sticky top-0" role="tablist">
            <button v-for="t in TABS" :key="t.id" type="button" role="tab" :aria-selected="tab === t.id" :data-test="`settings-tab-${t.id}`" @click="select(t.id)"
                    :class="tab === t.id ? 'bg-gray-900 text-white dark:bg-white dark:text-zinc-900 shadow-lg shadow-black/5' : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-100 dark:hover:bg-zinc-800'"
                    class="flex items-center gap-3 px-4 py-2.5 rounded-sm text-[10px] font-bold uppercase tracking-widest transition-all text-left">
              <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" :d="t.icon" /></svg>
              <span class="flex-1">{{ t.label }}</span>
              <span v-if="t.id === 'invitations' && invitations.length" class="opacity-60">{{ invitations.length }}</span>
            </button>
            <div class="my-4 border-t border-gray-100 dark:border-zinc-800"></div>
            <button type="button" role="tab" :aria-selected="tab === 'danger'" data-test="settings-tab-danger" @click="select('danger')"
                    :class="tab === 'danger' ? 'bg-red-600 text-white shadow-lg shadow-red-500/20' : 'text-red-500 hover:bg-red-50 dark:hover:bg-red-500/10'"
                    class="flex items-center gap-3 px-4 py-2.5 rounded-sm text-[10px] font-bold uppercase tracking-widest transition-all text-left">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" /></svg>
              Danger Zone
            </button>
          </nav>
        </div>

        <div class="flex-1 min-w-0">
          <div class="bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-sm shadow-sm overflow-hidden min-h-[500px] p-4 sm:p-8">
            <!-- Members: anyone removes anyone else. -->
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
            </section>

            <!-- Invitations: the form, then those not yet answered. -->
            <section v-else-if="tab === 'invitations'" class="space-y-5">
              <form @submit.prevent="invite" data-test="invite-form" class="space-y-2">
                <label class="block text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest" for="invite-email">Invite by email</label>
                <div class="flex flex-col sm:flex-row gap-2">
                  <input id="invite-email" ref="emailRef" v-model="inviteEmail" type="email" required placeholder="name@company.com" autocomplete="off"
                         class="min-w-0 flex-1 bg-gray-50 dark:bg-zinc-800/50 border border-gray-200 dark:border-zinc-800 rounded-sm px-3 py-2 text-sm focus:border-gray-900 dark:focus:border-white focus:ring-0 outline-none font-medium text-gray-900 dark:text-zinc-100" />
                  <button type="submit" :disabled="inviting"
                          class="bg-gray-900 dark:bg-white text-white dark:text-zinc-900 px-5 py-2 rounded-sm text-[10px] font-bold uppercase tracking-widest disabled:opacity-40">
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
            <section v-else-if="tab === 'workspaces'" class="space-y-3">
              <p class="text-[11px] text-gray-500 dark:text-zinc-400 font-medium">Teammates in {{ team?.name }} see the workspaces you share here, and their tasks, read-only.</p>
              <WorkspaceSharing :team-id="teamId" />
            </section>

            <!-- Leaving, and deleting it for everyone. -->
            <section v-else class="space-y-4">
              <div class="p-6 border border-red-100 dark:border-red-900/30 bg-red-50/30 dark:bg-red-500/5 rounded-sm flex flex-col md:flex-row md:items-center justify-between gap-6 shadow-sm">
                <div class="flex-1">
                  <h4 class="text-sm font-bold text-gray-900 dark:text-zinc-100">Leave Team</h4>
                  <p class="text-[11px] text-gray-600 dark:text-zinc-400 mt-1 font-medium">Your shares to it are removed. The last member to leave deletes the team.</p>
                </div>
                <button type="button" data-test="leave" @click="confirming = 'leave'" :disabled="busy"
                        class="px-6 py-2.5 bg-white dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700 text-[10px] font-bold text-gray-900 dark:text-zinc-100 hover:border-black dark:hover:border-white transition-all shadow-sm rounded-sm uppercase tracking-widest whitespace-nowrap disabled:opacity-40">Leave team</button>
              </div>
              <div class="p-6 border border-red-200 dark:border-red-900/50 bg-red-50 dark:bg-red-900/10 rounded-sm flex flex-col md:flex-row md:items-center justify-between gap-6 shadow-sm">
                <div class="flex-1">
                  <h4 class="text-sm font-bold text-red-600 dark:text-red-500">Delete Team</h4>
                  <p class="text-[11px] text-gray-600 dark:text-zinc-400 mt-1 font-medium">Delete the team for everyone in it. This action is irreversible.</p>
                </div>
                <button type="button" data-test="delete" @click="confirming = 'delete'" :disabled="busy"
                        class="px-8 py-2.5 bg-red-600 text-white border border-red-700 text-[10px] font-bold hover:bg-red-700 transition-all shadow-lg shadow-red-600/20 rounded-sm uppercase tracking-widest whitespace-nowrap disabled:opacity-40">Delete team</button>
              </div>
            </section>
          </div>
        </div>
      </div>
    </div>

    <DeleteModal :show="!!removing" title="Remove member" confirm-label="Remove"
                 :message="removing ? `Remove ${removing.name || removing.email} from ${team?.name}? Their shared workspaces leave ${team?.name} too.` : ''"
                 @close="removing = null" @confirm="removeMember" />
    <DeleteModal :show="confirming === 'leave'" title="Leave team" confirm-label="Leave"
                 :message="members.length <= 1 ? `You are the last member: leaving deletes ${team?.name}.` : `Leave ${team?.name}? Your shares to it are removed.`"
                 @close="confirming = ''" @confirm="leave" />
    <DeleteModal :show="confirming === 'delete'" title="Delete team" confirm-label="Delete team" :confirm-text="team?.name || ''"
                 :message="`Delete ${team?.name} for everyone in it? This cannot be undone.`"
                 @close="confirming = ''" @confirm="destroy" />
    <InviteLinkModal :link="inviteLink.link" :email="inviteLink.email" :email-sent="inviteLink.emailSent" @close="inviteLink = { link: '', email: '', emailSent: false }" />
  </div>
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
import {
  describeActivity, expiresIn, inviteError, inviterName, isVersionConflict, settingsTabOf, teamSettings, timeAgo, useTeamMode,
} from '../composables/useTeamMode';
import DeleteModal from '../components/DeleteModal.vue';
import InviteLinkModal from '../components/InviteLinkModal.vue';
import OwnerAvatar from '../components/OwnerAvatar.vue';
import TeamNameEditor from '../components/TeamNameEditor.vue';
import WorkspaceSharing from '../components/WorkspaceSharing.vue';

const TABS = [
  { id: 'members', label: 'Members', icon: 'M17 20h5v-2a3 3 0 00-5.356-1.857M17 20H7m10 0v-2c0-.656-.126-1.283-.356-1.857M7 20H2v-2a3 3 0 015.356-1.857M7 20v-2c0-.656.126-1.283.356-1.857m0 0a5.002 5.002 0 019.288 0M15 7a3 3 0 11-6 0 3 3 0 016 0zm6 3a2 2 0 11-4 0 2 2 0 014 0zM7 10a2 2 0 11-4 0 2 2 0 014 0z' },
  { id: 'invitations', label: 'Invitations', icon: 'M3 8l7.89 5.26a2 2 0 002.22 0L21 8M5 19h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z' },
  { id: 'activity', label: 'Activity', icon: 'M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z' },
  { id: 'workspaces', label: 'Your workspaces', icon: 'M3 7a2 2 0 012-2h4l2 2h8a2 2 0 012 2v8a2 2 0 01-2 2H5a2 2 0 01-2-2V7z' },
];
// An activity page; a full one may have more behind it.
const ACTIVITY_PAGE = 20;

const route = useRoute();
const router = useRouter();
const store = useTeamStore();
const { notifySuccess } = useToasts();
const { teamId, team, changeFailed } = useTeamMode();

const tab = computed(() => settingsTabOf(route));
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
const inviteLink = ref({ link: '', email: '', emailSent: false });
const emailRef = ref(null);

const isMe = (m) => String(m.userId) === String(me.value?.id);

const select = (id) => router.replace(teamSettings(teamId.value, id));

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
    // "Show older" is there only under a full page, so there is a last one.
    const before = more ? activities.value.at(-1).id : '';
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
    notifySuccess(res?.emailSent ? 'Invitation sent' : 'Invitation created');
    if (res?.invitation) invitations.value = [res.invitation, ...invitations.value.filter((i) => i.email !== email)];
    if (res?.link) inviteLink.value = { link: res.link, email, emailSent: !!res.emailSent };
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
watch(teamId, (id) => {
  if (!id) return;
  activities.value = null;
  load();
}, { immediate: true });
watch(tab, (t, before) => {
  if (t === 'activity' && before !== 'activity') loadActivity();
  if (t === 'invitations') nextTick(() => emailRef.value?.focus());
});
</script>
