<template>
  <TransitionGroup
    tag="div"
    class="fixed bottom-6 right-6 z-[200] flex flex-col gap-3 pointer-events-none"
    enter-active-class="transition duration-300 ease-out"
    enter-from-class="transform translate-y-4 opacity-0"
    enter-to-class="transform translate-y-0 opacity-100"
    leave-active-class="transition duration-200 ease-in"
    leave-from-class="transform opacity-100"
    leave-to-class="transform opacity-0"
  >
    <div
      v-for="toast in toasts"
      :key="toast.id"
      class="pointer-events-auto min-w-[320px] max-w-md bg-white border border-gray-100 rounded-2xl shadow-2xl p-4 flex items-start gap-4 animate-in slide-in-from-right-10 overflow-hidden relative"
      :class="{
        'border-red-100 bg-red-50/10': toast.type === 'error',
        'border-green-100 bg-green-50/10': toast.type === 'success',
        'border-indigo-100 bg-indigo-50/10': toast.type === 'info'
      }"
    >
      <!-- Progress Bar -->
      <div 
        class="absolute bottom-0 left-0 h-1 bg-current opacity-10 transition-all duration-[3000ms] ease-linear"
        :style="{ width: '100%' }"
      ></div>

      <div class="shrink-0 mt-0.5">
        <div 
          v-if="toast.type === 'error'" 
          class="w-10 h-10 rounded-full bg-red-100 flex items-center justify-center text-red-600"
        >
          <svg class="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
            <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
          </svg>
        </div>
        <div 
          v-else-if="toast.type === 'success'" 
          class="w-10 h-10 rounded-full bg-green-100 flex items-center justify-center text-green-600"
        >
          <svg class="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
            <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
          </svg>
        </div>
        <div 
          v-else 
          class="w-10 h-10 rounded-full bg-indigo-100 flex items-center justify-center text-indigo-600"
        >
          <svg class="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
            <path stroke-linecap="round" stroke-linejoin="round" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
        </div>
      </div>

      <div class="flex-1 min-w-0 pr-2">
        <h4 
          class="text-xs font-black uppercase tracking-widest mb-1"
          :class="{
            'text-red-900': toast.type === 'error',
            'text-green-900': toast.type === 'success',
            'text-indigo-900': toast.type === 'info'
          }"
        >
          {{ toast.title || (toast.type === 'error' ? 'Security Alert' : toast.type === 'success' ? 'Success' : 'Notice') }}
        </h4>
        <p class="text-sm font-medium text-gray-600 leading-relaxed">{{ toast.message }}</p>
      </div>

      <button 
        @click="removeToast(toast.id)" 
        class="shrink-0 text-gray-300 hover:text-gray-900 transition-colors"
      >
        <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
          <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
        </svg>
      </button>
    </div>
  </TransitionGroup>
</template>

<script setup>
import { onMounted, onUnmounted } from 'vue';
import { useToasts } from '../composables/useToasts';

const { toasts, removeToast } = useToasts();
</script>
