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
            <span class="text-[9px] font-bold text-gray-500 dark:text-zinc-500 bg-gray-100 dark:bg-zinc-800 px-1.5 py-0.5 rounded-sm">{{ grp.title === 'Completed' ? grp.totalCompleted : grp.tasks.length }}</span>
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
                      <button v-if="!isArchived && t.status === 'cron'" @click.stop="triggerEdit(t)" class="text-gray-500 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-100 dark:hover:bg-zinc-700 p-1 rounded-sm transition-all">
                        <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" /></svg>
                      </button>
                      <button v-if="!isArchived" @click.stop="triggerDelete(t)" class="text-gray-500 hover:text-red-500 hover:bg-red-50 dark:hover:bg-red-500/10 p-1 rounded-sm transition-all">
                        <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" /></svg>
                      </button>
                   </div>
                   <span class="text-[10px] text-gray-500 dark:text-zinc-400 font-medium uppercase tracking-wider tabular-nums shrink-0">{{ formatTime(t.createdAt) }}</span>
                </div>
              </div>

              <h3 :class="[ 'text-[13px] leading-relaxed line-clamp-2 transition-colors font-medium', 
                             String(selectedTaskId) === String(t.id) ? 'text-gray-800 dark:text-zinc-200' : 
                             t.status === 'completed' ? 'text-gray-500 dark:text-zinc-500' : 
                             'text-gray-700 dark:text-zinc-200 group-hover:text-black dark:group-hover:text-white' ]">
                {{ t.title }}
              </h3>

              <div v-if="t.status === 'cron' && t.cronSchedule" class="mt-2 flex items-center gap-1.5 text-[9px] text-gray-500 dark:text-zinc-500 font-medium uppercase tracking-tight">
                <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>
                <span>{{ getNextRunLabel(t.cronSchedule) }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>
      
      <!-- Sticky Load More Footer -->
      <div v-if="displayGroups.find(g => g.hasMore)" class="sticky bottom-0 left-0 right-0 p-2 bg-white/80 dark:bg-zinc-900/80 backdrop-blur-md border-t border-gray-100 dark:border-zinc-800 z-30">
        <template v-for="grp in displayGroups" :key="'more-' + grp.title">
          <button v-if="grp.hasMore" @click.stop="completedLimit += 5" class="w-full py-1.5 rounded-sm border border-dashed border-gray-200 dark:border-zinc-800 text-gray-500 dark:text-zinc-400 text-[10px] font-semibold hover:border-gray-300 dark:hover:border-zinc-700 hover:text-gray-900 dark:hover:text-zinc-50 hover:bg-gray-50 dark:hover:bg-zinc-800/50 transition-all shadow-sm">
            Load More ({{ grp.totalCompleted - completedLimit }} remaining)
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
import { deleteTask, respondToTask, updateTaskOrder, updateTaskStatus } from '../api';
import { useCron } from '../composables/useCron';
import DeleteModal from './DeleteModal.vue';
import { useToasts } from '../composables/useToasts';

const { formatCron, getNextRunLabel } = useCron();

const props = defineProps({
  workspaceId: { type: [String, Number], required: true },
  initialTasks: { type: Array, default: () => [] },
  liveEvents: { type: Array, default: () => [] },
  isArchived: { type: Boolean, default: false },
  isAgentConnected: { type: Boolean, default: false },
  filter: { type: String, default: 'active' },
  selectedTaskId: { type: [String, Number], default: null }
});

const emit = defineEmits(['filter-change']);

const router = useRouter();
const { notifyError, notifySuccess, notifyInfo } = useToasts();
const activeStatusMenuId = ref(null);

const showDeleteModal = ref(false);
const taskToDeleteId = ref(null);
const taskToDeleteTitle = ref('');

const localTasks = ref([...props.initialTasks]);
const completedLimit = ref(5);

watch(() => props.initialTasks, (newTasks) => {
  localTasks.value = [...newTasks];
}, { deep: true });

watch(() => props.liveEvents.length, (newLen, oldLen) => {
  if (newLen > oldLen) {
    const fresh = props.liveEvents.slice(oldLen);
    fresh.forEach(ev => {
      if (ev.type === 'task.deleted') {
        const id = ev.payload.id;
        localTasks.value = localTasks.value.filter(x => x.id !== id);
        return;
      }

      if (ev.type === 'task.updated' || ev.type === 'task.created' || ev.type === 'status.updated' || ev.type === 'respond.ack') {
        const t = ev.payload;
        const idx = localTasks.value.findIndex(x => x.id === t.id);
        if (idx !== -1) {
          localTasks.value[idx] = t;
        } else {
          localTasks.value.push(t);
          if (t.createdBy === 'agent') {
            notifyInfo(`Agent defined a new task: ${t.title}`, 'New Task');
          }
        }
      }
    });
  }
});

function formatTime(dateStr) {
  if (!dateStr) return 'Just now';
  const d = new Date(dateStr);
  if (isNaN(d.getTime())) return 'Just now';
  return d.toLocaleTimeString([], {hour: '2-digit', minute:'2-digit'});
}

function getTaskOrder(t) {
  if (t.sortOrder) return t.sortOrder;
  if (!t.createdAt) return Date.now();
  return new Date(t.createdAt).getTime() / 1000.0;
}

const emptyStateLabel = computed(() => {
  switch (props.filter) {
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
      return 'No tasks match this category.';
  }
});

const displayGroups = computed(() => {
  const f = props.filter;

  if (f === 'scheduled') {
    const cronTasks = localTasks.value.filter(t => t.status === 'cron').sort((a,b) => getTaskOrder(a) - getTaskOrder(b));
    if (cronTasks.length === 0) return [];
    
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
        grps.push({ title: cat.label, tasks: matched });
        matched.forEach(t => handledIds.add(t.id));
      }
    });

    const other = cronTasks.filter(t => !handledIds.has(t.id));
    if (other.length > 0) grps.push({ title: 'Other', tasks: other });

    return grps;
  }

  // Filter tasks based on status
  let filtered = localTasks.value.filter(t => t.status !== 'cron');
  
  if (f === 'active') {
    filtered = filtered.filter(t => ['ongoing', 'blocked', 'notstarted'].includes(t.status));
  } else if (f === 'notstarted') {
    filtered = filtered.filter(t => t.status === 'notstarted');
  } else if (f === 'ongoing') {
    filtered = filtered.filter(t => ['ongoing', 'blocked'].includes(t.status));
    filtered = filtered.filter(t => 
      t.status !== 'completed' && t.status !== 'rejected' && (
        (t.status === 'notstarted' && t.assignee === 'human') ||
        (t.messages && t.messages.some(m => m.metadata?.type === 'permission_request' && m.metadata?.status === 'pending'))
      )
    );
  } else if (f === 'completed') {
    filtered = filtered.filter(t => ['completed', 'rejected'].includes(t.status));
  }

  const ongoing = filtered.filter(t => ['ongoing', 'blocked'].includes(t.status)).sort((a,b) => getTaskOrder(b) - getTaskOrder(a));
  const notStarted = filtered.filter(t => ['notstarted'].includes(t.status)).sort((a,b) => getTaskOrder(b) - getTaskOrder(a));
  const completed = filtered.filter(t => ['completed', 'rejected'].includes(t.status)).sort((a,b) => getTaskOrder(b) - getTaskOrder(a));

  const groups = [];
  if (f === 'active' || f === 'ongoing') {
    groups.push({ title: 'Ongoing', tasks: ongoing });
  }
  if (f === 'active' || f === 'notstarted') {
    groups.push({ title: 'Not Started', tasks: notStarted });
  }

  if (f === 'pending') {
    groups.push({ title: 'Action Required', tasks: filtered.sort((a,b) => getTaskOrder(b) - getTaskOrder(a)) });
  }

  if (f === 'completed') {
    groups.push({
      title: 'Completed',
      tasks: completed.slice(0, completedLimit.value),
      hasMore: completed.length > completedLimit.value,
      totalCompleted: completed.length
    });
  }

  return groups;
});

const pendingInputCount = computed(() => {
  return localTasks.value.filter(t => 
    t.status !== 'completed' && t.status !== 'rejected' && (
      (t.status === 'notstarted' && t.assignee === 'human') ||
      (t.messages && t.messages.some(m => m.metadata?.type === 'permission_request' && m.metadata?.status === 'pending'))
    )
  ).length;
});

const scheduledCount = computed(() => {
  return localTasks.value.filter(t => t.status === 'cron').length;
});

const activeTaskCount = computed(() => {
  return localTasks.value.length - scheduledCount.value;
});

function getTaskBgStyle(t) {
  const isSelected = String(props.selectedTaskId) === String(t.id);
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
}

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

function getTaskBadgeStyle(status) {
  if (status === 'ongoing') return 'bg-yellow-100 dark:bg-yellow-500/20 text-yellow-700 dark:text-yellow-500 border-yellow-200 dark:border-yellow-500/30';
  if (status === 'blocked') return 'bg-red-100 dark:bg-red-500/20 text-red-700 dark:text-red-500 border-red-200 dark:border-red-500/30';
  if (status === 'completed') return 'bg-gray-900 dark:bg-white text-white dark:text-black border-black dark:border-white';
  if (status === 'cron') return 'bg-sky-100 dark:bg-sky-500/20 text-sky-700 dark:text-sky-500 border-sky-200 dark:border-sky-500/30';
  return 'bg-gray-100 dark:bg-zinc-800 text-gray-500 dark:text-zinc-400 border-gray-200 dark:border-zinc-700';
}

function getTaskLabel(status) {
  if (status === 'notstarted') return 'NOT STARTED';
  if (status === 'cron') return 'SCHEDULED';
  return status;
}

function startCreate() {
  router.push(`/workspaces/${props.workspaceId}/tasks/new`);
}

function openTask(task) {
  if (task.status === 'cron') {
    router.push(`/workspaces/${props.workspaceId}/tasks/${task.id}/instances`);
    return;
  }
  // This router push will now load inside the WorkspaceDetailView's router-view
  router.push(`/workspaces/${props.workspaceId}/tasks/${task.id}`);
}

function triggerEdit(task) {
  router.push(`/workspaces/${props.workspaceId}/tasks/${task.id}/edit`);
}

async function respond(taskId, action) {
  try {
    let res;
    if (['notstarted', 'ongoing', 'completed', 'rejected'].includes(action)) {
        res = await updateTaskStatus(props.workspaceId, taskId, action);
    } else {
        res = await respondToTask(props.workspaceId, taskId, action, '');
    }
    const idx = localTasks.value.findIndex(x => x.id === taskId);
    if (idx !== -1) localTasks.value[idx] = res.task;
    notifySuccess('Status updated successfully');
  } catch(err) {
    notifyError("Failed to update status: " + err.message);
  }
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
    localTasks.value = localTasks.value.filter(x => x.id !== taskId);
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
    const localIdx = localTasks.value.findIndex(x => x.id === task.id);
    if (localIdx !== -1) localTasks.value[localIdx] = res.task;
  } catch (err) {
    notifyError('Reorder Error: ' + err.message);
  }
}

defineExpose({ startCreate });
</script>
