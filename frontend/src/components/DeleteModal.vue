<script setup>
defineProps({
  show: Boolean,
  title: {
    type: String,
    default: 'Delete Task'
  },
  taskTitle: {
    type: String,
    default: ''
  }
})

const emit = defineEmits(['close', 'confirm'])

const closeModal = () => {
  emit('close')
}

const confirmDelete = () => {
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
          <div v-if="show" class="inline-block relative z-[110] align-bottom bg-white rounded-3xl text-left overflow-hidden shadow-2xl transform transition-all sm:my-8 sm:align-middle sm:max-w-md sm:w-full border border-gray-100">
            <div class="bg-white px-6 pt-7 pb-6 sm:p-8 sm:pb-7">
              <div class="sm:flex sm:items-start">
                <div class="mx-auto shrink-0 flex items-center justify-center h-12 w-12 rounded-full bg-red-50 sm:mx-0 sm:h-10 sm:w-10 border border-red-100 mb-4 sm:mb-0">
                  <svg class="h-5 w-5 text-red-600" fill="none" viewBox="0 0 24 24" stroke="currentColor" aria-hidden="true">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                  </svg>
                </div>

                <div class="mt-3 text-center sm:mt-0 sm:ml-4 sm:text-left">
                  <h3 class="text-xl leading-8 font-bold text-gray-900 tracking-tight" id="modal-title">
                    {{ title }}
                  </h3>
                  <div class="mt-2">
                    <p class="text-[14px] leading-relaxed text-gray-500 font-medium">
                      Are you sure you want to delete <span class="text-black font-semibold">{{ taskTitle }}</span>? This action cannot be undone.
                    </p>
                  </div>
                </div>
              </div>
            </div>

            <div class="bg-gray-50/50 px-6 py-5 sm:px-8 sm:flex sm:flex-row-reverse gap-3">
              <button type="button" @click="confirmDelete"
                class="w-full inline-flex justify-center rounded-2xl shadow-lg shadow-red-200/50 px-6 py-2.5 bg-red-600 text-sm font-bold text-white hover:bg-red-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-red-500 transition-all duration-200 sm:w-auto">
                Delete
              </button>

              <button type="button" @click="closeModal"
                class="mt-3 w-full inline-flex justify-center rounded-2xl border border-gray-200 shadow-sm px-6 py-2.5 bg-white text-sm font-bold text-gray-700 hover:bg-gray-50 hover:border-gray-300 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-black sm:mt-0 transition-all duration-200 sm:w-auto">
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
