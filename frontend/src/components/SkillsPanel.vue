<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
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
        <RouterLink to="/skills" data-test="skills-page-link" class="underline hover:text-gray-800 dark:hover:text-zinc-100">All skills</RouterLink>
      </p>
    </div>

    <!-- Import from GitHub -->
    <!-- Boxed from sm up; on a phone a box in a box leaves too little room. -->
    <form class="sm:p-4 sm:bg-gray-50 sm:dark:bg-zinc-800/50 sm:rounded-sm sm:border border-gray-100 dark:border-zinc-800 space-y-3" @submit.prevent="runImport">
      <label for="skill-import-url" class="block text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Import from GitHub</label>
      <div class="flex flex-col sm:flex-row gap-2">
        <input id="skill-import-url" v-model="importUrl" type="url" autocomplete="off" spellcheck="false"
               placeholder="https://github.com/obra/superpowers"
               class="min-w-0 flex-1 px-3 py-2 text-xs font-mono bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700 rounded-sm text-gray-900 dark:text-zinc-100 placeholder-gray-300 dark:placeholder-zinc-600 focus:outline-none focus:border-gray-400 dark:focus:border-zinc-500" />
        <button type="submit" data-test="skill-import" :disabled="importing || !importUrl.trim()"
                class="shrink-0 px-4 py-2 bg-black dark:bg-white text-white dark:text-black rounded-lg text-[11px] font-black uppercase tracking-widest disabled:opacity-40">
          {{ importing ? 'Importing…' : 'Import' }}
        </button>
      </div>
      <div class="flex flex-wrap items-center justify-between gap-2">
        <label class="flex items-center gap-2 text-[11px] text-gray-600 dark:text-zinc-300">
          <input v-model="importOverwrite" type="checkbox" class="rounded-sm border-gray-300 dark:border-zinc-600" />
          Overwrite skills you already have
        </label>
        <span v-if="importUrl.trim() && !githubImportUrlValid(importUrl)" class="text-[10px] text-amber-600 dark:text-amber-400">
          Expected https://github.com/owner/repo, optionally /tree/&lt;ref&gt;/&lt;path&gt;
        </span>
      </div>
      <!-- Where the imported skills are turned on. In a workspace, that one
           starts ticked, so what is imported there is on there. -->
      <fieldset v-if="workspaceChoices.length" data-test="skill-import-workspaces" class="min-w-0">
        <legend class="mb-1.5 text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Turn on in</legend>
        <div class="flex flex-wrap gap-1.5">
          <label v-for="w in workspaceChoices" :key="w.id" data-test="skill-import-workspace"
                 :class="importWorkspaceIds.includes(String(w.id)) ? 'border-gray-900 dark:border-zinc-100 text-gray-900 dark:text-zinc-100' : 'border-gray-200 dark:border-zinc-700 text-gray-500 dark:text-zinc-400 hover:border-gray-300 dark:hover:border-zinc-600'"
                 class="inline-flex items-center gap-1.5 max-w-full min-w-0 px-2 py-1 border rounded-sm text-[11px] cursor-pointer transition-colors">
            <input v-model="importWorkspaceIds" type="checkbox" :value="String(w.id)" class="shrink-0 rounded-sm border-gray-300 dark:border-zinc-600" />
            <span class="min-w-0 truncate">{{ w.name }}</span>
          </label>
        </div>
      </fieldset>
      <p v-if="importError" class="text-[11px] font-bold text-red-600 dark:text-red-400 break-words">{{ importError }}</p>
      <!-- A repository too large to import whole: choose what to take. -->
      <div v-if="choice" data-test="skill-import-choice" class="space-y-2 text-[11px] text-gray-700 dark:text-zinc-300">
        <p class="font-bold break-words">
          {{ choice.repo }} is too large to import whole. Choose the skills to import:
        </p>
        <div class="flex flex-wrap items-center gap-3">
          <button type="button" data-test="skill-choice-all" @click="chosen = choosablePaths(choice.candidates)"
                  class="text-[9px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400 hover:text-gray-800 dark:hover:text-zinc-100">Select all</button>
          <button type="button" data-test="skill-choice-none" @click="chosen = []"
                  class="text-[9px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400 hover:text-gray-800 dark:hover:text-zinc-100">Select none</button>
          <span class="text-[10px] text-gray-400 dark:text-zinc-500">{{ chosen.length }} of {{ choosablePaths(choice.candidates).length }} selected</span>
        </div>
        <ul class="max-h-72 overflow-y-auto space-y-1 bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700 rounded-sm p-2">
          <li v-for="c in choice.candidates" :key="c.path" data-test="skill-candidate">
            <label class="flex items-start gap-2 min-w-0" :class="c.reason ? 'opacity-50' : 'cursor-pointer'">
              <input v-model="chosen" type="checkbox" :value="c.path" :disabled="!!c.reason" class="mt-0.5 shrink-0 rounded-sm border-gray-300 dark:border-zinc-600" />
              <span class="min-w-0 flex-1">
                <span class="flex flex-wrap items-baseline gap-x-2">
                  <span data-test="skill-candidate-name" class="font-mono font-bold break-all">{{ c.name }}</span>
                  <span class="text-[10px] text-gray-400 dark:text-zinc-500 tabular-nums">{{ formatSkillSize(c.sizeBytes) }}</span>
                  <span v-if="c.path && c.path !== c.name" class="text-[10px] font-mono text-gray-400 dark:text-zinc-500 break-all">{{ c.path }}</span>
                </span>
                <span v-if="c.reason" class="block break-words text-amber-600 dark:text-amber-400">{{ c.reason }}</span>
              </span>
            </label>
          </li>
        </ul>
        <div class="flex items-center gap-2">
          <button type="button" data-test="skill-import-chosen" :disabled="importing || !chosen.length" @click="importChosen"
                  class="px-4 py-2 bg-black dark:bg-white text-white dark:text-black rounded-lg text-[11px] font-black uppercase tracking-widest disabled:opacity-40">
            {{ importing ? 'Importing…' : `Import ${chosen.length} selected` }}
          </button>
          <button type="button" @click="choice = null"
                  class="px-3 py-2 text-[10px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400 hover:text-gray-800 dark:hover:text-zinc-100">Cancel</button>
        </div>
      </div>
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

    <p v-if="view === SkillsState.Loading" class="text-[11px] text-gray-400 dark:text-zinc-500 ml-1">Loading skills…</p>

    <div v-else-if="view === SkillsState.Failed" class="p-4 bg-red-50 dark:bg-red-500/10 border border-red-100 dark:border-red-500/20 rounded-sm">
      <p class="text-[11px] font-bold text-red-600 dark:text-red-400">{{ workspaceId ? "Could not load this workspace's skills." : 'Could not load your skills.' }}</p>
      <button type="button" @click="loadSkills()" class="mt-2 text-[10px] font-black uppercase tracking-widest text-red-600 dark:text-red-400 hover:underline">Try again</button>
    </div>

    <div v-else-if="view === SkillsState.Empty" class="p-6 bg-gray-50 dark:bg-zinc-800/50 rounded-sm border border-gray-100 dark:border-zinc-800 text-center">
      <p class="text-[11px] font-bold text-gray-700 dark:text-zinc-200">No skills yet.</p>
      <p class="mt-1 text-[11px] text-gray-500 dark:text-zinc-400">
        Import some from GitHub above, or let an agent write one with <code>saveSkill</code>.
      </p>
    </div>

    <!-- Compact cards, three to a row where there is room. A card opens the
         skill's own page, which reads it and holds the workspaces it is on in
         and deleting. The switch sits beside the link, not in it, so a toggle
         never navigates. In a workspace it is that workspace's switch. -->
    <div v-else data-test="skill-grid" class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 sm:gap-2 divide-y sm:divide-y-0 divide-gray-100 dark:divide-zinc-800 border-y sm:border-y-0 border-gray-100 dark:border-zinc-800">
      <div v-for="s in orderedSkills" :key="s.name" class="relative min-w-0">
      <RouterLink :to="skillPagePath(workspaceId, s.name)" data-test="skill-card" :data-enabled="skillSwitchOn(s, workspaceId)"
                  class="group h-full flex flex-col gap-1.5 min-w-0 py-3 sm:p-3 sm:bg-gray-50 sm:dark:bg-zinc-800/50 sm:rounded-sm sm:border border-gray-100 dark:border-zinc-800 hover:border-gray-300 dark:hover:border-zinc-600 hover:bg-gray-100 dark:hover:bg-zinc-800 transition-colors">
        <!-- Dimmed when no agent sees it, here or anywhere. -->
        <span data-test="skill-card-name" class="truncate pr-12 text-xs leading-6 font-bold font-mono"
              :class="skillSeenByAgents(s, workspaceId) ? 'text-gray-800 dark:text-zinc-100' : 'text-gray-400 dark:text-zinc-500'">{{ s.name }}</span>
        <span class="line-clamp-2 min-h-[2lh] text-[11px] leading-snug break-words"
              :class="skillSeenByAgents(s, workspaceId) ? 'text-gray-500 dark:text-zinc-400' : 'text-gray-400 dark:text-zinc-500'">{{ s.description }}</span>
        <span class="mt-auto flex items-center gap-1.5 min-w-0 text-[10px] text-gray-400 dark:text-zinc-500 tabular-nums">
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
        </span>
      </RouterLink>
      <button type="button" role="switch" data-test="skill-card-enabled"
              :aria-checked="skillSwitchOn(s, workspaceId)" :aria-label="`${workspaceId ? 'On in this workspace' : 'Available to agents'}: ${s.name}`"
              :title="switchTitle(s)"
              :disabled="pendingEnabled.has(s.name)" @click="toggleEnabled(s)"
              class="absolute top-3 right-0 sm:right-3 shrink-0 w-11 h-6 rounded-full border transition-colors disabled:opacity-50"
              :class="skillSwitchOn(s, workspaceId) ? 'bg-gray-900 dark:bg-white border-gray-900 dark:border-white' : 'bg-gray-200 dark:bg-zinc-700 border-gray-300 dark:border-zinc-600'">
        <span class="absolute top-0.5 w-4 h-4 rounded-full transition-all"
              :class="skillSwitchOn(s, workspaceId) ? 'left-[22px] bg-white dark:bg-zinc-900' : 'left-0.5 bg-white dark:bg-zinc-400'"></span>
      </button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue';

import { importSkills, searchSkills, searchWorkspaceSkills, setSkillEnabled, setWorkspaceSkillEnabled } from '../api';
import {
  SKILL_FILE,
  SkillsState,
  choosablePaths,
  contentWorkspaceId,
  formatSkillSize,
  githubImportUrlValid,
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
  withWorkspace,
} from '../composables/useSkills';
import { useToasts } from '../composables/useToasts';
import { onWebMCPChange } from '../composables/useWebMCPChanges';
import { useWorkspaceStore } from '../stores/workspaceStore';

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
async function loadSkills({ quiet = false } = {}) {
  if (!quiet) loading.value = true;
  error.value = null;
  try {
    const res = props.workspaceId ? await searchWorkspaceSkills(props.workspaceId) : await searchSkills();
    skills.value = res.skills || [];
  } catch (err) {
    error.value = err;
  } finally {
    loading.value = false;
  }
}

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
// A repository too large to import whole offers its skills instead: the link
// they came from, and which of them are ticked.
const choice = ref(null);
const chosen = ref([]);

// The workspaces to turn what is imported on in. A workspace's tab starts with
// its own ticked — a fork's parent, whose skills the fork uses — and moves the
// tick if the store says otherwise once loaded.
const workspaceChoices = computed(() => skillWorkspaceChoices(workspaceStore.workspaces));
const homeWorkspaceId = computed(() =>
  props.workspaceId ? contentWorkspaceId(workspaceStore.getWorkspace(props.workspaceId), props.workspaceId) : '',
);
const importWorkspaceIds = ref([]);
watch(homeWorkspaceId, (id, old) => {
  const ids = old ? withWorkspace(importWorkspaceIds.value, old, false) : importWorkspaceIds.value;
  importWorkspaceIds.value = id ? withWorkspace(ids, id, true) : ids;
}, { immediate: true });

async function doImport(url, skills) {
  importing.value = true;
  importError.value = '';
  importReport.value = null;
  try {
    const res = await importSkills(url, importOverwrite.value, skills, [...importWorkspaceIds.value]);
    if (res.candidates?.length) {
      choice.value = { url, repo: res.sourceRepo, candidates: orderCandidates(res.candidates) };
      chosen.value = [];
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
  return doImport(importUrl.value.trim());
}

function importChosen() {
  return doImport(choice.value.url, [...chosen.value]);
}

onMounted(() => {
  loadSkills();
  if (!workspaceStore.workspaces.length) workspaceStore.fetchWorkspaces();
});
onWebMCPChange(() => loadSkills({ quiet: true }));
watch(() => props.workspaceId, () => loadSkills());
</script>
