<template>
  <div class="space-y-6">
    <!-- Page Header -->
    <div class="border-b-2 border-black pb-5 flex flex-col md:flex-row md:items-end justify-between gap-4">
      <div class="flex-1">
        <div class="flex items-center justify-between md:justify-start gap-3 mb-1">
          <div class="flex items-center gap-3">
            <h1 class="text-2xl font-black text-black uppercase tracking-tight">Workspaces</h1>
            <span v-if="!loadingWorkspaces" class="border-2 border-black px-2 py-0.5 text-[10px] font-black uppercase tracking-widest bg-white">
              {{ activeWorkspaces.length }}
            </span>
          </div>

          <!-- Mobile Actions -->
          <div class="md:hidden flex items-center gap-1.5">
            <button @click="showArchived = !showArchived"
                    class="p-2 transition-all border-2 border-black"
                    :class="showArchived ? 'bg-black text-[#00FF88]' : 'bg-white text-gray-500'"
                    title="Toggle Archived">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4" /></svg>
            </button>
            <button @click="showCreate = true" class="bg-[#00FF88] text-black p-2 border-2 border-black shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] active:shadow-none" title="New Workspace">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3.5"><path d="M12 6v6m0 0v6m0-6h6m-6 0H6"/></svg>
            </button>
          </div>
        </div>
        <p class="text-[11px] font-bold text-gray-400 uppercase tracking-widest">Manage your AgentRQ pipelines</p>

        <!-- Search Bar -->
        <div class="mt-4 relative max-w-sm">
          <svg class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" /></svg>
          <input v-model="searchQuery"
                 type="text"
                 placeholder="Search workspaces..."
                 class="w-full pl-10 pr-4 py-2.5 bg-white border-2 border-black text-sm outline-none font-medium placeholder:text-gray-300 focus:border-black shadow-[2px_2px_0px_0px_rgba(0,0,0,1)]" />
        </div>
      </div>

      <div class="hidden md:flex items-center gap-2">
        <button @click="showArchived = !showArchived"
                class="px-4 py-2.5 text-[11px] font-black uppercase tracking-widest transition-all border-2 flex items-center gap-2"
                :class="showArchived ? 'bg-black text-white border-black' : 'bg-white text-gray-500 border-black hover:bg-black hover:text-white'">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4" /></svg>
          {{ showArchived ? 'Hide Archived' : 'Show Archived' }}
        </button>
        <button @click="showCreate = true" class="bg-[#00FF88] text-black px-5 py-2.5 border-2 border-black hover:bg-black hover:text-[#00FF88] transition-all text-[11px] font-black uppercase tracking-widest shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] flex items-center gap-2">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M12 6v6m0 0v6m0-6h6m-6 0H6"/></svg>
          New Workspace
        </button>
      </div>
    </div>

    <!-- Create workspace form -->
    <Transition name="fade-down">
      <div v-if="showCreate" class="fixed inset-0 z-40 flex items-center justify-center p-4 md:relative md:inset-auto md:p-0 md:bg-transparent md:z-10 md:block">
        <!-- Backdrop for mobile -->
        <div class="fixed inset-0 bg-black/60 md:hidden" @click="showCreate = false"></div>
        
        <div class="bg-white border-2 border-black shadow-[4px_4px_0px_0px_rgba(0,0,0,1)] w-full max-w-2xl md:mb-6 shrink-0 z-10 relative flex flex-col max-h-[90vh] md:max-h-none">
        <div class="px-6 py-4 border-b-2 border-black bg-black flex justify-between items-center">
          <h2 class="text-sm font-black text-white uppercase tracking-widest">Initialize New Workspace</h2>
          <button @click="showCreate = false" class="text-white/60 hover:text-[#00FF88] transition-colors p-1 border border-white/20 hover:border-[#00FF88]">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M6 18L18 6M6 6l12 12" /></svg>
          </button>
        </div>
        <div class="p-6">
          <form @submit.prevent="submit" class="grid grid-cols-1 gap-5">
            <div class="grid grid-cols-1 md:grid-cols-4 gap-5">
              <div class="md:col-span-1 space-y-1.5">
                <label class="block text-[10px] font-black text-gray-500 uppercase tracking-widest">Icon (32x32, 64KB)</label>
                <div class="relative">
                  <input type="file" ref="createFileInput" class="hidden" accept="image/*" @change="handleIconUpload" />
                  <div @click="$refs.createFileInput.click()" class="w-full bg-gray-50 border-2 border-dashed border-gray-300 h-[46px] flex items-center justify-center cursor-pointer hover:border-black transition-all overflow-hidden">
                    <img v-if="form.icon" :src="form.icon" class="w-8 h-8 object-contain" />
                    <svg v-else class="w-5 h-5 text-gray-300 hover:text-black transition-colors" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" /></svg>
                  </div>
                </div>
                <p v-if="iconError" class="text-[9px] font-bold text-red-500 uppercase tracking-widest">{{ iconError }}</p>
              </div>
              <div class="md:col-span-3 space-y-1.5">
                <label class="block text-[10px] font-black text-gray-500 uppercase tracking-widest">Workspace Name</label>
                <input v-model="form.name" type="text" required class="w-full bg-white border-2 border-black px-4 py-2.5 text-sm outline-none font-bold text-gray-900 focus:shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] transition-all" placeholder="e.g. my-saas-backend" />
              </div>
            </div>
            <div class="space-y-1.5">
              <label class="block text-[10px] font-black text-gray-500 uppercase tracking-widest">Mission / Description</label>
              <textarea v-model="form.description" rows="3" class="w-full bg-white border-2 border-black px-4 py-2.5 text-sm outline-none font-medium text-gray-800 transition-all resize-none focus:shadow-[2px_2px_0px_0px_rgba(0,0,0,1)]" placeholder="What are we building? Describe the mission of this workspace..."></textarea>
            </div>
            <div class="flex justify-end gap-3 pt-2">
              <button type="button" @click="showCreate = false" class="px-5 py-2.5 border-2 border-black text-xs font-black uppercase tracking-widest hover:bg-gray-100 transition-all">Cancel</button>
              <button type="submit" class="bg-black text-white px-6 py-2.5 border-2 border-black text-xs font-black uppercase tracking-widest hover:bg-[#00FF88] hover:text-black transition-all shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] flex items-center gap-2" :disabled="loading">
                <svg v-if="loading" class="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M4 12a8 8 0 018-8v8H4z" /></svg>
                {{ loading ? 'Initializing...' : 'Create Workspace' }}
              </button>
            </div>
          </form>
        </div>
        </div>
      </div>
    </Transition>

    <div v-if="error" class="bg-red-50 border-2 border-red-500 text-red-700 px-5 py-3 text-xs font-bold uppercase tracking-widest">
      {{ error }}
    </div>

    <!-- Workspace list -->
    <div v-if="loadingWorkspaces" class="text-xs font-black text-gray-400 uppercase tracking-widest animate-pulse flex items-center gap-3 py-8">
      <div class="w-2 h-2 bg-black animate-pulse"></div>
      Loading Workspaces...
    </div>

    <div v-else class="space-y-10">
      <!-- Active Workspaces -->
      <section v-if="filteredActiveWorkspaces.length > 0">
        <div class="text-[9px] font-black text-gray-400 uppercase tracking-[0.3em] mb-4">Your workspaces</div>
        <div class="space-y-3">
          <div v-for="p in filteredActiveWorkspaces"
               :key="p.id"
               class="border-2 border-black p-4 cursor-pointer group flex flex-col justify-between transition-all duration-150 hover:shadow-[4px_4px_0px_0px_rgba(0,0,0,1)] hover:-translate-y-0.5"
               :class="'bg-white'"
               @click="goToWorkspace(p.id)">

            <div class="flex items-center justify-between mb-3">
              <div class="flex items-center gap-3">
                <!-- Status dot -->
                <div class="w-3 h-3 rounded-full border border-black"
                     :class="p.agent_connected ? 'bg-green-500 animate-pulse' : 'bg-gray-300'"></div>
                <!-- Icon -->
                <div class="w-8 h-8 border-2 border-black bg-white flex items-center justify-center overflow-hidden shrink-0">
                  <template v-if="p.icon">
                    <img v-if="p.icon.startsWith('data:image')" :src="p.icon" class="w-full h-full object-cover" />
                    <span v-else class="text-sm">{{ p.icon }}</span>
                  </template>
                  <svg v-else viewBox="0 0 24 24" class="w-4 h-4" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" />
                    <path d="M12 7l-3.5 8" /><path d="M12 7l3.5 8" /><path d="M9.5 12h5" />
                  </svg>
                </div>
                <span class="font-black text-sm uppercase tracking-tight">{{ p.name }}</span>
              </div>
              <div class="flex items-center gap-2">
                <span v-if="p.agent_connected" class="text-[10px] font-black bg-black text-white px-2 py-0.5 uppercase tracking-widest">Claude connected</span>
                <span v-else class="text-[10px] font-bold text-gray-500 uppercase tracking-widest">Idle</span>
                
                <div class="flex items-center border-l-2 border-black/5 ml-1 pl-2 gap-1 opacity-100 sm:opacity-0 sm:group-hover:opacity-100 transition-opacity">
                  <button @click.stop="toggleArchive(p)" 
                          class="p-1 hover:bg-gray-100 border border-transparent hover:border-black transition-all"
                          title="Archive workspace">
                    <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4" />
                    </svg>
                  </button>
                  <button @click.stop="removeWorkspace(p.id)" 
                          class="p-1 hover:bg-red-50 border border-transparent hover:border-red-600 group/del transition-all"
                          title="Delete workspace">
                    <svg class="w-3.5 h-3.5 text-gray-400 group-hover/del:text-red-600" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                      <path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"></path>
                    </svg>
                  </button>
                </div>
              </div>
            </div>

            <p v-if="p.description" class="text-[11px] font-medium line-clamp-2 leading-relaxed pl-1 text-black/50 mb-3"
            >{{ p.description }}</p>

            <div class="mt-4 pt-4 border-t-2 border-black/10 flex items-center justify-between text-[9px] uppercase font-black tracking-widest">
              <span class="text-black/30">{{ new Date(p.created_at).toLocaleDateString() }}</span>
              <div class="flex items-center gap-1 opacity-100 sm:opacity-0 sm:group-hover:opacity-100 transition-all">
                Open
                <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M9 5l7 7-7 7" /></svg>
              </div>
            </div>
          </div>
        </div>
      </section>

      <!-- Empty State for Search -->
      <div v-if="filteredActiveWorkspaces.length === 0 && searchQuery && !loadingWorkspaces" class="py-16 text-center border-2 border-dashed border-gray-300">
        <svg class="mx-auto h-12 w-12 text-gray-200 mb-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" /></svg>
        <p class="text-sm font-black text-gray-900 uppercase">No matches for "{{ searchQuery }}"</p>
        <button @click="searchQuery = ''" class="mt-4 text-xs font-black uppercase tracking-widest text-black border-2 border-black px-4 py-2 hover:bg-black hover:text-white transition-colors">Clear Search</button>
      </div>

      <!-- No Workspaces State -->
      <div v-if="activeWorkspaces.length === 0 && !showCreate && !loadingWorkspaces && !searchQuery" class="py-16 text-center border-2 border-dashed border-gray-300 bg-gray-50">
        <svg class="mx-auto h-12 w-12 text-gray-200 mb-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1" d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z"></path></svg>
        <p class="text-sm font-black text-gray-900 uppercase tracking-widest">No active workspaces</p>
        <p class="text-xs text-gray-400 mt-2 font-bold uppercase tracking-widest">Build your first AgentRQ pipeline today.</p>
        <button @click="showCreate = true" class="mt-6 px-6 py-3 bg-[#00FF88] text-black border-2 border-black text-xs font-black uppercase tracking-widest hover:bg-black hover:text-[#00FF88] transition-all shadow-[2px_2px_0px_0px_rgba(0,0,0,1)]">+ New Workspace</button>
      </div>

      <!-- Archived Workspaces -->
      <section v-if="showArchived" class="border-t-2 border-black pt-8">
        <div class="flex items-center gap-4 mb-6">
          <span class="text-[9px] font-black uppercase tracking-[0.3em] text-gray-400">Archived Workspaces</span>
          <div class="flex-1 h-0.5 bg-gray-200"></div>
        </div>

        <div v-if="filteredArchivedWorkspaces.length > 0" class="space-y-2">
          <div v-for="p in filteredArchivedWorkspaces"
               :key="p.id"
               class="border-2 border-gray-300 bg-gray-50 p-4 opacity-60 hover:opacity-100 hover:border-black hover:shadow-[4px_4px_0px_0px_rgba(0,0,0,1)] transition-all cursor-pointer group flex items-center justify-between"
               @click="goToWorkspace(p.id)">
            <div class="flex items-center gap-3">
              <div class="w-7 h-7 border border-gray-300 bg-white flex items-center justify-center overflow-hidden">
                <template v-if="p.icon">
                  <img v-if="p.icon.startsWith('data:image')" :src="p.icon" class="w-full h-full object-cover" />
                  <span v-else class="text-sm">{{ p.icon }}</span>
                </template>
                <svg v-else viewBox="0 0 24 24" class="w-4 h-4 text-gray-400" fill="none" stroke="currentColor" stroke-width="2">
                  <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" />
                  <path d="M12 7l-3.5 8" /><path d="M12 7l3.5 8" /><path d="M9.5 12h5" />
                </svg>
              </div>
              <div>
                <span class="font-black text-sm text-gray-600 group-hover:text-black uppercase tracking-tight transition-colors">{{ p.name }}</span>
                <div class="text-[9px] font-black text-red-400 uppercase tracking-widest">Archived {{ new Date(p.ArchivedAt).toLocaleDateString() }}</div>
              </div>
            </div>
            <div class="flex items-center gap-1">
              <button @click.stop="toggleArchive(p)" class="text-gray-400 hover:text-black transition-all p-1.5 border border-transparent hover:border-black" title="Restore Workspace">
                <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path d="M3 10h10a5 5 0 015 5v2M3 10l5-5M3 10l5 5"/></svg>
              </button>
              <button @click.stop="removeWorkspace(p.id)" class="text-gray-400 hover:text-red-600 transition-all p-1.5 border border-transparent hover:border-red-300">
                <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"></path></svg>
              </button>
            </div>
          </div>
        </div>
        <div v-else class="py-8 text-center text-xs font-black text-gray-300 uppercase tracking-widest border-2 border-dashed border-gray-200">
          No archived workspaces found
        </div>
      </section>
    </div>

    <!-- Purge Confirmation Modal -->
    <DeleteModal
      :show="showPurgeModal"
      :taskTitle="workspaceToPurge?.name || ''"
      title="Purge Workspace"
      @close="closePurgeModal"
      @confirm="onPurgeConfirm"
    />
  </div>
</template>

<script setup>
import { ref, onMounted, computed, watch } from 'vue';
import { useRouter } from 'vue-router';
import { fetchWorkspaces, createWorkspace, deleteWorkspace, archiveWorkspace, unarchiveWorkspace } from '../api';
import { useToasts } from '../composables/useToasts';
import DeleteModal from '../components/DeleteModal.vue';

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
const showPurgeModal = ref(false);
const workspaceToPurge = ref(null);
const error = ref(null);

const form = ref({ name: '', description: '', icon: '' });

const activeWorkspaces = computed(() => {
  return workspaces.value.filter(p => !p.ArchivedAt);
});

const archivedWorkspaces = computed(() => {
  return workspaces.value.filter(p => !!p.ArchivedAt);
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
    form.value = { name: '', description: '', icon: '' };
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
    await createWorkspace(form.value.name, form.value.description, form.value.icon);
    showCreate.value = false;
    form.value = { name: '', description: '', icon: '' };
    iconError.value = ''; // Clear icon error on successful submission
    await loadWorkspaces();
  } catch (err) {
    error.value = 'Failed to create workspace: ' + err.message;
  } finally {
    loading.value = false;
  }
}

async function toggleArchive(p) {
  try {
    if (p.ArchivedAt) {
      await unarchiveWorkspace(p.id);
      notifySuccess('Workspace protocol restored');
    } else {
      await archiveWorkspace(p.id);
      notifySuccess('Workspace archived');
    }
    await loadWorkspaces();
  } catch (err) {
    notifyError(err.message, 'Operation Failed');
  }
}

async function removeWorkspace(id) {
  const p = workspaces.value.find(x => x.id === id);
  if (!p) return;
  workspaceToPurge.value = p;
  showPurgeModal.value = true;
}

function closePurgeModal() {
  showPurgeModal.value = false;
  workspaceToPurge.value = null;
}

async function onPurgeConfirm() {
  const id = workspaceToPurge.value?.id;
  if (!id) return;
  
  try {
    await deleteWorkspace(id);
    await loadWorkspaces();
    notifySuccess('Workspace and all data purged', 'Purge Complete');
  } catch (err) {
    notifyError('Failed to purge workspace: ' + err.message, 'Purge Error');
  } finally {
    closePurgeModal();
  }
}

function goToWorkspace(id) {
  router.push(`/workspaces/${id}`);
}

onMounted(loadWorkspaces);
</script>

<style scoped>
.fade-down-enter-active,
.fade-down-leave-active {
  transition: all 0.3s cubic-bezier(0.16, 1, 0.3, 1);
}

.fade-down-enter-from,
.fade-down-leave-to {
  opacity: 0;
  transform: translateY(-20px);
}
</style>
