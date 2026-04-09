<template>
  <Transition name="fade">
    <div v-if="show" class="fixed inset-0 z-[60] flex items-center justify-center p-4 sm:p-6">
      <div class="absolute inset-0 bg-black/40 backdrop-blur-sm" @click="close"></div>
      
      <div class="relative bg-white w-full max-w-xl rounded-2xl shadow-2xl overflow-hidden border border-gray-100 animate-in zoom-in-95 duration-200">
        <div class="px-8 py-6 bg-gray-50/50 flex justify-between items-center">
          <h2 class="text-lg font-bold text-gray-900">Workspace Settings</h2>
          <button @click="close" class="text-gray-400 hover:text-black transition-colors p-2">
            <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M6 18L18 6M6 6l12 12" /></svg>
          </button>
        </div>

        <div class="flex border-b border-gray-100 px-8 bg-white z-10">
          <button @click="activeTab = 'general'" :class="activeTab === 'general' ? 'border-black text-black' : 'border-transparent text-gray-400'" class="py-4 text-[10px] font-black uppercase tracking-widest border-b-2 mr-8 transition-all">General</button>
          <button @click="activeTab = 'automations'" :class="activeTab === 'automations' ? 'border-black text-black' : 'border-transparent text-gray-400'" class="py-4 text-[10px] font-black uppercase tracking-widest border-b-2 mr-8 transition-all">Automations</button>
          <button @click="activeTab = 'notifications'" :class="activeTab === 'notifications' ? 'border-black text-black' : 'border-transparent text-gray-400'" class="py-4 text-[10px] font-black uppercase tracking-widest border-b-2 transition-all">Notifications</button>
          <button @click="activeTab = 'danger'" :class="activeTab === 'danger' ? 'border-red-600 text-red-600' : 'border-transparent text-gray-400'" class="py-4 text-[10px] font-black uppercase tracking-widest border-b-2 ml-auto transition-all hover:text-red-500 hover:border-red-500">Danger Zone</button>
        </div>

        <div class="p-8 max-h-[70vh] overflow-y-auto">
          <form @submit.prevent="submit" class="grid grid-cols-1 gap-6">
            <div v-if="activeTab === 'general'" class="space-y-6 animate-in fade-in duration-300">
              <div class="grid grid-cols-1 md:grid-cols-4 gap-6">
                <div class="md:col-span-1 space-y-2">
                  <label class="block text-[10px] font-black text-gray-400 uppercase tracking-widest ml-1">Icon</label>
                  <div class="relative group">
                    <input type="file" ref="fileInput" class="hidden" accept="image/*" @change="handleIconUpload" />
                    <div @click="$refs.fileInput.click()" class="w-full bg-gray-50 border-2 border-dashed border-gray-200 rounded-lg h-[46px] flex items-center justify-center cursor-pointer hover:border-black transition-all overflow-hidden group">
                      <img v-if="form.icon" :src="form.icon" class="w-6 h-6 object-contain" />
                      <svg v-else class="w-4 h-4 text-gray-300 group-hover:text-black transition-colors" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" /></svg>
                    </div>
                  </div>
                  <p v-if="error" class="text-[9px] font-bold text-red-500 mt-1 uppercase tracking-widest">{{ error }}</p>
                </div>
                <div class="md:col-span-3 space-y-2">
                  <label class="block text-[10px] font-black text-gray-400 uppercase tracking-widest ml-1">Workspace Name</label>
                  <input v-model="form.name" type="text" required class="w-full bg-gray-50 border border-gray-200 rounded-lg px-4 py-3 text-sm focus:ring-1 focus:ring-black focus:border-black outline-none font-semibold text-gray-900 transition-all" placeholder="e.g. Workspace Redstone" />
                </div>
              </div>

              <div class="space-y-2">
                <label class="block text-[10px] font-black text-gray-400 uppercase tracking-widest ml-1">Vision / Description</label>
                <textarea v-model="form.description" rows="4" class="w-full bg-gray-50 border border-gray-200 rounded-lg px-4 py-3 text-sm focus:ring-1 focus:ring-black focus:border-black outline-none font-medium text-gray-800 transition-all resize-none" placeholder="What are we building together? Describe the mission of this workspace..."></textarea>
              </div>
            </div>

            <div v-if="activeTab === 'automations'" class="space-y-6 animate-in fade-in duration-300">
              <div class="space-y-4">
                <div class="flex items-center justify-between">
                  <label class="block text-[10px] font-black text-gray-400 uppercase tracking-widest ml-1">Auto-Allow List</label>
                  <span class="text-[9px] font-bold text-gray-300 uppercase tracking-wider bg-gray-50 px-2 py-0.5 rounded border border-gray-100">{{ form.auto_allowed_tools.length }} Tools</span>
                </div>
                
                <p class="text-[11px] text-gray-500 leading-relaxed px-1">
                  These tools will execute autonomously without requiring manual confirmation. Auto-approving trusted tools speeds up agent execution.
                </p>

                <div v-if="form.auto_allowed_tools.length > 0" class="grid grid-cols-1 gap-2 mt-4">
                  <div v-for="tool in form.auto_allowed_tools" :key="tool" class="flex items-center justify-between p-3.5 bg-gray-50 rounded-xl border border-gray-100 group hover:bg-white hover:border-gray-200 transition-all">
                    <div class="flex items-center gap-3">
                      <div class="p-2 bg-white rounded-lg shadow-sm border border-gray-100">
                        <svg class="w-3.5 h-3.5 text-indigo-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path d="M13 10V3L4 14h7v7l9-11h-7z" /></svg>
                      </div>
                      <div v-if="isShellToolEntry(tool)" class="flex flex-col gap-0.5">
                        <span class="text-[9px] font-black text-indigo-400 uppercase tracking-tighter">{{ getToolName(tool) }}</span>
                        <span class="text-xs font-bold text-gray-800 font-mono">{{ getShellPattern(tool) }}</span>
                      </div>
                      <div v-else-if="tool.includes(':')" class="flex flex-col gap-0.5">
                        <span class="text-[9px] font-black text-indigo-400 uppercase tracking-tighter">{{ tool.split(':')[0] }}</span>
                        <span class="text-xs font-bold text-gray-800 font-mono">{{ tool.split(':').slice(1).join(':') }}</span>
                      </div>
                      <span v-else class="text-xs font-bold text-gray-800">{{ tool }}</span>
                    </div>
                    <button type="button" @click="form.auto_allowed_tools = form.auto_allowed_tools.filter(t => t !== tool)" class="text-gray-300 hover:text-red-500 transition-colors p-1.5 opacity-0 group-hover:opacity-100">
                      <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" /></svg>
                    </button>
                  </div>
                </div>
                <div v-else class="py-12 border-2 border-dashed border-gray-100 rounded-3xl flex flex-col items-center justify-center text-center px-8">
                  <div class="w-12 h-12 rounded-full bg-gray-50 flex items-center justify-center mb-4 border border-gray-100">
                    <svg class="w-6 h-6 text-gray-200" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" /></svg>
                  </div>
                  <p class="text-[10px] font-black uppercase tracking-widest text-gray-300">No tools auto approved yet</p>
                  <p class="text-[11px] text-gray-400 mt-2">Tools are added here automatically when you select 'Allow All' during a permission request for the workspace tasks.</p>
                </div>
              </div>
            </div>

            <div v-if="activeTab === 'notifications'" class="space-y-8 animate-in fade-in duration-300">
               <div>
                  <label class="block text-[10px] font-black text-gray-400 uppercase tracking-widest ml-1 mb-4">Event Triggers</label>
                  <div class="space-y-3">
                    <label v-for="evt in eventTypes" :key="evt.key" class="flex items-center justify-between p-4 bg-gray-50 rounded-xl cursor-pointer hover:bg-gray-100/80 transition-all border border-transparent hover:border-gray-200">
                      <div class="flex items-center gap-3">
                        <div class="p-2 bg-white rounded-lg shadow-sm">
                          <svg v-html="evt.icon" class="w-3.5 h-3.5 text-black" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"></svg>
                        </div>
                        <span class="text-xs font-bold text-gray-700">{{ evt.label }}</span>
                      </div>
                      <div class="relative inline-flex items-center cursor-pointer">
                        <input type="checkbox" v-model="form.notification_settings[evt.key]" class="sr-only peer" />
                        <div class="w-9 h-5 bg-gray-200 peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-4 after:w-4 after:transition-all peer-checked:bg-black"></div>
                      </div>
                    </label>
                  </div>
               </div>

               <div>
                  <label class="block text-[10px] font-black text-gray-400 uppercase tracking-widest ml-1 mb-4">Channels</label>
                  <div class="flex flex-wrap gap-3">
                    <label class="flex items-center gap-2.5 px-4 py-2 bg-indigo-50 border border-indigo-100 rounded-xl cursor-pointer hover:bg-indigo-100/50 transition-all group">
                      <input type="checkbox" checked disabled class="accent-indigo-600 w-3.5 h-3.5" />
                      <span class="text-[10px] font-black text-indigo-700 uppercase tracking-widest">Email Delivery</span>
                    </label>
                    <div class="px-4 py-2 bg-gray-50 border border-gray-100 rounded-xl opacity-40 cursor-not-allowed">
                       <span class="text-[10px] font-black text-gray-400 uppercase tracking-widest">Slack (Coming Soon)</span>
                    </div>
                  </div>
               </div>
            </div>

            <div v-if="activeTab === 'danger'" class="space-y-6 animate-in fade-in duration-300">
               <div>
                  <h3 class="text-sm font-bold text-red-600 mb-4">Destructive Actions</h3>
                  
                  <div class="space-y-4">
                    <div class="p-5 border border-red-100 bg-red-50/50 rounded-xl flex flex-col md:flex-row md:items-center justify-between gap-4">
                      <div>
                        <h4 class="text-sm font-bold text-gray-900">{{ workspace?.archived_at ? 'Restore Workspace' : 'Archive Workspace' }}</h4>
                        <p class="text-[11px] text-gray-600 mt-1">{{ workspace?.archived_at ? 'Restore this workspace to allow modifications and reactivate connections.' : 'Move this workspace to the archive. It will become read-only.' }}</p>
                      </div>
                      <button type="button" @click="$emit(workspace?.archived_at ? 'unarchive' : 'archive')" class="px-5 py-2.5 bg-white border border-gray-200 text-xs font-black uppercase tracking-widest text-black hover:border-black transition-all shadow-sm rounded-lg whitespace-nowrap">
                        {{ workspace?.archived_at ? 'Unarchive' : 'Archive' }}
                      </button>
                    </div>

                    <div class="p-5 border border-red-200 bg-red-50 rounded-xl flex flex-col md:flex-row md:items-center justify-between gap-4">
                      <div>
                        <h4 class="text-sm font-bold text-gray-900">Delete Workspace</h4>
                        <p class="text-[11px] text-gray-600 mt-1">Permanently delete this workspace and all associated tasks. This action cannot be undone.</p>
                      </div>
                      <button type="button" @click="$emit('delete')" class="px-6 py-2.5 bg-red-600 text-white border border-red-700 text-xs font-black uppercase tracking-widest hover:bg-red-700 transition-all shadow-sm rounded-lg whitespace-nowrap">
                        Purge Workspace
                      </button>
                    </div>
                  </div>
               </div>
            </div>

            <div class="flex justify-end gap-3 pt-4 border-t border-gray-100 mt-4">
              <button type="button" @click="close" class="px-6 py-3.5 text-xs font-black uppercase tracking-widest text-gray-400 hover:text-black transition-colors">Cancel</button>
              <button type="submit" class="bg-black text-white px-8 py-3.5 rounded-xl text-xs font-black uppercase tracking-widest hover:bg-zinc-800 shadow-xl shadow-black/10 transition-all active:scale-95 flex items-center gap-2" :disabled="loading">
                <svg v-if="loading" class="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M4 12a8 8 0 018-8v8H4z" /></svg>
                {{ loading ? 'Saving Changes...' : 'Update Workspace' }}
              </button>
            </div>
          </form>
        </div>
      </div>
    </div>
  </Transition>
</template>

<script setup>
import { ref, watch } from 'vue';

const props = defineProps({
  show: Boolean,
  workspace: Object,
  loading: Boolean
});

const emit = defineEmits(['close', 'submit', 'archive', 'unarchive', 'delete']);

const activeTab = ref('general');
const fileInput = ref(null);
const error = ref('');
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
  auto_allowed_tools: []
});

const eventTypes = [
  { key: 'task_created', label: 'Task Created', icon: '<path d="M12 4v16m8-8H4" />' },
  { key: 'task_status_updated', label: 'Status Change', icon: '<path d="M4 4h16M4 12h16M4 20h16" />' },
  { key: 'task_received_message', label: 'New Message', icon: '<path d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />' },
];

watch(() => props.workspace, (newVal) => {
  if (newVal) {
    form.value = {
      name: newVal.name || '',
      description: newVal.description || '',
      icon: newVal.icon || '',
      notification_settings: {
        task_created: newVal.notification_settings?.task_created || false,
        task_status_updated: newVal.notification_settings?.task_status_updated || false,
        task_received_message: newVal.notification_settings?.task_received_message || false,
        workspace_archived: newVal.notification_settings?.workspace_archived || false,
        workspace_unarchived: newVal.notification_settings?.workspace_unarchived || false,
        channels: newVal.notification_settings?.channels || ['email']
      },
      auto_allowed_tools: newVal.auto_allowed_tools || []
    };
  }
}, { immediate: true });

function close() {
  activeTab.value = 'general';
  error.value = '';
  emit('close');
}

async function handleIconUpload(e) {
  const file = e.target.files[0];
  if (!file) return;
  error.value = '';

  if (file.size > 64 * 1024) {
    error.value = 'Too large (Max 64KB)';
    return;
  }

  const reader = new FileReader();
  reader.onload = async (event) => {
    const base64 = event.target.result;
    
    // Check squareness
    const img = new Image();
    img.src = base64;
    await img.decode();
    if (img.width !== img.height) {
      error.value = 'Image must be square';
      return;
    }
    
    form.value.icon = base64;
  };
  reader.readAsDataURL(file);
}

const SHELL_TOOLS = ['Bash', 'shell_execute', 'execute_command'];

function isShellToolEntry(tool) {
  const name = tool.split(':')[0];
  return SHELL_TOOLS.includes(name);
}

function getToolName(tool) {
  return tool.split(':')[0];
}

function getShellPattern(tool) {
  if (!tool.includes(':')) return 'all commands';
  const pattern = tool.split(':').slice(1).join(':');
  return pattern === '*' ? 'all commands' : pattern;
}

function submit() {
  emit('submit', { ...form.value });
}
</script>

<style scoped>
.fade-enter-active, .fade-leave-active { transition: opacity 0.2s ease; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
</style>
