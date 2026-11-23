<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<script setup>
import { ref, computed, watch } from 'vue';
import { useWorkspaceStore } from '../stores/workspaceStore';
import { useFormat } from '../composables/useFormat';
import { forkWorkspace } from '../api';
import { canFork, kebabName, MAX_WORKSPACE_NAME, workspaceTree } from '../composables/useWorkspaceForks';
import ForkIcon from './ForkIcon.vue';

const props = defineProps({
  show: Boolean,
  taskTitle: { type: String, default: '' },
  currentWorkspaceId: { type: [String, Number], default: null }
});

const emit = defineEmits(['close', 'confirm']);

/** The destination that is not there yet: a fork made for this move. */
const NEW_FORK = '__new_fork__';

const { toKebabCase, liveKebabCase } = useFormat();
const workspaceStore = useWorkspaceStore();
const destinationWorkspaceId = ref('');
const newForkName = ref('');
const forking = ref(false);
const forkError = ref('');

// Named as the workspace form names a workspace: kebab-case as it is typed.
watch(newForkName, (value) => {
  const formatted = liveKebabCase(value);
  if (formatted !== value) newForkName.value = formatted;
});

const isCurrent = (w) => String(w.id) === String(props.currentWorkspaceId);
const current = computed(() => workspaceStore.getWorkspace(props.currentWorkspaceId));
const canForkHere = computed(() => canFork(current.value));

/**
 * The destinations, forks under their parent as the sidebar draws them. The
 * current workspace is not somewhere to move to, but it stays as a heading
 * when it has forks, so they read as its forks.
 */
const destinationGroups = computed(() =>
  workspaceTree(
    workspaceStore.workspaces
      .filter(w => !w.archivedAt)
      .slice()
      .sort((a, b) => a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' }))
  )
    .map(g => ({ ...g, forks: g.forks.filter(f => !isCurrent(f)) }))
    .filter(g => !isCurrent(g.workspace) || g.forks.length > 0)
);

const hasDestinations = computed(() => destinationGroups.value.length > 0 || canForkHere.value);

watch(() => props.show, (visible) => {
  if (visible) {
    if (workspaceStore.workspaces.length === 0) {
      workspaceStore.fetchWorkspaces();
    }
    destinationWorkspaceId.value = '';
    newForkName.value = kebabName(props.taskTitle);
    forkError.value = '';
  }
});

function closeModal() {
  emit('close');
}

async function confirmMove() {
  if (!destinationWorkspaceId.value || forking.value) return;
  if (destinationWorkspaceId.value !== NEW_FORK) {
    emit('confirm', destinationWorkspaceId.value);
    return;
  }
  // Forks first and moves second, as Spin up does. A fork that was made and
  // then not moved into is still a fork, and the sidebar shows it.
  forking.value = true;
  forkError.value = '';
  try {
    const res = await forkWorkspace(props.currentWorkspaceId, { name: kebabName(newForkName.value) });
    await workspaceStore.fetchWorkspaces();
    emit('confirm', res.workspace.id);
  } catch (err) {
    forkError.value = err.message;
  } finally {
    forking.value = false;
  }
}
</script>

<template>
  <Transition name="fade">
    <div v-if="show" class="fixed inset-0 z-[100] overflow-y-auto" aria-labelledby="modal-title" role="dialog" aria-modal="true">
      <div class="flex items-center justify-center min-h-screen pt-4 px-4 pb-20 text-center sm:block sm:p-0">
        <!-- Overlay -->
        <div class="fixed inset-0 bg-gray-900/60 backdrop-blur-sm transition-opacity" aria-hidden="true" @click="closeModal"></div>

        <span class="hidden sm:inline-block sm:align-middle sm:h-screen" aria-hidden="true">&#8203;</span>

        <!-- Modal Content -->
        <Transition name="modal">
          <div v-if="show" class="inline-block relative z-[110] align-bottom bg-white dark:bg-zinc-900 rounded-sm text-left overflow-hidden shadow-2xl transform transition-all sm:my-8 sm:align-middle sm:max-w-md sm:w-full border border-gray-100 dark:border-zinc-800">
            <div class="bg-white dark:bg-zinc-900 px-6 pt-7 pb-6 sm:p-8 sm:pb-7">
              <div class="sm:flex sm:items-start">
                <div class="mx-auto shrink-0 flex items-center justify-center h-12 w-12 rounded-sm bg-gray-50 dark:bg-zinc-800 sm:mx-0 sm:h-10 sm:w-10 border border-gray-100 dark:border-zinc-700 mb-4 sm:mb-0">
                  <svg class="h-5 w-5 text-gray-700 dark:text-zinc-300" fill="none" viewBox="0 0 24 24" stroke="currentColor" aria-hidden="true">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M17 8l4 4m0 0l-4 4m4-4H3" />
                  </svg>
                </div>

                <div class="mt-3 text-center sm:mt-0 sm:ml-4 sm:text-left w-full">
                  <h3 class="text-xl leading-8 font-bold text-gray-900 dark:text-zinc-50 tracking-tight" id="modal-title">
                    Move Task
                  </h3>
                  <div class="mt-2">
                    <p class="text-[14px] leading-relaxed text-gray-500 dark:text-zinc-400 font-medium">
                      Move <span class="text-black dark:text-white font-semibold">{{ toKebabCase(taskTitle) }}</span> to another workspace you own.
                    </p>
                  </div>

                  <div class="mt-4">
                    <div v-if="hasDestinations"
                         class="max-h-56 overflow-y-auto rounded-sm border border-gray-200 dark:border-zinc-700 divide-y divide-gray-100 dark:divide-zinc-800">
                      <template v-for="g in destinationGroups" :key="g.workspace.id">
                        <div v-if="isCurrent(g.workspace)"
                             class="px-3 py-2 text-[11px] font-medium text-gray-400 dark:text-zinc-500 bg-gray-50/60 dark:bg-zinc-800/40">
                          {{ g.workspace.name }} <span class="text-[10px]">(this workspace)</span>
                        </div>
                        <button v-else type="button"
                                @click="destinationWorkspaceId = g.workspace.id"
                                class="w-full text-left px-3 py-2.5 text-[12px] font-medium transition-colors duration-150 focus:outline-none"
                                :class="String(destinationWorkspaceId) === String(g.workspace.id)
                                  ? 'bg-black dark:bg-white text-white dark:text-black'
                                  : 'bg-white dark:bg-zinc-900 text-gray-900 dark:text-zinc-100 hover:bg-gray-50 dark:hover:bg-zinc-800'">
                          {{ g.workspace.name }}
                        </button>
                        <button v-for="f in g.forks" :key="f.id" type="button" data-test="move-fork"
                                @click="destinationWorkspaceId = f.id"
                                class="w-full text-left pl-7 pr-3 py-2.5 text-[12px] font-medium transition-colors duration-150 focus:outline-none flex items-center gap-2 min-w-0"
                                :class="String(destinationWorkspaceId) === String(f.id)
                                  ? 'bg-black dark:bg-white text-white dark:text-black'
                                  : 'bg-white dark:bg-zinc-900 text-gray-900 dark:text-zinc-100 hover:bg-gray-50 dark:hover:bg-zinc-800'">
                          <ForkIcon class="w-3 h-3 shrink-0 opacity-60" />
                          <span class="truncate">{{ f.name }}</span>
                        </button>
                      </template>
                      <button v-if="canForkHere" type="button" data-test="move-new-fork"
                              @click="destinationWorkspaceId = NEW_FORK"
                              class="w-full text-left px-3 py-2.5 text-[12px] font-semibold transition-colors duration-150 focus:outline-none flex items-center gap-2"
                              :class="destinationWorkspaceId === NEW_FORK
                                ? 'bg-black dark:bg-white text-white dark:text-black'
                                : 'bg-white dark:bg-zinc-900 text-gray-700 dark:text-zinc-300 hover:bg-gray-50 dark:hover:bg-zinc-800'">
                        <ForkIcon class="w-3 h-3 shrink-0" />
                        New fork of this workspace…
                      </button>
                    </div>
                    <p v-else class="mt-2 text-[11px] text-gray-500 dark:text-zinc-500">
                      No other workspaces available to move this task to.
                    </p>

                    <div v-if="destinationWorkspaceId === NEW_FORK" class="mt-3">
                      <label for="move-fork-name" class="block text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest">Fork name</label>
                      <input id="move-fork-name" v-model="newForkName" type="text" :maxlength="MAX_WORKSPACE_NAME"
                             spellcheck="false" autocapitalize="off" autocorrect="off"
                             class="mt-1 w-full bg-gray-50 dark:bg-zinc-800/50 border border-gray-200 dark:border-zinc-800 rounded-sm px-3 py-2 text-sm focus:border-gray-900 dark:focus:border-white focus:ring-0 outline-none font-semibold text-gray-900 dark:text-zinc-100" />
                    </div>
                    <p v-if="forkError" class="mt-2 text-[11px] text-red-600 dark:text-red-400 font-medium">{{ forkError }}</p>
                  </div>
                </div>
              </div>
            </div>

            <div class="bg-gray-50/50 dark:bg-zinc-800/50 px-6 py-5 sm:px-8 sm:flex sm:flex-row-reverse gap-3 border-t border-gray-100 dark:border-zinc-800">
              <button type="button" @click="confirmMove" :disabled="!destinationWorkspaceId || forking || (destinationWorkspaceId === NEW_FORK && !kebabName(newForkName))"
                class="w-full inline-flex justify-center rounded-sm px-6 py-2.5 bg-black dark:bg-white text-[10px] font-semibold text-white dark:text-black hover:bg-gray-800 dark:hover:bg-gray-100 disabled:opacity-40 disabled:cursor-not-allowed transition-all duration-200 sm:w-auto">
                {{ forking ? 'Forking…' : destinationWorkspaceId === NEW_FORK ? 'Fork and move' : 'Move' }}
              </button>

              <button type="button" @click="closeModal"
                class="mt-3 w-full inline-flex justify-center rounded-sm border border-gray-200 dark:border-zinc-700 px-6 py-2.5 bg-white dark:bg-zinc-900 text-[10px] font-semibold text-gray-700 dark:text-zinc-300 hover:bg-gray-50 dark:hover:bg-zinc-800 sm:mt-0 transition-all duration-200 sm:w-auto">
                Cancel
              </button>
            </div>
          </div>
        </Transition>
      </div>
    </div>
  </Transition>
</template>

<style scoped>
.fade-enter-active, .fade-leave-active { transition: opacity 0.3s ease; }
.fade-enter-from, .fade-leave-to { opacity: 0; }

.modal-enter-active { transition: all 0.4s cubic-bezier(0.16, 1, 0.3, 1); }
.modal-leave-active { transition: all 0.25s cubic-bezier(0.16, 1, 0.3, 1); }
.modal-enter-from { opacity: 0; transform: scale(0.9) translateY(20px); }
.modal-leave-to { opacity: 0; transform: scale(0.95); }
</style>
