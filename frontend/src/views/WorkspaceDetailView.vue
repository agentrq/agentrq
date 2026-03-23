<template>
  <div class="h-full flex flex-col space-y-4" v-if="workspace">
    <div class="border-b border-gray-100 pb-3 flex flex-col md:flex-row md:items-center justify-between gap-4">
      <div>
        <div class="flex items-center gap-3">
          <div class="w-10 h-10 rounded-xl bg-gray-50 flex items-center justify-center shadow-inner border border-gray-100 overflow-hidden">
             <template v-if="workspace.icon">
               <img v-if="workspace.icon.startsWith('data:image')" :src="workspace.icon" class="w-full h-full object-cover" />
               <span v-else class="text-xl">{{ workspace.icon }}</span>
             </template>
             <svg v-else viewBox="0 0 24 24" class="w-6 h-6 text-black" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" />
                <path d="M12 7l-3.5 8" />
                <path d="M12 7l3.5 8" />
                <path d="M9.5 12h5" />
             </svg>
          </div>
          <h1 class="text-2xl font-bold text-gray-900 tracking-tight">{{ workspace.name }}</h1>
          <span :class="isConnected ? 'bg-green-50 text-green-600 border-green-100' : 'bg-red-50 text-red-600 border-red-100'" class="px-2 py-0.5 rounded-full text-[10px] font-black uppercase tracking-[0.1em] border" title="Your browser connection">
            {{ isConnected ? 'Live' : 'Offline' }}
          </span>
          <span :class="isAgentConnected ? 'bg-indigo-50 text-indigo-600 border-indigo-100' : 'bg-gray-50 text-gray-400 border-gray-100'" class="px-2 py-0.5 rounded-full text-[10px] font-black uppercase tracking-[0.1em] border transition-all" title="Agent (MCP) connection">
            Agent: {{ isAgentConnected ? 'Active' : 'Missing' }}
          </span>
          <button v-if="!workspace.archived_at" @click="showEditModal = true" class="p-1.5 text-gray-300 hover:text-indigo-600 hover:bg-indigo-50 rounded-lg transition-all" title="Edit Workspace Settings">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" /><path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" /></svg>
          </button>
        </div>
        
        <!-- Expandable Description -->
        <div v-if="workspace.description" class="mt-1 transition-all duration-300">
          <p class="text-[13px] text-gray-500 font-medium leading-relaxed max-w-4xl cursor-pointer hover:text-gray-700"
             :class="isDescriptionExpanded ? '' : 'line-clamp-1'"
             @click="isDescriptionExpanded = !isDescriptionExpanded">
            {{ workspace.description }}
            <span v-if="!isDescriptionExpanded" class="text-[10px] text-indigo-500 font-black uppercase tracking-widest ml-1 opacity-60 hover:opacity-100">...Read More</span>
          </p>
        </div>
      </div>
      
      <div class="flex items-center gap-2 max-w-full overflow-hidden mt-4 md:mt-0">
        <div class="bg-gray-50 px-3 py-2 rounded-xl border border-gray-100 flex items-center gap-2 shadow-sm w-full overflow-hidden max-w-full">
          <h3 class="text-[11px] font-bold text-gray-400 uppercase tracking-[0.2em] shrink-0">Endpoint</h3>
          <code class="text-[11px] text-gray-600 bg-white border border-gray-200/50 px-2 py-1 rounded-md font-mono select-all truncate shrink min-w-0 max-w-full">{{ workspace.mcp_url }}</code>
          <div class="flex items-center gap-1 ml-1 pl-1 border-l border-gray-200 shrink-0">
            <button @click="copyUrl" 
                    class="p-1.5 transition-all duration-300 focus:outline-none rounded-lg" 
                    :class="isCopied ? 'text-green-600 bg-green-50 scale-110' : 'text-gray-400 hover:text-black hover:bg-gray-100 bg-white'"
                    title="Copy URL">
              <svg v-if="!isCopied" class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"></path></svg>
              <svg v-else class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M5 13l4 4L19 7" /></svg>
            </button>
            <button @click="showSetupModal = true" 
                    class="p-1.5 text-gray-400 hover:text-black hover:bg-gray-100 rounded-lg transition-all"
                    title="How to connect Claude">
              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M8.228 9c.549-1.165 2.03-2 3.772-2 2.21 0 4 1.343 4 3 0 1.4-1.278 2.575-3.006 2.907-.542.104-.994.54-.994 1.093m0 3h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- Chat Area Wrapper -->
    <div class="flex-1 flex flex-col min-h-0 overflow-hidden">
      <!-- Archive Banner in detail view -->
      <div v-if="workspace.archived_at" class="bg-amber-50 border border-amber-100 p-3 mx-4 mt-2 mb-4 rounded-xl flex items-center justify-between gap-4 animate-in slide-in-from-top-4 duration-300">
        <div class="flex items-center gap-3">
          <div class="p-2 bg-white rounded-lg shadow-sm">
            <svg class="w-4 h-4 text-amber-600" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" /></svg>
          </div>
          <div>
            <p class="text-[11px] font-black text-amber-900 uppercase tracking-widest">Locked for Records</p>
            <p class="text-[10px] text-amber-600 font-bold">This mission is vaulted and currently read-only.</p>
          </div>
        </div>
        <button @click="handleUnarchive" class="px-5 py-2 bg-black text-white rounded-lg text-[10px] font-black uppercase tracking-widest hover:bg-zinc-800 transition-all active:scale-95 shadow-lg shadow-black/10">Unarchive Mission</button>
      </div>

      <TaskFeed :workspaceId="workspace.id" :initialTasks="tasks" :liveEvents="events" :isArchived="!!workspace.archived_at" @archive="handleArchive" />
    </div>
  </div>
  <div v-else-if="loading" class="text-center py-12 text-sm text-gray-500">Loading workspace context...</div>
  <div v-else class="text-center py-12 text-sm text-red-500">{{ error }}</div>

  <!-- Archive Modal -->
  <ArchiveModal 
    :show="showArchiveModal" 
    :workspaceName="workspace?.name || ''" 
    @close="showArchiveModal = false" 
    @confirm="doArchive" 
  />
  <!-- Edit Modal -->
  <EditWorkspaceModal 
    :show="showEditModal" 
    :workspace="workspace" 
    :loading="isUpdating" 
    @close="showEditModal = false" 
    @submit="handleUpdate" 
  />

  <SetupModal 
    :show="showSetupModal" 
    :mcpUrl="workspace?.mcp_url" 
    :workspaceId="workspace?.id"
    @close="showSetupModal = false" 
  />

</template>

<script setup>
import { ref, onMounted, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { getWorkspace, fetchTasks, archiveWorkspace, unarchiveWorkspace, updateWorkspace, getWorkspaceToken } from '../api';
import { useEventBus } from '../useEventBus';
import { useToasts } from '../composables/useToasts';
import TaskFeed from '../components/TaskFeed.vue';
import ArchiveModal from '../components/ArchiveModal.vue';
import SetupModal from '../components/SetupModal.vue';
import EditWorkspaceModal from '../components/EditWorkspaceModal.vue';

const route = useRoute();
const router = useRouter(); 
const { notifySuccess, notifyError } = useToasts();
const workspaceId = route.params.id;

const workspace = ref(null);
const tasks = ref([]);
const loading = ref(true);
const error = ref(null);
const isCopied = ref(false);
const showArchiveModal = ref(false);
const isDescriptionExpanded = ref(false);
const showSetupModal = ref(false);
const showEditModal = ref(false);
const isUpdating = ref(false);

const { connect, disconnect, events, isConnected } = useEventBus(workspaceId);
const isAgentConnected = ref(false);

watch(events, (evts) => {
  const last = evts[evts.length - 1];
  if (last && last.type === 'agent.connected') {
    isAgentConnected.value = last.payload.connected;
  }
}, { deep: true });

watch(isConnected, (val, old) => {
  if (val && old === false) {
    load();
  }
});

async function load() {
  try {
    const pRes = await getWorkspace(workspaceId);
    workspace.value = pRes.workspace;
    isAgentConnected.value = workspace.value.agent_connected;
    const tRes = await fetchTasks(workspaceId);
    tasks.value = tRes.tasks || [];
    // Start SSE stream
    connect();
  } catch (err) {
    error.value = err.message;
  } finally {
    loading.value = false;
  }
}

async function handleArchive() {
  showArchiveModal.value = true;
}

async function doArchive() {
  showArchiveModal.value = false;
  try {
    await archiveWorkspace(workspaceId);
    notifySuccess('Mission vaulted successfully');
    router.push('/');
  } catch (err) {
    notifyError("Failed to archive workspace: " + err.message);
  }
}

// getCookie removed as we use getWorkspaceToken API

async function handleUnarchive() {
  try {
    await unarchiveWorkspace(workspaceId);
    await load();
    notifySuccess('Mission protocol restored');
  } catch (err) {
    notifyError("Failed to restore workspace: " + err.message);
  }
}

async function handleUpdate(updatedForm) {
  isUpdating.value = true;
  try {
    const res = await updateWorkspace(workspaceId, updatedForm);
    workspace.value = res.workspace;
    showEditModal.value = false;
    notifySuccess('Mission protocol updated');
  } catch (err) {
    notifyError("Failed to update workspace: " + err.message);
  } finally {
    isUpdating.value = false;
  }
}

async function copyUrl() {
  if (workspace.value && workspace.value.mcp_url) {
    try {
      const res = await getWorkspaceToken(workspaceId);
      const token = res.token;
      let url = workspace.value.mcp_url;
      if (token) {
        url += `?token=${token}`;
      }
      navigator.clipboard.writeText(url);
      isCopied.value = true;
      setTimeout(() => {
        isCopied.value = false;
      }, 2000);
    } catch (err) {
      alert("Failed to get security token: " + err.message);
    }
  }
}

onMounted(() => {
  load();
});
</script>

<style scoped>
</style>
