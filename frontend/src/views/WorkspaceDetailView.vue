<template>
  <div class="flex flex-col h-full w-full bg-transparent">
    
    <div v-if="loading" class="flex-1 flex flex-col items-center justify-center py-20 text-sm font-bold text-gray-500 dark:text-zinc-500 animate-pulse">
      Loading workspace context...
    </div>
    
    <div v-else-if="error" class="flex-1 text-center py-6 text-sm font-bold text-red-600 border border-red-300 bg-red-50 p-4 rounded-sm">
      {{ error }}
    </div>
    
    <template v-else>
      <!-- Global Header -->
      <!-- Global Header -->
      <div class="w-full px-4 py-2 mb-6 shrink-0 flex flex-col sm:flex-row sm:items-center justify-between gap-3 sm:gap-4"
           :class="{'hidden sm:flex': selectedTaskId}">
        
        <!-- Title Row -->
        <div class="flex items-center gap-3 min-w-0 flex-1">
          <!-- Agent Status Heartbeat -->
          <div class="relative flex items-center justify-center shrink-0 cursor-help"
               @mouseenter="tooltipStore.show($event, isAgentConnected ? 'Agent Online' : 'Agent Offline', 'bottom')"
               @mouseleave="tooltipStore.hide()">
            <div :class="isAgentConnected ? 'bg-green-500 shadow-[0_0_8px_rgba(34,197,94,0.4)]' : 'bg-red-500 shadow-[0_0_8px_rgba(239,68,68,0.4)]'" 
                 class="w-2 h-2 md:w-2.5 md:h-2.5 rounded-full z-10 border border-white dark:border-zinc-900 transition-colors duration-500"></div>
            <div v-if="isAgentConnected"
                 class="absolute w-2 md:w-2.5 md:h-2.5 rounded-full bg-green-500 animate-ping opacity-75"></div>
          </div>

          <div class="flex flex-col min-w-0 flex-1">
            <h1 class="text-lg md:text-2xl font-black text-gray-800 dark:text-zinc-200 tracking-tight leading-tight truncate">
              <span v-if="$route.path.includes('/settings') || $route.path.includes('/analytics')" class="opacity-50 cursor-pointer hover:opacity-100" @click="router.push(`/workspaces/${workspaceId}`)">{{ workspace?.name }}</span>
              <span v-else>{{ workspace?.name || 'Workspace' }}</span>
              <template v-if="$route.path.includes('/settings') || $route.path.includes('/analytics')">
                <span class="mx-1.5 text-gray-300 dark:text-zinc-700 font-medium">/</span>
                <span class="text-gray-400 dark:text-zinc-500">{{ $route.path.includes('/settings') ? 'Settings' : 'Analytics' }}</span>
              </template>
            </h1>
          </div>
        </div>
        
        <!-- Stats & Actions Row -->
        <div class="flex items-center gap-4 shrink-0 justify-between sm:justify-end">
          <div class="flex items-center gap-2">
            <!-- Filters Toggle & Menu Wrapper -->
            <div class="relative flex items-center">
              <button @click="showMobileFilters = !showMobileFilters" 
                      class="md:hidden h-8 w-8 text-gray-500 hover:bg-gray-100 dark:hover:bg-zinc-800 bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-lg transition-all shadow-sm flex items-center justify-center shrink-0" 
                      :class="{'bg-gray-100 dark:bg-zinc-800 text-black dark:text-white border-black dark:border-white': showMobileFilters}"
                      title="Filters">
                <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M3 4h13M3 8h9m-9 4h6m4 0l4-4m0 0l4 4m-4-4v12" /></svg>
              </button>

              <!-- Filters Segment Control (Top Right) -->
              <div v-if="showMobileFilters || !isMobile" 
                   class="h-8 p-0.5 bg-gray-100 dark:bg-zinc-800 rounded-md border border-gray-200 dark:border-zinc-700/50 shadow-inner mr-2 overflow-x-auto no-scrollbar transition-all duration-300"
                   :class="[showMobileFilters ? 'absolute top-10 left-0 z-50 flex shadow-2xl border-gray-900 dark:border-white w-max animate-in fade-in slide-in-from-top-2' : 'hidden md:flex items-center']">
                <button v-for="f in filters" :key="f.id"
                        @click="activeFilter = f.id; isMobile && (showMobileFilters = false); tooltipStore.hide()"
                        @mouseenter="tooltipStore.show($event, f.label, 'bottom')"
                        @mouseleave="tooltipStore.hide()"
                        :class="[activeFilter === f.id ? 'bg-white dark:bg-zinc-700 text-black dark:text-white shadow-sm' : 'text-gray-500 dark:text-zinc-400 hover:text-gray-700 dark:hover:text-zinc-300']"
                        class="h-7 px-2 rounded-sm transition-all duration-200 flex items-center justify-center">
                  <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                    <path stroke-linecap="round" stroke-linejoin="round" :d="f.icon" />
                  </svg>
                </button>
              </div>
            </div>

            <button @click="router.push(`/workspaces/${workspaceId}/analytics`)" class="h-8 w-8 text-gray-500 dark:text-zinc-400 hover:text-black dark:hover:text-white hover:bg-gray-100 dark:hover:bg-zinc-800 bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-lg transition-all shadow-sm flex items-center justify-center" title="Analytics">
              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M7 12l3-3 3 3 4-4M8 21l4-4 4 4M3 4h18M4 4h16v12a1 1 0 01-1 1H5a1 1 0 01-1-1V4z" /></svg>
            </button>
            <button v-if="!workspace?.archived_at" @click="router.push(`/workspaces/${workspaceId}/settings`)" class="h-8 w-8 text-gray-500 dark:text-zinc-400 hover:text-black dark:hover:text-white hover:bg-gray-100 dark:hover:bg-zinc-800 bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-lg transition-all shadow-sm flex items-center justify-center" title="Workspace Settings">
              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" /></svg>
            </button>
            <div class="w-px h-4 bg-gray-200 dark:bg-zinc-800"></div>
            <button v-if="!workspace?.archived_at" @click="router.push(`/workspaces/${workspaceId}/tasks/new`)" 
                    class="group flex items-center gap-2 bg-gray-900 dark:bg-white text-white dark:text-zinc-900 px-3 h-8 rounded-sm text-[10px] font-bold shadow-sm transition-all hover:bg-gray-800 dark:hover:bg-zinc-100 uppercase tracking-widest"
                    title="New Task">
              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path stroke-linecap="round" stroke-linejoin="round" d="M12 4v16m8-8H4"/></svg>
              <span class="hidden sm:inline">New Task</span>
           </button>
          </div>
        </div>
      </div>

      <!-- Content Area (Split Pane) -->
      <div class="flex flex-col md:flex-row flex-1 min-h-0 w-full bg-transparent">
        <!-- Tasks Sidebar (Left Pane) -->
        <div v-show="!$route.path.endsWith('/analytics') && !$route.path.endsWith('/settings') && (!selectedTaskId || !isMobile)" class="w-full md:w-96 shrink-0 h-full flex flex-col min-h-0 bg-transparent md:border-r border-gray-100 dark:border-zinc-800">
          
          <!-- Task Feed Area -->
          <div class="flex-1 flex flex-col min-h-0 overflow-hidden">
          <TaskFeed
            ref="taskFeed"
            :workspaceId="workspaceId"
            :initialTasks="tasks"
            :liveEvents="events"
            :isArchived="!!workspace?.archived_at"
            :isAgentConnected="isAgentConnected"
            :filter="activeFilter"
            :selectedTaskId="selectedTaskId"
            @filter-change="activeFilter = $event"
          />
        </div>
        </div>
    <!-- Task Detail / Analytics Pane (Right) -->
    <div v-show="selectedTaskId || $route.path.endsWith('/analytics') || $route.path.endsWith('/settings') || !isMobile" 
         class="flex-1 min-w-0 flex flex-col h-full bg-transparent">
      <router-view v-if="selectedTaskId || $route.path.endsWith('/analytics') || $route.path.endsWith('/settings')" />
      
      <!-- Empty state when no task is selected -->
      <div v-else class="flex-1 flex flex-col items-center justify-center m-4 p-8 text-center h-full bg-gray-50 dark:bg-zinc-900/50 rounded-sm border border-dashed border-gray-200 dark:border-zinc-800 animate-in fade-in zoom-in-95 duration-500">
        
        <template v-if="!isAgentConnected">
           <div class="w-16 h-16 bg-white dark:bg-zinc-800 rounded-sm border border-gray-100 dark:border-zinc-700 flex items-center justify-center mb-5 shadow-sm">
             <svg class="w-8 h-8 text-gray-400 dark:text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
               <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M13 10V3L4 14h7v7l9-11h-7z" />
             </svg>
           </div>
           <h3 class="text-xl font-black text-gray-800 dark:text-zinc-100 tracking-tight">{{ tasks.length === 0 ? 'Connect your first Agent' : 'Agent is Offline' }}</h3>
           <p class="text-sm text-gray-500 dark:text-zinc-400 mt-2 max-w-[320px] leading-relaxed font-medium">
             {{ tasks.length === 0 ? 'This workspace is currently offline. Connect Claude or Gemini via MCP to start automating tasks.' : 'This workspace is currently offline. Reconnect Claude or Gemini via MCP to start automating tasks.' }}
           </p>
           <button @click="router.push({ path: `/workspaces/${workspaceId}/settings`, query: { tab: 'setup' } })" class="mt-8 px-8 py-3 bg-black dark:bg-white text-white dark:text-zinc-900 rounded-sm text-[10px] font-black uppercase tracking-widest shadow-lg hover:shadow-xl active:scale-95 transition-all">
             Open Setup Guide
           </button>
        </template>

        <template v-else-if="tasks.length === 0">
           <div class="w-16 h-16 bg-white dark:bg-zinc-800 rounded-sm border border-gray-100 dark:border-zinc-700 flex items-center justify-center mb-5 shadow-sm">
             <svg class="w-8 h-8 text-gray-400 dark:text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
               <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M12 4v16m8-8H4" />
             </svg>
           </div>
           <h3 class="text-xl font-black text-gray-800 dark:text-zinc-100 tracking-tight">No tasks yet</h3>
           <p class="text-sm text-gray-500 dark:text-zinc-400 mt-2 max-w-[320px] leading-relaxed font-medium">Agent is connected and ready. Create your first task to see the agent in action.</p>
           <button @click="router.push(`/workspaces/${workspaceId}/tasks/new`)" class="mt-8 px-8 py-3 bg-black dark:bg-white text-white dark:text-zinc-900 rounded-sm text-[10px] font-black uppercase tracking-widest shadow-lg hover:shadow-xl active:scale-95 transition-all">
             Create First Task
           </button>
        </template>

        <template v-else>
          <div class="w-16 h-16 bg-white dark:bg-zinc-800 rounded-sm border border-gray-100 dark:border-zinc-700 flex items-center justify-center mb-5 shadow-sm">
            <svg class="w-8 h-8 text-gray-400 dark:text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-3 7h3m-3 4h3m-6-4h.01M9 16h.01" />
            </svg>
          </div>
          <h3 class="text-xl font-black text-gray-800 dark:text-zinc-100 tracking-tight">Select a task</h3>
          <p class="text-sm text-gray-500 dark:text-zinc-400 mt-2 max-w-[320px] leading-relaxed font-medium">Choose a task from the list to view its details, conversation history, and take actions.</p>
        </template>
      </div>
    </div>
      </div>
    </template>
  </div>

  <!-- Modals -->
</template>

<script setup>
import { ref, onMounted, watch, computed, onUnmounted } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { getWorkspace, fetchTasks, archiveWorkspace, unarchiveWorkspace, updateWorkspace, deleteWorkspace } from '../api';
import { useEventBus } from '../useEventBus';
import { useToasts } from '../composables/useToasts';
import { useTooltipStore } from '../stores/tooltipStore';
import { useViewport } from '../composables/useViewport';
import { useWorkspaceStore } from '../stores/workspaceStore';
import TaskFeed from '../components/TaskFeed.vue';

const route = useRoute();
const router = useRouter();
const { notifySuccess, notifyError } = useToasts();
const { isMobile } = useViewport();
const workspaceId = computed(() => route.params.id);
const selectedTaskId = computed(() => route.params.taskId);

const workspaceStore = useWorkspaceStore();
const workspace = computed(() => workspaceStore.workspaces.find(w => w.id == workspaceId.value) || localWorkspace.value);
const localWorkspace = ref(null);
const tasks = ref([]);
const loading = ref(true);
const error = ref(null);
const activeFilter = ref(route.query.filter || 'active');
const showMobileFilters = ref(false);
const tooltipStore = useTooltipStore();
const filters = [
  { id: 'active', label: 'Active', icon: 'M4 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2V6zM14 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2zM14 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z' },
  { id: 'notstarted', label: 'Not Started', icon: 'M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2' },
  { id: 'pending', label: 'Pending on Me', icon: 'M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z' },
  { id: 'ongoing', label: 'Ongoing', icon: 'M13 10V3L4 14h7v7l9-11h-7z' },
  { id: 'completed', label: 'Completed', icon: 'M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z' },
  { id: 'scheduled', label: 'Scheduled', icon: 'M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z' }
];
const taskFeed = ref(null);

const { connect, disconnect, events, isConnected } = useEventBus(workspaceId.value);

const scheduledCount = computed(() => tasks.value.filter(t => t.status === 'cron').length);
const activeTaskCount = computed(() => tasks.value.length - scheduledCount.value);
const pendingInputCount = computed(() => tasks.value.filter(t => t.createdBy === 'agent' && (t.status === 'notstarted')).length);

const isAgentConnected = ref(false);

onMounted(() => {
  load();
});

onUnmounted(() => {
  disconnect();
});

watch(activeFilter, (newVal) => {
  // When filter changes, we unselect the current task by navigating to the base workspace route
  router.push({ 
    path: `/workspaces/${workspaceId.value}`,
    query: { ...route.query, filter: newVal } 
  });
});

watch(workspaceId, (newId) => {
  if (newId) {
    disconnect();
    load();
  }
});

watch(events, (evts) => {
  const last = evts[evts.length - 1];
  if (!last) return;

  if (last.type === 'agent.connected') {
    isAgentConnected.value = last.payload.connected;
  }
  
  // Refresh tasks if something changed
  if (['task.created', 'task.updated', 'status.updated', 'task.deleted', 'respond.ack'].includes(last.type)) {
    load(false);
  }
}, { deep: true });

watch(isConnected, (val, old) => {
  if (val && old === false) {
    // Re-fetch everything if reconnected
    load(false);
  }
});

async function load(showLoading = true) {
  if (showLoading) loading.value = true;
  try {
    const [pRes, tRes] = await Promise.all([
      getWorkspace(workspaceId.value),
      fetchTasks(workspaceId.value)
    ]);
    tasks.value = tRes.tasks || [];
    localWorkspace.value = pRes.workspace;
    workspaceStore.updateWorkspaceMetadata(pRes.workspace);
    isAgentConnected.value = workspace.value?.agentConnected;
    if (!isConnected.value) connect();
  } catch (err) {
    error.value = err.message;
  } finally {
    loading.value = false;
  }
}

async function handleArchive() { router.push(`/workspaces/${workspaceId.value}/settings`) }

watch(() => workspace.value?.name, (name) => {
  if (name) document.title = `${name} | AgentRQ`;
}, { immediate: true });
</script>
