<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<template>
  <div class="h-full page-surface flex flex-col w-full max-w-full overflow-x-hidden relative">
    <!-- Breadcrumb Header, the same shell as the new-task page -->
    <header class="py-2 border-b border-gray-100 dark:border-zinc-800 shrink-0 flex items-center justify-between gap-4 page-surface sticky top-0 z-30 px-4">
      <div class="flex items-center gap-2 text-xs font-semibold min-w-0 flex-1">
        <router-link to="/" class="text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-50 transition-colors shrink-0">
          Workspaces
        </router-link>
        <svg class="w-3 h-3 text-gray-300 dark:text-zinc-600 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" /></svg>
        <span class="text-gray-900 dark:text-zinc-50 truncate flex-1 min-w-0 text-sm">New Workspace</span>
      </div>
      <button type="button" @click="goBack" title="Cancel"
              class="p-2 text-gray-500 dark:text-zinc-500 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-100 dark:hover:bg-zinc-800 rounded-sm transition-all shrink-0">
        <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" /></svg>
      </button>
    </header>

    <main class="flex-1 overflow-y-auto pt-6 md:pt-14 pb-24 px-3 md:px-4 custom-scrollbar flex items-start justify-center">
      <div class="w-full max-w-3xl lg:max-w-5xl">
        <div class="text-center mb-6 md:mb-8 px-2">
          <h1 class="text-xl md:text-3xl font-black text-gray-800 dark:text-zinc-200 tracking-tight">What are you building?</h1>
          <p class="mt-2 text-xs md:text-sm font-medium text-gray-500 dark:text-zinc-400">
            A workspace is where you and an agent share tasks, memory and skills for one project.
          </p>
        </div>

        <form id="workspaceForm" @submit.prevent="submit" @keydown="onKeydown"
              class="flex flex-col bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl shadow-sm">
          <!-- Two columns where there is room: the short fields side by side,
               then the two long texts side by side at the same height. -->
          <div class="p-4 md:p-6 grid grid-cols-1 lg:grid-cols-2 gap-x-6 gap-y-6">
            <!-- Name -->
            <div>
              <label for="workspaceName" class="block text-xs font-bold text-gray-800 dark:text-zinc-200 mb-1.5">
                Name <span class="text-red-500" aria-hidden="true">*</span>
              </label>
              <div class="relative">
                <svg class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400 dark:text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 8V4H8" /><rect width="16" height="12" x="4" y="8" rx="2" /><path d="M2 14h2" /><path d="M20 14h2" /><path d="M15 13v2" /><path d="M9 13v2" /></svg>
                <input id="workspaceName" ref="nameInput" v-model="form.name" @blur="form.name = toKebabCase(form.name)"
                       type="text" required spellcheck="false" autocapitalize="off" autocorrect="off" autocomplete="off"
                       aria-describedby="workspaceNameHelp"
                       :class="[FIELD, 'pl-10 font-bold']"
                       placeholder="my-saas-backend" />
              </div>
              <p id="workspaceNameHelp" class="mt-1.5 text-[11px] font-medium text-gray-500 dark:text-zinc-400">
                Lowercase letters, numbers and dashes. Spaces turn into dashes as you type.
              </p>
            </div>

            <!-- Working directory -->
            <div>
              <div class="flex items-baseline justify-between gap-2 mb-1.5">
                <label for="workspaceWorkingDirectory" class="text-xs font-bold text-gray-800 dark:text-zinc-200">Working directory</label>
                <span class="text-[11px] font-medium text-gray-400 dark:text-zinc-500">Optional</span>
              </div>
              <div class="flex items-stretch gap-2">
                <div class="relative min-w-0 flex-1">
                  <svg class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400 dark:text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z" /></svg>
                  <input id="workspaceWorkingDirectory" v-model="form.workingDirectory" type="text" spellcheck="false" autocapitalize="off" autocorrect="off"
                         aria-describedby="workspaceWorkingDirectoryHelp"
                         :class="[FIELD, 'pl-10 font-mono']"
                         :placeholder="workingDirectoryPlaceholder" />
                </div>
                <button v-if="canBrowseDirectories" type="button" @click="chooseWorkingDirectory" :disabled="isChoosingDirectory"
                        class="shrink-0 px-4 bg-white dark:bg-zinc-900 border border-gray-300 dark:border-zinc-700 rounded-lg text-[11px] font-black uppercase tracking-widest text-gray-800 dark:text-zinc-200 hover:bg-gray-50 dark:hover:bg-zinc-800 transition-colors disabled:opacity-50 disabled:cursor-not-allowed">
                  {{ isChoosingDirectory ? 'Choosing…' : 'Browse' }}
                </button>
              </div>
              <p id="workspaceWorkingDirectoryHelp" class="mt-1.5 text-[11px] font-medium text-gray-500 dark:text-zinc-400">
                The absolute path the agent starts in on its machine.
              </p>
            </div>

            <!-- Mission templates, across both columns -->
            <MissionTemplatePicker v-model="form.description" class="lg:col-span-2" />

            <!-- Mission -->
            <div class="flex flex-col">
              <div class="flex items-baseline justify-between gap-2 mb-1.5">
                <label for="workspaceMission" class="text-xs font-bold text-gray-800 dark:text-zinc-200">Mission</label>
                <button v-if="form.description !== DEFAULT_WORKSPACE_MISSION" type="button" @click="form.description = DEFAULT_WORKSPACE_MISSION"
                        class="text-[11px] font-semibold text-gray-500 hover:text-gray-900 dark:text-zinc-400 dark:hover:text-zinc-100 underline-offset-2 hover:underline">
                  Restore default
                </button>
              </div>
              <textarea id="workspaceMission" v-model="form.description" rows="12" aria-describedby="workspaceMissionHelp"
                        :class="[FIELD, 'flex-1 leading-relaxed resize-y min-h-[200px] custom-scrollbar']"
                        placeholder="What are we building? Describe the mission of this workspace..."></textarea>
              <p id="workspaceMissionHelp" class="mt-1.5 text-[11px] font-medium text-gray-500 dark:text-zinc-400">
                Every agent working here reads this first. Markdown is supported.
              </p>
            </div>

            <!-- Self-learning loop -->
            <div class="flex flex-col">
              <div class="flex items-baseline justify-between gap-2 mb-1.5">
                <label for="workspaceSelfLearningNote" class="text-xs font-bold text-gray-800 dark:text-zinc-200">Self-learning loop</label>
                <button v-if="form.selfLearningLoopNote !== DEFAULT_SELF_LEARNING_LOOP_NOTE" type="button" @click="form.selfLearningLoopNote = DEFAULT_SELF_LEARNING_LOOP_NOTE"
                        class="text-[11px] font-semibold text-gray-500 hover:text-gray-900 dark:text-zinc-400 dark:hover:text-zinc-100 underline-offset-2 hover:underline">
                  Restore default
                </button>
              </div>
              <textarea id="workspaceSelfLearningNote" v-model="form.selfLearningLoopNote" rows="12" aria-describedby="workspaceSelfLearningNoteHelp"
                        :class="[FIELD, 'flex-1 leading-relaxed resize-y min-h-[200px] custom-scrollbar']"
                        placeholder="Upon completing the task, evaluate your execution path..."></textarea>
              <p id="workspaceSelfLearningNoteHelp" class="mt-1.5 text-[11px] font-medium text-gray-500 dark:text-zinc-400">
                Appended to every task an agent is given here.
              </p>
            </div>
          </div>

          <div v-if="error" class="mx-4 md:mx-6 mb-4 bg-red-50 dark:bg-red-500/10 border border-red-200 dark:border-red-500/30 text-red-700 dark:text-red-400 px-3 py-2 rounded-md text-[11px] font-bold">
            {{ error }}
          </div>

          <!-- Actions. Stacked full width on a phone, Create on top; not
               pinned there, since the shell's floating Menu button owns the
               bottom edge of the screen. -->
          <div class="flex flex-col-reverse md:flex-row md:items-center md:justify-end gap-2 md:gap-3 px-4 md:px-6 py-4 md:py-3 border-t border-gray-100 dark:border-zinc-800 bg-gray-50/60 dark:bg-zinc-950/40 rounded-b-xl">
            <span class="hidden md:inline text-[10px] font-medium text-gray-400 dark:text-zinc-500 mr-auto">Cmd ⌘ + Enter to create</span>
            <button type="button" @click="goBack"
                    class="w-full md:w-auto px-4 py-2.5 rounded-lg text-[11px] font-black uppercase tracking-widest text-gray-600 dark:text-zinc-300 hover:bg-gray-100 dark:hover:bg-zinc-800 transition-colors">
              Cancel
            </button>
            <button type="submit" :disabled="!canCreate"
                    class="w-full md:w-auto justify-center bg-black dark:bg-white text-white dark:text-zinc-900 px-5 py-2.5 rounded-lg text-[11px] font-black uppercase tracking-widest hover:opacity-90 disabled:opacity-30 transition-all flex items-center gap-2">
              <svg v-if="loading" class="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 12a8 8 0 018-8v8H4z" /></svg>
              {{ loading ? 'Creating…' : 'Create Workspace' }}
            </button>
          </div>
        </form>
      </div>
    </main>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue';
import { useRouter } from 'vue-router';
import { createWorkspace } from '../api';
import MissionTemplatePicker from '../components/MissionTemplatePicker.vue';
import { useToasts } from '../composables/useToasts';
import { useFormat } from '../composables/useFormat';
import { usePlatformStore } from '../stores/platformStore';
import { useWorkspaceStore } from '../stores/workspaceStore';
import {
  chooseDirectory,
  directoryPickerState,
  workingDirectoryPlaceholder as directoryPlaceholderFor,
} from '../composables/useDirectoryPicker';
import {
  DEFAULT_SELF_LEARNING_LOOP_NOTE,
  DEFAULT_WORKSPACE_MISSION,
  canCreateWorkspace,
  emptyWorkspaceForm,
} from '../utils/workspaceForm';

/** One look for every field on the page. */
const FIELD =
  'w-full bg-white dark:bg-zinc-950 border border-gray-300 dark:border-zinc-700 rounded-lg px-3 py-2.5 ' +
  'text-sm text-gray-900 dark:text-zinc-100 placeholder:text-gray-400 dark:placeholder:text-zinc-600 ' +
  'outline-none transition-shadow focus:border-gray-900 dark:focus:border-zinc-300 ' +
  'focus:ring-2 focus:ring-gray-900/10 dark:focus:ring-white/10';

const router = useRouter();
const { notifyError } = useToasts();
const { toKebabCase, liveKebabCase } = useFormat();
const platformStore = usePlatformStore();
const workspaceStore = useWorkspaceStore();

const form = ref(emptyWorkspaceForm());
const loading = ref(false);
const error = ref(null);
const isChoosingDirectory = ref(false);
const nameInput = ref(null);

const canCreate = computed(() => !loading.value && canCreateWorkspace(form.value));

// Offered only when it can actually work: the browser cannot produce an
// absolute path, and a desktop build whose bridge lacks the chooser would give
// a button that does nothing when clicked.
const canBrowseDirectories = computed(
  () => directoryPickerState({ isDesktop: platformStore.isDesktop, bridge: window.agentrq?.dialog }).available
);
const workingDirectoryPlaceholder = computed(() => directoryPlaceholderFor(window.agentrq?.platform));

watch(() => form.value.name, (value) => {
  const formatted = liveKebabCase(value);
  if (formatted !== value) form.value.name = formatted;
});

async function chooseWorkingDirectory() {
  if (isChoosingDirectory.value) return;
  isChoosingDirectory.value = true;
  try {
    const chosen = await chooseDirectory({
      isDesktop: platformStore.isDesktop,
      bridge: window.agentrq?.dialog,
      currentPath: form.value.workingDirectory,
    });
    // '' means the dialog was dismissed, which must not wipe what is there.
    if (chosen) form.value.workingDirectory = chosen;
  } catch (err) {
    notifyError(err.message);
  } finally {
    isChoosingDirectory.value = false;
  }
}

function onKeydown(event) {
  if (event.key !== 'Enter' || !(event.metaKey || event.ctrlKey)) return;
  event.preventDefault();
  submit();
}

async function submit() {
  if (!canCreate.value) return;
  loading.value = true;
  error.value = null;
  try {
    const { name, description, icon, selfLearningLoopNote, workingDirectory } = form.value;
    const res = await createWorkspace(toKebabCase(name), description, icon, selfLearningLoopNote, workingDirectory);
    const newId = res.workspace?.id || res.id;
    // The sidebar and overview read the store, so it learns of the new
    // workspace now rather than on the next event.
    workspaceStore.fetchWorkspaces().catch(() => {});
    router.push(newId ? `/workspaces/${newId}` : '/');
  } catch (err) {
    error.value = 'Failed to create workspace: ' + err.message;
  } finally {
    loading.value = false;
  }
}

function goBack() {
  router.push('/');
}

onMounted(() => nameInput.value?.focus());
</script>
