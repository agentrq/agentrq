<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial
-->

<!-- Naming a new workspace fork. Laid out like the Move Task dialog, which is
     the other thing done to a workspace from a menu. -->
<script setup>
import { nextTick, ref, watch } from 'vue';
import ForkIcon from './ForkIcon.vue';
import { MAX_WORKSPACE_NAME, kebabName } from '../composables/useWorkspaceForks';
import { useFormat } from '../composables/useFormat';

const props = defineProps({
  show: Boolean,
  parentName: { type: String, default: '' },
  modelValue: { type: String, default: '' },
  busy: Boolean
});

const emit = defineEmits(['close', 'confirm', 'update:modelValue']);
const input = ref(null);
const { liveKebabCase } = useFormat();

watch(() => props.show, async (open) => {
  if (!open) return;
  await nextTick();
  input.value?.focus();
  input.value?.select();
});

// Named as the workspace form names a workspace: kebab-case as it is typed.
// The field is rewritten too, since a keystroke that is dropped leaves the
// model, and so the prop, unchanged.
function onInput(event) {
  const formatted = liveKebabCase(event.target.value);
  event.target.value = formatted;
  emit('update:modelValue', formatted);
}

function confirm() {
  if (!kebabName(props.modelValue) || props.busy) return;
  emit('confirm', props.modelValue);
}
</script>

<template>
  <Transition name="fade">
    <div v-if="show" class="fixed inset-0 z-[100] overflow-y-auto" aria-labelledby="fork-modal-title" role="dialog" aria-modal="true">
      <div class="flex items-center justify-center min-h-screen pt-4 px-4 pb-20 text-center sm:block sm:p-0">
        <div class="fixed inset-0 bg-gray-900/60 backdrop-blur-sm transition-opacity" aria-hidden="true" @click="emit('close')"></div>
        <span class="hidden sm:inline-block sm:align-middle sm:h-screen" aria-hidden="true">&#8203;</span>

        <form @submit.prevent="confirm"
              class="inline-block relative z-[110] align-bottom bg-white dark:bg-zinc-900 rounded-sm text-left overflow-hidden shadow-2xl transform transition-all sm:my-8 sm:align-middle sm:max-w-md sm:w-full border border-gray-100 dark:border-zinc-800">
          <div class="bg-white dark:bg-zinc-900 px-6 pt-7 pb-6 sm:p-8 sm:pb-7">
            <div class="sm:flex sm:items-start">
              <div class="mx-auto shrink-0 flex items-center justify-center h-12 w-12 rounded-sm bg-gray-50 dark:bg-zinc-800 sm:mx-0 sm:h-10 sm:w-10 border border-gray-100 dark:border-zinc-700 mb-4 sm:mb-0">
                <ForkIcon class="h-5 w-5 text-gray-700 dark:text-zinc-300" />
              </div>
              <div class="mt-3 text-center sm:mt-0 sm:ml-4 sm:text-left w-full min-w-0">
                <h3 class="text-xl leading-8 font-bold text-gray-900 dark:text-zinc-50 tracking-tight" id="fork-modal-title">Fork workspace</h3>
                <p class="mt-2 text-[14px] leading-relaxed text-gray-500 dark:text-zinc-400 font-medium">
                  A workspace of its own, with its own queue and agent. Settings, memory and skills stay
                  <span class="text-black dark:text-white font-semibold">{{ parentName }}</span>'s, and it is merged back when its tasks are done.
                </p>
                <label for="fork-name" class="block mt-4 text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest">Name</label>
                <input id="fork-name" ref="input" type="text" :value="modelValue" :maxlength="MAX_WORKSPACE_NAME"
                       @input="onInput"
                       spellcheck="false" autocapitalize="off" autocorrect="off"
                       class="mt-1 w-full bg-gray-50 dark:bg-zinc-800/50 border border-gray-200 dark:border-zinc-800 rounded-sm px-3 py-2 text-sm focus:border-gray-900 dark:focus:border-white focus:ring-0 outline-none font-semibold text-gray-900 dark:text-zinc-100" />
              </div>
            </div>
          </div>

          <div class="bg-gray-50/50 dark:bg-zinc-800/50 px-6 py-5 sm:px-8 sm:flex sm:flex-row-reverse gap-3 border-t border-gray-100 dark:border-zinc-800">
            <button type="submit" :disabled="!kebabName(modelValue) || busy"
                    class="w-full inline-flex justify-center rounded-sm px-6 py-2.5 bg-black dark:bg-white text-[10px] font-semibold text-white dark:text-black hover:bg-gray-800 dark:hover:bg-gray-100 disabled:opacity-40 disabled:cursor-not-allowed transition-all duration-200 sm:w-auto">
              {{ busy ? 'Forking…' : 'Fork' }}
            </button>
            <button type="button" @click="emit('close')"
                    class="mt-3 w-full inline-flex justify-center rounded-sm border border-gray-200 dark:border-zinc-700 px-6 py-2.5 bg-white dark:bg-zinc-900 text-[10px] font-semibold text-gray-700 dark:text-zinc-300 hover:bg-gray-50 dark:hover:bg-zinc-800 sm:mt-0 transition-all duration-200 sm:w-auto">
              Cancel
            </button>
          </div>
        </form>
      </div>
    </div>
  </Transition>
</template>

<style scoped>
.fade-enter-active, .fade-leave-active { transition: opacity 0.3s ease; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
</style>
