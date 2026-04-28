<template>
  <div v-if="toasts.length > 0" 
       class="fixed bottom-6 right-4 left-4 md:left-auto md:right-6 z-[200] md:w-[600px] border-2 border-black bg-white shadow-[6px_6px_0_0_rgba(0,0,0,1)] flex flex-col pointer-events-auto overflow-hidden animate-in fade-in slide-in-from-bottom-4 duration-300">
    
    <!-- Header -->
    <div class="bg-black text-white px-4 py-2.5 flex items-center justify-between shrink-0 border-b-2 border-black">
      <span class="text-[10px] font-black uppercase tracking-[0.2em] flex items-center gap-2">
        <span class="w-1.5 h-1.5 rounded-full bg-[#00FF88] pulse-green"></span>
        Notification Stream
      </span>
      <span class="text-[9px] font-black text-[#00FF88] uppercase tracking-widest px-1.5 py-0.5 border border-[#00FF88]">LIVE</span>
    </div>

    <!-- Scrollable Area -->
    <div class="max-h-[500px] overflow-y-auto overflow-x-hidden p-3 bg-gray-50/50">
      <TransitionGroup
        tag="div"
        class="flex flex-col gap-2"
        enter-active-class="transition duration-300 ease-out"
        enter-from-class="transform translate-x-4 opacity-0"
        enter-to-class="transform translate-x-0 opacity-100"
        leave-active-class="transition duration-200 ease-in"
        leave-from-class="transform opacity-100"
        leave-to-class="transform opacity-0"
      >
        <div
          v-for="(toast, index) in toasts"
          :key="toast.id"
          class="relative border-2 p-3 flex gap-3 items-start transition-all duration-300"
          :class="[
            index === toasts.length - 1 
              ? 'border-[#00FF88] bg-green-50 z-10' 
              : 'border-black bg-gray-50 opacity-80 scale-[0.98]'
          ]"
        >
          <!-- Item Progress Bar -->
          <div 
            class="absolute bottom-0 left-0 h-0.5 bg-black opacity-10 toast-progress-bar"
          ></div>

          <!-- Icon -->
          <div class="shrink-0 mt-0.5">
            <div 
              v-if="toast.type === 'error'" 
              class="w-5 h-5 flex items-center justify-center text-red-600"
            >
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3">
                <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
              </svg>
            </div>
            <div 
              v-else-if="toast.type === 'success'" 
              class="w-5 h-5 flex items-center justify-center text-green-600"
            >
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3.5">
                <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
              </svg>
            </div>
            <div 
              v-else 
              class="w-5 h-5 flex items-center justify-center text-gray-400"
            >
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3">
                <path stroke-linecap="round" stroke-linejoin="round" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
            </div>
          </div>

          <!-- Content -->
          <div class="flex-1 min-w-0">
            <div class="text-[8px] font-black uppercase tracking-widest text-gray-500 mb-0.5">
              {{ formatTime() }} · {{ toast.type === 'error' ? 'alert' : toast.type === 'success' ? 'success' : 'notice' }}
            </div>
            <h4 class="text-xs font-black uppercase tracking-tight text-black mb-0.5 truncate">
              {{ toast.title || (toast.type === 'error' ? 'Execution Error' : toast.type === 'success' ? 'Task Completed' : 'Update') }}
            </h4>
            <p class="text-[10px] font-bold text-gray-600 leading-tight">{{ toast.message }}</p>
          </div>

          <!-- Close -->
          <button 
            @click="removeToast(toast.id)" 
            class="shrink-0 text-gray-300 hover:text-black transition-colors -mt-1 -mr-1 p-1"
          >
            <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3">
              <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>
      </TransitionGroup>
    </div>
  </div>
</template>

<script setup>
import { useToasts } from '../composables/useToasts';

const { toasts, removeToast } = useToasts();

function formatTime() {
  return new Date().toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}
</script>

<style scoped>
.toast-progress-bar {
  animation: toast-progress 4s linear forwards;
}

@keyframes toast-progress {
  from { width: 100%; }
  to { width: 0%; }
}

@keyframes pulse-green {
  0%, 100% { box-shadow: 0 0 0 0 rgba(0, 255, 136, 0.4); }
  50% { box-shadow: 0 0 0 4px rgba(0, 255, 136, 0); }
}
.pulse-green { animation: pulse-green 2s ease-in-out infinite; }

/* Custom scrollbar for brutalist look */
.overflow-y-auto::-webkit-scrollbar {
  width: 4px;
}
.overflow-y-auto::-webkit-scrollbar-track {
  background: #f3f4f6;
}
.overflow-y-auto::-webkit-scrollbar-thumb {
  background: #000;
}
</style>
