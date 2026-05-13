<template>
  <div class="flex flex-col h-full w-full bg-transparent">
    <!-- Global Header -->
    <div class="w-full px-4 py-2 mb-6 shrink-0 flex flex-row items-center justify-between gap-4"
         :class="{'hidden sm:flex': selectedTaskId}">
      <div class="flex flex-col min-w-0 flex-1">
        <h1 class="text-lg md:text-2xl font-black text-gray-800 dark:text-zinc-200 truncate leading-tight">{{ title }}</h1>
      </div>



      <!-- Filters Segment Control (Top Right) -->
      <div class="flex items-center gap-2">
        <!-- Filters Toggle (Mobile) -->
        <button @click="showMobileFilters = !showMobileFilters" 
                class="md:hidden p-2 text-gray-500 hover:bg-gray-100 dark:hover:bg-zinc-800 bg-white dark:bg-zinc-900 border border-gray-100 dark:border-zinc-800 rounded-lg transition-all shadow-sm flex items-center justify-center shrink-0" 
                :class="{'bg-gray-100 dark:bg-zinc-800 text-black dark:text-white border-black dark:border-white': showMobileFilters}"
                title="Filters">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M3 4h13M3 8h9m-9 4h6m4 0l4-4m0 0l4 4m-4-4v12" /></svg>
        </button>

        <div v-if="showMobileFilters || !isMobile" 
             class="p-0.5 bg-gray-100 dark:bg-zinc-800 rounded-md border border-gray-200 dark:border-zinc-700/50 shadow-inner flex overflow-x-auto no-scrollbar"
             :class="[showMobileFilters ? 'fixed top-[70px] right-4 z-50 flex shadow-xl border-gray-900 dark:border-white animate-in fade-in slide-in-from-top-2' : 'hidden md:flex']">
          <button v-for="f in filters" :key="f.id"
                  @click="router.push(`/tasks/${f.id}`); isMobile && (showMobileFilters = false)"
                  @mouseenter="tooltipStore.show($event, f.label, 'bottom')"
                  @mouseleave="tooltipStore.hide()"
                  :class="[filterType === f.id ? 'bg-white dark:bg-zinc-700 text-black dark:text-white shadow-sm' : 'text-gray-500 dark:text-zinc-400 hover:text-gray-700 dark:hover:text-zinc-300']"
                  class="p-2 rounded-sm transition-all duration-200 whitespace-nowrap">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" :d="f.icon" />
            </svg>
          </button>
        </div>
      </div>
    </div>


    <!-- Content Area (Split Pane) -->
    <div class="flex flex-col md:flex-row flex-1 min-h-0 w-full bg-transparent">
      <!-- Tasks Sidebar (Left Pane) -->
      <div v-show="!selectedTaskId || !isMobile" class="w-full md:w-96 shrink-0 h-full flex flex-col min-h-0 bg-transparent md:border-r border-gray-100 dark:border-zinc-800">
        
        <!-- Task List -->
        <div v-if="loading" class="flex-1 overflow-y-auto custom-scrollbar min-h-0 px-2 pb-20 relative">
          Loading tasks...
        </div>
        
        <div v-else class="space-y-6 pb-6 overflow-y-auto custom-scrollbar px-4">
          <div v-for="grp in displayGroups" :key="grp.title" class="mb-4">
            <div class="mb-3 flex items-center gap-3">
              <h3 class="text-[10px] font-semibold text-gray-500 dark:text-zinc-400 uppercase tracking-widest">{{ grp.title }}</h3>
              <span class="text-[9px] font-bold text-gray-500 dark:text-zinc-500 bg-gray-100 dark:bg-zinc-800 px-1.5 py-0.5 rounded-sm">{{ grp.tasks.length }}</span>
            </div>
            
            <div v-if="grp.tasks.length === 0" class="py-4 px-4 border border-dashed border-gray-200 dark:border-zinc-800 rounded-xl text-[11px] text-gray-500 dark:text-zinc-500 font-medium">
              No {{ grp.title.toLowerCase() }} tasks found.
            </div>

            <div v-else class="space-y-2">
              <template v-for="task in grp.tasks" :key="task.id">
                <!-- Consistent Compact Task Item (KeywordInbox Style) -->
                <div @click="openTask(task)"
                     :class="[ 'p-4 cursor-pointer border-b border-gray-50 dark:border-zinc-800/50 group relative rounded-xl mb-1', String(selectedTaskId) === String(task.id) ? 'bg-white dark:bg-zinc-800 border-gray-100 dark:border-zinc-800 z-10' : 'bg-transparent hover:bg-gray-50 dark:hover:bg-zinc-800/50 ' ]">
                  
                  <div v-if="String(selectedTaskId) === String(task.id)" class="absolute left-0 top-4 bottom-4 w-1 bg-black dark:bg-white rounded-full"></div>
                  
                  <div class="flex items-center justify-between mb-2">
                    <div class="flex items-center gap-2">
                      <div class="w-2 h-2 rounded-full shrink-0" :class="getTaskDotStyle(task)"></div>
                      <span class="text-[10px] font-bold text-gray-500 dark:text-zinc-400 bg-gray-50 dark:bg-zinc-800/50 px-1.5 py-0.5 rounded uppercase tracking-tight group-hover:bg-gray-100 dark:group-hover:bg-zinc-700 group-hover:text-black dark:group-hover:text-white transition-colors">
                        {{ getWorkspaceName(task.workspaceId) }}
                      </span>
                    </div>
                    <div class="flex items-center gap-2 relative">
                       <!-- Reorder buttons for not started -->
                       <div v-if="filterType === 'notstarted'" class="flex items-center gap-1 mr-2 opacity-0 group-hover:opacity-100 transition-opacity">
                          <button @click.stop="reorderTask(grp.tasks, task, -1)" class="p-1 hover:bg-gray-100 dark:hover:bg-zinc-700 rounded text-gray-400 hover:text-black dark:hover:text-white transition-colors" title="Move Up">
                            <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path stroke-linecap="round" stroke-linejoin="round" d="M5 15l7-7 7 7" /></svg>
                          </button>
                          <button @click.stop="reorderTask(grp.tasks, task, 1)" class="p-1 hover:bg-gray-100 dark:hover:bg-zinc-700 rounded text-gray-400 hover:text-black dark:hover:text-white transition-colors" title="Move Down">
                            <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path stroke-linecap="round" stroke-linejoin="round" d="M19 9l-7 7-7-7" /></svg>
                          </button>
                       </div>
                       <span class="text-[10px] text-gray-500 dark:text-zinc-400 font-bold uppercase tracking-wider tabular-nums shrink-0">
                         {{ task.status === 'cron' ? formatDate(task.createdAt) : formatTime(task.createdAt) }}
                       </span>
                    </div>
                  </div>

                  <h3 :class="[ 'text-[13px] leading-relaxed line-clamp-2 transition-colors', 
                                 String(selectedTaskId) === String(task.id) ? 'text-gray-800 dark:text-zinc-200 font-bold' : 
                                 task.status === 'completed' ? 'text-gray-500 dark:text-zinc-500 font-light' : 
                                 'text-gray-700 dark:text-zinc-200 group-hover:text-black dark:group-hover:text-white font-semibold' ]">
                    {{ task.title }}
                  </h3>
                  
                  <!-- Quick actions for Pending -->
                  <div v-if="filterType === 'pending'" class="mt-3" @click.stop>
                    <div class="flex flex-wrap gap-2" v-if="isAgentConnected(task.workspaceId)">
                      <button @click="handleAction(task, 'allow')" class="px-2.5 py-1.5 bg-gray-900 dark:bg-white text-white dark:text-black rounded-lg text-[9px] font-black uppercase tracking-widest hover:bg-black dark:hover:bg-gray-100 transition-all shadow-sm">
                        Allow
                      </button>
                      <button @click="handleAction(task, 'deny')" class="px-2.5 py-1.5 bg-white dark:bg-zinc-800 text-red-600 dark:text-red-400 border border-gray-100 dark:border-zinc-700 rounded-lg text-[9px] font-black uppercase tracking-widest hover:bg-red-50 dark:hover:bg-red-900/10 transition-all shadow-sm">
                        Deny
                      </button>
                      <button @click="openTask(task)" class="px-2.5 py-1.5 bg-white dark:bg-zinc-800 text-gray-700 dark:text-zinc-200 border border-gray-100 dark:border-zinc-700 rounded-lg text-[9px] font-black uppercase tracking-widest hover:bg-gray-50 dark:hover:bg-zinc-700 transition-all shadow-sm">
                        Review
                      </button>
                    </div>
                  </div>

                  <!-- Next execution for scheduled tasks -->
                  <div v-if="task.status === 'cron' && task.cronSchedule" class="mt-2 flex items-center gap-1.5 text-[9px] text-gray-500 dark:text-zinc-500 font-medium uppercase tracking-tight">
                    <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>
                    <span>{{ getNextRunLabel(task.cronSchedule) }}</span>
                  </div>
                </div>
              </template>
            </div>
          </div>
        </div>

          <!-- Sticky Load More -->
          <div v-if="hasMore && tasks.length > 0" class="sticky bottom-0 left-0 right-0 p-4 flex justify-center bg-white/80 dark:bg-zinc-900/80 backdrop-blur-md z-30">
            <button @click="loadMore" :disabled="loading" 
                    class="bg-white dark:bg-zinc-800 text-gray-700 dark:text-zinc-200 border border-gray-200 dark:border-zinc-700 rounded-sm px-8 py-3 text-xs font-semibold hover:bg-gray-50 dark:hover:bg-zinc-700 hover:text-gray-900 dark:hover:text-white transition-all shadow-sm active:scale-95 flex items-center gap-2">
              <svg v-if="loading" class="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 12a8 8 0 018-8v8H4z" /></svg>
              {{ loading ? 'Loading...' : 'Load More Entries' }}
            </button>
          </div>
        </div>
      

      <!-- Task Detail Pane (Right) -->
      <div v-show="selectedTaskId || !isMobile" class="flex-1 min-w-0 flex flex-col h-full bg-transparent">
        <router-view v-if="selectedTaskId" />
        
        <!-- Empty state when no task is selected -->
        <div v-else class="flex-1 flex flex-col items-center justify-center m-4 p-8 text-center h-full bg-gray-50 dark:bg-zinc-900/50 rounded-sm border border-dashed border-gray-200 dark:border-zinc-800">
          <div class="w-16 h-16 bg-white dark:bg-zinc-800 rounded-sm border border-gray-100 dark:border-zinc-700 flex items-center justify-center mb-4 shadow-sm">
            <svg class="w-8 h-8 text-gray-300 dark:text-zinc-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-3 7h3m-3 4h3m-6-4h.01M9 16h.01" />
            </svg>
          </div>
          <p class="text-lg font-bold text-gray-800 dark:text-zinc-200 tracking-tight">Select a task</p>
          <p class="text-sm text-gray-500 dark:text-zinc-400 mt-2 max-w-[420px] leading-relaxed">Choose a task from the list to view its details, conversation history, and take actions.</p>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, onUnmounted } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { fetchGlobalTasks, fetchWorkspaces, sendPermissionVerdict, updateTaskAssignee, updateTaskStatus, updateTaskOrder } from '../api';
import { useToasts } from '../composables/useToasts';
import { useTooltipStore } from '../stores/tooltipStore';
import { useCron } from '../composables/useCron';
import { useEventBus } from '../useEventBus';
import { useViewport } from '../composables/useViewport';

const { getNextRunLabel, getNextRunDateTime, getNextRunDate } = useCron();
const route = useRoute();
const router = useRouter();
const { notifySuccess, notifyError } = useToasts();
const { isMobile } = useViewport();

const tasks = ref([]);
const workspaces = ref([]);
const loading = ref(false);
const offset = ref(0);
const limit = 10;
const hasMore = ref(true);
const tooltipStore = useTooltipStore();
const showMobileFilters = ref(false);

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
    active: 'Active Tasks',
    scheduled: 'Scheduled',
    pending: 'Pending on Me',
    notstarted: 'Not Started',
    ongoing: 'Ongoing Tasks',
    completed: 'Completed Tasks'
  };
  return map[filterType.value] || 'Active';
});

const emptyStateLabel = computed(() => {
  switch (filterType.value) {
    case 'active':
      return 'No active tasks found (includes not started and ongoing).';
    case 'notstarted':
      return 'No tasks waiting to start.';
    case 'pending':
      return 'No tasks pending your attention.';
    case 'ongoing':
      return 'No tasks currently in progress.';
    case 'completed':
      return 'No completed tasks found.';
    case 'scheduled':
      return 'No scheduled tasks configured.';
    default:
      return 'No tasks match this category across your workspaces.';
  }
});

const filters = [
  { id: 'active', label: 'Active', icon: 'M4 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2V6zM14 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2zM14 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z' },
  { id: 'notstarted', label: 'Not Started', icon: 'M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2' },
  { id: 'pending', label: 'Pending on Me', icon: 'M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z' },
  { id: 'ongoing', label: 'Ongoing', icon: 'M13 10V3L4 14h7v7l9-11h-7z' },
  { id: 'completed', label: 'Completed', icon: 'M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z' },
  { id: 'scheduled', label: 'Scheduled', icon: 'M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z' }
];

const displayGroups = computed(() => {
  const f = filterType.value || 'active';
  
  const ongoing = tasks.value.filter(t => t.status === 'ongoing');
  const blocked = tasks.value.filter(t => t.status === 'blocked');
  const notStarted = tasks.value.filter(t => t.status === 'notstarted');
  const completed = tasks.value.filter(t => t.status === 'completed');
  const rejected = tasks.value.filter(t => t.status === 'rejected');
  const cron = tasks.value.filter(t => t.status === 'cron');

  const groups = [];
  if (f === 'active' || f === 'ongoing') {
    groups.push({ title: 'Ongoing', tasks: ongoing });
    if (blocked.length > 0) groups.push({ title: 'Blocked', tasks: blocked });
  }
  if (f === 'active' || f === 'notstarted') {
    groups.push({ title: 'Not Started', tasks: notStarted });
  }
  if (f === 'pending') {
    const pendingActions = tasks.value.filter(t => 
      t.status !== 'completed' && t.status !== 'rejected' && (
        (t.status === 'notstarted' && t.assignee === 'human') ||
        (t.messages && t.messages.some(m => m.metadata?.type === 'permission_request' && m.metadata?.status === 'pending'))
      )
    );
    groups.push({ title: 'Action Required', tasks: pendingActions });
  }
  if (f === 'completed') {
    groups.push({ title: 'Completed', tasks: completed });
    if (rejected.length > 0) groups.push({ title: 'Rejected', tasks: rejected });
  }
  if (f === 'active' || f === 'scheduled') {
    // Sort cron tasks by next run time
    const sortedCron = [...cron].sort((a, b) => {
      const aTime = getNextRunDate(a.cronSchedule).getTime();
      const bTime = getNextRunDate(b.cronSchedule).getTime();
      return aTime - bTime;
    });
    
    if (sortedCron.length > 0) {
      groups.push({ title: 'Scheduled', tasks: sortedCron });
    }
  }

  return groups;
});

const activeTaskCount = computed(() => tasks.value.filter(t => t.status !== 'cron').length);
const scheduledCount = computed(() => tasks.value.filter(t => t.status === 'cron').length);
const pendingInputCount = computed(() => tasks.value.filter(t => 
  t.status !== 'completed' && t.status !== 'rejected' && (
    (t.status === 'notstarted' && t.assignee === 'human') ||
    (t.messages && t.messages.some(m => m.metadata?.type === 'permission_request' && m.metadata?.status === 'pending'))
  )
).length);

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
    if (task.status === 'notstarted' && task.assignee === 'human') {
      if (action === 'allow') {
        await updateTaskAssignee(task.workspaceId, task.id, 'agent');
        await updateTaskStatus(task.workspaceId, task.id, 'ongoing');
        notifySuccess('Task started and assigned to agent');
      } else {
        await updateTaskStatus(task.workspaceId, task.id, 'rejected');
        notifySuccess('Task rejected');
      }
      await fetchInitial();
      return;
    }

    // Find the latest message that is a permission_request and has no verdict yet
    const pendingMsg = [...(task.messages || [])].reverse().find(m => 
      m.metadata?.type === 'permission_request' && 
      m.metadata?.status !== 'allow' && 
      m.metadata?.status !== 'deny'
    );
    
    const requestId = pendingMsg?.metadata?.request_id || pendingMsg?.metadata?.requestId;
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

const getTaskOrder = (t) => {
  if (t.sortOrder) return t.sortOrder;
  if (!t.createdAt) return Date.now() / 1000.0;
  return new Date(t.createdAt).getTime() / 1000.0;
};

const reorderTask = async (groupTasks, task, direction) => {
  const idx = groupTasks.findIndex(x => x.id === task.id);
  if (idx === -1) return;
  
  const targetIdx = idx + direction;
  if (targetIdx < 0 || targetIdx >= groupTasks.length) return;
  
  const neighbor = groupTasks[targetIdx];
  const neighborOrder = getTaskOrder(neighbor);
  let newOrder = direction === -1 ? neighborOrder + 0.001 : neighborOrder - 0.001;
  
  try {
    await updateTaskOrder(task.workspaceId, task.id, newOrder);
    await fetchInitial();
  } catch (err) {
    notifyError('Reorder Error: ' + err.message);
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
  if (filter === 'active') return { status: 'ongoing,blocked,notstarted,cron' };
  if (filter === 'scheduled') return { status: 'cron' };
  if (filter === 'pending') return { filter: 'pending_approval' };
  if (filter === 'notstarted') return { status: 'notstarted' };
  if (filter === 'ongoing') return { status: 'ongoing,blocked' };
  if (filter === 'completed') return { status: 'completed,rejected' };
  return { status: 'ongoing,blocked,notstarted' };
};

const selectedTaskId = computed(() => route.params.taskId);

const openTask = (task) => {
  if (task.status === 'cron') {
    router.push(`/tasks/${filterType.value}/${task.workspaceId}/${task.id}/instances`);
  } else {
    router.push(`/tasks/${filterType.value}/${task.workspaceId}/${task.id}`);
  }
};

const formatDate = (dateStr) => {
  if (!dateStr) return '';
  const d = new Date(dateStr);
  if (isNaN(d.getTime())) return '';
  return d.toLocaleDateString([], { month: 'short', day: 'numeric' });
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

  if (minutes < 1) return 'Just Now';
  if (minutes < 60) return `${minutes}m ago`;
  if (hours < 24) return `${hours}h ago`;
  if (days < 7) return `${days}d ago`;
  
  return d.toLocaleDateString([], { month: 'short', day: 'numeric' });
};

const headerIconPath = computed(() => {
  switch (filterType.value) {
    case 'scheduled':
      return 'M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z';
    case 'pending':
      return 'M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z';
    case 'notstarted':
      return 'M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2';
    case 'ongoing':
      return 'M13 10V3L4 14h7v7l9-11h-7z';
    case 'completed':
      return 'M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z';
    default:
      return 'M4 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2V6zM14 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2zM14 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z';
  }
});

const headerIconClass = computed(() => {
  return 'text-gray-700 dark:text-zinc-400';
});

function getTaskDotStyle(t) {
  const status = typeof t === 'string' ? t : t.status;
  // If it's the task object, check if it's "Pending on Me"
  const isPendingOnMe = typeof t === 'object' && t.status !== 'completed' && t.status !== 'rejected' && (
    (t.status === 'notstarted' && t.assignee === 'human') ||
    (t.messages && t.messages.some(m => m.metadata?.type === 'permission_request' && m.metadata?.status === 'pending'))
  );

  if (isPendingOnMe) {
    return 'bg-yellow-400 shadow-[0_0_8px_rgba(250,204,21,0.4)]';
  }

  switch (status) {
    case 'ongoing':
      return 'bg-green-500 shadow-[0_0_8px_rgba(34,197,94,0.4)] animate-pulse';
    case 'notstarted':
      return 'bg-gray-400 dark:bg-zinc-500';
    case 'completed':
      return 'bg-green-500';
    case 'rejected':
      return 'bg-red-500';
    case 'blocked':
      return 'bg-red-500 shadow-[0_0_8px_rgba(239,68,68,0.4)]';
    case 'cron':
      return 'bg-cyan-300 shadow-[0_0_8px_rgba(103,232,249,0.4)]';
    default:
      return 'bg-gray-300 dark:bg-zinc-600';
  }
}

const getTaskBgStyle = (t) => {
  const isSelected = String(selectedTaskId.value) === String(t.id);
  if (isSelected) {
    if (t.status === 'ongoing') return 'bg-yellow-50 dark:bg-yellow-900/10 border-l-yellow-400 dark:border-l-yellow-500 shadow-sm';
    if (t.status === 'blocked') return 'bg-red-50 dark:bg-red-900/10 border-l-red-400 dark:border-l-red-500 shadow-sm';
    if (t.status === 'completed') return 'bg-gray-50 dark:bg-zinc-900 border-l-gray-900 dark:border-l-zinc-100 shadow-sm';
    if (t.status === 'cron') return 'bg-sky-50 dark:bg-sky-900/10 border-l-sky-400 dark:border-l-sky-500 shadow-sm';
    return 'bg-white dark:bg-zinc-800 border-l-gray-400 dark:border-l-gray-500 shadow-sm';
  }
  
  if (t.status === 'ongoing') return 'bg-yellow-50/50 dark:bg-yellow-900/5 hover:bg-yellow-50 dark:hover:bg-yellow-900/20 hover:shadow-sm';
  if (t.status === 'blocked') return 'bg-red-50/50 dark:bg-red-900/5 hover:bg-red-50 dark:hover:bg-red-900/20 hover:shadow-sm';
  if (t.status === 'completed') return 'bg-gray-50/50 dark:bg-zinc-900/5 hover:bg-gray-100 dark:hover:bg-zinc-800/80 hover:shadow-sm';
  if (t.status === 'cron') return 'bg-sky-50/50 dark:bg-sky-900/5 hover:bg-sky-50 dark:hover:bg-sky-900/20 hover:shadow-sm';
  return 'bg-white/40 dark:bg-zinc-900/30 hover:bg-white dark:hover:bg-zinc-900/80 hover:shadow-sm';
};

const getTaskLabel = (status) => {
  if (status === 'cron') return 'Scheduled';
  if (status === 'notstarted') return 'Not Started';
  return status;
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
