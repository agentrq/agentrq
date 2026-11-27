<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<!-- The account's skills: on the Skills page, or in a workspace's settings
     with that workspace's switch on each. The skills themselves, never their
     other files, which are listed on each skill's own page. -->
<template>
  <div class="space-y-6 animate-in fade-in slide-in-from-bottom-2 duration-300">
    <!-- The Skills page says this in its own header. -->
    <div v-if="workspaceId" class="space-y-1">
      <h3 class="text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest ml-1">Workspace Skills</h3>
      <p class="text-[11px] text-gray-500 dark:text-zinc-400 font-medium ml-1">
        Playbooks agents load when a task matches one: a
        <code class="bg-gray-100 dark:bg-zinc-800 px-1 py-0.5 rounded text-gray-900 dark:text-white">{{ SKILL_FILE }}</code>
        and the files it points to. Agents find them with
        <code class="bg-gray-100 dark:bg-zinc-800 px-1 py-0.5 rounded text-gray-900 dark:text-white">searchSkills</code>
        and read them with
        <code class="bg-gray-100 dark:bg-zinc-800 px-1 py-0.5 rounded text-gray-900 dark:text-white">loadSkill</code>.
        Skills belong to your account: turn one on here for this workspace's agents to see it.
      </p>
    </div>

    <!-- Import from GitHub or GitLab -->
    <!-- Boxed from sm up; on a phone a box in a box leaves too little room. -->
    <form class="sm:p-4 sm:bg-gray-50 sm:dark:bg-zinc-800/50 sm:rounded-sm sm:border border-gray-100 dark:border-zinc-800 space-y-3" @submit.prevent="runImport">
      <label for="skill-import-url" class="block text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Import from GitHub or GitLab</label>
      <div class="flex flex-col sm:flex-row gap-2">
        <input id="skill-import-url" v-model="importUrl" type="url" autocomplete="off" spellcheck="false"
               placeholder="https://github.com/obra/superpowers"
               class="min-w-0 flex-1 px-3 py-2 text-xs font-mono bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700 rounded-lg text-gray-900 dark:text-zinc-100 placeholder-gray-300 dark:placeholder-zinc-600 focus:outline-none focus:border-gray-400 dark:focus:border-zinc-500" />
        <button type="submit" data-test="skill-import" :disabled="importing || !importUrl.trim()"
                class="shrink-0 px-4 py-2 bg-black dark:bg-white text-white dark:text-black rounded-lg text-[11px] font-black uppercase tracking-widest disabled:opacity-40">
          {{ importing && !choice ? 'Discovering…' : 'Discover' }}
        </button>
      </div>
      <p v-if="importUrl.trim() && !importUrlValid(importUrl, gitlabHost)" class="text-[10px] text-amber-600 dark:text-amber-400">
        Expected https://github.com/owner/repo or https://{{ gitlabHost }}/group/project, optionally …/tree/&lt;ref&gt;/&lt;path&gt;
      </p>
      <GitHubConnection />
      <GitLabConnection />
      <!-- Where the imported skills are turned on: chosen on the Skills page;
           in a workspace, that workspace alone. -->
      <SkillWorkspaceChoices v-if="!workspaceId" v-model="importWorkspaceIds" :workspaces="workspaceChoices"
                             data-test="skill-import-workspaces" item-test="skill-import-workspace" />
      <p v-if="importError" class="text-[11px] font-bold text-red-600 dark:text-red-400 break-words">{{ importError }}</p>
      <!-- The repository's skills, to switch off those not to import. -->
      <SkillImportChoice v-if="choice" v-model="chosen" :choice="choice" :importing="importing"
                         @import="importChosen" @cancel="choice = null">
        <template #options>
          <label class="flex items-center gap-2 text-[11px] text-gray-600 dark:text-zinc-300">
            <input v-model="importOverwrite" data-test="skill-import-overwrite" type="checkbox" class="rounded-sm border-gray-300 dark:border-zinc-600" />
            Overwrite skills you already have
          </label>
        </template>
      </SkillImportChoice>
      <div v-if="importReport" data-test="skill-import-report" class="space-y-2 text-[11px] text-gray-700 dark:text-zinc-300">
        <p class="font-bold">
          Imported {{ importReport.imported.length }} {{ importReport.imported.length === 1 ? 'skill' : 'skills' }}
          from {{ importReport.sourceRepo }}<span v-if="importReport.sourceCommit">@{{ importReport.sourceCommit.slice(0, 7) }}</span>.
        </p>
        <div v-if="importReport.skipped.length">
          <p class="text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Skipped ({{ importReport.skipped.length }})</p>
          <ul class="mt-1 space-y-1">
            <li v-for="(s, i) in importReport.skipped" :key="i" class="break-words">
              <span class="font-mono font-bold">{{ s.path || s.name }}</span> — {{ s.reason }}
            </li>
          </ul>
        </div>
      </div>
    </form>

    <!-- The team workspaces' own skills, once the workspace is shared into one. -->
    <WorkspaceTeamSkills v-if="workspaceId && teamWorkspaces.length" :workspace-id="workspaceId" :skills="teamSkills"
                         @update="updateTeamSkill" @changed="reloadAll" />
    <h3 v-if="workspaceId && teamWorkspaces.length" class="text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest ml-1">Your skills</h3>

    <p v-if="view === SkillsState.Loading" class="text-[11px] text-gray-400 dark:text-zinc-500 ml-1">Loading skills…</p>

    <div v-else-if="view === SkillsState.Failed" class="p-4 bg-red-50 dark:bg-red-500/10 border border-red-100 dark:border-red-500/20 rounded-sm">
      <p class="text-[11px] font-bold text-red-600 dark:text-red-400">{{ workspaceId ? "Could not load this workspace's skills." : 'Could not load your skills.' }}</p>
      <button type="button" @click="loadSkills()" class="mt-2 text-[10px] font-black uppercase tracking-widest text-red-600 dark:text-red-400 hover:underline">Try again</button>
    </div>

    <div v-else-if="view === SkillsState.Empty" class="p-6 bg-gray-50 dark:bg-zinc-800/50 rounded-sm border border-gray-100 dark:border-zinc-800 text-center">
      <p class="text-[11px] font-bold text-gray-700 dark:text-zinc-200">No skills yet.</p>
      <p class="mt-1 text-[11px] text-gray-500 dark:text-zinc-400">
        Import some from GitHub or GitLab above, or let an agent write one with <code>saveSkill</code>.
      </p>
    </div>

    <!-- Compact cards, three to a row where there is room. A card opens the
         skill's own page, which reads it and holds the workspaces it is on in
         and deleting. The switch sits beside the link, not in it, so a toggle
         never navigates. In a workspace it is that workspace's switch. -->
    <div v-else data-test="skill-grid" class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 sm:gap-2 divide-y sm:divide-y-0 divide-gray-100 dark:divide-zinc-800 border-y sm:border-y-0 border-gray-100 dark:border-zinc-800">
      <SkillCard v-for="s in orderedSkills" :key="s.name" :to="skillPagePath(workspaceId, s.name)"
                 :name="s.name" :description="s.description" :muted="!skillSeenByAgents(s, workspaceId)"
                 :enabled="skillSwitchOn(s, workspaceId)" :action-width="canGive ? 'pr-28' : 'pr-12'">
        <template #meta>
          <span class="shrink-0">{{ formatSkillSize(s.totalBytes) }}</span>
          <!-- In a workspace the switch is its own, so the account-wide one
               being off, which hides it all the same, is said here. -->
          <span v-if="s.enabled === false" data-test="skill-card-off"
                :title="workspaceId ? 'Turned off for the whole account, so no agent sees it' : 'Hidden from agents in every workspace'"
                class="shrink-0 text-[8px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400 border border-gray-300 dark:border-zinc-600 rounded px-1 py-px">
            {{ workspaceId ? 'Off for all workspaces' : 'Off' }}
          </span>
          <span v-if="s.locallyModified"
                class="shrink-0 text-[8px] font-black uppercase tracking-widest text-amber-600 dark:text-amber-300 border border-amber-200 dark:border-amber-500/40 rounded px-1 py-px">
            Modified
          </span>
          <span v-if="!workspaceId" data-test="skill-card-workspaces" class="shrink-0">· {{ skillWorkspaceCount(s) }}</span>
          <span v-if="!s.locallyModified" class="min-w-0 truncate" :title="skillSource(s)">· {{ skillSource(s) }}</span>
        </template>
        <template #action>
          <span class="flex items-center gap-2">
          <!-- Moves it into a team workspace this one is shared into. -->
          <button v-if="canGive" type="button" data-test="skill-card-give" :aria-label="`Give ${s.name} to the team`"
                  @click="startGive(s)"
                  class="block px-2 py-1 rounded-lg border border-gray-300 dark:border-zinc-600 text-[8px] font-black uppercase tracking-widest text-gray-600 dark:text-zinc-300 hover:bg-gray-100 dark:hover:bg-zinc-800">
            Give to team
          </button>
          <button type="button" role="switch" data-test="skill-card-enabled"
                  :aria-checked="skillSwitchOn(s, workspaceId)" :aria-label="`${workspaceId ? 'On in this workspace' : 'Available to agents'}: ${s.name}`"
                  :title="switchTitle(s)"
                  :disabled="pendingEnabled.has(s.name)" @click="toggleEnabled(s)"
                  class="relative block w-11 h-6 rounded-full border transition-colors disabled:opacity-50"
                  :class="skillSwitchOn(s, workspaceId) ? 'bg-gray-900 dark:bg-white border-gray-900 dark:border-white' : 'bg-gray-200 dark:bg-zinc-700 border-gray-300 dark:border-zinc-600'">
            <span class="absolute top-[3px] w-4 h-4 rounded-full transition-all"
                  :class="skillSwitchOn(s, workspaceId) ? 'left-[23px] bg-white dark:bg-zinc-900' : 'left-[3px] bg-white dark:bg-zinc-400'"></span>
          </button>
          </span>
        </template>
      </SkillCard>
    </div>

    <GiveSkillModal :show="!!giving" :name="giving?.name || ''" :team-workspaces="teamWorkspaces"
                    :busy="givingBusy" :error="giveError" @close="giving = null" @confirm="give" />
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue';

import {
  giveSkillToTeam,
  importSkills,
  listWorkspaceTeamSkills,
  searchSkills,
  searchWorkspaceSkills,
  setSkillEnabled,
  setWorkspaceSkillEnabled,
} from '../api';
import {
  SKILL_FILE,
  SkillsState,
  choosablePaths,
  chosenImportPaths,
  contentWorkspaceId,
  formatSkillSize,
  importUrlValid,
  mergeSkill,
  orderCandidates,
  orderSkills,
  skillEnabledMessage,
  skillPagePath,
  skillSeenByAgents,
  skillSource,
  skillSwitchOn,
  skillWorkspaceChoices,
  skillWorkspaceCount,
  skillWorkspaceMessage,
  skillsState,
} from '../composables/useSkills';
import { useToasts } from '../composables/useToasts';
import { onWebMCPChange } from '../composables/useWebMCPChanges';
import { useWorkspaceStore } from '../stores/workspaceStore';
import { useEventBus } from '../useEventBus';
import { useGitLabHost } from '../composables/useGitLabHost';
import GitHubConnection from './GitHubConnection.vue';
import GitLabConnection from './GitLabConnection.vue';
import GiveSkillModal from './GiveSkillModal.vue';
import SkillCard from './SkillCard.vue';
import SkillImportChoice from './SkillImportChoice.vue';
import SkillWorkspaceChoices from './SkillWorkspaceChoices.vue';
import WorkspaceTeamSkills from './WorkspaceTeamSkills.vue';

// With no workspace, the account's view: the Skills page.
const props = defineProps({ workspaceId: { type: String, default: '' } });

const workspaceStore = useWorkspaceStore();
const { notifySuccess, notifyError } = useToasts();

const skills = ref([]);
const loading = ref(false);
const error = ref(null);
const view = computed(() => skillsState({ loading: loading.value, error: error.value, skills: skills.value }));
const orderedSkills = computed(() => orderSkills(skills.value));

// A quiet reload, after an agent's change, keeps the list on screen.
// A workspace switch or a browser agent's change reloads while a load is out;
// only the newest may write, or the previous workspace's list replaces it.
let loadSeq = 0;

async function loadSkills({ quiet = false } = {}) {
  const mine = ++loadSeq;
  if (!quiet) loading.value = true;
  error.value = null;
  try {
    const res = props.workspaceId ? await searchWorkspaceSkills(props.workspaceId) : await searchSkills();
    if (mine === loadSeq) skills.value = res.skills || [];
  } catch (err) {
    if (mine === loadSeq) error.value = err;
  } finally {
    if (mine === loadSeq) loading.value = false;
  }
}

// ── Team skills ─────────────────────────────────────────────────────────────
// In a workspace, the team skills it reaches and the team workspaces it is
// shared into. None until read, and none when they cannot be: the group then
// stays hidden, and your own skills are drawn as ever.
const teamSkills = ref([]);
const teamWorkspaces = ref([]);
let teamSeq = 0;

async function loadTeamSkills() {
  const mine = ++teamSeq;
  if (!props.workspaceId) {
    teamSkills.value = [];
    teamWorkspaces.value = [];
    return;
  }
  try {
    const res = await listWorkspaceTeamSkills(props.workspaceId);
    if (mine !== teamSeq) return;
    teamSkills.value = res?.skills || [];
    teamWorkspaces.value = res?.teamWorkspaces || [];
  } catch {
    if (mine !== teamSeq) return;
    teamSkills.value = [];
    teamWorkspaces.value = [];
  }
}

function updateTeamSkill(skill) {
  teamSkills.value = teamSkills.value.map((x) => (x.id === skill.id ? skill : x));
}

function reloadAll() {
  return Promise.all([loadSkills({ quiet: true }), loadTeamSkills()]);
}

// "Give to team": the own skill being given, while the confirm is open.
const canGive = computed(() => !!props.workspaceId && teamWorkspaces.value.length > 0);
const giving = ref(null);
const givingBusy = ref(false);
const giveError = ref('');

function startGive(s) {
  giveError.value = '';
  giving.value = s;
}

async function give(teamWorkspaceId) {
  const s = giving.value;
  if (!s) return;
  givingBusy.value = true;
  giveError.value = '';
  try {
    await giveSkillToTeam(props.workspaceId, s.name, teamWorkspaceId);
    const to = teamWorkspaces.value.find((t) => t.id === teamWorkspaceId);
    notifySuccess(`Gave ${s.name} to ${to?.name || 'the team'}`);
    giving.value = null;
    await reloadAll();
  } catch (err) {
    giveError.value = err.message;
  } finally {
    givingBusy.value = false;
  }
}

// A teammate's change to a team skill, or another tab's, reads both again:
// a skill given away leaves your list as it joins the team's.
const { connect, disconnect, onEvent } = useEventBus(undefined, { buffer: false });
const TEAM_SKILL_EVENT = /^team\.skill\./;

// ── Turning a skill on or off ───────────────────────────────────────────────
// Skills whose switch was just flipped and whose answer is still out.
const pendingEnabled = ref(new Set());

function switchTitle(s) {
  const on = skillSwitchOn(s, props.workspaceId);
  if (props.workspaceId) return on ? "On: this workspace's agents can find and load it" : "Off: this workspace's agents do not see it";
  return on ? 'On: agents can find and load it where it is on' : 'Off: hidden from agents in every workspace';
}

async function toggleEnabled(s) {
  const on = !skillSwitchOn(s, props.workspaceId);
  pendingEnabled.value = new Set(pendingEnabled.value).add(s.name);
  try {
    const res = props.workspaceId
      ? await setWorkspaceSkillEnabled(props.workspaceId, s.name, on)
      : await setSkillEnabled(s.name, on);
    skills.value = skills.value.map((x) => (x.name === s.name ? mergeSkill(x, res.skill, props.workspaceId) : x));
    notifySuccess(props.workspaceId ? skillWorkspaceMessage(s.name, on) : skillEnabledMessage(s.name, on));
  } catch (err) {
    notifyError(err.message);
  } finally {
    const next = new Set(pendingEnabled.value);
    next.delete(s.name);
    pendingEnabled.value = next;
  }
}

// ── Importing ───────────────────────────────────────────────────────────────
const importUrl = ref('');
const importOverwrite = ref(false);
const importing = ref(false);
const importReport = ref(null);
const importError = ref('');
// An import lists the repository's skills first: the link they came from,
// and which of them are switched on — all that can be, unless the repository
// is too large to import whole.
const choice = ref(null);
const chosen = ref([]);

// The workspaces to turn what is imported on in: ticked on the Skills page; in
// a workspace's tab, that workspace — a fork's parent, whose skills the fork uses.
const workspaceChoices = computed(() => skillWorkspaceChoices(workspaceStore.workspaces));
const importWorkspaceIds = ref([]);
// The GitLab a link may name besides GitHub, for the hint.
const gitlabHost = useGitLabHost();

function importTargets() {
  if (!props.workspaceId) return [...importWorkspaceIds.value];
  return [contentWorkspaceId(workspaceStore.getWorkspace(props.workspaceId), props.workspaceId)];
}

async function doImport(url, skills, list = false) {
  importing.value = true;
  importError.value = '';
  importReport.value = null;
  try {
    const res = await importSkills(url, importOverwrite.value, skills, importTargets(), list);
    if (res.candidates?.length) {
      const candidates = orderCandidates(res.candidates);
      choice.value = { url, repo: res.sourceRepo, candidates, tooLarge: !!res.tooLarge };
      chosen.value = res.tooLarge ? [] : choosablePaths(candidates);
      return;
    }
    choice.value = null;
    importReport.value = res;
    await loadSkills();
  } catch (err) {
    importError.value = err.message;
  } finally {
    importing.value = false;
  }
}

function runImport() {
  choice.value = null;
  return doImport(importUrl.value.trim(), undefined, true);
}

function importChosen() {
  return doImport(choice.value.url, chosenImportPaths(choice.value, chosen.value));
}

onMounted(() => {
  loadSkills();
  if (!workspaceStore.workspaces.length) workspaceStore.fetchWorkspaces();
  if (props.workspaceId) {
    loadTeamSkills();
    onEvent((event) => {
      if (TEAM_SKILL_EVENT.test(event?.type || '')) reloadAll();
    });
    connect();
  }
});
onUnmounted(() => disconnect());
onWebMCPChange(() => reloadAll());
watch(() => props.workspaceId, () => {
  loadSkills();
  loadTeamSkills();
});
</script>
