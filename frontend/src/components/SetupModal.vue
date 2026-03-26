<template>
  <Transition name="fade">
    <div v-if="show" class="fixed inset-0 z-[100] flex items-center justify-center p-4 bg-black/40 backdrop-blur-sm">
      <div class="bg-white rounded-xl border border-gray-200 w-full max-w-2xl overflow-hidden animate-in zoom-in-95 duration-200">
        <!-- Header -->
        <div class="px-8 py-6 border-b border-gray-100 bg-gray-50/50 flex justify-between items-center">
          <div class="flex items-center gap-3">
            <div class="w-10 h-10 rounded-xl bg-black flex items-center justify-center text-white">
              <svg class="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>
            </div>
            <div>
              <h2 class="text-xl font-bold text-gray-900 leading-tight">Connect to Claude</h2>
              <p class="text-[11px] font-bold text-gray-400 uppercase tracking-widest mt-0.5">Setup Guide & Best Practices</p>
            </div>
          </div>
          <button @click="$emit('close')" class="text-gray-400 hover:text-black transition-colors p-2 rounded-lg hover:bg-gray-100">
            <svg class="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
          </button>
        </div>

        <!-- Content -->
        <div class="p-8 space-y-8 overflow-y-auto max-h-[70vh] custom-scrollbar">
          <section class="space-y-4">
            <h3 class="text-sm font-black text-gray-900 uppercase tracking-widest flex items-center gap-2">
              <span class="w-5 h-5 rounded-full bg-indigo-50 text-indigo-600 flex items-center justify-center text-[10px]">1</span>
              Recommended Strategy
            </h3>
            <p class="text-[13px] text-gray-600 leading-relaxed font-medium">
              We recommend creating a <code class="bg-gray-100 px-1.5 py-0.5 rounded text-indigo-600 font-bold">.mcp.json</code>(dot is required as prefix) file in each of your local project directories. This ensures that each Claude / Claude-Code instance is isolated and only responsible for its specific workspace.
            </p>
          </section>

          <section class="space-y-4">
            <h3 class="text-sm font-black text-gray-900 uppercase tracking-widest flex items-center gap-2">
              <span class="w-5 h-5 rounded-full bg-indigo-50 text-indigo-600 flex items-center justify-center text-[10px]">2</span>
              Configuration
            </h3>
            <div class="bg-zinc-900 rounded-xl p-5 relative group">
              <div class="flex justify-between items-center mb-4">
                <span class="text-[10px] font-black text-zinc-500 uppercase tracking-widest">.mcp.json</span>
                <div class="flex items-center gap-3">
                  <button @click="copyConfig" class="text-[10px] font-black text-zinc-400 hover:text-white uppercase tracking-widest transition-colors flex items-center gap-1.5">
                    {{ isCopied ? 'Copied!' : 'Copy Config' }}
                  </button>
                </div>
              </div>
              <pre class="text-[12px] text-zinc-300 font-mono leading-relaxed overflow-x-auto"><code>{
  "mcpServers": {
    "{{ serverName }}": {
      "type": "http",
      "url": "{{ authenticatedUrl }}"
    }
  }
}</code></pre>
            </div>
          </section>

          <section class="space-y-4 bg-indigo-50/50 p-6 rounded-2xl border border-indigo-100/50">
            <h3 class="text-xs font-black text-indigo-600 uppercase tracking-widest flex items-center gap-2">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path d="M13 10V3L4 14h7v7l9-11h-7z" /></svg>
              Quick Startup
            </h3>
            <p class="text-[13px] text-indigo-950/70 font-medium leading-relaxed">
              Once the file is created, run the following command in your terminal:
            </p>
            <div class="bg-white/80 p-3 rounded-lg border border-indigo-100 flex items-center justify-between group">
              <code class="text-[11px] text-indigo-600 font-bold overflow-hidden text-ellipsis">{{ startCommand }}</code>
              <button @click="copyCommand" class="opacity-0 group-hover:opacity-100 text-[9px] font-black text-indigo-500 uppercase tracking-widest pl-2">Copy</button>
            </div>
          </section>
        </div>

        <!-- Footer -->
        <div class="px-8 py-6 bg-gray-50/50 border-t border-gray-100 flex justify-end items-center gap-4">
           <span class="text-[10px] font-bold text-gray-400 uppercase tracking-widest flex-1">Isolated. Secure. Collaborative.</span>
           <button @click="$emit('close')" class="bg-black text-white px-8 py-3 rounded-lg text-[11px] font-black uppercase tracking-widest hover:bg-zinc-800 transition-all active:scale-95">
             Got it
           </button>
        </div>
      </div>
    </div>
  </Transition>
</template>

<script setup>
import { ref, computed, watch } from 'vue';
import { getWorkspaceToken } from '../api';

const props = defineProps({
  show: Boolean,
  mcpUrl: String,
  workspaceId: [String, Number]
});

const isCopied = ref(false);
const token = ref('');

const authenticatedUrl = computed(() => {
  if (!token.value) return props.mcpUrl;
  return `${props.mcpUrl}?token=${token.value}`;
});

watch([() => props.show, () => props.workspaceId, () => props.mcpUrl], async ([newShow, newId, newUrl]) => {
  if (newShow && newId && newUrl) {
    try {
      const res = await getWorkspaceToken(newId);
      token.value = res.token || '';
    } catch (err) {
      console.error('Failed to fetch token:', err);
    }
  }
}, { immediate: true });

const serverName = computed(() => `agentrq-${props.workspaceId}`);
const startCommand = computed(() => `claude --dangerously-load-development-channels server:${serverName.value}`);

function copyConfig() {
  const config = JSON.stringify({
    mcpServers: {
      [serverName.value]: {
        type: "http",
        url: authenticatedUrl.value
      }
    }
  }, null, 2);
  
  navigator.clipboard.writeText(config);
  isCopied.value = true;
  setTimeout(() => isCopied.value = false, 2000);
}

function copyCommand() {
  navigator.clipboard.writeText(startCommand.value);
}
</script>

<style scoped>
.fade-enter-active, .fade-leave-active { transition: opacity 0.2s ease; }
.fade-enter-from, .fade-leave-to { opacity: 0; }

.custom-scrollbar::-webkit-scrollbar { width: 5px; }
.custom-scrollbar::-webkit-scrollbar-track { background: transparent; }
.custom-scrollbar::-webkit-scrollbar-thumb { background: #e5e7eb; border-radius: 10px; }
.custom-scrollbar::-webkit-scrollbar-thumb:hover { background: #d1d5db; }
</style>
