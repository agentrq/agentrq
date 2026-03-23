<template>
  <div class="space-y-8 p-6 md:p-8">
    <div class="border-b border-b-gray-100 pb-6 flex flex-col md:flex-row md:items-end justify-between gap-6">
      <div class="flex-1">
        <h1 class="text-2xl font-bold text-gray-900 tracking-tight">Workspaces</h1>
        <p class="text-sm font-medium text-gray-400 mt-1 uppercase tracking-widest">Manage your AgentRQ pipelines</p>
        
        <!-- Search Bar -->
        <div class="mt-6 relative max-w-md group">
          <svg class="absolute left-4 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400 group-focus-within:text-black transition-colors" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" /></svg>
          <input v-model="searchQuery" 
                 type="text" 
                 placeholder="Search workspaces by name or description..." 
                 class="w-full pl-11 pr-4 py-3 bg-gray-50 border border-gray-100 rounded-2xl text-sm focus:ring-1 focus:ring-black focus:border-black outline-none transition-all placeholder:text-gray-300 font-medium shadow-sm" />
        </div>
      </div>
      
      <div class="flex items-center gap-3">
         <button @click="showArchived = !showArchived" 
                 class="px-5 py-3 rounded-lg text-[11px] font-black uppercase tracking-widest transition-all border flex items-center gap-2"
                 :class="showArchived ? 'bg-indigo-50 text-indigo-600 border-indigo-100' : 'bg-white text-gray-400 border-gray-100 hover:text-black'">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4" /></svg>
            {{ showArchived ? 'Hide Archived' : 'Show Archived' }}
         </button>
         <button @click="showCreate = true" class="bg-black text-white px-6 py-3 rounded-lg hover:bg-zinc-800 transition-all text-[11px] font-black uppercase tracking-widest active:scale-95">
           New Workspace
         </button>
      </div>
    </div>

    <!-- Create workspace form -->
    <Transition name="fade-down">
      <div v-if="showCreate" class="bg-white rounded-xl border border-gray-100 mb-8 animate-in slide-in-from-top-4 duration-300">
        <div class="px-8 py-6 border-b border-gray-100 bg-gray-50/50 flex justify-between items-center">
          <h2 class="text-lg font-bold text-gray-900">Initialize New Workspace</h2>
          <button @click="showCreate = false" class="text-gray-400 hover:text-black transition-colors p-2">
            <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M6 18L18 6M6 6l12 12" /></svg>
          </button>
        </div>
        <div class="p-8">
          <form @submit.prevent="submit" class="grid grid-cols-1 gap-6">
            <div class="grid grid-cols-1 md:grid-cols-4 gap-6">
              <div class="md:col-span-1 space-y-2">
                <label class="block text-[10px] font-black text-gray-400 uppercase tracking-widest ml-1">Icon (32x32, 64KB)</label>
                <div class="relative group">
                  <input type="file" ref="createFileInput" class="hidden" accept="image/*" @change="handleIconUpload" />
                  <div @click="$refs.createFileInput.click()" class="w-full bg-gray-50 border-2 border-dashed border-gray-200 rounded-lg h-[46px] flex items-center justify-center cursor-pointer hover:border-black transition-all overflow-hidden group">
                    <img v-if="form.icon" :src="form.icon" class="w-8 h-8 object-contain" />
                    <svg v-else class="w-5 h-5 text-gray-300 group-hover:text-black transition-colors" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" /></svg>
                  </div>
                </div>
                <p v-if="iconError" class="text-[9px] font-bold text-red-500 mt-1 uppercase tracking-widest">{{ iconError }}</p>
              </div>
              <div class="md:col-span-3 space-y-2">
                <label class="block text-[10px] font-black text-gray-400 uppercase tracking-widest ml-1">Workspace Name</label>
                <input v-model="form.name" type="text" required class="w-full bg-gray-50 border border-gray-200 rounded-lg px-4 py-3 text-sm focus:ring-1 focus:ring-black focus:border-black outline-none font-semibold text-gray-900 transition-all" placeholder="e.g. Workspace Redstone" />
              </div>
            </div>
            <div class="space-y-2">
              <label class="block text-[10px] font-black text-gray-400 uppercase tracking-widest ml-1">Vision / Description</label>
              <textarea v-model="form.description" rows="3" class="w-full bg-gray-50 border border-gray-200 rounded-lg px-4 py-3 text-sm focus:ring-1 focus:ring-black focus:border-black outline-none font-medium text-gray-800 transition-all resize-none" placeholder="What are we building together? Describe the mission of this workspace..."></textarea>
            </div>
            <div class="flex justify-end gap-3 pt-4">
              <button type="submit" class="bg-black text-white px-8 py-3.5 rounded-xl text-xs font-black uppercase tracking-widest hover:bg-zinc-800 shadow-xl shadow-black/10 transition-all active:scale-95 flex items-center gap-2" :disabled="loading">
                <svg v-if="loading" class="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M4 12a8 8 0 018-8v8H4z" /></svg>
                {{ loading ? 'Initializing...' : 'Create Workspace' }}
              </button>
            </div>
          </form>
        </div>
      </div>
    </Transition>

    <div v-if="error" class="bg-red-50 border border-red-100 text-red-600 px-6 py-4 rounded-2xl text-xs font-bold uppercase tracking-widest mb-8 animate-in bounce-in duration-300">
      {{ error }}
    </div>

    <!-- Workspace list -->
    <div v-if="loadingWorkspaces" class="text-xs font-bold text-gray-400 uppercase tracking-widest animate-pulse flex items-center gap-3">
      <div class="w-2 h-2 rounded-full bg-black"></div>
      Loading Workspaces...
    </div>
    
    <div v-else class="space-y-12">
      <!-- Active Workspaces -->
      <section v-if="filteredActiveWorkspaces.length > 0">
        <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          <div v-for="p in filteredActiveWorkspaces" 
               :key="p.id" 
               class="bg-white rounded-xl border border-gray-100 p-6 hover:border-gray-200 transition-all duration-300 cursor-pointer group flex flex-col justify-between relative overflow-hidden" 
               @click="goToWorkspace(p.id)">
            
            <div class="absolute top-0 right-0 w-24 h-24 bg-indigo-50/10 rounded-full blur-2xl -mr-12 -mt-12 group-hover:bg-indigo-100/20 transition-colors duration-500"></div>

            <div class="relative z-10">
              <div class="flex justify-between items-start mb-4">
              <div class="w-8 h-8 rounded-xl bg-gray-50 flex items-center justify-center group-hover:bg-black group-hover:text-white transition-all duration-300 shadow-inner overflow-hidden relative">
                <template v-if="p.icon">
                  <img v-if="p.icon.startsWith('data:image')" :src="p.icon" class="w-full h-full object-cover" />
                  <span v-else class="text-sm">{{ p.icon }}</span>
                </template>
                <svg v-else viewBox="0 0 24 24" class="w-4 h-4" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" />
                  <path d="M12 7l-3.5 8" />
                  <path d="M12 7l3.5 8" />
                  <path d="M9.5 12h5" />
                </svg>
                <!-- Agent connection dot -->
                <div v-if="p.agent_connected" class="absolute bottom-0 right-0 w-2.5 h-2.5 bg-green-500 border-2 border-white rounded-full transition-all"></div>
              </div>
                <button @click.stop="removeWorkspace(p.id)" class="text-gray-100 hover:text-red-500 transition-all p-1.5 hover:bg-red-50 rounded-lg">
                  <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"></path></svg>
                </button>
              </div>
              <h3 class="text-lg font-bold text-gray-900 mb-1 truncate group-hover:text-indigo-600 transition-colors">{{ p.name }}</h3>
              <p class="text-[12px] font-medium text-gray-400 line-clamp-2 leading-relaxed">{{ p.description || 'No vision defined yet.' }}</p>
            </div>
            
            <div class="mt-6 pt-4 border-t border-gray-50 flex items-center justify-between relative z-10 text-[9px] uppercase font-black tracking-widest">
              <span class="text-gray-300 group-hover:text-black transition-colors">{{ new Date(p.created_at).toLocaleDateString() }}</span>
              <div class="flex items-center gap-1 text-black opacity-0 group-hover:opacity-100 transition-all transform translate-x-2 group-hover:translate-x-0">
                Go to Workspace
                <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M13 7l5 5-5 5M6 7l5 5-5 5" /></svg>
              </div>
            </div>
          </div>
        </div>
      </section>

      <!-- Empty State for Search -->
      <div v-if="filteredActiveWorkspaces.length === 0 && searchQuery && !loadingWorkspaces" class="py-20 text-center bg-white rounded-[3rem] border-2 border-dashed border-gray-100">
         <svg class="mx-auto h-16 w-16 text-gray-100 mb-6" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" /></svg>
         <p class="text-lg font-bold text-gray-900">No matches for "{{ searchQuery }}"</p>
         <p class="text-sm text-gray-400 mt-2">Try adjusting your search criteria.</p>
         <button @click="searchQuery = ''" class="mt-6 text-xs font-black uppercase tracking-widest text-indigo-600 hover:text-indigo-800 p-2">Clear Search</button>
      </div>

      <!-- No Workspaces State -->
      <div v-if="activeWorkspaces.length === 0 && !showCreate && !loadingWorkspaces && !searchQuery" class="py-24 text-center bg-gray-50 border-2 border-dashed border-gray-200 rounded-[3rem]">
        <svg class="mx-auto h-16 w-16 text-gray-200 mb-6" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1" d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z"></path></svg>
        <p class="text-lg font-bold text-gray-900">No active workspaces</p>
        <p class="text-sm text-gray-400 mt-2 font-medium">Build your first AgentRQ pipeline today.</p>
        <button @click="showCreate = true" class="mt-8 px-8 py-4 bg-black text-white rounded-2xl text-xs font-black uppercase tracking-widest hover:bg-zinc-800 transition-all shadow-2xl shadow-black/20">Create Workspace</button>
      </div>

      <!-- Archived Workspaces -->
      <section v-if="showArchived" class="border-t border-gray-100 pt-12 animate-in fade-in slide-in-from-bottom-6 duration-700">
        <div class="flex items-center gap-4 mb-8">
           <span class="text-[10px] font-black uppercase tracking-[0.3em] text-gray-300">Vaulted Workspaces</span>
           <div class="flex-1 h-px bg-gray-100"></div>
        </div>
        
        <div v-if="filteredArchivedWorkspaces.length > 0" class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-8">
          <div v-for="p in filteredArchivedWorkspaces" 
               :key="p.id" 
               class="bg-gray-100/50 grayscale-[0.8] opacity-60 rounded-[2.5rem] border border-gray-200/50 p-8 hover:grayscale-0 hover:opacity-100 hover:bg-white hover:shadow-2xl hover:shadow-gray-200/50 transition-all duration-500 cursor-pointer group flex flex-col justify-between"
               @click="goToWorkspace(p.id)">
            
            <div>
              <div class="flex justify-between items-start mb-6">
              <div class="w-10 h-10 rounded-xl bg-gray-50 flex items-center justify-center shadow-inner border border-gray-100 overflow-hidden">
                <template v-if="p.icon">
                  <img v-if="p.icon.startsWith('data:image')" :src="p.icon" class="w-full h-full object-cover" />
                  <span v-else class="text-xl">{{ p.icon }}</span>
                </template>
                <svg v-else viewBox="0 0 24 24" class="w-6 h-6 text-black" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" />
                  <path d="M12 7l-3.5 8" />
                  <path d="M12 7l3.5 8" />
                  <path d="M9.5 12h5" />
                </svg>
              </div>
                <div class="flex items-center gap-1">
                  <button @click.stop="restoreWorkspace(p.id)" class="text-gray-300 hover:text-indigo-600 transition-all p-2 hover:bg-indigo-50 rounded-xl" title="Restore Workspace">
                    <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path d="M3 10h10a5 5 0 015 5v2M3 10l5-5M3 10l5 5"/></svg>
                  </button>
                  <button @click.stop="removeWorkspace(p.id)" class="text-gray-300 hover:text-red-500 transition-all p-2 hover:bg-red-50 rounded-xl">
                    <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"></path></svg>
                  </button>
                </div>
              </div>
              <h3 class="text-xl font-black text-gray-500 mb-2 truncate group-hover:text-black transition-colors">{{ p.name }}</h3>
              <div class="inline-flex items-center gap-1.5 text-[9px] font-black uppercase tracking-widest text-red-400 bg-red-50 px-2.5 py-1 rounded-md mb-4 border border-red-100/50">
                Archived
              </div>
            </div>
            
            <div class="mt-8 pt-6 border-t border-gray-100 flex items-center justify-between text-[10px] uppercase font-black tracking-widest">
              <span class="text-gray-400">Archived {{ new Date(p.ArchivedAt).toLocaleDateString() }}</span>
              <svg class="w-4 h-4 text-gray-300 group-hover:text-black transition-all" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M9 5l7 7-7 7"></path></svg>
            </div>
          </div>
        </div>
        <div v-else class="py-12 text-center text-xs font-bold text-gray-300 uppercase tracking-widest">
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
import { fetchWorkspaces, createWorkspace, deleteWorkspace, unarchiveWorkspace } from '../api';
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

async function restoreWorkspace(id) {
  try {
    await unarchiveWorkspace(id);
    await loadWorkspaces();
    notifySuccess('Workspace protocol restored');
  } catch (err) {
    notifyError(err.message, 'Restoration Failed');
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
