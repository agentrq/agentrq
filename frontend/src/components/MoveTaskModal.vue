<script setup>
import { ref, computed, watch } from 'vue';
import { useWorkspaceStore } from '../stores/workspaceStore';
import { useFormat } from '../composables/useFormat';

const props = defineProps({
  show: Boolean,
  taskTitle: { type: String, default: '' },
  currentWorkspaceId: { type: [String, Number], default: null }
});

const emit = defineEmits(['close', 'confirm']);

const { toKebabCase } = useFormat();
const workspaceStore = useWorkspaceStore();
const destinationWorkspaceId = ref('');

const destinationOptions = computed(() =>
  workspaceStore.workspaces
    .filter(w => !w.archivedAt && String(w.id) !== String(props.currentWorkspaceId))
    .slice()
    .sort((a, b) => a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' }))
);

watch(() => props.show, (visible) => {
  if (visible) {
    if (workspaceStore.workspaces.length === 0) {
      workspaceStore.fetchWorkspaces();
    }
    destinationWorkspaceId.value = '';
  }
});

function closeModal() {
  emit('close');
}

function confirmMove() {
  if (!destinationWorkspaceId.value) return;
  emit('confirm', destinationWorkspaceId.value);
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
                    <div v-if="destinationOptions.length > 0"
                         class="max-h-56 overflow-y-auto rounded-sm border border-gray-200 dark:border-zinc-700 divide-y divide-gray-100 dark:divide-zinc-800">
                      <button v-for="w in destinationOptions" :key="w.id" type="button"
                              @click="destinationWorkspaceId = w.id"
                              class="w-full text-left px-3 py-2.5 text-[12px] font-medium transition-colors duration-150 focus:outline-none"
                              :class="String(destinationWorkspaceId) === String(w.id)
                                ? 'bg-black dark:bg-white text-white dark:text-black'
                                : 'bg-white dark:bg-zinc-900 text-gray-900 dark:text-zinc-100 hover:bg-gray-50 dark:hover:bg-zinc-800'">
                        {{ w.name }}
                      </button>
                    </div>
                    <p v-else class="mt-2 text-[11px] text-gray-500 dark:text-zinc-500">
                      No other workspaces available to move this task to.
                    </p>
                  </div>
                </div>
              </div>
            </div>

            <div class="bg-gray-50/50 dark:bg-zinc-800/50 px-6 py-5 sm:px-8 sm:flex sm:flex-row-reverse gap-3 border-t border-gray-100 dark:border-zinc-800">
              <button type="button" @click="confirmMove" :disabled="!destinationWorkspaceId"
                class="w-full inline-flex justify-center rounded-sm px-6 py-2.5 bg-black dark:bg-white text-[10px] font-semibold text-white dark:text-black hover:bg-gray-800 dark:hover:bg-gray-100 disabled:opacity-40 disabled:cursor-not-allowed transition-all duration-200 sm:w-auto">
                Move
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
