<template>
  <div class="h-full flex flex-col w-full max-w-full overflow-x-hidden" v-if="!loading">

    <!-- Breadcrumb Header -->
    <header class="pb-2 border-b-2 border-black shrink-0 flex items-center justify-between gap-4">
      <div class="flex items-center gap-2 text-xs font-black uppercase tracking-widest min-w-0 flex-1">
        <router-link :to="'/workspaces/' + workspaceId" class="hidden md:block text-gray-400 hover:text-black transition-colors shrink-0">
          {{ workspaceName }}
        </router-link>
        <svg class="hidden md:block w-3 h-3 text-gray-300 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M9 5l7 7-7 7" /></svg>
        <span class="text-gray-400 truncate flex-1 min-w-0 hidden sm:block">{{ scheduledTask?.title }}</span>
        <svg class="hidden sm:block w-3 h-3 text-gray-300 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M9 5l7 7-7 7" /></svg>
        <span class="text-black text-sm shrink-0">Instances</span>
      </div>
      <button @click="router.push('/workspaces/' + workspaceId)" class="p-1.5 text-gray-400 hover:text-black border-2 border-transparent hover:border-black transition-all shrink-0">
        <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M6 18L18 6M6 6l12 12" /></svg>
      </button>
    </header>

    <!-- Error -->
    <div v-if="error" class="mt-6 text-center py-8 text-sm font-black text-red-600 uppercase tracking-widest border-2 border-red-300 bg-red-50 p-6">
      {{ error }}
    </div>

    <div v-else class="flex-1 flex flex-col min-h-0 overflow-y-auto pt-4 pb-6 custom-scrollbar space-y-6">

      <!-- Scheduled Task Card -->
      <div v-if="scheduledTask" class="border-2 border-indigo-400 bg-indigo-50 shadow-[4px_4px_0px_0px_rgba(99,102,241,1)]">
        <div class="bg-indigo-600 px-4 py-2 flex items-center justify-between">
          <div class="flex items-center gap-2">
            <svg class="w-3.5 h-3.5 text-indigo-200" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3">
              <path stroke-linecap="round" stroke-linejoin="round" d="M12 6v6h4m6 0a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <span class="text-[10px] font-black text-indigo-100 uppercase tracking-widest">Scheduled Task</span>
          </div>
          <div class="flex items-center gap-2">
            <span class="text-[9px] font-black text-indigo-300 uppercase tracking-widest bg-indigo-800 px-2 py-0.5">
              ⏰ {{ formatCron(scheduledTask.cron_schedule) }}
            </span>
            <span v-if="nextRunLabel" class="text-[9px] font-black text-indigo-200 uppercase tracking-widest">
              NEXT: {{ nextRunLabel }}
            </span>
          </div>
        </div>
        <div class="px-4 py-3">
          <p class="font-black text-sm text-indigo-900 leading-snug">{{ scheduledTask.title }}</p>
          <p v-if="scheduledTask.body" class="text-xs text-indigo-700 mt-1 leading-relaxed line-clamp-2">{{ scheduledTask.body }}</p>
          <div class="flex flex-wrap items-center gap-3 mt-2 text-[9px] font-black uppercase tracking-widest text-indigo-500">
            <span>Created {{ formatDate(scheduledTask.created_at) }}</span>
            <span class="border-l border-indigo-300 pl-3">{{ instances.length }} instance{{ instances.length !== 1 ? 's' : '' }} found</span>
          </div>
        </div>
      </div>

      <!-- Instances List -->
      <div>
        <div class="mb-3 flex items-center gap-2">
          <h3 class="text-xs font-black uppercase tracking-widest text-black">Recent Instances</h3>
          <span class="text-[10px] font-bold text-gray-400 border border-gray-200 bg-white px-1 shadow-[1px_1px_0px_0px_rgba(0,0,0,0.1)]">{{ instances.length }}</span>
          <div class="h-px bg-gray-200 flex-1 ml-2"></div>
        </div>

        <div v-if="instances.length === 0" class="flex flex-col items-center justify-center text-gray-300 opacity-80 py-16 border-2 border-dashed border-gray-200 bg-gray-50">
          <svg class="w-8 h-8 mb-3 text-gray-300" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
            <path stroke-linecap="round" stroke-linejoin="round" d="M12 6v6h4m6 0a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          <span class="text-xs font-black uppercase tracking-widest text-gray-400">No instances yet</span>
          <p class="text-[10px] text-gray-400 mt-1">This scheduled task hasn't run yet.</p>
        </div>

        <div v-else class="space-y-3">
          <div v-for="instance in instances" :key="instance.id"
               @click="router.push(`/workspaces/${workspaceId}/tasks/${instance.id}`)"
               class="flex items-center justify-between p-3.5 border-2 cursor-pointer bg-white shadow-[3px_3px_0px_0px_rgba(0,0,0,1)] hover:-translate-y-0.5 hover:shadow-[5px_5px_0px_0px_rgba(0,0,0,1)] transition-all group rounded-sm"
               :class="getTaskBgStyle(instance.status)">

            <div class="flex items-center gap-3.5 flex-1 min-w-0 pr-4">
              <span class="w-2.5 h-2.5 rounded-full border border-black shrink-0" :class="getTaskDotStyle(instance.status)"></span>
              <div class="flex flex-col gap-1 w-full relative">
                <span class="font-black text-[14px] text-gray-900 leading-snug truncate group-hover:text-black transition-colors w-full font-bold text-sm">
                  {{ instance.title }}
                </span>
                <div class="flex flex-wrap items-center gap-2 md:gap-3 text-[9px] font-black uppercase tracking-widest mt-0.5">
                  <span class="text-gray-500">{{ formatDate(instance.created_at) }}</span>
                  <span class="flex items-center gap-1 bg-white border border-gray-200 px-1 text-gray-600" v-if="instance.Messages && instance.Messages.length > 0">
                    <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M8 10h.01M12 10h.01M16 10h.01M9 16H5a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v8a2 2 0 01-2 2h-5l-5 5v-5z" /></svg>
                    {{ instance.Messages.length }}
                  </span>
                </div>
              </div>
            </div>

            <div class="flex items-center gap-3 shrink-0">
              <div class="hidden md:block text-[10px] font-black uppercase tracking-widest px-2 py-1 border-2 min-w-[90px] text-center"
                   :class="getTaskBadgeStyle(instance.status)">
                {{ getTaskLabel(instance.status) }}
              </div>
              <svg class="w-4 h-4 text-gray-300 group-hover:text-gray-500 transition-colors" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M9 5l7 7-7 7" /></svg>
            </div>
          </div>
        </div>
      </div>

    </div>
  </div>

  <div v-else class="text-center py-12 text-sm font-black text-gray-400 uppercase tracking-widest animate-pulse">
    Loading...
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import cronParser from 'cron-parser';
import { fetchTasks, getWorkspace } from '../api';

const route = useRoute();
const router = useRouter();

const workspaceId = route.params.workspaceId;
const taskId = route.params.taskId;

const loading = ref(true);
const error = ref('');
const workspaceName = ref('');
const scheduledTask = ref(null);
const instances = ref([]);

onMounted(async () => {
  try {
    const [wsData, tasksData] = await Promise.all([
      getWorkspace(workspaceId),
      fetchTasks(workspaceId)
    ]);

    workspaceName.value = wsData.workspace?.name || wsData.name || '';

    const allTasks = tasksData.tasks || tasksData || [];

    scheduledTask.value = allTasks.find(t => t.id === taskId) || null;

    // Find instances: tasks whose parent_id matches this task's id, sorted newest first, limit 5
    instances.value = allTasks
      .filter(t => t.parent_id === taskId)
      .sort((a, b) => new Date(b.created_at) - new Date(a.created_at))
      .slice(0, 5);
  } catch (err) {
    error.value = err.message || 'Failed to load instances';
  } finally {
    loading.value = false;
  }
});

const nextRunLabel = computed(() => {
  if (!scheduledTask.value?.cron_schedule) return '';
  return getNextRunLabel(scheduledTask.value.cron_schedule);
});

function formatDate(dateStr) {
  if (!dateStr) return '—';
  const d = new Date(dateStr);
  if (isNaN(d.getTime())) return '—';
  return d.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
}

function formatCron(cron) {
  if (!cron) return '';
  const parts = cron.split(' ');
  if (parts.length === 5 && parts[2] !== '*' && parts[3] !== '*') return 'One-time';
  const presets = {
    '0 * * * *': 'Hourly',
    '*/15 * * * *': 'Every 15m',
    '*/30 * * * *': 'Every 30m',
  };
  if (presets[cron]) return presets[cron];
  try {
    const [min, hour, dom, month, dow] = parts;
    if (dow !== '*' && dom === '*' && month === '*') {
      return `Weekly at ${hour}:${min.padStart(2, '0')}`;
    }
    if (dom !== '*' && month === '*' && dow === '*') {
      return `Monthly (Day ${dom}) at ${hour}:${min.padStart(2, '0')}`;
    }
    if (dom === '*' && month === '*' && dow === '*') {
      return `Daily at ${hour}:${min.padStart(2, '0')}`;
    }
  } catch (e) {}
  return cron;
}

function getNextRunLabel(cron) {
  if (!cron) return '';
  try {
    const interval = cronParser.parseExpression(cron);
    const next = interval.next().toDate();
    const now = new Date();
    const diffMs = next.getTime() - now.getTime();
    const diffMin = Math.floor(diffMs / 60000);
    const diffHour = Math.floor(diffMin / 60);
    const diffDay = Math.floor(diffHour / 24);
    if (diffDay > 0) return `In ${diffDay}d ${diffHour % 24}h`;
    if (diffHour > 0) return `In ${diffHour}h ${diffMin % 60}m`;
    if (diffMin > 0) return `In ${diffMin}m`;
    return 'Soon';
  } catch (e) {
    return '';
  }
}

function getTaskBgStyle(status) {
  if (status === 'ongoing') return 'bg-yellow-50 border-black';
  if (status === 'blocked') return 'bg-red-50 border-black';
  if (status === 'completed') return 'bg-green-50 border-black';
  return 'bg-gray-50 border-gray-300 border-dashed shadow-none text-gray-500 hover:border-gray-400 hover:bg-gray-100';
}

function getTaskDotStyle(status) {
  if (status === 'ongoing') return 'bg-yellow-400';
  if (status === 'blocked') return 'bg-red-500';
  if (status === 'completed') return 'bg-green-500';
  return 'bg-gray-300 border-gray-400';
}

function getTaskBadgeStyle(status) {
  if (status === 'ongoing') return 'bg-yellow-200 border-black text-black';
  if (status === 'blocked') return 'bg-red-200 border-black text-black';
  if (status === 'completed') return 'bg-green-200 border-black text-black';
  if (status === 'rejected') return 'bg-red-100 border-red-400 text-red-700';
  return 'bg-gray-100 border-gray-300 text-gray-500 font-bold';
}

function getTaskLabel(status) {
  if (status === 'notstarted') return 'NOT STARTED';
  return status.toUpperCase();
}
</script>
