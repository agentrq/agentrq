<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- A team's settings (§2 and §8 of the flows): members and invitations,
     your workspaces shared with it, its name, and deleting or leaving it.
     Laid out as Workspace settings is, tabs on the left and a card. Members
     see the lists; owners and admins change them. -->
<template>
  <div class="flex flex-col h-full w-full min-w-0 bg-transparent overflow-y-auto custom-scrollbar">
    <div class="w-full px-4 py-2 mb-6 shrink-0 flex flex-row items-start justify-between gap-4">
      <div class="flex flex-col min-w-0 flex-1">
        <h1 class="text-lg md:text-2xl font-black text-gray-800 dark:text-zinc-200 truncate leading-tight">{{ team?.name || 'Team' }}</h1>
        <p class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5">Team settings<template v-if="team?.role"> · you are {{ article(team.role) }} {{ team.role }}</template></p>
      </div>
    </div>

    <div class="flex-1 px-4 pb-20 min-w-0">
      <div class="w-full min-w-0 flex flex-col md:flex-row gap-8">
        <div class="w-full md:w-48 shrink-0">
          <nav class="flex flex-col gap-1 sticky top-0">
            <button v-for="tab in tabs" :key="tab.id" type="button" @click="selectTab(tab.id)" :data-test="`tab-${tab.id}`"
                    :class="[activeTab === tab.id ? 'bg-gray-900 text-white dark:bg-white dark:text-zinc-900 shadow-lg shadow-black/5' : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-100 dark:hover:bg-zinc-800']"
                    class="flex items-center gap-3 px-4 py-2.5 rounded-sm text-[10px] font-bold uppercase tracking-widest transition-all text-left">
              <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" :d="tab.icon" /></svg>
              {{ tab.label }}
            </button>
            <div class="my-4 border-t border-gray-100 dark:border-zinc-800"></div>
            <button type="button" @click="selectTab('danger')" data-test="tab-danger"
                    :class="[activeTab === 'danger' ? 'bg-red-600 text-white shadow-lg shadow-red-500/20' : 'text-red-500 hover:bg-red-50 dark:hover:bg-red-500/10']"
                    class="flex items-center gap-3 px-4 py-2.5 rounded-sm text-[10px] font-bold uppercase tracking-widest transition-all text-left">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" /></svg>
              Danger Zone
            </button>
          </nav>
        </div>

        <div class="flex-1 min-w-0">
          <div class="bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-sm shadow-sm overflow-hidden min-h-[500px] p-4 sm:p-8">

            <!-- Members, the invite form, and invitations not yet answered. -->
            <div v-if="activeTab === 'members'" class="space-y-8">
              <div v-if="welcome" data-test="share-hint" class="flex items-start gap-3 p-4 rounded-sm border border-gray-200 dark:border-zinc-700 bg-gray-50 dark:bg-zinc-800/40">
                <p class="flex-1 text-[12px] text-gray-700 dark:text-zinc-300 font-medium leading-relaxed">
                  <span class="font-bold text-gray-900 dark:text-white">Share your workspaces.</span>
                  Teammates see only what you share, read-only.
                </p>
                <button type="button" @click="selectTab('workspaces')" class="shrink-0 text-[10px] font-bold uppercase tracking-widest text-gray-900 dark:text-white underline">Choose</button>
              </div>

              <form v-if="manage" @submit.prevent="invite" data-test="invite-form" class="space-y-2">
                <label class="block text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest ml-1" for="invite-email">Invite</label>
                <div class="flex flex-col sm:flex-row gap-2">
                  <input id="invite-email" ref="emailRef" v-model="inviteEmail" type="email" required placeholder="name@company.com" autocomplete="off"
                         class="min-w-0 flex-1 bg-gray-50 dark:bg-zinc-800/50 border border-gray-200 dark:border-zinc-800 rounded-sm px-4 py-2.5 text-sm focus:border-gray-900 dark:focus:border-white focus:ring-0 outline-none font-medium text-gray-900 dark:text-zinc-100 shadow-sm" />
                  <select v-model="inviteRole" aria-label="Role" data-test="invite-role"
                          class="bg-gray-50 dark:bg-zinc-800/50 border border-gray-200 dark:border-zinc-800 rounded-sm px-3 py-2.5 text-sm text-gray-900 dark:text-zinc-100 outline-none capitalize">
                    <option v-for="r in roles" :key="r" :value="r">{{ r }}</option>
                  </select>
                  <button type="submit" :disabled="inviting"
                          class="bg-gray-900 dark:bg-white text-white dark:text-zinc-900 px-6 py-2.5 rounded-sm text-[10px] font-bold hover:bg-black dark:hover:bg-zinc-100 uppercase tracking-widest disabled:opacity-40">
                    {{ inviting ? 'Sending…' : 'Send' }}
                  </button>
                </div>
                <p v-if="inviteErr" data-test="invite-error" role="alert" class="text-[11px] font-semibold text-red-600 dark:text-red-400 ml-1">{{ inviteErr }}</p>
              </form>

              <section>
                <h3 class="text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest ml-1 mb-2">Members · {{ members.length }}</h3>
                <ul class="divide-y divide-gray-100 dark:divide-zinc-800">
                  <li v-for="m in members" :key="m.userId" data-test="member-row" class="py-3 flex items-center gap-3">
                    <OwnerAvatar :owner="m" size="md" />
                    <div class="flex-1 min-w-0">
                      <p class="text-sm font-bold text-gray-900 dark:text-zinc-100 truncate">{{ m.name || m.email }}<span v-if="isMe(m)" class="ml-1 text-[11px] font-medium text-gray-400">(you)</span></p>
                      <p v-if="m.name" class="text-[11px] text-gray-500 dark:text-zinc-400 truncate">{{ m.email }}</p>
                    </div>
                    <select v-if="canChange(m)" :value="m.role" @change="setRole(m, $event.target.value)" data-test="member-role" :aria-label="`Role of ${m.name || m.email}`"
                            class="bg-gray-50 dark:bg-zinc-800/50 border border-gray-200 dark:border-zinc-800 rounded-sm px-2 py-1 text-xs text-gray-900 dark:text-zinc-100 outline-none capitalize">
                      <option v-for="r in roles" :key="r" :value="r">{{ r }}</option>
                    </select>
                    <span v-else class="text-[11px] text-gray-500 dark:text-zinc-400 capitalize">{{ m.role }}</span>
                    <button v-if="canChange(m)" type="button" data-test="member-remove" @click="removing = m"
                            class="text-[10px] font-bold uppercase tracking-widest text-red-600 dark:text-red-400 hover:underline">Remove</button>
                  </li>
                </ul>
              </section>

              <section v-if="manage">
                <h3 class="text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest ml-1 mb-2">Pending invitations · {{ invitations.length }}</h3>
                <p v-if="invitations.length === 0" class="text-[11px] text-gray-500 dark:text-zinc-400 ml-1">None.</p>
                <ul v-else class="divide-y divide-gray-100 dark:divide-zinc-800">
                  <li v-for="i in invitations" :key="i.id" data-test="invitation-row" class="py-3 flex flex-col sm:flex-row sm:items-center gap-2">
                    <div class="flex-1 min-w-0">
                      <p class="text-sm font-bold text-gray-900 dark:text-zinc-100 truncate">{{ i.email }}</p>
                      <p class="text-[11px] text-gray-500 dark:text-zinc-400"><span class="capitalize">{{ i.role }}</span><template v-if="inviterName(i, members)"> · sent by {{ inviterName(i, members) }}</template> · expires {{ expiresIn(i.expiresAt) }}</p>
                    </div>
                    <div class="flex gap-3 shrink-0">
                      <button type="button" data-test="invitation-resend" @click="resend(i)" :disabled="inviting" class="text-[10px] font-bold uppercase tracking-widest text-gray-700 dark:text-zinc-200 hover:underline disabled:opacity-40">Resend</button>
                      <button type="button" data-test="invitation-revoke" @click="revoke(i)" class="text-[10px] font-bold uppercase tracking-widest text-red-600 dark:text-red-400 hover:underline">Revoke</button>
                    </div>
                  </li>
                </ul>
              </section>
            </div>

            <!-- Your workspaces, each with a switch for this team. -->
            <div v-else-if="activeTab === 'workspaces'" class="space-y-4">
              <p class="text-[11px] text-gray-500 dark:text-zinc-400 font-medium">Teammates in {{ team?.name }} see the workspaces you share here, and their tasks, read-only.</p>
              <WorkspaceSharing :team-id="teamId" />
            </div>

            <!-- The name: owners and admins. -->
            <form v-else-if="activeTab === 'general'" @submit.prevent="rename" class="space-y-4 max-w-md">
              <label for="team-rename" class="block text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest ml-1">Team name</label>
              <input id="team-rename" v-model="newName" type="text" required maxlength="100" :disabled="!manage"
                     class="w-full bg-gray-50 dark:bg-zinc-800/50 border border-gray-200 dark:border-zinc-800 rounded-sm px-4 py-3 text-sm focus:border-gray-900 dark:focus:border-white focus:ring-0 outline-none font-bold text-gray-900 dark:text-zinc-100 shadow-sm disabled:opacity-60" />
              <div v-if="manage" class="flex justify-end">
                <button type="submit" :disabled="!newName.trim() || newName.trim() === team?.name"
                        class="bg-gray-900 dark:bg-white text-white dark:text-zinc-900 px-8 py-2.5 rounded-sm text-[10px] font-bold uppercase tracking-widest disabled:opacity-40">Rename</button>
              </div>
            </form>

            <!-- Leaving, for anyone; deleting, for its owner. -->
            <div v-else class="space-y-4">
              <div class="p-6 border border-red-100 dark:border-red-900/30 bg-red-50/30 dark:bg-red-500/5 rounded-sm flex flex-col md:flex-row md:items-center justify-between gap-6 shadow-sm">
                <div class="flex-1">
                  <h4 class="text-sm font-bold text-gray-900 dark:text-zinc-100">Leave team</h4>
                  <p class="text-[11px] text-gray-600 dark:text-zinc-400 mt-1 font-medium">Your workspaces shared with {{ team?.name }} stop being shared. The last owner hands over ownership or deletes the team instead.</p>
                </div>
                <button type="button" data-test="leave" @click="confirming = 'leave'"
                        class="px-6 py-2.5 bg-white dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700 text-[10px] font-bold text-gray-900 dark:text-zinc-100 hover:border-black dark:hover:border-white shadow-sm rounded-sm uppercase tracking-widest whitespace-nowrap">Leave team</button>
              </div>
              <div v-if="team?.role === 'owner'" class="p-6 border border-red-200 dark:border-red-900/50 bg-red-50 dark:bg-red-900/10 rounded-sm flex flex-col md:flex-row md:items-center justify-between gap-6 shadow-sm">
                <div class="flex-1">
                  <h4 class="text-sm font-bold text-red-600 dark:text-red-500">Delete team</h4>
                  <p class="text-[11px] text-gray-600 dark:text-zinc-400 mt-1 font-medium">Removes the team, its members and every share to it. Nobody's workspaces or tasks are deleted.</p>
                </div>
                <button type="button" data-test="delete" @click="confirming = 'delete'"
                        class="px-8 py-2.5 bg-red-600 text-white border border-red-700 text-[10px] font-bold hover:bg-red-700 shadow-lg shadow-red-600/20 rounded-sm uppercase tracking-widest whitespace-nowrap">Delete team</button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <DeleteModal :show="!!removing" title="Remove member" confirm-label="Remove"
                 :message="removing ? `Remove ${removing.name || removing.email} from ${team?.name}? Their shared workspaces leave ${team?.name} too.` : ''"
                 @close="removing = null" @confirm="removeMember" />
    <DeleteModal :show="confirming === 'leave'" title="Leave team" confirm-label="Leave"
                 :message="`Leave ${team?.name}? Your shares to it are removed.`"
                 @close="confirming = ''" @confirm="leave" />
    <DeleteModal :show="confirming === 'delete'" title="Delete team" confirm-label="Delete team" :confirm-text="team?.name || ''"
                 :message="`Delete ${team?.name} for everyone in it? This cannot be undone.`"
                 @close="confirming = ''" @confirm="destroy" />
    <InviteLinkModal :link="inviteLink.link" :email="inviteLink.email" @close="inviteLink = { link: '', email: '' }" />
  </div>
</template>

<script setup>
import { computed, nextTick, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import {
  createTeamInvitation, deleteTeam, fetchUser, getTeam, leaveTeam as apiLeaveTeam, listTeamInvitations,
  listTeamMembers, removeTeamMember, renameTeam, revokeTeamInvitation, setTeamMemberRole,
} from '../api';
import { useTeamStore, rememberTeam } from '../stores/teamStore';
import { useToasts } from '../composables/useToasts';
import { assignableRoles, canManage, expiresIn, inviteError, inviterName, lostTeam, teamOf, useTeamMode } from '../composables/useTeamMode';
import DeleteModal from '../components/DeleteModal.vue';
import InviteLinkModal from '../components/InviteLinkModal.vue';
import OwnerAvatar from '../components/OwnerAvatar.vue';
import WorkspaceSharing from '../components/WorkspaceSharing.vue';

const route = useRoute();
const router = useRouter();
const store = useTeamStore();
const { notifySuccess, notifyError } = useToasts();
const { teamId, team, leaveTeam: lose } = useTeamMode();

const tabs = [
  { id: 'members', label: 'Members', icon: 'M17 20h5v-2a3 3 0 00-5.356-1.857M17 20H7m10 0v-2c0-.656-.126-1.283-.356-1.857M7 20H2v-2a3 3 0 015.356-1.857M7 20v-2c0-.656.126-1.283.356-1.857m0 0a5.002 5.002 0 019.288 0M15 7a3 3 0 11-6 0 3 3 0 016 0z' },
  { id: 'workspaces', label: 'Your workspaces', icon: 'M4 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2V6zM14 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2zM14 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z' },
  { id: 'general', label: 'General', icon: 'M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z' },
];
const TAB_IDS = [...tabs.map((t) => t.id), 'danger'];

const activeTab = ref(TAB_IDS.includes(route.query.tab) ? route.query.tab : 'members');
const welcome = ref(route.query.welcome === '1');
function selectTab(id) {
  activeTab.value = id;
  router.replace({ path: route.path, query: { team: teamId.value, tab: id } });
}

const me = ref(null);
const members = ref([]);
const invitations = ref([]);
const manage = computed(() => canManage(team.value?.role));
const roles = computed(() => assignableRoles(team.value?.role));
const isMe = (m) => String(m.userId) === String(me.value?.id);
// Yourself never, and an owner only by an owner.
const canChange = (m) => manage.value && !isMe(m) && (m.role !== 'owner' || team.value?.role === 'owner');
const article = (role) => (role === 'owner' || role === 'admin' ? 'an' : 'a');

const inviteEmail = ref('');
const inviteRole = ref('member');
const inviting = ref(false);
const inviteErr = ref('');
const inviteLink = ref({ link: '', email: '' });
const emailRef = ref(null);

const newName = ref('');
watch(() => team.value?.name, (n) => { newName.value = n || ''; }, { immediate: true });

const removing = ref(null);
const confirming = ref('');

// Any refusal saying the team is not yours to see any more sends you back to
// Me; the rest are shown where they happened.
function failed(err) {
  if (lostTeam(err)) return lose();
  notifyError(err.message);
}

async function load() {
  try {
    me.value = await fetchUser();
    store.upsert(teamOf(await getTeam(teamId.value)));
    members.value = (await listTeamMembers(teamId.value))?.members || [];
    invitations.value = manage.value ? (await listTeamInvitations(teamId.value))?.invitations || [] : [];
  } catch (err) {
    failed(err);
  }
}

async function send(email, role) {
  inviting.value = true;
  inviteErr.value = '';
  try {
    const res = await createTeamInvitation(teamId.value, email, role);
    // Always the same words, whether or not that address has an account.
    notifySuccess('Invitation sent');
    if (res?.invitation) invitations.value = [res.invitation, ...invitations.value.filter((i) => i.email !== email)];
    if (!res?.emailSent && res?.link) inviteLink.value = { link: res.link, email };
    return true;
  } catch (err) {
    inviteErr.value = inviteError(err);
    return false;
  } finally {
    inviting.value = false;
  }
}

async function invite() {
  if (await send(inviteEmail.value.trim(), inviteRole.value)) inviteEmail.value = '';
}

// A new invitation in place of the old one: one pending per address.
async function resend(i) {
  try {
    await revokeTeamInvitation(teamId.value, i.id);
  } catch (err) {
    return failed(err);
  }
  invitations.value = invitations.value.filter((x) => x.id !== i.id);
  await send(i.email, i.role);
}

async function revoke(i) {
  try {
    await revokeTeamInvitation(teamId.value, i.id);
    invitations.value = invitations.value.filter((x) => x.id !== i.id);
    notifySuccess(`Invitation to ${i.email} revoked`);
  } catch (err) {
    failed(err);
  }
}

async function setRole(m, role) {
  try {
    await setTeamMemberRole(teamId.value, m.userId, role);
    members.value = members.value.map((x) => (x.userId === m.userId ? { ...x, role } : x));
    notifySuccess(`${m.name || m.email} is now ${article(role)} ${role}`);
  } catch (err) {
    // The select shows what was chosen; put it back to the truth.
    members.value = members.value.map((x) => ({ ...x }));
    failed(err);
  }
}

async function removeMember() {
  const m = removing.value;
  removing.value = null;
  try {
    await removeTeamMember(teamId.value, m.userId);
    members.value = members.value.filter((x) => x.userId !== m.userId);
    notifySuccess(`${m.name || m.email} removed`);
  } catch (err) {
    failed(err);
  }
}

async function rename() {
  try {
    store.upsert(teamOf(await renameTeam(teamId.value, newName.value.trim())));
    notifySuccess('Team renamed');
  } catch (err) {
    failed(err);
  }
}

// Leaving or deleting: the team goes from the switch, and you go back to Me.
async function goodbye(work, message) {
  confirming.value = '';
  try {
    await work();
  } catch (err) {
    return failed(err);
  }
  store.drop(teamId.value);
  rememberTeam('');
  notifySuccess(message);
  return router.push('/');
}

const leave = () => goodbye(() => apiLeaveTeam(teamId.value), `You left ${team.value?.name}`);
const destroy = () => goodbye(() => deleteTeam(teamId.value), `${team.value?.name} deleted`);

onMounted(async () => {
  await load();
  if (welcome.value && manage.value) nextTick(() => emailRef.value?.focus());
});
</script>
