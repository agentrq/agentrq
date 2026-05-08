<template>
  <Transition name="fade">
    <div v-if="show" class="fixed inset-0 z-[100] flex items-center justify-center p-4 bg-black/40 backdrop-blur-sm">
      <div class="bg-white dark:bg-zinc-900 rounded-sm border border-gray-200 dark:border-zinc-800 w-full max-w-2xl overflow-hidden animate-in zoom-in-95 duration-200">
        <!-- Header -->
        <div class="px-8 py-6 border-b border-gray-100 dark:border-zinc-800 bg-gray-50/50 dark:bg-zinc-800/50 flex justify-between items-center">
          <div class="flex items-center gap-3">
            <div class="w-10 h-10 rounded-sm bg-black dark:bg-white flex items-center justify-center text-white dark:text-black">
              <svg class="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>
            </div>
            <div>
              <h2 class="text-xl font-bold text-gray-900 leading-tight">Connect Agent</h2>
              <p class="text-[11px] font-bold text-gray-500 mt-0.5">Setup Guide & Best Practices</p>
            </div>
          </div>
          <button @click="$emit('close')" class="text-gray-500 dark:text-zinc-400 hover:text-black dark:hover:text-white transition-colors p-2 rounded-md hover:bg-gray-100 dark:hover:bg-zinc-800">
            <svg class="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
          </button>
        </div>

        <!-- Tabs -->
        <div class="px-8 pt-4 flex gap-6 border-b border-gray-100 dark:border-zinc-800">
          <button @click="activeTab = 'claude'" 
                  :class="activeTab === 'claude' ? 'text-black border-black font-bold' : 'text-gray-500 border-transparent font-bold hover:text-gray-600'"
                  class="pb-3 text-[11px] border-b-2 transition-all">
            Claude
          </button>
          <button @click="activeTab = 'gemini'" 
                  :class="activeTab === 'gemini' ? 'text-black border-black font-bold' : 'text-gray-500 border-transparent font-bold hover:text-gray-600'"
                  class="pb-3 text-[11px] border-b-2 transition-all">
            Gemini / ACP
          </button>
        </div>

        <!-- Content -->
        <div class="p-8 space-y-8 overflow-y-auto max-h-[60vh] custom-scrollbar">
          <section class="space-y-4">
            <h3 class="text-sm font-bold text-gray-900 flex items-center gap-2">
              <span class="w-5 h-5 rounded-full bg-indigo-50 text-indigo-600 flex items-center justify-center text-[10px]">1</span>
              Recommended Strategy
            </h3>
            <p class="text-[13px] text-gray-600 leading-relaxed font-medium">
              We recommend creating a <code class="bg-gray-100 px-1.5 py-0.5 rounded text-indigo-600 font-bold">.mcp.json</code> (dot is required as prefix) file in each of your local project directories. This ensures that each instance is isolated and only responsible for its specific workspace.
            </p>
          </section>

          <section class="space-y-4">
            <h3 class="text-sm font-bold text-gray-900 flex items-center gap-2">
              <span class="w-5 h-5 rounded-full bg-indigo-50 text-indigo-600 flex items-center justify-center text-[10px]">2</span>
              Configuration
            </h3>
            <div class="bg-zinc-900 rounded-sm p-5 relative group">
              <div class="flex justify-between items-center mb-4">
                <span class="text-[10px] font-semibold text-zinc-500">.mcp.json</span>
                <div class="flex items-center gap-3">
                  <button @click="copyConfig" class="text-[10px] font-semibold text-zinc-400 hover:text-white transition-colors flex items-center gap-1.5">
                    {{ isCopied ? 'Copied!' : 'Copy Config' }}
                  </button>
                </div>
              </div>
              <pre class="text-[12px] text-zinc-300 font-mono leading-relaxed overflow-x-auto"><code>{{ configJson }}</code></pre>
            </div>
          </section>

          <section v-if="activeTab === 'claude'" class="space-y-4">
            <h3 class="text-sm font-bold text-gray-900 flex items-center gap-2">
              <span class="w-5 h-5 rounded-full bg-indigo-50 text-indigo-600 flex items-center justify-center text-[10px]">3</span>
              Claude Permissions
            </h3>
            <p class="text-[13px] text-gray-600 leading-relaxed font-medium">
              To enable Claude to use this MCP server without permission prompts, add a
              <code class="bg-gray-100 px-1.5 py-0.5 rounded text-indigo-600 font-bold">.claude/settings.local.json</code>
              file in your project directory:
            </p>
            <div class="bg-zinc-900 rounded-sm p-5 relative group">
              <div class="flex justify-between items-center mb-4">
                <span class="text-[10px] font-semibold text-zinc-500">.claude/settings.local.json</span>
                <button @click="copyPermissionsConfig" class="text-[10px] font-semibold text-zinc-400 hover:text-white transition-colors flex items-center gap-1.5">
                  {{ isPermissionsCopied ? 'Copied!' : 'Copy Config' }}
                </button>
              </div>
              <pre class="text-[12px] text-zinc-300 font-mono leading-relaxed overflow-x-auto"><code>{{ permissionsConfigJson }}</code></pre>
            </div>
          </section>

          <section class="space-y-4 bg-indigo-50/50 p-6 rounded-sm border border-indigo-100/50">
            <h3 class="text-xs font-semibold text-indigo-600 flex items-center gap-2">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path d="M13 10V3L4 14h7v7l9-11h-7z" /></svg>
              Quick Startup
            </h3>
            
            <template v-if="activeTab === 'claude'">
              <p class="text-[13px] text-indigo-950/70 font-medium leading-relaxed">
                Once the files are created, run the following command in your terminal:
              </p>
              <div class="bg-white/80 p-3 rounded-sm border border-indigo-100 flex items-center justify-between group">
                <code class="text-[11px] text-indigo-600 font-bold overflow-hidden text-ellipsis">{{ startCommand }}</code>
                <button @click="copyCommand" class="group-hover:opacity-100 text-[9px] font-semibold pl-2 transition-all"
                        :class="isCommandCopied ? 'text-green-500 opacity-100' : 'opacity-0 text-indigo-500'">
                  {{ isCommandCopied ? 'Copied!' : 'Copy' }}
                </button>
              </div>
            </template>

            <template v-else>
              <p class="text-[13px] text-indigo-950/70 font-medium leading-relaxed">
                Run these commands to install and start the AgentRQ ACP Gateway with gemini cli:
              </p>
              <div class="space-y-2">
                <div class="bg-white/80 p-3 rounded-sm border border-indigo-100 flex items-center justify-between group">
                  <code class="text-[11px] text-indigo-600 font-bold overflow-hidden text-ellipsis">npm install -g @agentrq/acp-gateway@latest</code>
                  <button @click="copyToClipboard('npm install -g @agentrq/acp-gateway@latest', 'isGatewayInstalled')" 
                          class="group-hover:opacity-100 text-[9px] font-semibold pl-2 transition-all"
                          :class="isGatewayInstalled ? 'text-green-500 opacity-100' : 'opacity-0 text-indigo-500'">
                    {{ isGatewayInstalled ? 'Copied!' : 'Copy' }}
                  </button>
                </div>
                <div class="bg-white/80 p-3 rounded-sm border border-indigo-100 flex items-center justify-between group">
                  <code class="text-[11px] text-indigo-600 font-bold overflow-hidden text-ellipsis">npx @agentrq/acp-gateway -- gemini acp</code>
                  <button @click="copyToClipboard('npx @agentrq/acp-gateway -- gemini acp', 'isGatewayStarted')" 
                          class="group-hover:opacity-100 text-[9px] font-semibold pl-2 transition-all"
                          :class="isGatewayStarted ? 'text-green-500 opacity-100' : 'opacity-0 text-indigo-500'">
                    {{ isGatewayStarted ? 'Copied!' : 'Copy' }}
                  </button>
                </div>
              </div>
            </template>
          </section>
        </div>

        <!-- Footer -->
        <div class="px-8 py-6 bg-gray-50/50 dark:bg-zinc-800/50 border-t border-gray-100 dark:border-zinc-800 flex justify-end items-center gap-4">
           <span class="text-[10px] font-semibold text-gray-500 dark:text-zinc-500 flex-1">Isolated. Secure. Collaborative.</span>
           <button @click="$emit('close')" class="bg-black dark:bg-white text-white dark:text-zinc-900 px-8 py-3 rounded-sm text-[11px] font-bold hover:bg-zinc-800 dark:hover:bg-zinc-200 transition-all active:scale-95">
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
const isPermissionsCopied = ref(false);
const isCommandCopied = ref(false);
const isGatewayInstalled = ref(false);
const isGatewayStarted = ref(false);
const token = ref('');
const activeTab = ref('claude');

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

const mcpConfig = computed(() => ({
  mcpServers: {
    [serverName.value]: {
      type: "http",
      url: authenticatedUrl.value
    }
  }
}));

const configJson = computed(() => JSON.stringify(mcpConfig.value, null, 2));

const permissionsConfig = computed(() => ({
  permissions: {
    allow: [
      `mcp__${serverName.value}__updateTaskStatus`,
      `mcp__${serverName.value}__getWorkspace`,
      `mcp__${serverName.value}__reply`,
      `mcp__${serverName.value}__createTask`,
      `mcp__${serverName.value}__downloadAttachment`,
      `mcp__${serverName.value}__getTaskMessages`,
      `mcp__${serverName.value}__getNextTask`,
    ]
  },
  enableAllProjectMcpServers: true,
  enabledMcpjsonServers: [serverName.value]
}));

const permissionsConfigJson = computed(() => JSON.stringify(permissionsConfig.value, null, 2));

function copyConfig() {
  navigator.clipboard.writeText(configJson.value);
  isCopied.value = true;
  setTimeout(() => isCopied.value = false, 2000);
}

function copyPermissionsConfig() {
  navigator.clipboard.writeText(permissionsConfigJson.value);
  isPermissionsCopied.value = true;
  setTimeout(() => isPermissionsCopied.value = false, 2000);
}

function copyCommand() {
  navigator.clipboard.writeText(startCommand.value);
  isCommandCopied.value = true;
  setTimeout(() => isCommandCopied.value = false, 2000);
}

function copyToClipboard(text, flagRefName) {
  navigator.clipboard.writeText(text);
  if (flagRefName === 'isGatewayInstalled') {
    isGatewayInstalled.value = true;
    setTimeout(() => isGatewayInstalled.value = false, 2000);
  } else if (flagRefName === 'isGatewayStarted') {
    isGatewayStarted.value = true;
    setTimeout(() => isGatewayStarted.value = false, 2000);
  }
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
