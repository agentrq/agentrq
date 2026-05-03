<template>
  <div class="h-full flex flex-col gap-4" v-if="workspace">
    <!-- Workspace Header -->
    <div class="border-b-2 border-black pb-4 flex flex-col md:flex-row md:items-start justify-between gap-4">
      <div class="flex-1 min-w-0">
        <div class="flex items-center gap-3 flex-nowrap min-w-0">
          <!-- Icon -->
          <div class="w-6 h-6 bg-white flex items-center justify-center overflow-hidden shrink-0">
            <template v-if="workspace.icon">
              <img v-if="workspace.icon.startsWith('data:image')" :src="workspace.icon" class="w-full h-full object-cover" />
              <span v-else class="text-sm">{{ workspace.icon }}</span>
            </template>
            <svg v-else viewBox="0 0 24 24" class="w-3.5 h-3.5 text-black" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" />
              <path d="M12 7l-3.5 8" /><path d="M12 7l3.5 8" /><path d="M9.5 12h5" />
            </svg>
          </div>
          <h1 class="text-xl font-black text-black uppercase tracking-tight truncate flex-1 min-w-0">{{ workspace.name }}</h1>
          <!-- Agent connection removed from header -->
          <div class="flex items-center gap-1 shrink-0">
            <button v-if="!workspace.archived_at" @click="showEditModal = true" class="p-1.5 text-gray-400 hover:text-black border-2 border-transparent hover:border-black transition-all" title="Edit Workspace Settings">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" /><path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" /></svg>
            </button>
            <button @click="showSetupModal = true"
                    class="md:hidden p-1.5 text-gray-400 hover:text-black border-2 border-transparent hover:border-black transition-all"
                    title="Connection Guide">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M8.228 9c.549-1.165 2.03-2 3.772-2 2.21 0 4 1.343 4 3 0 1.4-1.278 2.575-3.006 2.907-.542.104-.994.54-.994 1.093m0 3h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>
            </button>

            <!-- Mobile Actions: Stats, Scheduled, Archive, +Task -->
            <div class="md:hidden flex items-center gap-1 border-l border-gray-200 ml-1 pl-1">
              <button @click="activeTab = activeTab === 'tasks' ? 'stats' : 'tasks'"
                      :class="activeTab === 'stats' ? 'text-[#00FF88] border-black bg-black shadow-[1px_1px_0px_0px_rgba(0,0,0,1)]' : 'text-gray-400 border-transparent'"
                      class="p-1.5 border-2 transition-all" title="Toggle Stats">
                <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M3 3v18h18M7 16l4-4 4 4 5-5" /></svg>
              </button>
              <button @click="showScheduledOnly = !showScheduledOnly"
                      :class="showScheduledOnly ? 'text-[#00FF88] border-black bg-black shadow-[1px_1px_0px_0px_rgba(0,0,0,1)]' : 'text-gray-400 border-transparent'"
                      class="p-1.5 border-2 transition-all" title="Toggle Scheduled">
                <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path stroke-linecap="round" stroke-linejoin="round" d="M12 6v6h4m6 0a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>
              </button>

              <button @click="taskFeed?.startCreate()" class="p-1.5 text-black bg-[#00FF88] border-2 border-black transition-all shadow-[1px_1px_0px_0px_rgba(0,0,0,1)] active:shadow-none" title="New Task">
                <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M12 6v6m0 0v6m0-6h6m-6 0H6"/></svg>
              </button>
            </div>

            <!-- Desktop Actions: Stats, Scheduled -->
            <div class="hidden md:flex items-center gap-1 border-l border-gray-200 ml-1 pl-1">
              <button @click="activeTab = activeTab === 'tasks' ? 'stats' : 'tasks'"
                      :class="activeTab === 'stats' ? 'text-[#00FF88] border-black bg-black shadow-[1px_1px_0px_0px_rgba(0,0,0,1)]' : 'text-gray-400 border-transparent'"
                      class="p-1.5 border-2 transition-all" title="Toggle Stats">
                <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M3 3v18h18M7 16l4-4 4 4 5-5" /></svg>
              </button>
              <button @click="showScheduledOnly = !showScheduledOnly"
                      :class="showScheduledOnly ? 'text-[#00FF88] border-black bg-black shadow-[1px_1px_0px_0px_rgba(0,0,0,1)]' : 'text-gray-400 border-transparent'"
                      class="p-1.5 border-2 transition-all" title="Toggle Scheduled">
                <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path stroke-linecap="round" stroke-linejoin="round" d="M12 6v6h4m6 0a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>
              </button>
            </div>
          </div>
        </div>

        <!-- Description -->
        <div v-if="workspace.description" class="mt-2 hidden md:block">
          <p class="text-xs text-gray-500 font-medium leading-relaxed max-w-3xl cursor-pointer hover:text-black transition-colors"
             :class="isDescriptionExpanded ? '' : 'line-clamp-1'"
             @click="isDescriptionExpanded = !isDescriptionExpanded">
            {{ workspace.description }}
            <span v-if="!isDescriptionExpanded" class="text-[10px] text-black font-black uppercase tracking-widest ml-1 underline">expand</span>
          </p>
        </div>
      </div>

      <!-- MCP Endpoint -->
      <div class="hidden md:flex items-center gap-2 shrink-0 max-w-full overflow-hidden">
        <div class="bg-gray-950 border-2 border-black px-3 py-2 flex items-center gap-2 shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] max-w-full overflow-hidden">
          <span class="text-[9px] font-black text-gray-400 uppercase tracking-widest shrink-0">MCP</span>
          <code class="text-[11px] text-[#00FF88] font-mono select-all truncate shrink min-w-0 max-w-[200px]">{{ authenticatedUrl }}</code>
          <div class="flex items-center gap-1 pl-1 border-l border-gray-700 shrink-0">
            <button @click="copyUrl"
                    class="p-1 transition-all border border-transparent focus:outline-none"
                    :class="isCopied ? 'text-[#00FF88] border-[#00FF88]' : 'text-gray-500 hover:text-white hover:border-gray-600'"
                    title="Copy URL">
              <svg v-if="!isCopied" class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"></path></svg>
              <svg v-else class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M5 13l4 4L19 7" /></svg>
            </button>
            <button @click="showSetupModal = true"
                    class="p-1 text-gray-500 hover:text-white border border-transparent hover:border-gray-600 transition-all"
                    title="Connection Guide">
              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M8.228 9c.549-1.165 2.03-2 3.772-2 2.21 0 4 1.343 4 3 0 1.4-1.278 2.575-3.006 2.907-.542.104-.994.54-.994 1.093m0 3h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>
            </button>
          </div>
        </div>
      </div>
    </div>

      <!-- Archive Banner -->
      <div v-if="workspace.archived_at" class="border-2 border-black bg-yellow-300 p-3 mb-3 flex items-center justify-between gap-4">
        <div class="flex items-center gap-3">
          <svg class="w-4 h-4 text-black shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" /></svg>
          <div>
            <p class="text-xs font-black text-black uppercase tracking-widest">Locked for Records</p>
            <p class="text-[10px] text-black/60 font-bold">This workspace is archived and read-only.</p>
          </div>
        </div>
      </div>

      <div v-show="activeTab === 'tasks'" class="flex-1 flex flex-col min-h-0 overflow-hidden">
        <TaskFeed
          ref="taskFeed"
          :workspaceId="workspace.id"
          :initialTasks="tasks"
          :liveEvents="events"
          :isArchived="!!workspace.archived_at"
          :isAgentConnected="isAgentConnected"
          :filterScheduled="showScheduledOnly"
          @toggleScheduled="showScheduledOnly = !showScheduledOnly"
        />
      </div>

      <div v-if="activeTab === 'stats'" class="flex-1 overflow-y-auto">
        <WorkspaceStats :workspaceId="workspace.id" />
      </div>
    </div>
  <div v-else-if="loading" class="text-center py-12 text-sm font-black text-gray-400 uppercase tracking-widest animate-pulse">Loading workspace context...</div>
  <div v-else class="text-center py-12 text-sm font-black text-red-600 uppercase tracking-widest border-2 border-red-300 bg-red-50 p-6">{{ error }}</div>

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
    @archive="handleArchive"
    @unarchive="handleUnarchive"
    @delete="handleDelete"
  />

  <DeleteModal
    :show="showPurgeModal"
    :taskTitle="workspace?.name || ''"
    title="Purge Workspace"
    @close="showPurgeModal = false"
    @confirm="doDelete"
  />

  <SetupModal
    :show="showSetupModal"
    :mcpUrl="workspace?.mcpUrl"
    :workspaceId="workspace?.id"
    @close="showSetupModal = false"
  />

</template>

<script setup>
import { ref, onMounted, watch, computed } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { getWorkspace, fetchTasks, archiveWorkspace, unarchiveWorkspace, updateWorkspace, getWorkspaceToken, deleteWorkspace } from '../api';
import { useEventBus } from '../useEventBus';
import { useToasts } from '../composables/useToasts';
import TaskFeed from '../components/TaskFeed.vue';
import WorkspaceStats from '../components/WorkspaceStats.vue';
import ArchiveModal from '../components/ArchiveModal.vue';
import SetupModal from '../components/SetupModal.vue';
import EditWorkspaceModal from '../components/EditWorkspaceModal.vue';
import DeleteModal from '../components/DeleteModal.vue';

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
const showPurgeModal = ref(false);
const isDescriptionExpanded = ref(false);
const showSetupModal = ref(false);
const showEditModal = ref(false);
const isUpdating = ref(false);
const showScheduledOnly = ref(route.query.scheduled === 'true');
const taskFeed = ref(null);
const activeTab = ref('tasks');

const { connect, disconnect, events, isConnected } = useEventBus(workspaceId);
const isAgentConnected = ref(false);
const token = ref('');

const authenticatedUrl = computed(() => {
  if (!workspace.value || !workspace.value.mcpUrl) return '';
  if (!token.value) return workspace.value.mcpUrl;
  return `${workspace.value.mcpUrl}?token=${token.value}`;
});

async function fetchToken() {
  if (workspaceId) {
    try {
      const res = await getWorkspaceToken(workspaceId);
      token.value = res.token || '';
    } catch (err) {
      console.error('Failed to fetch token:', err);
    }
  }
}

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
    const [pRes, tRes] = await Promise.all([
      getWorkspace(workspaceId),
      fetchTasks(workspaceId)
    ]);
    tasks.value = tRes.tasks || [];
    workspace.value = pRes.workspace;
    isAgentConnected.value = workspace.value.agentConnected;
    // Fetch token for display
    fetchToken();
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

async function handleDelete() {
  showPurgeModal.value = true;
}

async function doDelete() {
  showPurgeModal.value = false;
  try {
    await deleteWorkspace(workspaceId);
    notifySuccess('Workspace and all data purged', 'Purge Complete');
    router.push('/');
  } catch (err) {
    notifyError("Failed to purge workspace: " + err.message);
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
  if (workspace.value && workspace.value.mcpUrl) {
    try {
      const res = await getWorkspaceToken(workspaceId);
      const token = res.token;
      let url = workspace.value.mcpUrl;
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

watch(() => workspace.value?.name, (name) => {
  if (name) document.title = `${name} | AgentRQ`;
}, { immediate: true });

onMounted(() => {
  load();
});
</script>

<style scoped>
</style>
