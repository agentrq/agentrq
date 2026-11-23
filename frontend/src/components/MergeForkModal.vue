<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<!-- Confirming a merge: what stops, what moves, what is removed, and the folder
     that is left behind. Laid out like the Move Task dialog. -->
<script setup>
import { computed, ref, watch } from 'vue';
import ForkIcon from './ForkIcon.vue';
import { mergeConfirmMessage } from '../composables/useWorkspaceForks';

const props = defineProps({
  fork: { type: Object, default: null },
  parentName: { type: String, default: '' },
  busy: Boolean
});

const emit = defineEmits(['close', 'confirm']);
const deleteFolder = ref(false);
// Each merge starts with the folder kept; deleting it is never carried over.
watch(() => props.fork, () => { deleteFolder.value = false; });
const hasFolder = computed(() => !!props.fork?.workingDirectory);
const message = computed(() => mergeConfirmMessage(props.fork, props.parentName, deleteFolder.value));
</script>

<template>
  <Transition name="fade">
    <div v-if="fork" class="fixed inset-0 z-[100] overflow-y-auto" aria-labelledby="merge-modal-title" role="dialog" aria-modal="true">
      <div class="flex items-center justify-center min-h-screen pt-4 px-4 pb-20 text-center sm:block sm:p-0">
        <div class="fixed inset-0 bg-gray-900/60 backdrop-blur-sm transition-opacity" aria-hidden="true" @click="emit('close')"></div>
        <span class="hidden sm:inline-block sm:align-middle sm:h-screen" aria-hidden="true">&#8203;</span>

        <div class="inline-block relative z-[110] align-bottom bg-white dark:bg-zinc-900 rounded-sm text-left overflow-hidden shadow-2xl transform transition-all sm:my-8 sm:align-middle sm:max-w-md sm:w-full border border-gray-100 dark:border-zinc-800">
          <div class="bg-white dark:bg-zinc-900 px-6 pt-7 pb-6 sm:p-8 sm:pb-7">
            <div class="sm:flex sm:items-start">
              <div class="mx-auto shrink-0 flex items-center justify-center h-12 w-12 rounded-sm bg-gray-50 dark:bg-zinc-800 sm:mx-0 sm:h-10 sm:w-10 border border-gray-100 dark:border-zinc-700 mb-4 sm:mb-0">
                <ForkIcon class="h-5 w-5 text-gray-700 dark:text-zinc-300 rotate-180" />
              </div>
              <div class="mt-3 text-center sm:mt-0 sm:ml-4 sm:text-left w-full min-w-0">
                <h3 class="text-xl leading-8 font-bold text-gray-900 dark:text-zinc-50 tracking-tight break-words" id="merge-modal-title">
                  Merge into {{ parentName }}
                </h3>
                <p data-test="merge-message" class="mt-2 text-[14px] leading-relaxed text-gray-500 dark:text-zinc-400 font-medium break-words">{{ message }}</p>
                <label v-if="hasFolder" class="mt-4 flex items-start gap-2.5 cursor-pointer">
                  <input v-model="deleteFolder" data-test="merge-delete-folder" type="checkbox" :disabled="busy"
                         class="mt-0.5 shrink-0 rounded-sm border-gray-300 dark:border-zinc-600" />
                  <span class="text-[13px] leading-snug font-semibold text-gray-700 dark:text-zinc-300">Delete the fork's folder on the machine</span>
                </label>
              </div>
            </div>
          </div>

          <div class="bg-gray-50/50 dark:bg-zinc-800/50 px-6 py-5 sm:px-8 sm:flex sm:flex-row-reverse gap-3 border-t border-gray-100 dark:border-zinc-800">
            <button type="button" @click="emit('confirm', hasFolder && deleteFolder)" :disabled="busy"
                    class="w-full inline-flex justify-center rounded-sm px-6 py-2.5 bg-black dark:bg-white text-[10px] font-semibold text-white dark:text-black hover:bg-gray-800 dark:hover:bg-gray-100 disabled:opacity-40 disabled:cursor-not-allowed transition-all duration-200 sm:w-auto">
              {{ busy ? 'Merging…' : 'Merge' }}
            </button>
            <button type="button" @click="emit('close')"
                    class="mt-3 w-full inline-flex justify-center rounded-sm border border-gray-200 dark:border-zinc-700 px-6 py-2.5 bg-white dark:bg-zinc-900 text-[10px] font-semibold text-gray-700 dark:text-zinc-300 hover:bg-gray-50 dark:hover:bg-zinc-800 sm:mt-0 transition-all duration-200 sm:w-auto">
              Cancel
            </button>
          </div>
        </div>
      </div>
    </div>
  </Transition>
</template>

<style scoped>
.fade-enter-active, .fade-leave-active { transition: opacity 0.3s ease; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
</style>
