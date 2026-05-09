<template>
  <div class="h-full flex flex-col gap-8 w-full">
    <!-- Page Header -->
    <div class="border-b border-gray-200 dark:border-zinc-800 pb-6 flex flex-col md:flex-row md:items-end justify-between gap-4 px-4">
      <div class="flex-1">
        <div class="flex items-center justify-between md:justify-start gap-4 mb-2">
          <div class="flex items-center gap-3">
            <h1 class="text-xl md:text-2xl font-black text-gray-800 dark:text-zinc-200">Workspaces</h1>
            <span v-if="!loadingWorkspaces" class="border border-gray-200 dark:border-zinc-700 rounded-sm px-2.5 py-1 text-[11px] font-bold bg-gray-50 dark:bg-zinc-800 text-gray-600 dark:text-zinc-300 shadow-sm">
              {{ activeWorkspaces.length }}
            </span>
          </div>

          <!-- Mobile Actions -->
          <!-- Mobile Actions -->
          <div class="md:hidden flex items-center gap-2">
            <button @click="showArchived = !showArchived"
                    class="p-2.5 rounded-sm transition-all border"
                    :class="showArchived ? 'bg-gray-100 dark:bg-zinc-800 border-gray-200 dark:border-zinc-700 text-black dark:text-white shadow-sm' : 'bg-white dark:bg-zinc-900 border-gray-200 dark:border-zinc-700 text-gray-500 dark:text-zinc-400'"
                    title="Toggle Archived">
              <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4" /></svg>
            </button>
            <button @click="showCreate = true" class="bg-gray-900 dark:bg-white text-white dark:text-zinc-900 p-2.5 rounded-sm border border-transparent shadow-sm active:scale-95 transition-all" title="New Workspace">
              <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path d="M12 6v6m0 0v6m0-6h6m-6 0H6"/></svg>
            </button>
          </div>
        </div>
        <p class="text-[10px] font-black text-gray-500 dark:text-zinc-400">Manage your AgentRQ pipelines</p>

        <!-- Search Bar -->
        <div class="mt-8 relative max-w-md">
          <svg class="absolute left-3.5 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500 dark:text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" /></svg>
          <input v-model="searchQuery"
                 type="text"
                 placeholder="Search workspaces..."
                 class="w-full pl-10 pr-4 py-3 bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-sm text-sm outline-none font-black text-gray-800 dark:text-zinc-200 placeholder:text-gray-500 dark:placeholder:text-zinc-500 focus:border-gray-900 dark:focus:border-white focus:ring-0 shadow-sm transition-all" />
        </div>
      </div>

      <div class="hidden md:flex items-center gap-3">
        <button @click="showArchived = !showArchived"
                class="px-5 py-2.5 text-[10px] font-black transition-all rounded-sm border flex items-center gap-2"
                :class="showArchived ? 'bg-gray-100 dark:bg-zinc-800 text-black dark:text-white border-gray-200 dark:border-zinc-700 shadow-sm' : 'bg-white dark:bg-zinc-900 text-gray-500 dark:text-zinc-400 border-gray-200 dark:border-zinc-700 hover:bg-gray-50 dark:hover:bg-zinc-800 shadow-sm'">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4" /></svg>
          {{ showArchived ? 'Hide Archived' : 'Show Archived' }}
        </button>
        <button @click="showCreate = true" class="bg-gray-900 dark:bg-zinc-100 text-white dark:text-gray-900 px-6 py-2.5 rounded-sm border border-transparent hover:bg-zinc-700 dark:hover:bg-zinc-200 transition-all text-[10px] font-black shadow-md flex items-center gap-2">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path d="M12 6v6m0 0v6m0-6h6m-6 0H6"/></svg>
          New Workspace
        </button>
      </div>
    </div>

    <!-- Create workspace form -->
    <Transition name="fade-down">
      <div v-if="showCreate" class="fixed inset-0 z-[110] flex items-center justify-center p-4 md:relative md:inset-auto md:p-0 md:bg-transparent md:z-10 md:block">
        <!-- Backdrop for mobile -->
        <div class="fixed inset-0 bg-black/60 backdrop-blur-sm md:hidden" @click="showCreate = false"></div>
        
        <div class="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-sm shadow-xl w-full max-w-2xl md:mb-6 shrink-0 z-10 relative flex flex-col max-h-[90vh] md:max-h-none overflow-hidden">
        <div class="px-6 py-4 border-b border-gray-200 dark:border-zinc-800 bg-gray-50 dark:bg-zinc-800/80 flex justify-between items-center">
          <h2 class="text-[10px] font-black text-gray-800 dark:text-zinc-200">Initialize New Workspace</h2>
          <button @click="showCreate = false" class="text-gray-500 hover:text-gray-900 dark:hover:text-white transition-colors p-1.5 rounded-md hover:bg-gray-200 dark:hover:bg-zinc-700">
            <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" /></svg>
          </button>
        </div>
        <div class="p-6 overflow-y-auto">
          <form @submit.prevent="submit" class="grid grid-cols-1 gap-6">
            <div class="space-y-2">
              <label class="block text-[10px] font-black text-gray-500 dark:text-zinc-400">Workspace Name</label>
              <input v-model="form.name" type="text" required class="w-full bg-white dark:bg-zinc-950 border border-gray-200 dark:border-zinc-700 rounded-sm px-4 py-3 text-sm outline-none font-black text-gray-800 dark:text-zinc-200 focus:border-gray-900 dark:focus:border-white focus:ring-0 transition-all shadow-sm" placeholder="e.g. my-saas-backend" />
            </div>
            <div class="space-y-2">
              <label class="block text-[10px] font-black text-gray-500 dark:text-zinc-400">Mission / Description</label>
              <textarea v-model="form.description" rows="3" class="w-full bg-white dark:bg-zinc-950 border border-gray-200 dark:border-zinc-700 rounded-sm px-4 py-3 text-sm outline-none font-medium text-gray-800 dark:text-zinc-200 transition-all resize-none focus:border-gray-900 dark:focus:border-white focus:ring-0 shadow-sm" placeholder="What are we building? Describe the mission of this workspace..."></textarea>
            </div>
            <div class="space-y-2">
              <label class="block text-[10px] font-black text-gray-500 dark:text-zinc-400 flex justify-between items-center">
                Self Learning Loop Note
                <span class="text-[9px] text-gray-500 font-medium normal-case tracking-normal">Optional guidelines for agent learning</span>
              </label>
              <textarea v-model="form.selfLearningLoopNote" rows="4" class="w-full bg-white dark:bg-zinc-950 border border-gray-200 dark:border-zinc-700 rounded-sm px-4 py-3 text-sm outline-none font-medium text-gray-800 dark:text-zinc-200 transition-all resize-none focus:border-gray-900 dark:focus:border-white focus:ring-0 shadow-sm" placeholder="Upon completing the task, evaluate your execution path. If you encountered friction—such as repeated errors, failed tool calls, or requiring multiple iterations to find a solution—extract the successful workaround..."></textarea>
            </div>
            <div class="flex justify-end gap-3 pt-4 border-t border-gray-100 dark:border-zinc-800">
              <button type="button" @click="showCreate = false" class="px-6 py-2.5 rounded-sm border border-gray-200 dark:border-zinc-700 bg-white dark:bg-zinc-800 text-gray-700 dark:text-zinc-300 text-[10px] font-black hover:bg-gray-50 dark:hover:bg-zinc-700 transition-all shadow-sm">Cancel</button>
              <button type="submit" class="bg-gray-900 dark:bg-zinc-100 text-white dark:text-gray-900 px-6 py-2.5 rounded-sm border border-transparent text-[10px] font-black hover:bg-zinc-700 dark:hover:bg-zinc-200 transition-all shadow-md flex items-center gap-2" :disabled="loading">
                <svg v-if="loading" class="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 12a8 8 0 018-8v8H4z" /></svg>
                {{ loading ? 'Initializing...' : 'Create Workspace' }}
              </button>
            </div>
          </form>
        </div>
        </div>
      </div>
    </Transition>

    <div v-if="error" class="bg-red-50 dark:bg-red-500/10 border border-red-200 dark:border-red-500/30 text-red-700 dark:text-red-400 px-5 py-3 rounded-sm text-[10px] font-black shadow-sm">
      {{ error }}
    </div>

    <!-- Workspace list -->
    <div class="flex-1 overflow-y-auto px-4 pb-10 custom-scrollbar">
      <div v-if="loadingWorkspaces" class="text-[10px] font-black text-gray-500 dark:text-zinc-500 animate-pulse flex items-center gap-3 py-8 justify-center">
        <div class="w-5 h-5 border-2 border-gray-300 border-t-gray-500 rounded-full animate-spin"></div>
        Loading Workspaces...
      </div>

      <div v-else class="space-y-12">
        <!-- Active Workspaces -->
        <section v-if="filteredActiveWorkspaces.length > 0">
          <div class="text-[10px] font-black text-gray-500 dark:text-zinc-400 mb-6 flex items-center gap-3">
            Your workspaces
            <div class="h-px bg-gray-200 dark:bg-zinc-800 flex-1"></div>
          </div>
          <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            <div v-for="p in filteredActiveWorkspaces"
                 :key="p.id"
                 class="group relative bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-sm p-6 cursor-pointer transition-all duration-300 hover:border-gray-400 dark:hover:border-zinc-600 flex flex-col gap-4"
                 @click="goToWorkspace(p.id)">

                <div class="flex items-start justify-between gap-4">
                  <div class="flex items-center gap-4 min-w-0">
                    <div class="w-2 md:w-2.5 h-2 md:h-2.5 rounded-full shrink-0 mt-1" 
                         :class="p.agentConnected ? 'bg-green-500 shadow-[0_0_8px_rgba(34,197,94,0.4)]' : 'bg-gray-300 dark:bg-zinc-600'"></div>
                    <div class="min-w-0">
                      <h3 class="font-black text-base text-gray-800 dark:text-zinc-200 truncate group-hover:text-black dark:group-hover:text-white transition-colors">{{ p.name }}</h3>
                      <div class="flex items-center gap-1.5 mt-0.5">
                        <span class="text-[9px] font-black text-gray-500 dark:text-zinc-500 uppercase tracking-widest">{{ p.agentConnected ? 'Live' : 'Offline' }}</span>
                      </div>
                    </div>
                  </div>
                  
                  <div class="shrink-0">
                    <svg class="w-5 h-5 text-gray-300 dark:text-zinc-400 group-hover:text-gray-900 dark:group-hover:text-white transition-colors" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M13 7l5 5m0 0l-5 5m5-5H6" /></svg>
                  </div>
                </div>

                <p v-if="p.description" class="text-xs text-gray-500 dark:text-zinc-400 line-clamp-2 leading-relaxed font-medium bg-gray-50/50 dark:bg-zinc-800/30 p-3 rounded-sm border border-gray-100 dark:border-zinc-800">
                  {{ p.description }}
                </p>
            </div>
          </div>
        </section>

        <!-- Empty State for Search -->
        <div v-if="filteredActiveWorkspaces.length === 0 && searchQuery && !loadingWorkspaces" class="py-16 text-center border border-dashed border-gray-200 dark:border-zinc-800 rounded-sm bg-gray-50 dark:bg-zinc-900/50">
          <svg class="mx-auto h-12 w-12 text-gray-300 dark:text-zinc-600 mb-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" /></svg>
          <p class="text-sm font-bold text-gray-800 dark:text-zinc-200">No matches for "{{ searchQuery }}"</p>
          <button @click="searchQuery = ''" class="mt-4 text-xs font-semibold text-gray-600 dark:text-zinc-300 border border-gray-300 dark:border-zinc-600 rounded-sm px-5 py-2 hover:bg-gray-100 dark:hover:bg-zinc-800 transition-colors shadow-sm">Clear Search</button>
        </div>

        <!-- No Workspaces State -->
        <div v-if="activeWorkspaces.length === 0 && !showCreate && !loadingWorkspaces && !searchQuery" class="py-16 text-center border border-dashed border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 rounded-sm">
          <div class="w-16 h-16 bg-gray-50 dark:bg-zinc-800 rounded-sm mx-auto flex items-center justify-center mb-5 border border-gray-100 dark:border-zinc-700">
            <svg class="h-8 w-8 text-gray-300 dark:text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z"></path></svg>
          </div>
          <p class="text-sm font-bold text-gray-700 dark:text-zinc-100">No active workspaces</p>
          <p class="text-xs text-gray-500 dark:text-zinc-400 mt-2 font-bold">Build your first AgentRQ pipeline today.</p>
          <button @click="showCreate = true" class="mt-6 px-6 py-2.5 bg-black dark:bg-white text-white dark:text-zinc-900 rounded-sm border border-transparent text-xs font-semibold transition-all shadow-sm inline-flex items-center gap-2">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M12 4v16m8-8H4"/></svg>
            New Workspace
          </button>
        </div>

        <!-- Archived Workspaces -->
        <section v-if="showArchived" class="border-t border-gray-200 dark:border-zinc-800 pt-8 mt-8">
          <div class="flex items-center gap-4 mb-8">
            <span class="text-[10px] font-black text-gray-500 dark:text-zinc-500">Archived Workspaces</span>
            <div class="flex-1 h-px bg-gray-200 dark:bg-zinc-800"></div>
          </div>

          <div v-if="filteredArchivedWorkspaces.length > 0" class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            <div v-for="p in filteredArchivedWorkspaces"
                 :key="p.id"
                 class="group relative border border-gray-200 dark:border-zinc-800 bg-gray-50/50 dark:bg-zinc-900/50 rounded-sm p-6 opacity-60 hover:opacity-100 hover:border-gray-300 dark:hover:border-zinc-700 transition-all cursor-pointer flex flex-col gap-4"
                 @click="goToWorkspace(p.id)">
              
              <div class="flex items-start justify-between gap-4">
                <div class="flex items-center gap-4 min-w-0">
                  <div class="w-1.5 h-1.5 rounded-full bg-gray-300 dark:bg-zinc-600 shrink-0 mt-1"></div>
                  <div class="min-w-0">
                    <div class="font-black text-base text-gray-500 dark:text-zinc-400 group-hover:text-gray-900 dark:group-hover:text-zinc-100 transition-colors truncate">{{ p.name }}</div>
                    <div class="text-[9px] font-black text-amber-600/70 truncate mt-0.5 uppercase tracking-widest">Archived {{ new Date(p.archivedAt).toLocaleDateString() }}</div>
                  </div>
                </div>

                <div class="flex items-center gap-1 shrink-0">
                  <button @click.stop="toggleArchive(p)" class="text-gray-500 dark:text-zinc-500 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-white dark:hover:bg-zinc-800 transition-all p-2 rounded-sm border border-transparent shadow-sm hover:border-gray-200 dark:hover:border-zinc-700" title="Restore Workspace">
                    <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M3 10h10a5 5 0 015 5v2M3 10l5-5M3 10l5 5"/></svg>
                  </button>
                </div>
              </div>

              <p v-if="p.description" class="text-xs text-gray-500 dark:text-zinc-500 line-clamp-1 leading-relaxed font-medium">
                {{ p.description }}
              </p>
            </div>
          </div>
          <div v-else class="py-12 text-center text-[10px] font-black text-gray-500 dark:text-zinc-600 border border-dashed border-gray-200 dark:border-zinc-800 rounded-sm bg-gray-50 dark:bg-zinc-900/50">
            No archived workspaces found
          </div>
        </section>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted, computed, watch } from 'vue';
import { useRouter } from 'vue-router';
import { fetchWorkspaces, createWorkspace, unarchiveWorkspace } from '../api';
import { useToasts } from '../composables/useToasts';
import { useEventBus } from '../useEventBus';

const router = useRouter();
const { notifySuccess, notifyError } = useToasts();
const workspaces = ref([]);
const showCreate = ref(false);
const showArchived = ref(false);
const searchQuery = ref('');
const loading = ref(false);
const loadingWorkspaces = ref(true);
const iconError = ref('');
const createFileInput = ref(null);
const error = ref(null);

const form = ref({ name: '', description: '', icon: '', selfLearningLoopNote: '' });

const activeWorkspaces = computed(() => {
  return workspaces.value.filter(p => !p.archivedAt);
});

const archivedWorkspaces = computed(() => {
  return workspaces.value.filter(p => !!p.archivedAt);
});

const filteredActiveWorkspaces = computed(() => {
  if (!searchQuery.value) return activeWorkspaces.value;
  const q = searchQuery.value.toLowerCase();
  return activeWorkspaces.value.filter(p => 
    p.name.toLowerCase().includes(q) || 
    (p.description && p.description.toLowerCase().includes(q))
  );
});

const filteredArchivedWorkspaces = computed(() => {
  if (!searchQuery.value) return archivedWorkspaces.value;
  const q = searchQuery.value.toLowerCase();
  return archivedWorkspaces.value.filter(p => 
    p.name.toLowerCase().includes(q) || 
    (p.description && p.description.toLowerCase().includes(q))
  );
});

async function loadWorkspaces() {
  try {
    const res = await fetchWorkspaces(true); // Include archived
    workspaces.value = res.workspaces || [];
    if (workspaces.value.length === 0) {
      showCreate.value = true;
    }
  } catch (err) {
    error.value = err.message;
  } finally {
    loadingWorkspaces.value = false;
  }
}

watch(showCreate, (val) => {
  if (!val) {
    form.value = { name: '', description: '', icon: '', selfLearningLoopNote: '' };
    iconError.value = '';
  }
});

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

async function submit() {
  if (!form.value.name.trim()) return;
  loading.value = true;
  error.value = null;
  try {
    const res = await createWorkspace(form.value.name, form.value.description, form.value.icon, form.value.selfLearningLoopNote);
    const newId = res.workspace?.id || res.id;
    
    showCreate.value = false;
    form.value = { name: '', description: '', icon: '', selfLearningLoopNote: '' };
    iconError.value = ''; 
    
    if (newId) {
      router.push(`/workspaces/${newId}`);
    } else {
      await loadWorkspaces();
    }
  } catch (err) {
    error.value = 'Failed to create workspace: ' + err.message;
  } finally {
    loading.value = false;
  }
}

async function toggleArchive(p) {
  try {
    if (p.archivedAt) {
      await unarchiveWorkspace(p.id);
      notifySuccess('Workspace protocol restored');
    }
    await loadWorkspaces();
  } catch (err) {
    notifyError(err.message, 'Operation Failed');
  }
}

function goToWorkspace(id) {
  router.push(`/workspaces/${id}`);
}

const { connect, disconnect, events } = useEventBus();

watch(events, (newEvents) => {
  if (newEvents.length === 0) return;
  const event = newEvents[newEvents.length - 1];

  if (event.type === 'agent.connected') {
    const { connected, workspaceId } = event.payload;
    const ws = workspaces.value.find(w => w.id === workspaceId);
    if (ws) {
      ws.agentConnected = connected;
    }
  }

  if (event.type === 'workspace.updated') {
    const updatedWs = event.payload;
    const idx = workspaces.value.findIndex(w => w.id === updatedWs.id);
    if (idx !== -1) {
      workspaces.value[idx] = { ...workspaces.value[idx], ...updatedWs };
    }
  }
}, { deep: true });

onMounted(async () => {
  await loadWorkspaces();
  connect();
});

onUnmounted(() => {
  disconnect();
});
</script>

<style scoped>
.fade-down-enter-active,
.fade-down-leave-active {
  transition: all 0.3s cubic-bezier(0.16, 1, 0.3, 1);
}

.fade-down-enter-from,
.fade-down-leave-to {
  opacity: 0;
  transform: translateY(-10px);
}
</style>
