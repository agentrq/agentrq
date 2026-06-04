<template>
  <div class="flex-1 flex flex-col min-h-0 w-full bg-transparent relative">

    <div v-if="isArchived" class="p-3 bg-amber-50 dark:bg-amber-500/10 border-b border-amber-100 dark:border-amber-500/20 flex items-center justify-center gap-2">
      <svg class="w-3.5 h-3.5 text-amber-600 dark:text-amber-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" /></svg>
      <span class="text-[10px] font-semibold text-amber-900 dark:text-amber-500">Archived Workspace • Read Only</span>
    </div>
    
    <!-- Delete Confirmation Modal -->
    <DeleteModal 
      :show="showDeleteModal" 
      :taskTitle="taskToDeleteTitle"
      title="Delete Task" 
      @close="closeDeleteModal" 
      @confirm="onDeleteConfirm" 
    />

    <!-- Action Bar moved to parent for better layout consistency -->

    <!-- Single List Area -->
    <div class="flex-1 overflow-y-auto pb-20 custom-scrollbar relative px-4">
      <div class="space-y-6">
        <div v-for="grp in displayGroups" :key="grp.title" class="mb-4">
          <div class="mb-3 flex items-center gap-3">
            <h3 class="text-[10px] font-semibold text-gray-500 dark:text-zinc-400 uppercase tracking-widest">{{ grp.title }}</h3>
            <span class="text-[9px] font-bold text-gray-500 dark:text-zinc-500 bg-gray-100 dark:bg-zinc-800 px-1.5 py-0.5 rounded-sm">{{ grp.totalCount !== undefined ? grp.totalCount : grp.tasks.length }}</span>
          </div>
          
          <div v-if="grp.tasks.length === 0" class="py-4 px-4 border border-dashed border-gray-200 dark:border-zinc-800 rounded-xl text-[11px] text-gray-500 dark:text-zinc-500 font-medium">
            No {{ grp.title.toLowerCase() }} tasks found.
          </div>

          <div v-else class="space-y-2">
            <div v-for="(t, idx) in grp.tasks" :key="t.id"
                 @click="openTask(t)"
                 :class="[ 'p-4 cursor-pointer border-b border-gray-50 dark:border-zinc-800/50 group relative rounded-xl mb-1', String(selectedTaskId) === String(t.id) ? 'bg-white dark:bg-zinc-800 border-gray-100 dark:border-zinc-800 z-10' : 'bg-transparent hover:bg-gray-50 dark:hover:bg-zinc-800/50 ' ]">
              
              <div v-if="String(selectedTaskId) === String(t.id)" class="absolute left-0 top-4 bottom-4 w-1 bg-black dark:bg-white rounded-full"></div>
              
              <div class="flex items-center justify-between mb-2">
                <div class="flex items-center gap-2">
                  <div class="w-1.5 h-1.5 rounded-full" :class="getTaskDotStyle(t)"></div>
                  <span class="text-[10px] font-medium text-gray-500 dark:text-zinc-400 bg-gray-50 dark:bg-zinc-800/50 px-1.5 py-0.5 rounded uppercase tracking-tight group-hover:bg-gray-100 dark:group-hover:bg-zinc-700 group-hover:text-black dark:group-hover:text-white transition-colors">{{ t.assignee === 'agent' ? 'Agent' : 'Human' }}</span>
                </div>
                <div class="flex items-center gap-2">
                   <!-- Action Menu (Hover) -->
                   <div class="opacity-0 group-hover:opacity-100 flex items-center gap-1 mr-2 transition-opacity duration-150">
                      <!-- Task Reordering -->
                      <template v-if="!isArchived && t.status === 'notstarted' && grp.title === 'Not Started'">
                        <button @click.stop="reorderTask(t, -1)" class="text-gray-500 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-100 dark:hover:bg-zinc-700 p-1 rounded-sm transition-all" title="Move Up">
                          <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path stroke-linecap="round" stroke-linejoin="round" d="M5 15l7-7 7 7" /></svg>
                        </button>
                        <button @click.stop="reorderTask(t, 1)" class="text-gray-500 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-100 dark:hover:bg-zinc-700 p-1 rounded-sm transition-all" title="Move Down">
                          <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path stroke-linecap="round" stroke-linejoin="round" d="M19 9l-7 7-7-7" /></svg>
                        </button>
                      </template>

                      <button v-if="!isArchived && t.status === 'cron'" @click.stop="triggerEdit(t)" class="text-gray-500 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-100 dark:hover:bg-zinc-700 p-1 rounded-sm transition-all">
                        <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" /></svg>
                      </button>
                      <button v-if="!isArchived" @click.stop="triggerDelete(t)" class="text-gray-500 hover:text-red-500 hover:bg-red-50 dark:hover:bg-red-500/10 p-1 rounded-sm transition-all">
                        <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" /></svg>
                      </button>
                   </div>
                   <span class="text-[10px] text-gray-500 dark:text-zinc-400 font-medium uppercase tracking-wider tabular-nums shrink-0">
                     {{ t.status === 'cron' ? formatDate(t.createdAt) : formatTime(t.createdAt) }}
                   </span>
                </div>
              </div>

              <h3 :class="[ 'text-[13px] leading-relaxed line-clamp-2 transition-colors font-medium', 
                             String(selectedTaskId) === String(t.id) ? 'text-gray-800 dark:text-zinc-200' : 
                             t.status === 'completed' ? 'text-gray-500 dark:text-zinc-500' : 
                             'text-gray-700 dark:text-zinc-200 group-hover:text-black dark:group-hover:text-white' ]">
                {{ t.title }}
              </h3>

              <!-- Quick actions for Pending -->
              <div v-if="grp.title === 'Action Required'" class="mt-3" @click.stop>
                <div class="flex flex-wrap gap-2" v-if="isAgentConnected">
                  <button @click="handleAction(t, 'allow')" class="px-2.5 py-1.5 bg-gray-900 dark:bg-white text-white dark:text-black rounded-lg text-[9px] font-black uppercase tracking-widest hover:bg-black dark:hover:bg-gray-100 transition-all shadow-sm">
                    Allow
                  </button>
                  <button @click="handleAction(t, 'deny')" class="px-2.5 py-1.5 bg-white dark:bg-zinc-800 text-red-600 dark:text-red-400 border border-gray-100 dark:border-zinc-700 rounded-lg text-[9px] font-black uppercase tracking-widest hover:bg-red-50 dark:hover:bg-red-900/10 transition-all shadow-sm">
                    Deny
                  </button>
                </div>
              </div>

              <div v-if="t.status === 'cron' && t.cronSchedule" class="mt-2 flex items-center gap-1.5 text-[9px] text-gray-500 dark:text-zinc-500 font-medium uppercase tracking-tight">
                <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>
                <span>{{ getNextRunLabel(t.cronSchedule) }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>
      
      <!-- Sticky Load More Footer -->
      <div v-if="displayGroups.find(g => g.hasMore)" class="sticky bottom-0 left-0 right-0 p-2 bg-white/80 dark:bg-zinc-900/80 backdrop-blur-md border-t border-gray-100 dark:border-zinc-800 z-30 flex flex-col gap-1.5">
        <template v-for="grp in displayGroups" :key="'more-' + grp.title">
          <button v-if="grp.hasMore" @click.stop="fetchGroup(grp.category, true)" class="w-full py-1.5 rounded-sm border border-dashed border-gray-200 dark:border-zinc-800 text-gray-500 dark:text-zinc-400 text-[10px] font-semibold hover:border-gray-300 dark:hover:border-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-50 dark:hover:bg-zinc-800/50 transition-all shadow-sm">
            Load More {{ grp.title }}
          </button>
        </template>
      </div>
    </div>
    

  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, onUnmounted } from 'vue';
import { useRouter } from 'vue-router';
import cronParser from 'cron-parser';
import { deleteTask, respondToTask, updateTaskOrder, updateTaskStatus, sendPermissionVerdict, updateTaskAssignee, fetchTasks, fetchTaskCounts } from '../api';
import { useCron } from '../composables/useCron';
import DeleteModal from './DeleteModal.vue';
import { useToasts } from '../composables/useToasts';

const { formatCron, getNextRunLabel, getNextRunDate } = useCron();

const counts = ref({
  ongoing: 0,
  notstarted: 0,
  scheduled: 0,
  pending: 0,
  completed: 0
});

async function loadCounts() {
  try {
    const data = await fetchTaskCounts(props.workspaceId);
    counts.value = {
      ongoing: data.ongoing || 0,
      notstarted: data.notstarted || 0,
      scheduled: data.scheduled || 0,
      pending: data.pending || 0,
      completed: data.completed || 0
    };
  } catch (err) {
    console.error('Failed to load task counts', err);
  }
}

const props = defineProps({
  workspaceId: { type: [String, Number], required: true },
  initialTasks: { type: Array, default: () => [] },
  liveEvents: { type: Array, default: () => [] },
  isArchived: { type: Boolean, default: false },
  isAgentConnected: { type: Boolean, default: false },
  filter: { type: String, default: 'active' },
  selectedTaskId: { type: [String, Number], default: null }
});

const emit = defineEmits(['filter-change', 'tasks-updated']);

const router = useRouter();
const { notifyError, notifySuccess, notifyInfo } = useToasts();
const activeStatusMenuId = ref(null);

const showDeleteModal = ref(false);
const taskToDeleteId = ref(null);
const taskToDeleteTitle = ref('');

const ongoingTasks = ref([]);
const notStartedTasks = ref([]);
const scheduledTasks = ref([]);
const pendingTasks = ref([]);
const completedTasks = ref([]);

const ongoingOffset = ref(0);
const notStartedOffset = ref(0);
const scheduledOffset = ref(0);
const pendingOffset = ref(0);
const completedOffset = ref(0);

const ongoingHasMore = ref(false);
const notStartedHasMore = ref(false);
const scheduledHasMore = ref(false);
const pendingHasMore = ref(false);
const completedHasMore = ref(false);

const loadingTasks = ref(false);
const PAGE_SIZE = 10;

async function fetchGroup(category, isLoadMore = false) {
  if (!isLoadMore) {
    if (category === 'ongoing') { ongoingTasks.value = []; ongoingOffset.value = 0; ongoingHasMore.value = false; }
    if (category === 'notstarted') { notStartedTasks.value = []; notStartedOffset.value = 0; notStartedHasMore.value = false; }
    if (category === 'scheduled') { scheduledTasks.value = []; scheduledOffset.value = 0; scheduledHasMore.value = false; }
    if (category === 'pending') { pendingTasks.value = []; pendingOffset.value = 0; pendingHasMore.value = false; }
    if (category === 'completed') { completedTasks.value = []; completedOffset.value = 0; completedHasMore.value = false; }
  }

  let offset = 0;
  if (category === 'ongoing') offset = ongoingOffset.value;
  if (category === 'notstarted') offset = notStartedOffset.value;
  if (category === 'scheduled') offset = scheduledOffset.value;
  if (category === 'pending') offset = pendingOffset.value;
  if (category === 'completed') offset = completedOffset.value;

  try {
    let res;
    if (category === 'ongoing') {
      res = await fetchTasks(props.workspaceId, { status: 'ongoing,blocked', limit: PAGE_SIZE, offset });
      ongoingTasks.value = isLoadMore ? [...ongoingTasks.value, ...res.tasks] : res.tasks;
      ongoingHasMore.value = res.tasks.length === PAGE_SIZE;
      ongoingOffset.value += res.tasks.length;
    } else if (category === 'notstarted') {
      res = await fetchTasks(props.workspaceId, { status: 'notstarted', limit: PAGE_SIZE, offset });
      notStartedTasks.value = isLoadMore ? [...notStartedTasks.value, ...res.tasks] : res.tasks;
      notStartedHasMore.value = res.tasks.length === PAGE_SIZE;
      notStartedOffset.value += res.tasks.length;
    } else if (category === 'scheduled') {
      res = await fetchTasks(props.workspaceId, { status: 'cron', limit: PAGE_SIZE, offset });
      scheduledTasks.value = isLoadMore ? [...scheduledTasks.value, ...res.tasks] : res.tasks;
      scheduledHasMore.value = res.tasks.length === PAGE_SIZE;
      scheduledOffset.value += res.tasks.length;
    } else if (category === 'pending') {
      res = await fetchTasks(props.workspaceId, { filter: 'pending_approval', limit: PAGE_SIZE, offset });
      pendingTasks.value = isLoadMore ? [...pendingTasks.value, ...res.tasks] : res.tasks;
      pendingHasMore.value = res.tasks.length === PAGE_SIZE;
      pendingOffset.value += res.tasks.length;
    } else if (category === 'completed') {
      res = await fetchTasks(props.workspaceId, { status: 'completed,rejected', limit: PAGE_SIZE, offset });
      completedTasks.value = isLoadMore ? [...completedTasks.value, ...res.tasks] : res.tasks;
      completedHasMore.value = res.tasks.length === PAGE_SIZE;
      completedOffset.value += res.tasks.length;
    }
    emitUpdatedTasks();
  } catch (err) {
    notifyError(`Failed to fetch ${category} tasks: ` + err.message);
  }
}

async function loadAllForFilter() {
  loadingTasks.value = true;
  const f = props.filter;
  if (f === 'active') {
    await Promise.all([
      fetchGroup('ongoing'),
      fetchGroup('notstarted'),
      fetchGroup('scheduled')
    ]);
  } else if (f === 'notstarted') {
    await fetchGroup('notstarted');
  } else if (f === 'pending') {
    await fetchGroup('pending');
  } else if (f === 'ongoing') {
    await fetchGroup('ongoing');
  } else if (f === 'completed') {
    await fetchGroup('completed');
  } else if (f === 'scheduled') {
    await fetchGroup('scheduled');
  }
  loadingTasks.value = false;
}

function emitUpdatedTasks() {
  const allTasks = [
    ...ongoingTasks.value,
    ...notStartedTasks.value,
    ...scheduledTasks.value,
    ...pendingTasks.value,
    ...completedTasks.value
  ];
  emit('tasks-updated', allTasks);
}

onMounted(() => {
  loadAllForFilter();
  loadCounts();
});

watch(() => props.filter, () => {
  loadAllForFilter();
});

watch(() => props.workspaceId, () => {
  ongoingTasks.value = [];
  notStartedTasks.value = [];
  scheduledTasks.value = [];
  pendingTasks.value = [];
  completedTasks.value = [];
  loadAllForFilter();
  loadCounts();
});

watch(() => props.liveEvents.length, (newLen, oldLen) => {
  if (newLen > oldLen) {
    const fresh = props.liveEvents.slice(oldLen);
    fresh.forEach(ev => {
      if (ev.type === 'task.deleted') {
        const id = ev.payload.id;
        ongoingTasks.value = ongoingTasks.value.filter(x => String(x.id) !== String(id));
        notStartedTasks.value = notStartedTasks.value.filter(x => String(x.id) !== String(id));
        scheduledTasks.value = scheduledTasks.value.filter(x => String(x.id) !== String(id));
        pendingTasks.value = pendingTasks.value.filter(x => String(x.id) !== String(id));
        completedTasks.value = completedTasks.value.filter(x => String(x.id) !== String(id));
        loadCounts();
        emitUpdatedTasks();
        return;
      }

      if (ev.type === 'task.updated' || ev.type === 'task.created' || ev.type === 'status.updated' || ev.type === 'respond.ack') {
        const t = ev.payload;
        if (t && t.workspaceId && String(t.workspaceId) !== String(props.workspaceId)) {
          return;
        }

        const getListForStatus = (status) => {
          if (status === 'cron') return scheduledTasks.value;
          if (['completed', 'rejected'].includes(status)) return completedTasks.value;
          if (status === 'notstarted') return notStartedTasks.value;
          if (['ongoing', 'blocked'].includes(status)) return ongoingTasks.value;
          return [];
        };

        const targetList = getListForStatus(t.status);
        const existing = targetList.find(x => String(x.id) === String(t.id));
        if (existing && new Date(existing.updatedAt).getTime() >= new Date(t.updatedAt).getTime()) {
          return;
        }

        ongoingTasks.value = ongoingTasks.value.filter(x => String(x.id) !== String(t.id));
        notStartedTasks.value = notStartedTasks.value.filter(x => String(x.id) !== String(t.id));
        scheduledTasks.value = scheduledTasks.value.filter(x => String(x.id) !== String(t.id));
        pendingTasks.value = pendingTasks.value.filter(x => String(x.id) !== String(t.id));
        completedTasks.value = completedTasks.value.filter(x => String(x.id) !== String(t.id));

        if (t.status === 'cron') {
          scheduledTasks.value.push(t);
        } else if (['completed', 'rejected'].includes(t.status)) {
          completedTasks.value.push(t);
        } else if (t.status === 'notstarted') {
          notStartedTasks.value.push(t);
        } else if (['ongoing', 'blocked'].includes(t.status)) {
          ongoingTasks.value.push(t);
        }

        const isPendingOnMe = t.status !== 'completed' && t.status !== 'rejected' && (
          (t.status === 'notstarted' && t.assignee === 'human') ||
          (t.messages && t.messages.some(m => m.metadata?.type === 'permission_request' && m.metadata?.status === 'pending'))
        );
        if (isPendingOnMe) {
          pendingTasks.value.push(t);
        }

        if (ev.type === 'task.created' && t.createdBy === 'agent') {
          notifyInfo(`Agent defined a new task: ${t.title}`, 'New Task');
        }
        loadCounts();
        emitUpdatedTasks();
      }
    });
  }
});

function formatDate(dateStr) {
  if (!dateStr) return '';
  const d = new Date(dateStr);
  if (isNaN(d.getTime())) return '';
  return d.toLocaleDateString([], { month: 'short', day: 'numeric' });
}

function formatTime(dateStr) {
  if (!dateStr) return 'Just now';
  const d = new Date(dateStr);
  if (isNaN(d.getTime())) return 'Just now';
  return d.toLocaleTimeString([], {hour: '2-digit', minute:'2-digit'});
}

function getTaskOrder(t) {
  if (t.sortOrder) return t.sortOrder;
  if (!t.createdAt) return Date.now() / 1000.0;
  return new Date(t.createdAt).getTime() / 1000.0;
}

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
      return;
    }

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
  } catch (err) {
    notifyError(`Failed to ${action} task: ` + err.message);
  }
};

const displayGroups = computed(() => {
  const f = props.filter;

  if (f === 'scheduled') {
    const cronTasks = [...scheduledTasks.value].sort((a,b) => getTaskOrder(a) - getTaskOrder(b));
    if (cronTasks.length === 0) return [{ title: 'Scheduled', tasks: [], hasMore: scheduledHasMore.value, category: 'scheduled' }];
    
    const categories = [
      { label: 'Every 15 mins', values: ['*/15 * * * *'] },
      { label: 'Every 30 mins', values: ['*/30 * * * *'] },
      { label: 'Hourly', values: ['0 * * * *'] },
      { label: 'Daily', values: ['0 0 * * *'] },
      { label: 'Weekly', values: ['0 0 * * 0'] },
      { label: 'Monthly', values: ['0 0 1 * *'] },
    ];

    const grps = [];
    const handledIds = new Set();

    categories.forEach(cat => {
      const matched = cronTasks.filter(t => cat.values.includes(t.cronSchedule));
      if (matched.length > 0) {
        grps.push({ title: cat.label, tasks: matched, hasMore: false });
        matched.forEach(t => handledIds.add(t.id));
      }
    });

    const other = cronTasks.filter(t => !handledIds.has(t.id));
    if (other.length > 0) grps.push({ title: 'Other', tasks: other, hasMore: false });

    if (grps.length > 0) {
      grps[grps.length - 1].hasMore = scheduledHasMore.value;
      grps[grps.length - 1].category = 'scheduled';
    }

    return grps;
  }

  const groups = [];
  if (f === 'active' || f === 'ongoing') {
    groups.push({ 
      title: 'Ongoing', 
      tasks: [...ongoingTasks.value].sort((a,b) => getTaskOrder(b) - getTaskOrder(a)),
      hasMore: ongoingHasMore.value,
      category: 'ongoing',
      totalCount: counts.value.ongoing
    });
  }
  if (f === 'active' || f === 'notstarted') {
    groups.push({
      title: 'Not Started',
      tasks: [...notStartedTasks.value].sort((a,b) => getTaskOrder(a) - getTaskOrder(b)),
      hasMore: notStartedHasMore.value,
      category: 'notstarted',
      totalCount: counts.value.notstarted
    });
  }

  if (f === 'active') {
    const sortedCron = [...scheduledTasks.value].sort((a, b) => {
      const aTime = getNextRunDate(a.cronSchedule).getTime();
      const bTime = getNextRunDate(b.cronSchedule).getTime();
      return aTime - bTime;
    });
    groups.push({ 
      title: 'Scheduled', 
      tasks: sortedCron,
      hasMore: scheduledHasMore.value,
      category: 'scheduled',
      totalCount: counts.value.scheduled
    });
  }

  if (f === 'pending') {
    groups.push({ 
      title: 'Action Required', 
      tasks: [...pendingTasks.value].sort((a,b) => getTaskOrder(b) - getTaskOrder(a)),
      hasMore: pendingHasMore.value,
      category: 'pending',
      totalCount: counts.value.pending
    });
  }

  if (f === 'completed') {
    groups.push({
      title: 'Completed',
      tasks: [...completedTasks.value].sort((a,b) => getTaskOrder(b) - getTaskOrder(a)),
      hasMore: completedHasMore.value,
      category: 'completed',
      totalCount: counts.value.completed
    });
  }

  return groups;
});

function getTaskDotStyle(t) {
  const status = typeof t === 'string' ? t : t.status;
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

function startCreate() {
  router.push(`/workspaces/${props.workspaceId}/tasks/new`);
}

function openTask(task) {
  const query = router.currentRoute.value.query;
  if (task.status === 'cron') {
    router.push({
      path: `/workspaces/${props.workspaceId}/tasks/${task.id}/instances`,
      query
    });
    return;
  }
  router.push({
    path: `/workspaces/${props.workspaceId}/tasks/${task.id}`,
    query
  });
}

function triggerEdit(task) {
  router.push(`/workspaces/${props.workspaceId}/tasks/${task.id}/edit`);
}

async function triggerDelete(task) {
  taskToDeleteId.value = task.id;
  taskToDeleteTitle.value = task.title;
  showDeleteModal.value = true;
}

function closeDeleteModal() {
  showDeleteModal.value = false;
  taskToDeleteId.value = null;
  taskToDeleteTitle.value = '';
}

async function onDeleteConfirm() {
  const taskId = taskToDeleteId.value;
  if (!taskId) return;
  try {
    await deleteTask(props.workspaceId, taskId);
    ongoingTasks.value = ongoingTasks.value.filter(x => x.id !== taskId);
    notStartedTasks.value = notStartedTasks.value.filter(x => x.id !== taskId);
    scheduledTasks.value = scheduledTasks.value.filter(x => x.id !== taskId);
    pendingTasks.value = pendingTasks.value.filter(x => x.id !== taskId);
    completedTasks.value = completedTasks.value.filter(x => x.id !== taskId);
    emitUpdatedTasks();
    notifySuccess('Task deleted');
    if (String(props.selectedTaskId) === String(taskId)) {
      router.push(`/workspaces/${props.workspaceId}`);
    }
  } catch(err) {
    notifyError('Delete Error: ' + err.message);
  } finally {
    closeDeleteModal();
  }
}

async function reorderTask(task, direction) {
  const group = displayGroups.value.find(g => g.title === 'Not Started');
  if (!group) return;
  
  const idx = group.tasks.findIndex(x => x.id === task.id);
  if (idx === -1) return;
  
  const targetIdx = idx + direction;
  if (targetIdx < 0 || targetIdx >= group.tasks.length) return;
  
  const neighbor = group.tasks[targetIdx];
  const neighborOrder = getTaskOrder(neighbor);
  let newOrder = direction === -1 ? neighborOrder + 0.001 : neighborOrder - 0.001;
  
  try {
    const res = await updateTaskOrder(props.workspaceId, task.id, newOrder);
    if (direction === -1 || direction === 1) {
      await fetchGroup('notstarted');
    }
  } catch (err) {
    notifyError('Reorder Error: ' + err.message);
  }
}

defineExpose({ startCreate });
</script>
