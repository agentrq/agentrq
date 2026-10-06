<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<!-- One skill, opened from a card on the Skills page or a workspace's Skills
     tab and shown in its place: the workspaces it is on in, and its files
     beside the one being read, which also opens others by following its
     references. -->
<template>
  <div class="space-y-5 min-w-0 animate-in fade-in slide-in-from-bottom-2 duration-300">
    <RouterLink :to="skillsListPath(workspaceId)" data-test="skill-page-back-to-list"
                class="inline-block text-[9px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400 hover:text-gray-800 dark:hover:text-zinc-100">← Skills</RouterLink>

    <p v-if="state === 'loading'" class="text-[11px] text-gray-400 dark:text-zinc-500">Loading skill…</p>
    <p v-else-if="state === 'missing'" data-test="skill-page-missing" class="text-[11px] font-bold text-amber-600 dark:text-amber-400 break-words">
      There is no skill called {{ skillName }}.
    </p>
    <div v-else-if="state === 'failed'" class="p-4 bg-red-50 dark:bg-red-500/10 border border-red-100 dark:border-red-500/20 rounded-sm">
      <p class="text-[11px] font-bold text-red-600 dark:text-red-400 break-words">{{ loadError }}</p>
      <button type="button" @click="load" class="mt-2 text-[10px] font-black uppercase tracking-widest text-red-600 dark:text-red-400 hover:underline">Try again</button>
    </div>

    <template v-else>
      <header class="space-y-2 min-w-0">
        <div class="flex items-start gap-2 min-w-0">
          <div class="flex flex-wrap items-center gap-2 min-w-0 flex-1">
            <h2 data-test="skill-page-name" class="min-w-0 break-all text-base font-bold text-gray-900 dark:text-zinc-100 font-mono">{{ skill.name }}</h2>
            <span v-if="skill.locallyModified"
                  class="shrink-0 text-[8px] font-black uppercase tracking-widest text-amber-600 dark:text-amber-300 border border-amber-200 dark:border-amber-500/40 rounded px-1.5 py-0.5">
              Modified locally
            </span>
          </div>
          <!-- An icon alone on a phone. -->
          <button type="button" data-test="skill-delete" @click="pendingDelete = true" title="Delete" aria-label="Delete"
                  class="shrink-0 p-2 sm:px-3 sm:py-1.5 border border-red-200 dark:border-red-500/30 rounded-lg text-[10px] font-black uppercase tracking-widest text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-500/10">
            <svg class="w-3.5 h-3.5 sm:hidden" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" /></svg><span class="hidden sm:inline">Delete</span>
          </button>
        </div>
        <p v-if="skill.description" class="text-[12px] text-gray-600 dark:text-zinc-300 break-words">{{ skill.description }}</p>
        <p data-test="skill-page-meta" class="text-[10px] text-gray-400 dark:text-zinc-500 tabular-nums break-words">
          {{ formatSkillSize(skill.totalBytes) }} · {{ skillFileCount(skill.fileCount) }} · {{ skillSource(skill) }}
        </p>
      </header>

      <!-- The account-wide switch; the workspaces below each have their own. -->
      <div class="flex items-start justify-between gap-4 min-w-0">
        <div class="min-w-0">
          <p id="skill-enabled-label" class="text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Available to agents</p>
          <p data-test="skill-enabled-hint" class="mt-1 text-[11px] text-gray-500 dark:text-zinc-400 break-words">
            {{ skill.enabled === false
              ? 'Off: kept, but no agent finds or loads it, whichever workspaces it is on in.'
              : 'On: agents find it with searchSkills in the workspaces it is on in, and load it when a task needs it.' }}
          </p>
        </div>
        <button type="button" role="switch" data-test="skill-enabled" aria-labelledby="skill-enabled-label"
                :aria-checked="skill.enabled !== false" :disabled="pendingEnabled" @click="toggleEnabled"
                class="shrink-0 mt-1 w-11 h-6 rounded-full border transition-colors relative disabled:opacity-50"
                :class="skill.enabled !== false ? 'bg-gray-900 dark:bg-white border-gray-900 dark:border-white' : 'bg-gray-200 dark:bg-zinc-700 border-gray-300 dark:border-zinc-600'">
          <span class="absolute top-[3px] w-4 h-4 rounded-full transition-all"
                :class="skill.enabled !== false ? 'left-[23px] bg-white dark:bg-zinc-900' : 'left-[3px] bg-white dark:bg-zinc-400'"></span>
        </button>
      </div>

      <!-- Every workspace that can use it, ticked where it is on: a tick turns
           it on there and clearing one turns it off. Forks use their
           parent's, so they are not listed. -->
      <fieldset v-if="workspaceChoices.length" data-test="skill-workspaces" class="min-w-0">
        <legend class="mb-1.5 text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Workspaces</legend>
        <div class="flex flex-wrap gap-1.5">
          <label v-for="w in workspaceChoices" :key="w.id" data-test="skill-workspace-option"
                 :class="[isOn(w.id) ? 'border-gray-900 dark:border-zinc-100 text-gray-900 dark:text-zinc-100' : 'border-gray-200 dark:border-zinc-700 text-gray-500 dark:text-zinc-400 hover:border-gray-300 dark:hover:border-zinc-600',
                          pendingWorkspace.has(String(w.id)) ? 'opacity-50' : 'cursor-pointer']"
                 class="inline-flex items-center gap-1.5 max-w-full min-w-0 px-2 py-1 border rounded-sm text-[11px] transition-colors">
            <input type="checkbox" :checked="isOn(w.id)" :disabled="pendingWorkspace.has(String(w.id))" @change="toggleWorkspace(w, $event.target)"
                   class="shrink-0 rounded-sm border-gray-300 dark:border-zinc-600" />
            <span class="min-w-0 truncate">{{ w.name }}</span>
          </label>
        </div>
      </fieldset>

      <div class="pt-4 border-t border-gray-100 dark:border-zinc-800 flex flex-col md:flex-row gap-4 md:gap-6 min-w-0">
      <nav data-test="skill-files" aria-label="Files" class="md:w-56 shrink-0 min-w-0">
        <p class="hidden md:block mb-1.5 text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Files</p>
        <!-- Folded away on a phone, where it would push the file below the fold. -->
        <button type="button" data-test="skill-files-toggle" @click="filesOpen = !filesOpen" :aria-expanded="filesOpen"
                class="md:hidden flex items-center gap-1 text-[9px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400 hover:text-gray-800 dark:hover:text-zinc-100">
          Files ({{ skill.files?.length || 0 }})
          <svg class="w-3 h-3 transition-transform" :class="filesOpen ? 'rotate-90' : ''" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7" />
          </svg>
        </button>
        <ul :class="filesOpen ? 'mt-1.5' : 'hidden'" class="md:block md:mt-0 space-y-px max-h-60 md:max-h-none overflow-y-auto">
          <li v-for="row in tree" :key="`${row.kind}:${row.path}`">
            <button v-if="row.kind === 'dir'" type="button" data-test="skill-dir" @click="toggleDir(row.path)"
                    :aria-expanded="!collapsed.has(row.path)" :style="{ paddingLeft: `${0.5 + row.depth * 0.75}rem` }"
                    class="w-full flex items-center gap-1 pr-2 py-1 rounded-sm text-left text-[11px] font-mono text-gray-500 dark:text-zinc-400 hover:bg-gray-50 dark:hover:bg-zinc-800/60 transition-colors">
              <svg class="w-3 h-3 shrink-0 transition-transform" :class="collapsed.has(row.path) ? '' : 'rotate-90'"
                   fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7" />
              </svg>
              <span class="min-w-0 truncate" :title="row.path">{{ row.name }}/</span>
            </button>
            <button v-else type="button" data-test="skill-file" @click="openFile(row.path)"
                    :aria-current="isOpen(row.path) ? 'page' : undefined" :style="{ paddingLeft: `${0.5 + row.depth * 0.75 + (row.depth ? 1 : 0)}rem` }"
                    :class="isOpen(row.path) ? 'bg-gray-100 dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 font-bold' : 'text-gray-600 dark:text-zinc-400 hover:bg-gray-50 dark:hover:bg-zinc-800/60'"
                    class="w-full flex items-baseline justify-between gap-2 pr-2 py-1 rounded-sm text-left text-[11px] font-mono transition-colors">
              <span class="min-w-0 truncate" :title="row.path">{{ row.name }}</span>
              <span class="shrink-0 text-[9px] font-sans font-normal text-gray-400 dark:text-zinc-500 tabular-nums">{{ formatSkillSize(row.sizeBytes) }}</span>
            </button>
          </li>
        </ul>
      </nav>
      <section class="flex-1 space-y-3 min-w-0">
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
              {{ skillFullness(current.sizeBytes) }}% of the 96 KB limit
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
      </section>
      </div>
    </template>

    <DeleteModal
      :show="pendingDelete"
      title="Delete Skill"
      :message="`Delete the skill '${skillName}' and all its files? Every workspace loses it. This cannot be undone.`"
      @close="pendingDelete = false"
      @confirm="confirmDelete"
    />
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { useRouter } from 'vue-router';

import {
  deleteSkill,
  getSkill,
  getSkillFile,
  getWorkspaceSkill,
  getWorkspaceSkillFile,
  setSkillEnabled,
  setWorkspaceSkillEnabled,
} from '../api';
import DeleteModal from './DeleteModal.vue';
import {
  SKILL_FILE,
  formatSkillSize,
  skillFileTree,
  skillBody,
  skillBreadcrumb,
  skillFileCount,
  skillFullness,
  skillEnabledMessage,
  skillLinkFromEvent,
  skillSource,
  skillWorkspaceChoices,
  skillWorkspaceMessage,
  skillsListPath,
} from '../composables/useSkills';
import { useToasts } from '../composables/useToasts';
import { useWorkspaceStore } from '../stores/workspaceStore';
import { renderMarkdown } from '../utils/markdown';

// With no workspace, the skill as the Skills page opens it.
const props = defineProps({
  workspaceId: { type: String, default: '' },
  name: { type: String, required: true },
});

const router = useRouter();
const { notifySuccess, notifyError } = useToasts();
const workspaceStore = useWorkspaceStore();

const workspaceId = computed(() => props.workspaceId);
const skillName = computed(() => props.name);

// Read through the workspace when there is one, so the answer is that
// workspace's view of the skill.
const fetchSkill = (name) => (workspaceId.value ? getWorkspaceSkill(workspaceId.value, name) : getSkill(name));
const fetchFile = (name, path) =>
  workspaceId.value ? getWorkspaceSkillFile(workspaceId.value, name, path) : getSkillFile(name, path);

// ── The skill this page is for ──────────────────────────────────────────────
const skill = ref(null);
const state = ref('loading');
const loadError = ref('');

async function load() {
  state.value = 'loading';
  history.value = [];
  collapsed.value = new Set();
  Object.assign(current, { skill: '', files: [] });
  try {
    const res = await fetchSkill(skillName.value);
    skill.value = res.skill;
    Object.assign(current, { skill: res.skill.name, files: res.skill.files || [] });
    state.value = 'ready';
  } catch (err) {
    // Only the server saying "not found" makes it missing.
    state.value = err.status === 404 ? 'missing' : 'failed';
    loadError.value = err.message || 'Could not load this skill.';
    return;
  }
  await showFile(skill.value.name, SKILL_FILE);
}

// ── Reading it ──────────────────────────────────────────────────────────────
// One file is on screen at a time; following a reference pushes where the
// reader was, so Back retraces the path they took through the skill.
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

async function showFile(name, path) {
  const seq = ++loadSeq;
  Object.assign(current, { path, content: '', error: '', missing: '', loading: true });
  try {
    if (current.skill !== name || !current.files.length) {
      const res = await fetchSkill(name);
      if (seq !== loadSeq) return;
      current.files = res.skill?.files || [];
      current.skill = name;
    }
    const file = current.files.find((f) => f.path === path);
    if (!file) {
      current.missing = `There is no ${path} in the skill ${name}.`;
      return;
    }
    current.sizeBytes = file.sizeBytes;
    const res = await fetchFile(name, path);
    if (seq !== loadSeq) return;
    current.content = res.file?.content || '';
  } catch (err) {
    if (seq !== loadSeq) return;
    // Only the server saying "not found" makes it missing; anything else is
    // a failure worth showing as one.
    if (err.status !== 404) {
      current.error = err.message || 'Could not load this file.';
    } else if (current.skill !== name) {
      current.skill = name;
      current.files = [];
      current.missing = `There is no skill called ${name}.`;
    } else {
      current.missing = `There is no ${path} in the skill ${name}.`;
    }
  } finally {
    if (seq === loadSeq) current.loading = false;
  }
}

function onSkillLinkClick(event) {
  const target = skillLinkFromEvent(event);
  if (!target) return;
  event.preventDefault();
  history.value.push({ skill: current.skill, path: current.path });
  showFile(target.skill, target.path);
}

// The page's own skill's files, whichever skill a followed link has since
// opened, as a tree whose folders start open.
const collapsed = ref(new Set());
const filesOpen = ref(false);
const tree = computed(() => skillFileTree(skill.value?.files || [], collapsed.value));

function toggleDir(path) {
  const next = new Set(collapsed.value);
  if (!next.delete(path)) next.add(path);
  collapsed.value = next;
}

function isOpen(path) {
  return current.skill === skill.value?.name && current.path === path;
}

function openFile(path) {
  // Folded again on a phone, so the file chosen is what shows.
  filesOpen.value = false;
  if (isOpen(path)) return;
  history.value.push({ skill: current.skill, path: current.path });
  showFile(skill.value.name, path);
}

function goBack() {
  const previous = history.value.pop();
  if (previous) showFile(previous.skill, previous.path);
}

// ── Turning it on or off ────────────────────────────────────────────────────
const pendingEnabled = ref(false);

async function toggleEnabled() {
  const enabled = skill.value.enabled === false;
  pendingEnabled.value = true;
  try {
    const res = await setSkillEnabled(skillName.value, enabled);
    skill.value = { ...skill.value, enabled: res.skill.enabled };
    notifySuccess(skillEnabledMessage(skillName.value, enabled));
  } catch (err) {
    notifyError(err.message);
  } finally {
    pendingEnabled.value = false;
  }
}

// ── The workspaces it is on in ──────────────────────────────────────────────
// Workspaces whose box was just changed and whose answer is still out.
const pendingWorkspace = ref(new Set());
const workspaceChoices = computed(() => skillWorkspaceChoices(workspaceStore.workspaces));

function isOn(id) {
  return (skill.value?.workspaceIds || []).some((x) => String(x) === String(id));
}

async function toggleWorkspace(w, input) {
  const key = String(w.id);
  const on = input.checked;
  pendingWorkspace.value = new Set(pendingWorkspace.value).add(key);
  try {
    const res = await setWorkspaceSkillEnabled(key, skillName.value, on);
    skill.value = { ...skill.value, workspaceIds: res.skill.workspaceIds || [] };
    notifySuccess(skillWorkspaceMessage(skillName.value, on, w.name));
  } catch (err) {
    notifyError(err.message);
  } finally {
    // A failed box is put back by hand: its binding never changed, so Vue
    // would leave the click showing.
    input.checked = isOn(key);
    const next = new Set(pendingWorkspace.value);
    next.delete(key);
    pendingWorkspace.value = next;
  }
}

// ── Deleting ────────────────────────────────────────────────────────────────
const pendingDelete = ref(false);

async function confirmDelete() {
  pendingDelete.value = false;
  try {
    await deleteSkill(skillName.value);
    notifySuccess(`Deleted ${skillName.value}`);
    router.push(skillsListPath(workspaceId.value));
  } catch (err) {
    notifyError(err.message);
  }
}

onMounted(() => {
  load();
  if (!workspaceStore.workspaces.length) workspaceStore.fetchWorkspaces();
});
watch(() => [props.workspaceId, props.name], load);
</script>
