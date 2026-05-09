<template>
  <div class="h-full flex flex-col w-full max-w-full overflow-x-hidden bg-transparent" v-if="!loading">


    <!-- Error -->
    <div v-if="error" class="mt-6 text-center py-8 text-sm font-bold text-red-600 dark:text-red-400 border border-red-200 dark:border-red-500/30 bg-red-50 dark:bg-red-500/10 rounded-sm m-6">
      {{ error }}
    </div>

    <div v-else class="flex-1 flex flex-col min-h-0 overflow-y-auto custom-scrollbar w-full">
      <!-- Main Detail Section (Matching KeywordInbox Design) -->
      <div v-if="scheduledTask" class="px-4 py-2 mb-6">
        <div class="flex flex-col gap-1">
          <!-- Top Metadata Row -->

          <!-- Title -->
          <div class="flex items-center justify-between gap-4">
            <div class="flex items-center gap-3 min-w-0">
              <h1 class="text-lg md:text-xl font-black text-gray-800 dark:text-zinc-200 tracking-tight leading-tight truncate">
                {{ scheduledTask.title }}
              </h1>
            </div>
            <div class="flex items-center gap-3 shrink-0">
              <div class="flex items-center gap-2 px-4 py-2 text-[10px] font-black text-gray-700 dark:text-zinc-200 bg-gray-100 dark:bg-zinc-800 rounded-sm border border-transparent h-8">
                <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>
                <span>RUNS {{ formatCron(scheduledTask.cronSchedule).toUpperCase() }}</span>
              </div>
            </div>
          </div>

          <!-- Secondary Metadata -->
          <div class="flex flex-wrap items-center gap-4 text-[9px] text-gray-500 dark:text-zinc-500 font-bold uppercase tracking-wider">
            <div class="flex items-center gap-2">
              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" /></svg>
              <span>NEXT RUN: {{ getNextRunDateTime(scheduledTask.cronSchedule) }}</span>
            </div>
            <div class="flex items-center gap-2">
              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 20l4-16m2 16l4-16" /></svg>
              <span>{{ workspaceName }}</span>
            </div>
          </div>

          <div v-if="scheduledTask.body" class="mt-1">
            <p @click="isDescriptionCollapsed = !isDescriptionCollapsed"
               :class="[
                 isDescriptionCollapsed 
                   ? 'truncate text-[11px] text-gray-500 dark:text-zinc-500 py-1' 
                   : 'whitespace-pre-wrap p-4 bg-gray-50/50 dark:bg-zinc-800/30 rounded-xl border border-gray-100 dark:border-zinc-800 text-[13px] text-gray-600 dark:text-zinc-300 animate-in fade-in slide-in-from-top-1 duration-200'
               ]"
               class="cursor-pointer font-medium leading-relaxed transition-all hover:text-gray-800 dark:hover:text-zinc-100">
              {{ isDescriptionCollapsed ? stripNote(scheduledTask.body) : scheduledTask.body }}
            </p>
          </div>
        </div>
      </div>

      <!-- Instances List Section -->
      <div class="px-4 pb-8">
        <div class="flex items-center justify-between mb-8">
          <div class="flex items-center gap-3">
            <h3 class="text-xs font-black text-gray-900 dark:text-zinc-100 uppercase tracking-widest">Recent Instances</h3>
            <span class="text-[10px] font-black text-gray-500 dark:text-zinc-400 bg-gray-100 dark:bg-zinc-800 rounded-md px-2 py-0.5 border border-gray-200 dark:border-zinc-700 shadow-sm">{{ instances.length }}</span>
          </div>
        </div>

        <div v-if="instances.length === 0" class="flex flex-col items-center justify-center text-gray-500 dark:text-zinc-500 opacity-80 py-20 border border-dashed border-gray-200 dark:border-zinc-800 bg-gray-50/50 dark:bg-zinc-900/50 rounded-3xl">
          <svg class="w-10 h-10 mb-4 text-gray-300 dark:text-zinc-600" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
            <path stroke-linecap="round" stroke-linejoin="round" d="M12 6v6h4m6 0a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          <span class="text-sm font-bold">No instances yet</span>
          <p class="text-[11px] mt-2 text-gray-500 dark:text-zinc-500">This scheduled task hasn't run yet.</p>
        </div>

        <div v-else class="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-2">
          <div v-for="instance in instances" :key="instance.id"
               @click="router.push(`/workspaces/${workspaceId}/tasks/${instance.id}`)"
               class="flex items-center justify-between p-3 cursor-pointer border border-gray-100 dark:border-zinc-800/50 bg-gray-50/30 dark:bg-zinc-900/30 hover:bg-gray-100 dark:hover:bg-zinc-800 rounded-sm group transition-all">
            
            <div class="flex items-center gap-3 min-w-0">
              <div class="w-2 h-2 rounded-full shrink-0" :class="getTaskDotStyle(instance)"></div>
              <span class="text-[11px] font-bold text-gray-700 dark:text-zinc-200 truncate uppercase tracking-tight group-hover:text-black dark:group-hover:text-white">{{ formatDateShort(instance.createdAt) }}</span>
            </div>
            
            <div class="flex items-center gap-2 shrink-0">
              <span class="text-[9px] font-black text-gray-400 dark:text-zinc-500 tracking-tighter">#{{ instance.id.toString().slice(-4) }}</span>
              <svg class="w-3 h-3 text-gray-300 dark:text-zinc-600 group-hover:text-gray-900 dark:group-hover:text-zinc-400 transition-colors" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" /></svg>
            </div>
          </div>
        </div>
      </div>

    </div>
  </div>

  <div v-else class="h-full flex items-center justify-center bg-transparent">
    <div class="p-8 flex flex-col items-center gap-4 opacity-50">
      <div class="w-12 h-12 rounded-full border-4 border-gray-200 dark:border-zinc-700 border-t-gray-900 dark:border-t-white animate-spin"></div>
      <p class="text-[10px] font-semibold text-gray-500 dark:text-zinc-500">Loading Instances...</p>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import cronParser from 'cron-parser';
import { fetchTasks, getWorkspace } from '../api';
import { useCron } from '../composables/useCron';

const { formatCron, getNextRunLabel, getNextRunDateTime } = useCron();

const route = useRoute();
const router = useRouter();

const workspaceId = computed(() => route.params.id || route.params.workspaceId);
const taskId = computed(() => route.params.taskId);

const loading = ref(true);
const error = ref('');
const workspaceName = ref('');
const scheduledTask = ref(null);
const instances = ref([]);
const isDescriptionCollapsed = ref(true);

async function loadData() {
  loading.value = true;
  try {
    const [wsData, tasksData] = await Promise.all([
      getWorkspace(workspaceId.value),
      fetchTasks(workspaceId.value)
    ]);

    workspaceName.value = wsData.workspace?.name || wsData.name || '';
    const allTasks = tasksData.tasks || tasksData || [];
    scheduledTask.value = allTasks.find(t => t.id === taskId.value) || null;

    // Find instances: tasks whose parentId matches this task's id, sorted newest first, limit 24
    instances.value = allTasks
      .filter(t => t.parentId === taskId.value)
      .sort((a, b) => new Date(b.createdAt) - new Date(a.createdAt))
      .slice(0, 24);
  } catch (err) {
    error.value = err.message || 'Failed to load instances';
  } finally {
    loading.value = false;
  }
}

onMounted(loadData);
watch(taskId, loadData);

const nextRunLabel = computed(() => {
  if (!scheduledTask.value?.cronSchedule) return '';
  return getNextRunLabel(scheduledTask.value.cronSchedule);
});

function formatDate(dateStr) {
  if (!dateStr) return '—';
  const d = new Date(dateStr);
  if (isNaN(d.getTime())) return '—';
  return d.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
}

function formatDateShort(dateStr) {
  if (!dateStr) return '';
  const date = new Date(dateStr);
  const timeStr = date.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', hour12: false });
  const dateStrShort = date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
  return `${dateStrShort} ${timeStr}`;
}


function getTaskBgStyle(status) {
  if (status === 'ongoing') return 'border-yellow-200 dark:border-yellow-500/30';
  if (status === 'blocked') return 'border-red-200 dark:border-red-500/30';
  if (status === 'completed') return 'border-gray-900 dark:border-white';
  return 'border-gray-200 dark:border-zinc-800';
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
  if (status === 'rejected') return 'bg-red-100 dark:bg-red-500/20 text-red-700 dark:text-red-500 border-red-200 dark:border-red-500/30';
  return 'bg-gray-100 dark:bg-zinc-800 text-gray-500 dark:text-zinc-400 border-gray-200 dark:border-zinc-700';
}

function getTaskLabel(status) {
  if (status === 'notstarted') return 'NOT STARTED';
  return status.toUpperCase();
}
function stripNote(body) {
  if (!body) return '';
  const markerRegex = /\n\n(Self[\s-]Learning[\s-]Loop[\s-]Note|\[Self[\s-]Learning[\s-]Loop[\s-]Note\]|Self[\s-]Learning[\s-]Loop:)/i;
  const match = body.match(markerRegex);
  if (match) {
    return body.substring(0, match.index).trim();
  }
  return body;
}
</script>
