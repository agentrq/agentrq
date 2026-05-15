<template>
  <div class="flex flex-col h-full w-full min-w-0 bg-transparent overflow-y-auto custom-scrollbar">
    <!-- Main Content Grid -->
    <div class="flex-1 px-4 pb-20 min-w-0">
      <div class="w-full min-w-0 flex flex-col md:flex-row gap-8">
        
        <!-- Sidebar Navigation -->
        <div class="w-full md:w-48 shrink-0">
          <nav class="flex flex-col gap-1 sticky top-0">
            <button v-for="tab in navItems" :key="tab.id"
                    @click="activeTab = tab.id"
                    :class="[activeTab === tab.id ? 'bg-gray-900 text-white dark:bg-white dark:text-zinc-900 shadow-lg shadow-black/5' : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-100 dark:hover:bg-zinc-800']"
                    class="flex items-center gap-3 px-4 py-2.5 rounded-sm text-[10px] font-bold uppercase tracking-widest transition-all text-left">
              <div class="w-4 h-4 flex items-center justify-center shrink-0">
                <div v-if="tab.icon.startsWith('<')" v-html="tab.icon" class="w-full h-full flex items-center justify-center"></div>
                <svg v-else class="w-full h-full" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                  <path stroke-linecap="round" stroke-linejoin="round" :d="tab.icon" />
                </svg>
              </div>
              {{ tab.label }}
            </button>
            <div class="my-4 border-t border-gray-100 dark:border-zinc-800"></div>
            <button @click="activeTab = 'danger'"
                    :class="[activeTab === 'danger' ? 'bg-red-600 text-white shadow-lg shadow-red-500/20' : 'text-red-500 hover:bg-red-50 dark:hover:bg-red-500/10']"
                    class="flex items-center gap-3 px-4 py-2.5 rounded-sm text-[10px] font-bold uppercase tracking-widest transition-all text-left">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
              </svg>
              Danger Zone
            </button>
          </nav>
        </div>

        <!-- Form Area -->
        <div class="flex-1 min-w-0">
          <div class="bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-sm shadow-sm overflow-hidden min-h-[500px]">
            <form @submit.prevent="save" class="h-full flex flex-col min-w-0">
              <div class="p-8 flex-1 min-w-0">
                
                <!-- General Settings -->
                <div v-if="activeTab === 'general'" class="space-y-8 animate-in fade-in slide-in-from-bottom-2 duration-300">
                  <div class="space-y-6">
                    <div class="space-y-2">
                      <label class="block text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest ml-1">Workspace Name</label>
                      <input v-model="form.name" @blur="form.name = toKebabCase(form.name)" type="text" required class="w-full bg-gray-50 dark:bg-zinc-800/50 border border-gray-200 dark:border-zinc-800 rounded-sm px-4 py-3 text-sm focus:border-gray-900 dark:focus:border-white focus:ring-0 outline-none font-bold text-gray-900 dark:text-zinc-100 transition-all shadow-sm" placeholder="e.g. project-redstone" />
                    </div>
                    <div class="space-y-2">
                      <label class="block text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest ml-1">Mission Description</label>
                      <textarea v-model="form.description" rows="3" class="w-full bg-gray-50 dark:bg-zinc-800/50 border border-gray-200 dark:border-zinc-800 rounded-sm px-4 py-3 text-sm focus:border-gray-900 dark:focus:border-white focus:ring-0 outline-none font-medium text-gray-800 dark:text-zinc-200 transition-all resize-none shadow-sm" placeholder="What are we building together?"></textarea>
                    </div>
                  </div>

                  <div class="space-y-2">
                    <label class="block text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest ml-1">Self-Learning Strategy</label>
                    <textarea v-model="form.selfLearningLoopNote" rows="6" class="w-full bg-gray-50 dark:bg-zinc-800/50 border border-gray-200 dark:border-zinc-800 rounded-sm px-4 py-3 text-sm focus:border-gray-900 dark:focus:border-white focus:ring-0 outline-none font-medium text-gray-800 dark:text-zinc-200 transition-all resize-none shadow-sm" placeholder="Extract successful workarounds and record them in skills md files..."></textarea>
                    <p class="text-[9px] text-gray-500 dark:text-zinc-500 font-bold uppercase tracking-wider ml-1 mt-2">Guidance for the agent to optimize its strategy over time.</p>
                  </div>
                </div>

                <!-- Setup -->
                <div v-if="activeTab === 'setup'" class="space-y-8 animate-in fade-in slide-in-from-bottom-2 duration-300">
                  <div class="flex gap-4 border-b border-gray-100 dark:border-zinc-800 pb-4">
                    <button type="button" @click="activeConnectionTab = 'claude'" :class="activeConnectionTab === 'claude' ? 'text-black dark:text-white border-black dark:border-white' : 'text-gray-400 border-transparent hover:text-gray-600 dark:hover:text-zinc-300'" class="pb-2 text-[10px] font-bold uppercase tracking-widest border-b-2 transition-all">Claude</button>
                    <button type="button" @click="activeConnectionTab = 'gemini'" :class="activeConnectionTab === 'gemini' ? 'text-black dark:text-white border-black dark:border-white' : 'text-gray-400 border-transparent hover:text-gray-600 dark:hover:text-zinc-300'" class="pb-2 text-[10px] font-bold uppercase tracking-widest border-b-2 transition-all">Gemini / ACP</button>
                  </div>

                  <section class="space-y-4 min-w-0 w-full overflow-hidden">
                    <h3 class="text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest ml-1">1. Configuration</h3>
                    <div class="bg-gray-50 dark:bg-zinc-800/50 rounded-sm p-5 relative group border border-gray-200 dark:border-zinc-800 w-full max-w-full overflow-hidden">
                      <div class="flex justify-between items-center mb-4">
                        <span class="text-[10px] font-semibold text-gray-500 dark:text-zinc-500 font-mono">.mcp.json</span>
                        <button type="button" @click="copyToClipboard(configJson, 'config')" class="text-[10px] font-bold text-gray-400 hover:text-black dark:text-zinc-400 dark:hover:text-white transition-colors uppercase tracking-widest">
                          {{ copiedState.config ? 'Copied!' : 'Copy Config' }}
                        </button>
                      </div>
                      <pre class="text-[11px] text-gray-800 dark:text-zinc-300 font-mono leading-relaxed p-1 overflow-x-auto custom-scrollbar whitespace-pre max-w-full block"><code>{{ configJson }}</code></pre>
                    </div>
                  </section>

                  <section v-if="activeConnectionTab === 'claude'" class="space-y-4 min-w-0 w-full overflow-hidden">
                    <h3 class="text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest ml-1">2. Claude Permissions</h3>
                    <p class="text-[11px] text-gray-500 dark:text-zinc-400 font-medium px-1">Add this to <code class="bg-gray-100 dark:bg-zinc-800 px-1 py-0.5 rounded text-gray-900 dark:text-white">.claude/settings.local.json</code> to bypass permission prompts.</p>
                    <div class="bg-gray-50 dark:bg-zinc-800/50 rounded-sm p-5 relative group border border-gray-200 dark:border-zinc-800 w-full max-w-full overflow-hidden">
                      <div class="flex justify-between items-center mb-4">
                        <span class="text-[10px] font-semibold text-gray-500 dark:text-zinc-500 font-mono">settings.local.json</span>
                        <button type="button" @click="copyToClipboard(permissionsConfigJson, 'permissions')" class="text-[10px] font-bold text-gray-400 hover:text-black dark:text-zinc-400 dark:hover:text-white transition-colors uppercase tracking-widest">
                          {{ copiedState.permissions ? 'Copied!' : 'Copy Config' }}
                        </button>
                      </div>
                      <pre class="text-[11px] text-gray-800 dark:text-zinc-300 font-mono leading-relaxed p-1 overflow-x-auto custom-scrollbar whitespace-pre max-w-full block"><code>{{ permissionsConfigJson }}</code></pre>
                    </div>
                  </section>

                  <section class="space-y-4 bg-gray-50 dark:bg-zinc-800/30 p-6 rounded-sm border border-gray-100 dark:border-zinc-800">
                    <h3 class="text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest flex items-center gap-2">
                      <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path d="M13 10V3L4 14h7v7l9-11h-7z" /></svg>
                      {{ activeConnectionTab === 'claude' ? 'Start Command' : 'ACP Gateway Setup' }}
                    </h3>
                    
                    <div v-if="activeConnectionTab === 'claude'" class="space-y-3 min-w-0">
                      <p class="text-[11px] text-gray-600 dark:text-zinc-400 font-medium">Run this in your terminal to start the MCP server:</p>
                      <div class="bg-white dark:bg-zinc-900 p-3 rounded-sm border border-gray-200 dark:border-zinc-700 flex items-center justify-between group shadow-sm overflow-hidden">
                        <div class="flex-1 min-w-0 overflow-x-auto no-scrollbar">
                          <code class="text-[10px] text-gray-900 dark:text-white font-bold whitespace-nowrap">{{ startCommand }}</code>
                        </div>
                        <button type="button" @click="copyToClipboard(startCommand, 'command')" class="text-[9px] font-bold uppercase tracking-widest pl-4 shrink-0 transition-colors" :class="copiedState.command ? 'text-green-500' : 'text-gray-400 hover:text-black dark:hover:text-white'">
                          {{ copiedState.command ? 'Copied!' : 'Copy' }}
                        </button>
                      </div>
                    </div>

                    <div v-else class="space-y-4 min-w-0">
                      <div class="space-y-2">
                        <p class="text-[11px] text-gray-600 dark:text-zinc-400 font-medium">1. Install the gateway:</p>
                        <div class="bg-white dark:bg-zinc-900 p-3 rounded-sm border border-gray-200 dark:border-zinc-700 flex items-center justify-between group shadow-sm overflow-hidden">
                          <div class="flex-1 min-w-0 overflow-x-auto no-scrollbar">
                            <code class="text-[10px] text-gray-900 dark:text-white font-bold whitespace-nowrap">npm install -g @agentrq/acp-gateway@latest</code>
                          </div>
                          <button type="button" @click="copyToClipboard('npm install -g @agentrq/acp-gateway@latest', 'gatewayInstall')" class="text-[9px] font-bold uppercase tracking-widest pl-4 shrink-0 transition-colors" :class="copiedState.gatewayInstall ? 'text-green-500' : 'text-gray-400 hover:text-black dark:hover:text-white'">
                            {{ copiedState.gatewayInstall ? 'Copied!' : 'Copy' }}
                          </button>
                        </div>
                      </div>
                      <div class="space-y-2">
                        <p class="text-[11px] text-gray-600 dark:text-zinc-400 font-medium">2. Start the bridge:</p>
                        <div class="bg-white dark:bg-zinc-900 p-3 rounded-sm border border-gray-200 dark:border-zinc-700 flex items-center justify-between group shadow-sm overflow-hidden">
                          <div class="flex-1 min-w-0 overflow-x-auto no-scrollbar">
                            <code class="text-[10px] text-gray-900 dark:text-white font-bold whitespace-nowrap">npx @agentrq/acp-gateway -- gemini acp</code>
                          </div>
                          <button type="button" @click="copyToClipboard('npx @agentrq/acp-gateway -- gemini acp', 'gatewayStart')" class="text-[9px] font-bold uppercase tracking-widest pl-4 shrink-0 transition-colors" :class="copiedState.gatewayStart ? 'text-green-500' : 'text-gray-400 hover:text-black dark:hover:text-white'">
                            {{ copiedState.gatewayStart ? 'Copied!' : 'Copy' }}
                          </button>
                        </div>
                      </div>
                    </div>
                  </section>
                </div>

                <!-- Automations -->
                <div v-if="activeTab === 'automations'" class="space-y-8 animate-in fade-in slide-in-from-bottom-2 duration-300">
                  <div class="space-y-4">
                    <div class="flex items-center justify-between">
                      <h3 class="text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest ml-1">Auto-Allow List</h3>
                      <span class="text-[9px] font-bold text-gray-400 bg-gray-100 dark:bg-zinc-800 px-2 py-0.5 rounded border border-gray-200 dark:border-zinc-700 uppercase">{{ form.autoAllowedTools.length }} Active</span>
                    </div>
                    
                    <p class="text-[11px] text-gray-500 dark:text-zinc-400 leading-relaxed px-1 font-medium">
                      These tools will execute autonomously without requiring manual confirmation. Trusted tools speed up execution significantly.
                    </p>

                    <div v-if="form.autoAllowedTools.length > 0" class="grid grid-cols-1 gap-2 mt-4">
                      <div v-for="tool in form.autoAllowedTools" :key="tool" class="flex items-center justify-between p-4 bg-gray-50 dark:bg-zinc-800/50 rounded-sm border border-gray-100 dark:border-zinc-800 group hover:border-gray-900 dark:hover:border-white transition-all shadow-sm">
                        <div class="flex items-center gap-4">
                          <div class="p-2 bg-white dark:bg-zinc-800 rounded-sm shadow-sm border border-gray-100 dark:border-zinc-700">
                            <svg class="w-4 h-4 text-black dark:text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path d="M13 10V3L4 14h7v7l9-11h-7z" /></svg>
                          </div>
                          <div class="flex flex-col gap-0.5">
                            <span class="text-[9px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest">{{ getToolName(tool) }}</span>
                            <span class="text-xs font-bold text-gray-800 dark:text-zinc-200 font-mono">{{ getShellPattern(tool) }}</span>
                          </div>
                        </div>
                        <button type="button" @click="form.autoAllowedTools = form.autoAllowedTools.filter(t => t !== tool)" class="text-gray-300 hover:text-red-500 transition-colors p-2">
                          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" /></svg>
                        </button>
                      </div>
                    </div>
                    <div v-else class="py-16 border-2 border-dashed border-gray-100 dark:border-zinc-800 rounded-sm flex flex-col items-center justify-center text-center px-8 bg-gray-50/30 dark:bg-zinc-900/30">
                      <div class="w-12 h-12 rounded-full bg-white dark:bg-zinc-800 flex items-center justify-center mb-4 border border-gray-100 dark:border-zinc-700 shadow-sm">
                        <svg class="w-6 h-6 text-gray-200 dark:text-zinc-700" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" /></svg>
                      </div>
                      <p class="text-[10px] font-bold text-gray-400 dark:text-zinc-600 uppercase tracking-widest">No tools auto-approved</p>
                      <p class="text-[11px] text-gray-500 dark:text-zinc-500 mt-2 font-medium">Tools appear here when you select 'Allow All' during a task execution.</p>
                    </div>
                  </div>

                  <!-- YOLO Mode -->
                  <div class="pt-8 border-t border-gray-100 dark:border-zinc-800">
                    <label class="flex items-center justify-between p-6 bg-red-50/30 dark:bg-red-500/5 rounded-sm cursor-pointer hover:bg-red-50/50 dark:hover:bg-red-500/10 transition-all border border-red-100 dark:border-red-900/20">
                      <div class="flex items-center gap-4">
                        <div class="p-2.5 bg-white dark:bg-zinc-900 rounded-sm shadow-sm border border-red-100 dark:border-red-900/30 text-red-500">
                          <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path d="M13 10V3L4 14h7v7l9-11h-7z" /></svg>
                        </div>
                        <div class="flex flex-col">
                          <span class="text-xs font-bold text-gray-900 dark:text-zinc-100">YOLO Mode (Execute All)</span>
                          <span class="text-[10px] text-red-600 dark:text-red-400 font-bold mt-0.5 uppercase tracking-tight">Warning: Agent will not ask for permission</span>
                        </div>
                      </div>
                      <div class="relative inline-flex items-center cursor-pointer">
                        <input type="checkbox" v-model="form.allowAllCommands" class="sr-only peer" />
                        <div class="w-10 h-6 bg-gray-200 dark:bg-zinc-800 peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-4 peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:rounded-full after:h-5 after:w-5 after:transition-all peer-checked:bg-black dark:peer-checked:bg-white dark:peer-checked:after:bg-zinc-900"></div>
                      </div>
                    </label>
                  </div>
                </div>

                <!-- Notifications -->
                <div v-if="activeTab === 'notifications'" class="space-y-8 animate-in fade-in slide-in-from-bottom-2 duration-300">
                   <div class="space-y-4">
                      <h3 class="text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest ml-1">Event Triggers</h3>
                      <div class="grid grid-cols-1 gap-2">
                        <label v-for="evt in eventTypes" :key="evt.key" class="flex items-center justify-between p-4 bg-gray-50 dark:bg-zinc-800/50 rounded-sm cursor-pointer hover:bg-gray-100 dark:hover:bg-zinc-800 transition-all border border-transparent hover:border-gray-200 dark:hover:border-zinc-700 shadow-sm">
                          <div class="flex items-center gap-4">
                            <div class="p-2 bg-white dark:bg-zinc-800 rounded-sm shadow-sm border border-gray-100 dark:border-zinc-700">
                              <svg v-html="evt.icon" class="w-4 h-4 text-black dark:text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"></svg>
                            </div>
                            <span class="text-xs font-bold text-gray-700 dark:text-zinc-200">{{ evt.label }}</span>
                          </div>
                          <div class="relative inline-flex items-center cursor-pointer">
                            <input type="checkbox" v-model="form.notification_settings[evt.key]" class="sr-only peer" />
                            <div class="w-10 h-6 bg-gray-200 dark:bg-zinc-800 peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-4 peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:rounded-full after:h-5 after:w-5 after:transition-all peer-checked:bg-black dark:peer-checked:bg-white dark:peer-checked:after:bg-zinc-900"></div>
                          </div>
                        </label>
                      </div>
                   </div>

                   <div class="pt-8 border-t border-gray-100 dark:border-zinc-800">
                      <h3 class="text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest ml-1 mb-4">Delivery Channels</h3>
                      <div class="flex flex-wrap gap-3">
                        <label class="flex items-center gap-3 px-4 py-2.5 bg-indigo-50 dark:bg-indigo-500/10 border border-indigo-100 dark:border-indigo-500/20 rounded-sm cursor-pointer hover:bg-indigo-100 dark:hover:bg-indigo-500/20 transition-all group shadow-sm">
                          <input type="checkbox" checked disabled class="accent-indigo-600 w-4 h-4" />
                          <span class="text-[10px] font-bold text-indigo-700 dark:text-indigo-400 uppercase tracking-widest">Email Delivery</span>
                        </label>
                        <div class="px-4 py-2.5 bg-gray-50 dark:bg-zinc-800/30 border border-gray-100 dark:border-zinc-800 rounded-sm opacity-40 cursor-not-allowed shadow-sm">
                           <span class="text-[10px] font-bold text-gray-400 uppercase tracking-widest">Slack (Coming Soon)</span>
                        </div>
                      </div>
                   </div>
                </div>

                <!-- Danger Zone -->
                <div v-if="activeTab === 'danger'" class="space-y-8 animate-in fade-in slide-in-from-bottom-2 duration-300">
                  <div class="space-y-4">
                    <div class="p-6 border border-red-100 dark:border-red-900/30 bg-red-50/30 dark:bg-red-500/5 rounded-sm flex flex-col md:flex-row md:items-center justify-between gap-6 shadow-sm">
                      <div class="flex-1">
                        <h4 class="text-sm font-bold text-gray-900 dark:text-zinc-100">{{ workspace?.archived_at ? 'Restore Workspace' : 'Archive Workspace' }}</h4>
                        <p class="text-[11px] text-gray-600 dark:text-zinc-400 mt-1 font-medium">{{ workspace?.archived_at ? 'Bring this workspace back to active status.' : 'Make this workspace read-only. Connections will be paused.' }}</p>
                      </div>
                      <button type="button" @click="handleArchiveToggle" class="px-6 py-2.5 bg-white dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700 text-[10px] font-bold text-gray-900 dark:text-zinc-100 hover:border-black dark:hover:border-white transition-all shadow-sm rounded-sm uppercase tracking-widest whitespace-nowrap">
                        {{ workspace?.archived_at ? 'Unarchive' : 'Archive Workspace' }}
                      </button>
                    </div>

                    <div class="p-6 border border-red-200 dark:border-red-900/50 bg-red-50 dark:bg-red-900/10 rounded-sm flex flex-col md:flex-row md:items-center justify-between gap-6 shadow-sm">
                      <div class="flex-1">
                        <h4 class="text-sm font-bold text-red-600 dark:text-red-500">Purge Workspace</h4>
                        <p class="text-[11px] text-gray-600 dark:text-zinc-400 mt-1 font-medium">Permanently delete this workspace and all its history. This action is irreversible.</p>
                      </div>
                      <button type="button" @click="handleDelete" class="px-8 py-2.5 bg-red-600 text-white border border-red-700 text-[10px] font-bold hover:bg-red-700 transition-all shadow-lg shadow-red-600/20 rounded-sm uppercase tracking-widest whitespace-nowrap">
                        Purge Permanent
                      </button>
                    </div>
                  </div>
                </div>

              </div>

              <!-- Action Bar Footer -->
              <div v-if="activeTab !== 'setup'" class="px-8 py-6 bg-gray-50/50 dark:bg-zinc-800/50 border-t border-gray-100 dark:border-zinc-800 flex justify-end gap-3">
                <button type="button" @click="router.back()" class="px-6 py-2.5 text-[10px] font-bold text-gray-500 dark:text-zinc-400 hover:text-black dark:hover:text-white uppercase tracking-widest transition-all">Cancel</button>
                <button type="submit" class="bg-gray-900 dark:bg-white text-white dark:text-zinc-900 px-10 py-2.5 rounded-sm text-[10px] font-bold hover:bg-black dark:hover:bg-zinc-100 shadow-xl shadow-black/10 transition-all active:scale-95 flex items-center gap-2 uppercase tracking-widest" :disabled="saving">
                  <svg v-if="saving" class="w-3.5 h-3.5 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M4 12a8 8 0 018-8v8H4z" /></svg>
                  {{ saving ? 'Saving...' : 'Update Workspace' }}
                </button>
              </div>
            </form>
          </div>
        </div>

      </div>
    </div>

    <!-- Confirm Modals -->
    <ArchiveModal
      :show="showArchiveConfirm"
      :workspaceName="workspace?.name || ''"
      @close="showArchiveConfirm = false"
      @confirm="doArchive"
    />
    <DeleteModal
      :show="showDeleteConfirm"
      title="Purge Workspace"
      :message="`Are you sure you want to permanently delete '${workspace?.name}'? This will erase all tasks, messages, and configurations. This cannot be undone.`"
      @close="showDeleteConfirm = false"
      @confirm="doDelete"
    />
  </div>
</template>

<script setup>
import { ref, onMounted, computed, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { getWorkspace, updateWorkspace, archiveWorkspace, unarchiveWorkspace, deleteWorkspace, getWorkspaceToken } from '../api';
import { useToasts } from '../composables/useToasts';
import ArchiveModal from '../components/ArchiveModal.vue';
import DeleteModal from '../components/DeleteModal.vue';
import { useWorkspaceStore } from '../stores/workspaceStore';
import { useFormat } from '../composables/useFormat';

const { toKebabCase, liveKebabCase } = useFormat();

const route = useRoute();
const router = useRouter();
const { notifySuccess, notifyError } = useToasts();
const workspaceId = computed(() => route.params.id);

const workspace = ref(null);
const loading = ref(true);
const saving = ref(false);
const workspaceStore = useWorkspaceStore();
const activeTab = ref('general');
const fileInput = ref(null);
const iconError = ref('');
const showArchiveConfirm = ref(false);
const showDeleteConfirm = ref(false);
const activeConnectionTab = ref('claude');
const token = ref('');
const copiedState = ref({
  config: false,
  permissions: false,
  command: false,
  gatewayInstall: false,
  gatewayStart: false
});

const authenticatedUrl = computed(() => {
  if (!token.value || !workspace.value?.mcpUrl) return workspace.value?.mcpUrl || '';
  return `${workspace.value.mcpUrl}?token=${token.value}`;
});

const serverName = computed(() => `agentrq-${workspaceId.value}`);
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

function copyToClipboard(text, key) {
  navigator.clipboard.writeText(text);
  copiedState.value[key] = true;
  setTimeout(() => copiedState.value[key] = false, 2000);
}

const navItems = [
  { id: 'general', label: 'General', icon: 'M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z' },
  { id: 'setup', label: 'Setup', icon: `<svg viewBox="0 0 16 17" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><path d="M1.62524 8.11636L7.6712 2.07042C8.50598 1.23564 9.85941 1.23564 10.6941 2.07042C11.5289 2.90518 11.5289 4.25861 10.6941 5.09339L6.12821 9.65934" stroke="currentColor"></path><path d="M6.19116 9.59684L10.6941 5.09385C11.5289 4.25908 12.8823 4.25908 13.7171 5.09385L13.7486 5.12534C14.5834 5.96011 14.5834 7.31354 13.7486 8.14831L8.28059 13.6164C8.00233 13.8946 8.00233 14.3457 8.28059 14.6239L9.40336 15.7468" stroke="currentColor"></path><path d="M9.18266 3.58203L4.71116 8.05351C3.87639 8.88826 3.87639 10.2417 4.71116 11.0765C5.54593 11.9112 6.89936 11.9112 7.73414 11.0765L12.2056 6.605" stroke="currentColor"></path></svg>` },
  { id: 'automations', label: 'Automations', icon: 'M13 10V3L4 14h7v7l9-11h-7z' },
  { id: 'notifications', label: 'Notifications', icon: 'M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9' },
];

const eventTypes = [
  { key: 'task_created', label: 'Task Created', icon: '<path d="M12 4v16m8-8H4" />' },
  { key: 'task_status_updated', label: 'Status Update', icon: '<path d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />' },
  { key: 'task_received_message', label: 'New Message', icon: '<path d="M8 10h.01M12 10h.01M16 10h.01M9 16H5a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v8a2 2 0 01-2 2h-5l-5 5v-5z" />' },
];

const form = ref({
  name: '',
  description: '',
  icon: '',
  notification_settings: {
    task_created: false,
    task_status_updated: false,
    task_received_message: false,
    workspace_archived: false,
    workspace_unarchived: false,
    channels: ['email']
  },
  autoAllowedTools: [],
  allowAllCommands: false,
  selfLearningLoopNote: ''
});

watch(() => form.value.name, (newVal) => {
  if (newVal) {
    const formatted = liveKebabCase(newVal);
    if (formatted !== newVal) {
      form.value.name = formatted;
    }
  }
});

async function load() {
  try {
    const res = await getWorkspace(workspaceId.value);
    workspace.value = res.workspace;
    form.value = {
      name: workspace.value.name || '',
      description: workspace.value.description || '',
      icon: workspace.value.icon || '',
      notification_settings: {
        task_created: workspace.value.notification_settings?.task_created || false,
        task_status_updated: workspace.value.notification_settings?.task_status_updated || false,
        task_received_message: workspace.value.notification_settings?.task_received_message || false,
        workspace_archived: workspace.value.notification_settings?.workspace_archived || false,
        workspace_unarchived: workspace.value.notification_settings?.workspace_unarchived || false,
        channels: workspace.value.notification_settings?.channels || ['email']
      },
      autoAllowedTools: workspace.value.autoAllowedTools || [],
      allowAllCommands: workspace.value.allowAllCommands || false,
      selfLearningLoopNote: workspace.value.selfLearningLoopNote || ''
    };
    try {
      const tokenRes = await getWorkspaceToken(workspaceId.value);
      token.value = tokenRes.token || '';
    } catch (err) { console.error('Failed to fetch token:', err); }
  } catch (err) {
    notifyError("Failed to load workspace settings: " + err.message);
    router.push('/');
  } finally {
    loading.value = false;
  }
}

async function save() {
  saving.value = true;
  try {
    const res = await updateWorkspace(workspaceId.value, form.value);
    workspace.value = res.workspace;
    workspaceStore.updateWorkspaceMetadata(res.workspace);
    notifySuccess("Workspace settings updated");
    try {
      const tokenRes = await getWorkspaceToken(workspaceId.value);
      token.value = tokenRes.token || '';
    } catch (err) { console.error('Failed to fetch token:', err); }
  } catch (err) {
    notifyError("Failed to save settings: " + err.message);
  } finally {
    saving.value = false;
  }
}

async function handleIconUpload(e) {
  const file = e.target.files[0];
  if (!file) return;
  iconError.value = '';

  if (file.size > 64 * 1024) {
    iconError.value = 'Too large (Max 64KB)';
    return;
  }

  const reader = new FileReader();
  reader.onload = async (event) => {
    const base64 = event.target.result;
    const img = new Image();
    img.src = base64;
    await img.decode();
    if (img.width !== img.height) {
      iconError.value = 'Image must be square';
      return;
    }
    form.value.icon = base64;
  };
  reader.readAsDataURL(file);
}

function handleArchiveToggle() {
  if (workspace.value?.archived_at) {
    doUnarchive();
  } else {
    showArchiveConfirm.value = true;
  }
}

async function doArchive() {
  showArchiveConfirm.value = false;
  try {
    await archiveWorkspace(workspaceId.value);
    notifySuccess("Workspace archived");
    await load();
  } catch (err) {
    notifyError("Archive failed: " + err.message);
  }
}

async function doUnarchive() {
  try {
    await unarchiveWorkspace(workspaceId.value);
    notifySuccess("Workspace restored");
    await load();
  } catch (err) {
    notifyError("Restore failed: " + err.message);
  }
}

function handleDelete() {
  showDeleteConfirm.value = true;
}

async function doDelete() {
  showDeleteConfirm.value = false;
  try {
    await deleteWorkspace(workspaceId.value);
    notifySuccess("Workspace purged");
    router.push('/');
  } catch (err) {
    notifyError("Delete failed: " + err.message);
  }
}

const SHELL_TOOLS = ['Bash', 'shell_execute', 'execute_command'];
function getToolName(tool) { return tool.split(':')[0]; }
function getShellPattern(tool) {
  if (!tool.includes(':')) return 'all commands';
  const pattern = tool.split(':').slice(1).join(':');
  return pattern === '*' ? 'all commands' : pattern;
}

onMounted(() => {
  load();
  if (route.query.tab) {
    activeTab.value = route.query.tab;
  }
});
</script>
