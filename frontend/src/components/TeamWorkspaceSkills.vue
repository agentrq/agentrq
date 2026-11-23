<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<!-- The skills a team workspace owns, on the team's Skills page
     (/teams/<id>/skills?workspace=<name>). They belong to no person: any
     member creates, imports, edits and deletes them, and each workspace
     shared into the team workspace is opted in to them unless its owner opts
     it out. The team workspace on screen is the route's `workspace`, else the
     first; picking another sets it, so the teammates' list below follows.
     A skill opens in place, with its files: one is edited in a textarea and
     saved against the version read, and a save over someone else's is
     refused, with a reload. -->
<template>
  <section data-test="team-workspace-skills" class="space-y-5">
    <p v-if="groups === null" class="text-[11px] text-gray-400 dark:text-zinc-500 ml-1">Loading…</p>
    <p v-else-if="loadError" data-test="tws-error" class="text-[11px] font-bold text-red-600 dark:text-red-400">{{ loadError }}</p>
    <div v-else-if="groups.length === 0" data-test="tws-none" class="p-6 bg-gray-50 dark:bg-zinc-800/50 rounded-sm border border-gray-100 dark:border-zinc-800 text-center">
      <p class="text-[11px] font-bold text-gray-700 dark:text-zinc-200">No team workspaces yet.</p>
      <p class="mt-1 text-[11px] text-gray-500 dark:text-zinc-400">Share a workspace with the team to start one; its skills show here.</p>
    </div>

    <template v-else>
      <div class="flex flex-wrap items-center gap-3">
        <label v-if="groups.length > 1" class="flex items-center gap-2">
          <span class="text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Team workspace</span>
          <select data-test="tws-pick" :value="current.id" @change="pick($event.target.value)"
                  class="px-2 py-1 text-xs bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700 rounded-sm text-gray-900 dark:text-zinc-100 focus:outline-none focus:border-gray-400 dark:focus:border-zinc-500">
            <option v-for="g in groups" :key="g.id" :value="g.id">{{ g.name }}</option>
          </select>
        </label>
        <p v-else data-test="tws-name" class="text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">
          Team workspace · <span class="text-gray-700 dark:text-zinc-200">{{ current.name }}</span>
        </p>
      </div>

      <!-- One skill, its files, and the one being edited. -->
      <div v-if="openId" data-test="tws-skill" class="space-y-4">
        <button type="button" data-test="tws-back" @click="closeSkill"
                class="text-[9px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-white">← Team skills</button>
        <p v-if="skillError" data-test="tws-skill-error" class="text-[11px] font-bold text-red-600 dark:text-red-400">{{ skillError }}</p>
        <p v-else-if="!skill" class="text-[11px] text-gray-400 dark:text-zinc-500">Loading…</p>
        <template v-else>
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div class="min-w-0">
              <h2 data-test="tws-skill-name" class="text-sm font-bold font-mono text-gray-900 dark:text-zinc-100 break-all">{{ skill.name }}</h2>
              <p v-if="skill.description" class="text-[12px] text-gray-500 dark:text-zinc-400">{{ skill.description }}</p>
            </div>
            <button type="button" data-test="tws-delete-skill" @click="confirming = { kind: 'skill' }"
                    class="shrink-0 px-3 py-1.5 rounded-lg border border-red-200 dark:border-red-500/30 text-red-600 dark:text-red-400 text-[9px] font-black uppercase tracking-widest hover:bg-red-50 dark:hover:bg-red-500/10">
              Delete skill
            </button>
          </div>

          <ul class="divide-y divide-gray-100 dark:divide-zinc-800 border-y border-gray-100 dark:border-zinc-800">
            <li v-for="f in files" :key="f.path" data-test="tws-file" class="py-2 flex items-center gap-3">
              <button type="button" data-test="tws-file-open" @click="openFile(f.path)"
                      class="flex-1 min-w-0 text-left text-xs font-mono break-all hover:underline"
                      :class="editor?.path === f.path ? 'font-bold text-gray-900 dark:text-white' : 'text-gray-700 dark:text-zinc-300'">{{ f.path }}</button>
              <span class="shrink-0 text-[10px] text-gray-400 dark:text-zinc-500 tabular-nums">{{ formatSkillSize(f.sizeBytes) }}</span>
              <button v-if="f.path !== SKILL_FILE" type="button" data-test="tws-file-delete" :aria-label="`Delete ${f.path}`"
                      @click="confirming = { kind: 'file', path: f.path }"
                      class="shrink-0 text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 hover:text-red-600 dark:hover:text-red-400">Delete</button>
            </li>
          </ul>

          <!-- The file being edited. -->
          <div v-if="editor" data-test="tws-editor" class="space-y-2">
            <label for="tws-file-content" class="block text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">
              {{ editor.isNew ? 'New file' : editor.path }}
            </label>
            <input v-if="editor.isNew" v-model="editor.path" data-test="tws-new-path" type="text" placeholder="references/notes.md" autocomplete="off" spellcheck="false"
                   class="w-full px-3 py-2 text-xs font-mono bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700 rounded-sm text-gray-900 dark:text-zinc-100 placeholder-gray-300 dark:placeholder-zinc-600 focus:outline-none focus:border-gray-400 dark:focus:border-zinc-500" />
            <p v-if="editor.loading" class="text-[11px] text-gray-400 dark:text-zinc-500">Loading…</p>
            <textarea v-else id="tws-file-content" v-model="editor.content" data-test="tws-content" rows="16" spellcheck="false"
                      class="w-full px-3 py-2 text-xs font-mono bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700 rounded-sm text-gray-900 dark:text-zinc-100 focus:outline-none focus:border-gray-400 dark:focus:border-zinc-500"></textarea>
            <div v-if="editor.error" data-test="tws-save-error" role="alert" class="flex flex-wrap items-center gap-3 text-[11px] font-bold text-red-600 dark:text-red-400">
              <span class="break-words">{{ editor.error }}</span>
              <button v-if="editor.stale" type="button" data-test="tws-reload" @click="openFile(editor.path, { force: true })"
                      class="px-2.5 py-1 rounded-lg border border-red-200 dark:border-red-500/30 text-[9px] font-black uppercase tracking-widest hover:bg-red-50 dark:hover:bg-red-500/10">
                Reload, dropping my changes
              </button>
            </div>
            <div class="flex items-center gap-2">
              <button type="button" data-test="tws-save" :disabled="editor.saving || editor.loading || !editor.path.trim() || (!editor.isNew && !dirty)" @click="saveFile"
                      class="px-4 py-2 bg-black dark:bg-white text-white dark:text-black rounded-lg text-[11px] font-black uppercase tracking-widest disabled:opacity-40">
                {{ editor.saving ? 'Saving…' : (editor.isNew ? 'Add file' : 'Save') }}
              </button>
              <button type="button" data-test="tws-cancel" @click="editor = null"
                      class="px-3 py-2 text-[10px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400 hover:text-gray-800 dark:hover:text-zinc-100">Close</button>
            </div>
          </div>
          <button v-else type="button" data-test="tws-add-file" @click="newFile"
                  class="px-3 py-1.5 rounded-lg border border-gray-300 dark:border-zinc-600 text-gray-900 dark:text-white text-[9px] font-black uppercase tracking-widest hover:bg-gray-100 dark:hover:bg-zinc-800">
            Add a file
          </button>
        </template>
      </div>

      <!-- The team workspace's skills, and adding to them. -->
      <template v-else>
        <div class="flex flex-wrap gap-2">
          <button type="button" data-test="tws-new" @click="toggleCreate"
                  class="px-3 py-1.5 rounded-lg bg-black dark:bg-white text-white dark:text-black text-[9px] font-black uppercase tracking-widest hover:opacity-90">
            New skill
          </button>
        </div>

        <form v-if="creating" data-test="tws-create" class="sm:p-4 sm:bg-gray-50 sm:dark:bg-zinc-800/50 sm:rounded-sm sm:border border-gray-100 dark:border-zinc-800 space-y-2" @submit.prevent="create">
          <label for="tws-create-content" class="block text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">{{ SKILL_FILE }}</label>
          <textarea id="tws-create-content" v-model="newContent" data-test="tws-create-content" rows="12" spellcheck="false"
                    class="w-full px-3 py-2 text-xs font-mono bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700 rounded-sm text-gray-900 dark:text-zinc-100 focus:outline-none focus:border-gray-400 dark:focus:border-zinc-500"></textarea>
          <p class="text-[10px] text-gray-500 dark:text-zinc-400">The frontmatter's name and description name the skill and say when to use it.</p>
          <p v-if="createError" data-test="tws-create-error" role="alert" class="text-[11px] font-bold text-red-600 dark:text-red-400 break-words">{{ createError }}</p>
          <div class="flex items-center gap-2">
            <button type="submit" data-test="tws-create-submit" :disabled="createBusy || !newContent.trim()"
                    class="px-4 py-2 bg-black dark:bg-white text-white dark:text-black rounded-lg text-[11px] font-black uppercase tracking-widest disabled:opacity-40">
              {{ createBusy ? 'Creating…' : 'Create' }}
            </button>
            <button type="button" @click="creating = false"
                    class="px-3 py-2 text-[10px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400 hover:text-gray-800 dark:hover:text-zinc-100">Cancel</button>
          </div>
        </form>

        <!-- Import from GitHub, as the account's Skills page does. -->
        <form class="sm:p-4 sm:bg-gray-50 sm:dark:bg-zinc-800/50 sm:rounded-sm sm:border border-gray-100 dark:border-zinc-800 space-y-3" @submit.prevent="runImport">
          <label for="tws-import-url" class="block text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Import from GitHub</label>
          <div class="flex flex-col sm:flex-row gap-2">
            <input id="tws-import-url" v-model="importUrl" data-test="tws-import-url" type="url" autocomplete="off" spellcheck="false"
                   placeholder="https://github.com/obra/superpowers"
                   class="min-w-0 flex-1 px-3 py-2 text-xs font-mono bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700 rounded-sm text-gray-900 dark:text-zinc-100 placeholder-gray-300 dark:placeholder-zinc-600 focus:outline-none focus:border-gray-400 dark:focus:border-zinc-500" />
            <button type="submit" data-test="tws-import" :disabled="importing || !importUrl.trim()"
                    class="shrink-0 px-4 py-2 bg-black dark:bg-white text-white dark:text-black rounded-lg text-[11px] font-black uppercase tracking-widest disabled:opacity-40">
              {{ importing ? 'Importing…' : 'Import' }}
            </button>
          </div>
          <div class="flex flex-wrap items-center justify-between gap-2">
            <label class="flex items-center gap-2 text-[11px] text-gray-600 dark:text-zinc-300">
              <input v-model="importOverwrite" data-test="tws-import-overwrite" type="checkbox" class="rounded-sm border-gray-300 dark:border-zinc-600" />
              Overwrite team skills of the same name
            </label>
            <span v-if="importUrl.trim() && !githubImportUrlValid(importUrl)" class="text-[10px] text-amber-600 dark:text-amber-400">
              Expected https://github.com/owner/repo, optionally /tree/&lt;ref&gt;/&lt;path&gt;
            </span>
          </div>
          <p v-if="importError" data-test="tws-import-error" class="text-[11px] font-bold text-red-600 dark:text-red-400 break-words">{{ importError }}</p>
          <div v-if="choice" data-test="tws-import-choice" class="space-y-2 text-[11px] text-gray-700 dark:text-zinc-300">
            <p class="font-bold break-words">{{ choice.repo }} is too large to import whole. Choose the skills to import:</p>
            <div class="flex flex-wrap items-center gap-3">
              <button type="button" data-test="tws-choice-all" @click="chosen = choosablePaths(choice.candidates)"
                      class="text-[9px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400 hover:text-gray-800 dark:hover:text-zinc-100">Select all</button>
              <button type="button" @click="chosen = []"
                      class="text-[9px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400 hover:text-gray-800 dark:hover:text-zinc-100">Select none</button>
              <span class="text-[10px] text-gray-400 dark:text-zinc-500">{{ chosen.length }} of {{ choosablePaths(choice.candidates).length }} selected</span>
            </div>
            <ul class="max-h-72 overflow-y-auto space-y-1 bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700 rounded-sm p-2">
              <li v-for="c in choice.candidates" :key="c.path" data-test="tws-candidate">
                <label class="flex items-start gap-2 min-w-0" :class="c.reason ? 'opacity-50' : 'cursor-pointer'">
                  <input v-model="chosen" type="checkbox" :value="c.path" :disabled="!!c.reason" class="mt-0.5 shrink-0 rounded-sm border-gray-300 dark:border-zinc-600" />
                  <span class="min-w-0 flex-1">
                    <span class="flex flex-wrap items-baseline gap-x-2">
                      <span class="font-mono font-bold break-all">{{ c.name }}</span>
                      <span class="text-[10px] text-gray-400 dark:text-zinc-500 tabular-nums">{{ formatSkillSize(c.sizeBytes) }}</span>
                    </span>
                    <span v-if="c.reason" class="block break-words text-amber-600 dark:text-amber-400">{{ c.reason }}</span>
                  </span>
                </label>
              </li>
            </ul>
            <div class="flex items-center gap-2">
              <button type="button" data-test="tws-import-chosen" :disabled="importing || !chosen.length" @click="importChosen"
                      class="px-4 py-2 bg-black dark:bg-white text-white dark:text-black rounded-lg text-[11px] font-black uppercase tracking-widest disabled:opacity-40">
                {{ importing ? 'Importing…' : `Import ${chosen.length} selected` }}
              </button>
              <button type="button" @click="choice = null"
                      class="px-3 py-2 text-[10px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400 hover:text-gray-800 dark:hover:text-zinc-100">Cancel</button>
            </div>
          </div>
          <div v-if="importReport" data-test="tws-import-report" class="space-y-2 text-[11px] text-gray-700 dark:text-zinc-300">
            <p class="font-bold">
              Imported {{ importReport.imported.length }} {{ importReport.imported.length === 1 ? 'skill' : 'skills' }}
              from {{ importReport.sourceRepo }}<span v-if="importReport.sourceCommit">@{{ importReport.sourceCommit.slice(0, 7) }}</span>.
            </p>
            <ul v-if="importReport.skipped.length" class="space-y-1">
              <li v-for="(s, i) in importReport.skipped" :key="i" class="break-words">
                <span class="font-mono font-bold">{{ s.path || s.name }}</span> — {{ s.reason }}
              </li>
            </ul>
          </div>
        </form>

        <p v-if="skills === null" class="text-[11px] text-gray-400 dark:text-zinc-500 ml-1">Loading skills…</p>
        <p v-else-if="listError" data-test="tws-list-error" class="text-[11px] font-bold text-red-600 dark:text-red-400">{{ listError }}</p>
        <div v-else-if="skills.length === 0" data-test="tws-empty" class="p-6 bg-gray-50 dark:bg-zinc-800/50 rounded-sm border border-gray-100 dark:border-zinc-800 text-center">
          <p class="text-[11px] font-bold text-gray-700 dark:text-zinc-200">No team skills yet.</p>
          <p class="mt-1 text-[11px] text-gray-500 dark:text-zinc-400">Create one, import some, or give one of yours from a workspace's Skills tab.</p>
        </div>
        <ul v-else class="divide-y divide-gray-100 dark:divide-zinc-800 border-y border-gray-100 dark:border-zinc-800">
          <li v-for="s in skills" :key="s.id" data-test="tws-row" class="py-3 flex items-start gap-3">
            <button type="button" data-test="tws-open" @click="openSkill(s.id)" class="flex-1 min-w-0 text-left space-y-1 group">
              <span data-test="tws-row-name" class="block text-xs font-bold font-mono text-gray-800 dark:text-zinc-100 break-all group-hover:underline">{{ s.name }}</span>
              <span v-if="s.description" class="block text-[11px] leading-snug text-gray-500 dark:text-zinc-400 break-words">{{ s.description }}</span>
              <span class="block text-[10px] text-gray-400 dark:text-zinc-500 tabular-nums">{{ s.fileCount || 0 }} {{ s.fileCount === 1 ? 'file' : 'files' }} · {{ formatSkillSize(s.totalBytes) }}</span>
            </button>
          </li>
        </ul>
      </template>
    </template>

    <DeleteModal :show="!!confirming" :title="confirming?.kind === 'file' ? 'Delete file' : 'Delete team skill'"
                 :confirm-label="'Delete'"
                 :message="confirming?.kind === 'file' ? `Deletes ${confirming.path} from ${skill?.name || 'the skill'}.` : `Deletes ${skill?.name || 'it'} for every workspace in the team.`"
                 @close="confirming = null" @confirm="confirmDelete" />
  </section>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import {
  createTeamWorkspaceSkill,
  deleteTeamWorkspaceSkill,
  deleteTeamWorkspaceSkillFile,
  fetchTeamWorkspaces,
  getTeamWorkspaceSkill,
  getTeamWorkspaceSkillFile,
  importTeamWorkspaceSkills,
  listTeamWorkspaceSkills,
  saveTeamWorkspaceSkillFile,
} from '../api';
import DeleteModal from './DeleteModal.vue';
import { SKILL_FILE, choosablePaths, formatSkillSize, githubImportUrlValid, orderCandidates } from '../composables/useSkills';
import { lostTeam, sameSharedName, sharedNameOf, useTeamMode } from '../composables/useTeamMode';
import { useToasts } from '../composables/useToasts';
import { onWebMCPChange } from '../composables/useWebMCPChanges';
import { useEventBus } from '../useEventBus';

const route = useRoute();
const router = useRouter();
const { teamId, leaveTeam } = useTeamMode();
const { notifySuccess, notifyError, notifyInfo } = useToasts();

// ── The team workspace on screen ────────────────────────────────────────────
// The team's shared workspaces, one entry per team workspace: { id, name }.
const groups = ref(null);
const loadError = ref('');

/** The team workspaces in the team's shared workspaces, by name. */
function teamWorkspacesOf(rows = []) {
  const byId = new Map();
  for (const w of rows) {
    if (!w.teamWorkspaceId || byId.has(w.teamWorkspaceId)) continue;
    byId.set(w.teamWorkspaceId, { id: String(w.teamWorkspaceId), name: sharedNameOf(w) });
  }
  return [...byId.values()].sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }));
}

const queryName = computed(() => (typeof route.query.workspace === 'string' ? route.query.workspace : ''));
const current = computed(() => {
  const list = groups.value || [];
  return list.find((g) => sameSharedName(g.name, queryName.value)) || list[0] || null;
});

function pick(id) {
  const g = (groups.value || []).find((x) => x.id === id);
  if (g) router.replace({ path: route.path, query: { ...route.query, workspace: g.name } });
}

async function loadGroups() {
  loadError.value = '';
  try {
    groups.value = teamWorkspacesOf((await fetchTeamWorkspaces(teamId.value))?.workspaces || []);
  } catch (err) {
    if (lostTeam(err)) return leaveTeam();
    groups.value = [];
    loadError.value = err.message;
  }
  return undefined;
}

// ── Its skills ──────────────────────────────────────────────────────────────
const skills = ref(null);
const listError = ref('');
let listSeq = 0;

async function loadSkills({ quiet = false } = {}) {
  const mine = ++listSeq;
  const tw = current.value;
  if (!tw) return;
  if (!quiet) skills.value = null;
  listError.value = '';
  try {
    const res = await listTeamWorkspaceSkills(teamId.value, tw.id);
    if (mine === listSeq) skills.value = res?.skills || [];
  } catch (err) {
    if (mine !== listSeq) return;
    skills.value = [];
    listError.value = err.message;
  }
}

// ── Creating one ────────────────────────────────────────────────────────────
const TEMPLATE = '---\nname: \ndescription: Use when \n---\n\n';
const creating = ref(false);
const newContent = ref(TEMPLATE);
const createBusy = ref(false);
const createError = ref('');

function toggleCreate() {
  creating.value = !creating.value;
  createError.value = '';
}

async function create() {
  createBusy.value = true;
  createError.value = '';
  try {
    const res = await createTeamWorkspaceSkill(teamId.value, current.value.id, newContent.value);
    notifySuccess(`Created ${res?.skill?.name || 'the skill'} in ${current.value.name}`);
    creating.value = false;
    newContent.value = TEMPLATE;
    await loadSkills({ quiet: true });
    if (res?.skill?.id) openSkill(res.skill.id);
  } catch (err) {
    createError.value = err.message;
  } finally {
    createBusy.value = false;
  }
}

// ── Importing ───────────────────────────────────────────────────────────────
const importUrl = ref('');
const importOverwrite = ref(false);
const importing = ref(false);
const importReport = ref(null);
const importError = ref('');
const choice = ref(null);
const chosen = ref([]);

async function doImport(url, picked) {
  importing.value = true;
  importError.value = '';
  importReport.value = null;
  try {
    const res = await importTeamWorkspaceSkills(teamId.value, current.value.id, url, importOverwrite.value, picked);
    if (res?.candidates?.length) {
      choice.value = { url, repo: res.sourceRepo, candidates: orderCandidates(res.candidates) };
      chosen.value = [];
      return;
    }
    choice.value = null;
    importReport.value = { imported: res?.imported || [], skipped: res?.skipped || [], sourceRepo: res?.sourceRepo || '', sourceCommit: res?.sourceCommit || '' };
    await loadSkills({ quiet: true });
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

// ── One skill, and its files ────────────────────────────────────────────────
const openId = ref('');
const skill = ref(null);
const skillError = ref('');
// The file being edited: { path, content, original, version, isNew, loading, saving, error, stale }.
const editor = ref(null);
// What to delete, while it is being confirmed: { kind: 'skill' } or { kind: 'file', path }.
const confirming = ref(null);

const files = computed(() => [...(skill.value?.files || [])].sort((a, b) => {
  if (a.path === SKILL_FILE) return -1;
  if (b.path === SKILL_FILE) return 1;
  return a.path.localeCompare(b.path);
}));
const dirty = computed(() => !!editor.value && editor.value.content !== editor.value.original);

async function loadSkill() {
  skillError.value = '';
  try {
    skill.value = (await getTeamWorkspaceSkill(teamId.value, current.value.id, openId.value))?.skill || null;
  } catch (err) {
    skillError.value = err.message;
  }
}

function openSkill(id) {
  openId.value = String(id);
  skill.value = null;
  editor.value = null;
  return loadSkill().then(() => {
    if (skill.value) openFile(SKILL_FILE);
  });
}

function closeSkill() {
  openId.value = '';
  skill.value = null;
  editor.value = null;
  return loadSkills({ quiet: true });
}

// Opens a file in the editor; not over unsaved changes unless `force`.
async function openFile(path, { force = false } = {}) {
  if (dirty.value && !force && editor.value?.path !== path) {
    notifyInfo('Save or close the file you are editing first');
    return;
  }
  editor.value = { path, content: '', original: '', version: undefined, isNew: false, loading: true, saving: false, error: '', stale: false };
  try {
    const res = await getTeamWorkspaceSkillFile(teamId.value, current.value.id, openId.value, path);
    if (editor.value?.path !== path) return;
    const content = res?.file?.content ?? '';
    editor.value = { ...editor.value, content, original: content, version: res?.file?.version, loading: false };
  } catch (err) {
    if (editor.value?.path === path) editor.value = { ...editor.value, loading: false, error: err.message };
  }
}

function newFile() {
  editor.value = { path: '', content: '', original: '', version: undefined, isNew: true, loading: false, saving: false, error: '', stale: false };
}

async function saveFile() {
  const ed = editor.value;
  ed.saving = true;
  ed.error = '';
  ed.stale = false;
  try {
    const path = ed.path.trim();
    const res = await saveTeamWorkspaceSkillFile(teamId.value, current.value.id, openId.value, path, ed.content, ed.isNew ? undefined : ed.version);
    if (res?.skill) skill.value = res.skill;
    editor.value = { ...ed, path, isNew: false, saving: false, original: ed.content, version: res?.file?.version ?? ed.version };
    notifySuccess(`Saved ${path}`);
  } catch (err) {
    ed.saving = false;
    ed.error = err.message;
    // Saved over by someone else since it was read: offer theirs.
    ed.stale = err.status === 409 && !ed.isNew;
  }
}

async function confirmDelete() {
  const what = confirming.value;
  confirming.value = null;
  if (!what) return;
  try {
    if (what.kind === 'file') {
      const res = await deleteTeamWorkspaceSkillFile(teamId.value, current.value.id, openId.value, what.path);
      if (res?.skill) skill.value = res.skill;
      if (editor.value?.path === what.path) editor.value = null;
      notifySuccess(`Deleted ${what.path}`);
    } else {
      const name = skill.value?.name || 'the skill';
      await deleteTeamWorkspaceSkill(teamId.value, current.value.id, openId.value);
      notifySuccess(`Deleted ${name} for the team`);
      await closeSkill();
    }
  } catch (err) {
    notifyError(err.message);
  }
}

// ── Keeping it current ──────────────────────────────────────────────────────
// A teammate's change to the team's skills reads them again, never over a
// file being edited; a skill deleted while open closes.
function changedElsewhere(event) {
  if (!event?.type?.startsWith('team.skill.') || String(event.teamId) !== String(teamId.value)) return;
  if (openId.value && event.type === 'team.skill.deleted' && String(event.payload?.id) === openId.value) {
    notifyInfo(`${skill.value?.name || 'This skill'} was deleted`);
    closeSkill();
    return;
  }
  if (openId.value) {
    if (!dirty.value) loadSkill();
    return;
  }
  loadSkills({ quiet: true });
}

const { connect, disconnect, onEvent } = useEventBus(undefined, { buffer: false });

onMounted(() => {
  onEvent(changedElsewhere);
  connect();
});
onUnmounted(() => disconnect());
onWebMCPChange(() => (openId.value ? (!dirty.value && loadSkill()) : loadSkills({ quiet: true })));

watch(teamId, async () => {
  groups.value = null;
  if (teamId.value) await loadGroups();
}, { immediate: true });
// Another team workspace: its list, with nothing open.
watch(() => current.value?.id, (id) => {
  openId.value = '';
  skill.value = null;
  editor.value = null;
  choice.value = null;
  importReport.value = null;
  if (id) loadSkills();
}, { immediate: true });
</script>
