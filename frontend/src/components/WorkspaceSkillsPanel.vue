<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
-->

<!-- A workspace's skills, in its settings.
     The list shows the skills themselves and never their other files: those
     open only by following a reference from the file being read, the way an
     agent reaches them. -->
<template>
  <div class="space-y-6 animate-in fade-in slide-in-from-bottom-2 duration-300">
    <div class="space-y-1">
      <h3 class="text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest ml-1">Workspace Skills</h3>
      <p class="text-[11px] text-gray-500 dark:text-zinc-400 font-medium ml-1">
        Playbooks agents load when a task matches one: a
        <code class="bg-gray-100 dark:bg-zinc-800 px-1 py-0.5 rounded text-gray-900 dark:text-white">{{ SKILL_FILE }}</code>
        and the files it points to. Agents find them with
        <code class="bg-gray-100 dark:bg-zinc-800 px-1 py-0.5 rounded text-gray-900 dark:text-white">searchSkills</code>
        and read them with
        <code class="bg-gray-100 dark:bg-zinc-800 px-1 py-0.5 rounded text-gray-900 dark:text-white">loadSkill</code>.
      </p>
    </div>

    <!-- Import from GitHub -->
    <form class="p-4 bg-gray-50 dark:bg-zinc-800/50 rounded-sm border border-gray-100 dark:border-zinc-800 space-y-3" @submit.prevent="runImport">
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
          Overwrite skills this workspace already has
        </label>
        <span v-if="importUrl.trim() && !githubImportUrlValid(importUrl)" class="text-[10px] text-amber-600 dark:text-amber-400">
          Expected https://github.com/owner/repo, optionally /tree/&lt;ref&gt;/&lt;path&gt;
        </span>
      </div>
      <p v-if="importError" class="text-[11px] font-bold text-red-600 dark:text-red-400 break-words">{{ importError }}</p>
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
      <p class="text-[11px] font-bold text-red-600 dark:text-red-400">Could not load this workspace's skills.</p>
      <button type="button" @click="loadSkills" class="mt-2 text-[10px] font-black uppercase tracking-widest text-red-600 dark:text-red-400 hover:underline">Try again</button>
    </div>

    <div v-else-if="view === SkillsState.Empty" class="p-6 bg-gray-50 dark:bg-zinc-800/50 rounded-sm border border-gray-100 dark:border-zinc-800 text-center">
      <p class="text-[11px] font-bold text-gray-700 dark:text-zinc-200">No skills yet.</p>
      <p class="mt-1 text-[11px] text-gray-500 dark:text-zinc-400">
        Import some from GitHub above, or let an agent write one with <code>saveSkill</code>.
      </p>
    </div>

    <div v-else class="space-y-2">
      <div v-for="s in orderedSkills" :key="s.name" data-test="skill-row"
           class="bg-gray-50 dark:bg-zinc-800/50 rounded-sm border border-gray-100 dark:border-zinc-800 overflow-hidden">
        <button type="button" @click="toggleSkill(s.name)"
                class="w-full flex items-start sm:items-center justify-between gap-4 p-4 text-left hover:bg-gray-100 dark:hover:bg-zinc-800 transition-colors">
          <span class="flex items-start sm:items-center gap-3 min-w-0">
            <svg class="w-3.5 h-3.5 mt-0.5 sm:mt-0 shrink-0 text-gray-400 dark:text-zinc-500 transition-transform"
                 :class="openName === s.name ? 'rotate-90' : ''"
                 fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7" />
            </svg>
            <span class="min-w-0 space-y-1">
              <span class="flex flex-wrap items-center gap-2 min-w-0">
                <span class="truncate text-xs font-bold text-gray-800 dark:text-zinc-100 font-mono">{{ s.name }}</span>
                <span v-if="s.sharedFromWorkspaceId"
                      class="shrink-0 text-[8px] font-black uppercase tracking-widest text-indigo-600 dark:text-indigo-300 border border-indigo-200 dark:border-indigo-500/40 rounded px-1.5 py-0.5">
                  Shared from {{ workspaceName(s.sharedFromWorkspaceId) }}
                </span>
                <span v-if="s.locallyModified"
                      class="shrink-0 text-[8px] font-black uppercase tracking-widest text-amber-600 dark:text-amber-300 border border-amber-200 dark:border-amber-500/40 rounded px-1.5 py-0.5">
                  Modified locally
                </span>
              </span>
              <span class="block truncate text-[11px] text-gray-500 dark:text-zinc-400">{{ s.description }}</span>
            </span>
          </span>
          <span class="shrink-0 flex flex-col sm:flex-row items-end sm:items-center gap-1 sm:gap-3 text-[10px] text-gray-400 dark:text-zinc-500 tabular-nums">
            <span>{{ formatSkillSize(s.totalBytes) }}</span>
            <span class="hidden sm:inline truncate max-w-[16rem]">{{ skillSource(s) }}</span>
          </span>
        </button>

        <div v-if="openName === s.name" class="border-t border-gray-100 dark:border-zinc-800 p-4 space-y-3">
          <!-- Where the reader is inside the skill, and the way back. -->
          <div class="flex flex-wrap items-center justify-between gap-2">
            <div class="flex items-center gap-2 min-w-0">
              <button v-if="history.length" type="button" data-test="skill-back" @click="goBack"
                      class="shrink-0 text-[9px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400 hover:text-gray-800 dark:hover:text-zinc-100">← Back</button>
              <nav data-test="skill-breadcrumb" class="min-w-0 text-[10px] font-mono text-gray-500 dark:text-zinc-400 break-all">
                <template v-for="(part, i) in breadcrumb" :key="i">
                  <span v-if="i" class="mx-1 text-gray-300 dark:text-zinc-600">/</span>
                  <span :class="i === breadcrumb.length - 1 ? 'text-gray-800 dark:text-zinc-100 font-bold' : ''">{{ part }}</span>
                </template>
              </nav>
            </div>
            <div class="flex items-center gap-3">
              <span v-if="current.path === SKILL_FILE && current.sizeBytes" class="text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">
                {{ skillFullness(current.sizeBytes) }}% of the 16 KB limit
              </span>
              <button type="button" @click="showRaw = !showRaw"
                      :class="showRaw ? 'text-gray-700 dark:text-zinc-200' : 'text-gray-400 dark:text-zinc-500 hover:text-gray-600 dark:hover:text-zinc-300'"
                      class="text-[8px] font-black uppercase tracking-wider transition-colors px-1 py-0.5 rounded">Raw</button>
            </div>
          </div>

          <p v-if="current.loading" class="text-[11px] text-gray-400 dark:text-zinc-500">Loading…</p>
          <p v-else-if="current.missing" data-test="skill-missing" class="text-[11px] font-bold text-amber-600 dark:text-amber-400 break-words">{{ current.missing }}</p>
          <p v-else-if="current.error" class="text-[11px] font-bold text-red-600 dark:text-red-400 break-words">{{ current.error }}</p>
          <div v-else-if="showRaw || !isMarkdown(current.path)"
               class="text-[12px] text-gray-800 dark:text-zinc-200 whitespace-pre-wrap break-all font-mono">{{ current.content }}</div>
          <div v-else data-test="skill-body" class="md-body text-[13px] text-gray-800 dark:text-zinc-200"
               @click="onSkillLinkClick" @keydown.enter="onSkillLinkClick"
               v-html="renderMarkdown(current.path === SKILL_FILE ? skillBody(current.content) : current.content, { skillName: current.skill, skillFiles: current.files, currentPath: current.path })"></div>

          <!-- Only the owning workspace may change a skill. -->
          <div v-if="!s.sharedFromWorkspaceId" class="pt-3 border-t border-gray-100 dark:border-zinc-800 space-y-3">
            <div class="flex flex-wrap items-center gap-2">
              <select v-model="shareTarget" data-test="skill-share-target"
                      class="min-w-0 flex-1 sm:flex-none sm:w-56 px-2 py-1.5 text-[11px] bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700 rounded-sm text-gray-800 dark:text-zinc-100">
                <option value="">Share with a workspace…</option>
                <option v-for="w in shareCandidates" :key="w.id" :value="w.id">{{ w.name }}</option>
              </select>
              <button type="button" data-test="skill-share" :disabled="!shareTarget || sharing" @click="share(s.name)"
                      class="px-3 py-1.5 bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700 rounded-lg text-[10px] font-black uppercase tracking-widest text-gray-700 dark:text-zinc-200 disabled:opacity-40">Share</button>
              <button type="button" data-test="skill-delete" @click="pendingDelete = s.name"
                      class="sm:ml-auto px-3 py-1.5 border border-red-200 dark:border-red-500/30 rounded-lg text-[10px] font-black uppercase tracking-widest text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-500/10">Delete</button>
            </div>
            <ul v-if="shares.length" data-test="skill-shares" class="space-y-1">
              <li v-for="sh in shares" :key="sh.targetWorkspaceId" class="flex items-center justify-between gap-2 text-[11px] text-gray-600 dark:text-zinc-300">
                <span class="min-w-0 truncate">Shared with {{ workspaceName(sh.targetWorkspaceId) }}</span>
                <button type="button" @click="unshare(s.name, sh.targetWorkspaceId)"
                        class="shrink-0 text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 hover:text-red-600 dark:hover:text-red-400">Remove</button>
              </li>
            </ul>
          </div>
        </div>
      </div>
    </div>

    <DeleteModal
      :show="!!pendingDelete"
      title="Delete Skill"
      :message="`Delete the skill '${pendingDelete}' and all its files? Workspaces it is shared with lose it too. This cannot be undone.`"
      @close="pendingDelete = ''"
      @confirm="confirmDelete"
    />
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue';

import {
  deleteWorkspaceSkill,
  fetchWorkspaceSkillShares,
  getWorkspaceSkill,
  getWorkspaceSkillFile,
  importWorkspaceSkills,
  searchWorkspaceSkills,
  shareWorkspaceSkill,
  unshareWorkspaceSkill,
} from '../api';
import { renderMarkdown } from '../utils/markdown';
import {
  SKILL_FILE,
  SkillsState,
  formatSkillSize,
  githubImportUrlValid,
  orderSkills,
  skillBody,
  skillBreadcrumb,
  skillFullness,
  skillLinkFromEvent,
  skillSource,
  skillsState,
} from '../composables/useSkills';
import { useToasts } from '../composables/useToasts';
import { useWorkspaceStore } from '../stores/workspaceStore';
import DeleteModal from './DeleteModal.vue';

const props = defineProps({ workspaceId: { type: String, required: true } });

const { notifySuccess, notifyError } = useToasts();
const workspaceStore = useWorkspaceStore();

const skills = ref([]);
const loading = ref(false);
const error = ref(null);
const view = computed(() => skillsState({ loading: loading.value, error: error.value, skills: skills.value }));
const orderedSkills = computed(() => orderSkills(skills.value));

async function loadSkills() {
  loading.value = true;
  error.value = null;
  try {
    const res = await searchWorkspaceSkills(props.workspaceId);
    skills.value = res.skills || [];
  } catch (err) {
    error.value = err;
  } finally {
    loading.value = false;
  }
}

function workspaceName(id) {
  return workspaceStore.getWorkspace(id)?.name || 'another workspace';
}

// ── Reading a skill ─────────────────────────────────────────────────────────
// One file is on screen at a time; following a reference pushes where the
// reader was, so Back retraces the path they took through the skill.
const openName = ref('');
const history = ref([]);
const showRaw = ref(false);
const current = reactive({ skill: '', path: SKILL_FILE, files: [], sizeBytes: 0, content: '', loading: false, error: '', missing: '' });
const breadcrumb = computed(() => skillBreadcrumb(current.skill, current.path));

function isMarkdown(path) {
  return /\.(md|markdown)$/i.test(path);
}

// Loads overlap when a reader clicks faster than the server answers; only the
// latest may write, or an earlier answer lands under the file now shown.
let loadSeq = 0;

async function showFile(skill, path) {
  const seq = ++loadSeq;
  Object.assign(current, { path, content: '', error: '', missing: '', loading: true });
  try {
    if (current.skill !== skill || !current.files.length) {
      const res = await getWorkspaceSkill(props.workspaceId, skill);
      if (seq !== loadSeq) return;
      current.files = res.skill?.files || [];
      current.skill = skill;
    }
    const file = current.files.find((f) => f.path === path);
    if (!file) {
      current.missing = `There is no ${path} in the skill ${skill}.`;
      return;
    }
    current.sizeBytes = file.sizeBytes;
    const res = await getWorkspaceSkillFile(props.workspaceId, skill, path);
    if (seq !== loadSeq) return;
    current.content = res.file?.content || '';
  } catch (err) {
    if (seq !== loadSeq) return;
    // Only the server saying "not found" makes it missing; anything else is
    // a failure worth showing as one.
    if (err.status !== 404) {
      current.error = err.message || 'Could not load this file.';
    } else if (current.skill !== skill) {
      current.skill = skill;
      current.files = [];
      current.missing = `There is no skill called ${skill} in this workspace.`;
    } else {
      current.missing = `There is no ${path} in the skill ${skill}.`;
    }
  } finally {
    if (seq === loadSeq) current.loading = false;
  }
}

async function toggleSkill(name) {
  if (openName.value === name) {
    openName.value = '';
    return;
  }
  openName.value = name;
  history.value = [];
  shareTarget.value = '';
  Object.assign(current, { skill: '', files: [] });
  const owned = skills.value.find((s) => s.name === name && !s.sharedFromWorkspaceId);
  shares.value = [];
  if (owned) loadShares(name);
  await showFile(name, SKILL_FILE);
}

function onSkillLinkClick(event) {
  const target = skillLinkFromEvent(event);
  if (!target) return;
  event.preventDefault();
  history.value.push({ skill: current.skill, path: current.path });
  showFile(target.skill, target.path);
}

function goBack() {
  const previous = history.value.pop();
  if (previous) showFile(previous.skill, previous.path);
}

// ── Importing ───────────────────────────────────────────────────────────────
const importUrl = ref('');
const importOverwrite = ref(false);
const importing = ref(false);
const importReport = ref(null);
const importError = ref('');

async function runImport() {
  importing.value = true;
  importError.value = '';
  importReport.value = null;
  try {
    importReport.value = await importWorkspaceSkills(props.workspaceId, importUrl.value.trim(), importOverwrite.value);
    await loadSkills();
  } catch (err) {
    importError.value = err.message;
  } finally {
    importing.value = false;
  }
}

// ── Deleting and sharing ────────────────────────────────────────────────────
const pendingDelete = ref('');

async function confirmDelete() {
  const name = pendingDelete.value;
  pendingDelete.value = '';
  try {
    await deleteWorkspaceSkill(props.workspaceId, name);
    if (openName.value === name) openName.value = '';
    notifySuccess(`Deleted ${name}`);
    await loadSkills();
  } catch (err) {
    notifyError(err.message);
  }
}

const shares = ref([]);
const shareTarget = ref('');
const sharing = ref(false);
const shareCandidates = computed(() =>
  workspaceStore.workspaces.filter(
    (w) => String(w.id) !== String(props.workspaceId) && !shares.value.some((sh) => String(sh.targetWorkspaceId) === String(w.id)),
  ),
);

async function loadShares(name) {
  try {
    const res = await fetchWorkspaceSkillShares(props.workspaceId, name);
    shares.value = res.shares || [];
  } catch (err) {
    notifyError(err.message);
  }
}

async function share(name) {
  sharing.value = true;
  try {
    await shareWorkspaceSkill(props.workspaceId, name, shareTarget.value);
    notifySuccess(`Shared ${name} with ${workspaceName(shareTarget.value)}`);
    shareTarget.value = '';
    await loadShares(name);
  } catch (err) {
    notifyError(err.message);
  } finally {
    sharing.value = false;
  }
}

async function unshare(name, targetWorkspaceId) {
  try {
    await unshareWorkspaceSkill(props.workspaceId, name, targetWorkspaceId);
    await loadShares(name);
  } catch (err) {
    notifyError(err.message);
  }
}

onMounted(() => {
  loadSkills();
  if (!workspaceStore.workspaces.length) workspaceStore.fetchWorkspaces();
});
watch(() => props.workspaceId, () => {
  openName.value = '';
  loadSkills();
});
</script>
