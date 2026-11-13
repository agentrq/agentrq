<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
const props = defineProps({
  show: Boolean,
  title: {
    type: String,
    default: 'Delete Task'
  },
  taskTitle: {
    type: String,
    default: ''
  },
  message: {
    type: String,
    default: ''
  },
  confirmLabel: {
    type: String,
    default: 'Delete'
  },
  // When set, the confirm button waits until this is typed: for what cannot
  // be undone and takes others' work with it, like deleting a team.
  confirmText: {
    type: String,
    default: ''
  }
})

import { ref, watch } from 'vue';
import { useFormat } from '../composables/useFormat';
const { toKebabCase } = useFormat();

const emit = defineEmits(['close', 'confirm'])
const typed = ref('')
// Empty each time it opens, so a name typed once is not still there.
watch(() => props.show, () => { typed.value = '' })

const closeModal = () => {
  emit('close')
}

const confirmDelete = () => {
  if (props.confirmText && typed.value !== props.confirmText) return
  emit('confirm')
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
                <div class="shrink-0 flex items-center justify-center h-12 w-12 rounded-sm bg-red-50 dark:bg-red-500/10 sm:h-10 sm:w-10 border border-red-100 dark:border-red-500/20 mb-4 sm:mb-0">
                  <svg class="h-5 w-5 text-red-600 dark:text-red-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" aria-hidden="true">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                  </svg>
                </div>

                <div class="mt-3 text-left sm:mt-0 sm:ml-4">
                  <h3 class="text-xl leading-8 font-bold text-gray-900 dark:text-zinc-50 tracking-tight" id="modal-title">
                    {{ title }}
                  </h3>
                  <div class="mt-2">
                    <p v-if="message" class="text-[14px] leading-relaxed text-gray-500 dark:text-zinc-400 font-medium">
                      {{ message }}
                    </p>
                    <p v-else class="text-[14px] leading-relaxed text-gray-500 dark:text-zinc-400 font-medium">
                      Are you sure you want to delete <span class="text-black dark:text-white font-semibold">{{ toKebabCase(taskTitle) }}</span>? This action cannot be undone.
                    </p>
                    <label v-if="confirmText" class="block mt-4">
                      <span class="block text-[12px] text-gray-500 dark:text-zinc-400 font-medium mb-1.5">Type <span class="font-semibold text-gray-900 dark:text-white">{{ confirmText }}</span> to confirm.</span>
                      <input v-model="typed" data-test="confirm-text" type="text" autocomplete="off" spellcheck="false" :aria-label="`Type ${confirmText} to confirm`"
                             class="w-full bg-gray-50 dark:bg-zinc-800/50 border border-gray-200 dark:border-zinc-700 rounded-sm px-3 py-2 text-sm focus:border-gray-900 dark:focus:border-white focus:ring-0 outline-none text-gray-900 dark:text-zinc-100" />
                    </label>
                  </div>
                </div>
              </div>
            </div>

            <div class="bg-gray-50/50 dark:bg-zinc-800/50 px-6 py-5 sm:px-8 sm:flex sm:flex-row-reverse gap-3 border-t border-gray-100 dark:border-zinc-800">
              <button type="button" @click="confirmDelete" :disabled="!!confirmText && typed !== confirmText"
                class="w-full inline-flex justify-center rounded-sm px-6 py-2.5 bg-red-600 text-[10px] font-semibold text-white hover:bg-red-700 transition-all duration-200 sm:w-auto disabled:opacity-40 disabled:cursor-not-allowed">
                {{ confirmLabel }}
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
