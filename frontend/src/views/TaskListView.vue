<template>
  <div class="h-full flex flex-col gap-6">
    <!-- Header -->
    <div class="border-b-2 border-black pb-4">
      <h1 class="text-2xl font-black text-black uppercase tracking-tight">{{ title }}</h1>
      <p class="text-xs text-gray-500 font-bold uppercase tracking-widest mt-1">Cross-workspace task management</p>
    </div>

    <!-- Task List -->
    <div class="flex-1 overflow-y-auto custom-scrollbar min-h-0">
      <div v-if="loading && tasks.length === 0" class="text-center py-12 text-sm font-black text-gray-400 uppercase tracking-widest animate-pulse">
        Loading tasks...
      </div>
      
      <div v-else-if="tasks.length === 0" class="flex flex-col items-center justify-center text-gray-300 opacity-80 py-16 border-2 border-dashed border-gray-200 bg-gray-50">
        <svg class="w-12 h-12 mb-4 text-gray-300" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
          <path stroke-linecap="round" stroke-linejoin="round" d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2" />
        </svg>
        <span class="text-sm font-black uppercase tracking-widest text-gray-500">No tasks found for this category</span>
      </div>

      <div v-else class="space-y-4 pb-6">
        <template v-for="task in tasks" :key="task.id">
          <!-- Specialized Pending Card -->
          <div v-if="filterType === 'pending'"
               class="border-2 border-black bg-yellow-50 p-4 shadow-[2px_2px_0px_0px_rgba(0,0,0,1)] hover:shadow-[4px_4px_0px_0px_rgba(0,0,0,1)] hover:-translate-y-0.5 transition-all cursor-pointer"
               @click="openTask(task)">
            <div class="flex items-start justify-between gap-3 mb-2">
              <div class="flex flex-col gap-1.5 min-w-0 flex-1">
                <div class="flex items-center gap-2 flex-wrap">
                  <span class="text-[9px] font-black px-2 py-0.5 border-2 border-black bg-black text-white uppercase tracking-widest">
                    {{ getWorkspaceName(task.workspaceId) }}
                  </span>
                  <span class="text-xs font-bold bg-yellow-200 border border-black px-2 py-0.5 mr-2 uppercase shrink-0">Pending</span>
                </div>
                <h3 class="font-black text-sm text-black truncate">{{ task.title }}</h3>
              </div>
              <span class="text-xs text-gray-400 shrink-0 font-bold uppercase">{{ formatTime(task.createdAt) }}</span>
            </div>
            
            <p class="text-xs text-gray-600 mb-3 line-clamp-2">
              {{ getLastMessageText(task) }}
            </p>
            
            <div class="flex gap-2" @click.stop v-if="isAgentConnected(task.workspaceId)">
              <button @click="handleAction(task, 'allow')"
                      class="px-3 py-1 bg-[#00FF88] border-2 border-black text-xs font-bold hover:bg-[#00e07a] shadow-[1px_1px_0px_0px_rgba(0,0,0,1)] active:shadow-none active:translate-y-[1px] transition-all">
                Allow
              </button>
              <button @click="openTask(task)"
                      class="px-3 py-1 bg-white border-2 border-black text-xs font-bold hover:bg-gray-100 shadow-[1px_1px_0px_0px_rgba(0,0,0,1)] active:shadow-none active:translate-y-[1px] transition-all">
                Reply
              </button>
              <button @click="handleAction(task, 'reject')"
                      class="px-3 py-1 bg-red-100 border-2 border-black text-xs font-bold hover:bg-red-200 shadow-[1px_1px_0px_0px_rgba(0,0,0,1)] active:shadow-none active:translate-y-[1px] transition-all">
                Deny
              </button>
            </div>
            <div v-else class="flex items-center gap-2 mt-2 px-3 py-1 bg-gray-100 border-2 border-black border-dashed w-fit">
              <span class="w-2 h-2 rounded-full bg-red-500 animate-pulse"></span>
              <span class="text-[10px] font-black text-gray-500 uppercase tracking-widest">Waiting for Agent Arrival</span>
            </div>
          </div>

          <!-- Standard Task Card -->
          <div v-else 
               @click="openTask(task)"
               class="group p-4 bg-white border-2 border-black shadow-[4px_4px_0_0_rgba(0,0,0,1)] hover:shadow-[6px_6px_0_0_rgba(0,0,0,1)] hover:-translate-y-0.5 transition-all cursor-pointer flex flex-col md:flex-row md:items-center justify-between gap-4">
            
            <div class="flex-1 min-w-0">
              <div class="flex items-center gap-2 mb-2 flex-wrap">
                <span class="text-[9px] font-black px-2 py-0.5 border-2 border-black bg-black text-white uppercase tracking-widest">
                  {{ getWorkspaceName(task.workspaceId) }}
                </span>
                <span class="text-[9px] font-black px-2 py-0.5 border-2 border-black uppercase tracking-widest shrink-0" :class="getTaskBadgeStyle(task.status)">
                  {{ getTaskLabel(task.status) }}
                </span>
                <div v-if="task.cronSchedule" class="flex items-center gap-2 flex-nowrap">
                  <span class="text-[9px] font-black px-2 py-0.5 bg-sky-50 border-2 border-sky-200 text-sky-700 uppercase tracking-widest shrink-0">Scheduled</span>
                  <template v-if="task.status === 'cron'">
                    <span v-if="getNextRunLabel(task.cronSchedule)" class="text-[9px] font-black text-sky-600 uppercase tracking-widest truncate">Next: {{ getNextRunLabel(task.cronSchedule) }} ({{ getNextRunDateTime(task.cronSchedule) }})</span>
                    <span v-else class="text-[9px] font-black text-gray-400 uppercase tracking-widest truncate">Next: DUE</span>
                  </template>
                </div>
              </div>
              
              <h3 class="text-sm font-black text-black group-hover:text-indigo-600 transition-colors uppercase truncate mb-1">{{ task.title }}</h3>
              <p class="text-[10px] text-gray-500 font-bold uppercase tracking-widest">
                <template v-if="task.status !== 'cron'">
                  {{ formatTime(task.createdAt) }} • 
                </template>
                BY {{ task.createdBy.toUpperCase() }}
              </p>
            </div>

            <div class="flex items-center gap-3 shrink-0">
               <div class="flex items-center gap-1.5 px-2 py-1 bg-gray-50 border-2 border-black" v-if="task.messages && task.messages.length > 0">
                  <svg class="w-3.5 h-3.5 text-gray-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M8 10h.01M12 10h.01M16 10h.01M9 16H5a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v8a2 2 0 01-2 2h-5l-5 5v-5z" /></svg>
                  <span class="text-[10px] font-black">{{ task.messages.length }}</span>
               </div>
               <svg class="w-4 h-4 text-black/20 group-hover:text-black transition-colors" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3">
                 <path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7" />
               </svg>
            </div>
          </div>
        </template>

        <!-- Load More -->
        <div v-if="hasMore" class="pt-4 flex justify-center">
          <button @click="loadMore" :disabled="loading" 
                  class="bg-white text-black border-2 border-black px-8 py-3 text-xs font-black uppercase tracking-widest hover:bg-black hover:text-white transition-all shadow-[4px_4px_0_0_rgba(0,0,0,1)] hover:shadow-[2px_2px_0_0_rgba(0,0,0,1)] active:shadow-none translate-y-0 active:translate-y-[4px]">
            {{ loading ? 'Loading...' : 'Load More Entries' }}
          </button>
        </div>
      </div>
    </div>

    <!-- Stats Footer -->
    <div v-if="tasks.length > 0" class="mt-4 pt-4 pb-2 border-t-2 border-dashed border-gray-200 flex items-center justify-between text-[11px] md:text-xs font-bold text-gray-400 uppercase tracking-widest shrink-0">
        <div class="flex items-center gap-4">
           <div class="flex items-center gap-1.5" title="Active Tasks">
             <span class="hidden md:inline">Active Tasks</span>
             <span class="md:hidden">⚡</span>
             <span class="text-black font-black">{{ activeTaskCount }}</span>
           </div>
           <div class="flex items-center gap-1.5 border-l-2 border-gray-100 pl-4" title="Scheduled Runs">
             <span class="hidden md:inline">Scheduled</span>
             <span class="md:hidden">⏰</span>
             <span class="text-black font-black">{{ scheduledCount }}</span>
           </div>
           <div class="flex items-center gap-1.5 border-l-2 border-gray-100 pl-4 transition-all" 
                :class="pendingInputCount > 0 ? 'bg-yellow-400 text-black px-2 -my-1 py-1 shadow-[2px_2px_0px_0px_rgba(0,0,0,1)]' : ''"
                title="Your Attention Needed">
             <span class="hidden md:inline">{{ pendingInputCount > 0 ? 'Action Required' : 'Pending on Me' }}</span>
             <span class="md:hidden">!</span>
             <span class="font-black" :class="pendingInputCount > 0 ? 'text-black' : 'text-gray-300'">{{ pendingInputCount }}</span>
           </div>
        </div>
        <div class="hidden md:flex items-center gap-1.5 text-[9px] font-black text-gray-300">
          GLOBAL VIEW · SYNCED
        </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, onUnmounted } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { fetchGlobalTasks, fetchWorkspaces, sendPermissionVerdict } from '../api';
import { useToasts } from '../composables/useToasts';
import { useCron } from '../composables/useCron';
import { useEventBus } from '../useEventBus';

const { getNextRunLabel, getNextRunDateTime } = useCron();
const route = useRoute();
const router = useRouter();
const { notifySuccess, notifyError } = useToasts();

const tasks = ref([]);
const workspaces = ref([]);
const loading = ref(false);
const offset = ref(0);
const limit = 10;
const hasMore = ref(true);

// Setup Global Event Bus
const { connect, disconnect, events } = useEventBus();

watch(events, (newEvents) => {
  if (newEvents.length === 0) return;
  const event = newEvents[newEvents.length - 1];
  
  // Refresh list on relevant task events
  if (['task.created', 'task.updated', 'status.updated', 'task.deleted', 'reply.received', 'respond.ack'].includes(event.type)) {
     // For global list, we refresh to keep it simple
     fetchInitial();
  }
}, { deep: true });

const filterType = computed(() => route.params.filter);

const title = computed(() => {
  const map = {
    pending: 'Pending on Me',
    notstarted: 'Not Started',
    ongoing: 'Ongoing Tasks',
    completed: 'Completed Tasks'
  };
  return map[filterType.value] || 'Global Tasks';
});

const activeTaskCount = computed(() => tasks.value.filter(t => t.status !== 'cron').length);
const scheduledCount = computed(() => tasks.value.filter(t => t.status === 'cron').length);
const pendingInputCount = computed(() => tasks.value.filter(t => t.createdBy === 'agent' && (t.status === 'notstarted')).length);

const getWorkspaceName = (workspaceId) => {
  const ws = workspaces.value.find(w => w.id === workspaceId);
  return ws ? ws.name : '...';
};

const isAgentConnected = (workspaceId) => {
  const ws = workspaces.value.find(w => w.id === workspaceId);
  return ws ? ws.agentConnected : false;
};

const getLastMessageText = (task) => {
  if (!task.messages || task.messages.length === 0) return 'No message content available.';
  const last = task.messages[task.messages.length - 1];
  return last.text || 'No message content available.';
};

const handleAction = async (task, action) => {
  try {
    // Find the latest message that is a permission_request and has no verdict yet
    const pendingMsg = [...(task.messages || [])].reverse().find(m => 
      m.metadata?.type === 'permission_request' && 
      m.metadata?.status !== 'allow' && 
      m.metadata?.status !== 'deny'
    );
    
    const requestId = pendingMsg?.metadata?.requestId;
    if (!requestId) throw new Error('No pending permission request found');
    
    const behavior = action === 'allow' ? 'allow' : 'deny';
    await sendPermissionVerdict(task.workspaceId, task.id, requestId, behavior);
    notifySuccess(`Permission ${action === 'allow' ? 'allowed' : 'denied'}`);
    // Refresh the list to remove the acted task
    await fetchInitial();
  } catch (err) {
    notifyError(`Failed to ${action} task: ` + err.message);
  }
};

const fetchInitial = async () => {
  loading.value = true;
  tasks.value = [];
  offset.value = 0;
  hasMore.value = true;
  
  try {
    const wsRes = await fetchWorkspaces();
    workspaces.value = wsRes.workspaces;
    
    await fetchNext();
  } catch (err) {
    console.error('Failed to fetch tasks:', err);
  } finally {
    loading.value = false;
  }
};

const fetchNext = async () => {
  const params = getBackendParams(filterType.value);
  params.limit = limit;
  params.offset = offset.value;
  
  try {
    const res = await fetchGlobalTasks(params);
    const newTasks = res.tasks || [];
    
    if (newTasks.length < limit) {
      hasMore.value = false;
    }
    
    tasks.value = [...tasks.value, ...newTasks];
    offset.value += newTasks.length;
  } catch (err) {
    console.error('Failed to load more tasks:', err);
  }
};

const loadMore = async () => {
  if (loading.value) return;
  loading.value = true;
  await fetchNext();
  loading.value = false;
};

const getBackendParams = (filter) => {
  if (filter === 'pending') return { filter: 'pending_approval' };
  if (filter === 'notstarted') return { status: 'notstarted' };
  if (filter === 'ongoing') return { status: 'ongoing,blocked' };
  if (filter === 'completed') return { status: 'completed,rejected' };
  return {};
};

const openTask = (task) => {
  router.push(`/workspaces/${task.workspaceId}/tasks/${task.id}`);
};

const formatTime = (dateStr) => {
  if (!dateStr) return '';
  const d = new Date(dateStr);
  if (isNaN(d.getTime())) return '';
  
  const now = new Date();
  const diff = now - d;
  const minutes = Math.floor(diff / 60000);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);

  if (minutes < 1) return 'JUST NOW';
  if (minutes < 60) return `${minutes}M AGO`;
  if (hours < 24) return `${hours}H AGO`;
  if (days < 7) return `${days}D AGO`;
  
  return d.toLocaleDateString([], { month: 'short', day: 'numeric' }).toUpperCase();
};

const getTaskBadgeStyle = (status) => {
  if (status === 'ongoing') return 'bg-yellow-50 text-yellow-600 border-yellow-200';
  if (status === 'blocked') return 'bg-red-50 text-red-600 border-red-200';
  if (status === 'completed') return 'bg-green-50 text-green-600 border-green-200';
  if (status === 'notstarted') return 'bg-gray-50 text-gray-500 border-gray-200';
  if (status === 'cron') return 'bg-sky-50 text-sky-700 border-sky-200';
  return 'bg-white text-gray-400 border-gray-100';
};

const getTaskLabel = (status) => {
  if (status === 'cron') return 'SCHEDULED';
  return status.toUpperCase();
};

watch(() => route.params.filter, () => {
  fetchInitial();
});

onMounted(() => {
  fetchInitial();
  connect();
});

onUnmounted(() => {
  disconnect();
});
</script>
