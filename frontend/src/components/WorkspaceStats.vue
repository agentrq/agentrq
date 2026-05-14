<template>
  <div class="flex flex-col gap-8">
    <!-- Filters -->
    <div class="flex flex-wrap items-center gap-1.5 bg-gray-100 dark:bg-zinc-900 p-1 border border-gray-200 dark:border-zinc-800 rounded-sm max-w-max">
      <button 
        v-for="opt in rangeOptions" 
        :key="opt.id"
        @click="setRange(opt.id)"
        class="px-3 py-1.5 text-[10px] font-black uppercase tracking-widest rounded-sm transition-all"
        :class="activeRange === opt.id 
          ? 'bg-white dark:bg-zinc-800 text-black dark:text-white shadow-sm border border-gray-200 dark:border-zinc-700 rounded-sm'
          : 'text-gray-500 hover:text-gray-900 dark:text-zinc-400 dark:hover:text-zinc-50 rounded-sm'"
      >
        {{ opt.label }}
      </button>
      
      <!-- Custom Date Inputs (only if custom is selected) -->
      <div v-if="activeRange === 'custom'" class="flex items-center gap-2 px-2 border-l border-gray-200 dark:border-zinc-800 ml-1">
        <input type="date" v-model="customFrom" class="text-[10px] bg-transparent border-none focus:ring-0 text-gray-900 dark:text-zinc-50 font-black p-0 w-24" />
        <span class="text-[10px] font-black text-gray-500 dark:text-zinc-500">–</span>
        <input type="date" v-model="customTo" class="text-[10px] bg-transparent border-none focus:ring-0 text-gray-900 dark:text-zinc-50 font-black p-0 w-24" />
        <button @click="load" class="p-1 text-gray-500 hover:text-black dark:hover:text-white transition-all">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M5 13l4 4L19 7" /></svg>
        </button>
      </div>
    </div>

    <!-- Summary Cards -->
    <div v-if="stats && stats.summary" class="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-4">
      <!-- Tasks Completed -->
      <div class="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-sm p-5 flex flex-col gap-1 shadow-sm hover:shadow-md transition-all duration-200">
        <span class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Completed</span>
        <span class="text-3xl font-black text-gray-900 dark:text-zinc-50 tabular-nums leading-none">{{ stats.summary.tasksCompleted.toLocaleString() }}</span>
      </div>

      <!-- Scheduled -->
      <div class="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-sm p-5 flex flex-col gap-1 shadow-sm hover:shadow-md transition-all duration-200">
        <span class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Scheduled</span>
        <span class="text-3xl font-black text-gray-900 dark:text-zinc-50 tabular-nums leading-none">{{ stats.summary.tasksScheduled.toLocaleString() }}</span>
      </div>

      <!-- Messages -->
      <div class="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-sm p-5 flex flex-col gap-1 shadow-sm hover:shadow-md transition-all duration-200">
        <span class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Messages</span>
        <span class="text-3xl font-black text-gray-900 dark:text-zinc-50 tabular-nums leading-none">{{ stats.summary.messages.toLocaleString() }}</span>
      </div>

      <!-- Manual -->
      <div class="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-sm p-5 flex flex-col gap-1 shadow-sm hover:shadow-md transition-all duration-200">
        <span class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Manual</span>
        <span class="text-3xl font-black text-gray-900 dark:text-zinc-50 tabular-nums leading-none">{{ stats.summary.manualApprovals.toLocaleString() }}</span>
      </div>

      <!-- Auto -->
      <div class="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-sm p-5 flex flex-col gap-1 shadow-sm hover:shadow-md transition-all duration-200">
        <span class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Auto</span>
        <span class="text-3xl font-black text-gray-900 dark:text-zinc-50 tabular-nums leading-none">{{ stats.summary.autoApprovals.toLocaleString() }}</span>
      </div>

      <!-- Denies -->
      <div class="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-sm p-5 flex flex-col gap-1 shadow-sm hover:shadow-md transition-all duration-200">
        <span class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Denies</span>
        <span class="text-3xl font-black text-gray-900 dark:text-zinc-50 tabular-nums leading-none">{{ stats.summary.denies.toLocaleString() }}</span>
      </div>
    </div>

    <!-- Charts Section -->
    <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
      <!-- Task Chart -->
      <div class="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-sm p-6 shadow-sm">
        <div class="flex items-center justify-between mb-6">
          <h3 class="text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-50">Task Completion Velocity</h3>
          <div class="text-gray-400 dark:text-zinc-500 text-[9px] font-black uppercase tracking-widest">Daily Trend</div>
        </div>
        <div class="h-56 w-full relative">
          <div v-if="loading" class="absolute inset-0 flex items-center justify-center z-10">
            <div class="text-[10px] font-black text-gray-300 dark:text-zinc-400 animate-pulse">Computing...</div>
          </div>
          <ChartSVG 
            v-if="stats && stats.timeseries && stats.timeseries.tasksCompleted"
            :data="stats.timeseries.tasksCompleted" 
            :color="isDark ? '#d4d4d8' : '#27272a'" 
            :fixed-length="chartFixedLength"
          />
        </div>
      </div>

      <!-- Message Chart -->
      <div class="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-sm p-6 shadow-sm">
        <div class="flex items-center justify-between mb-6">
          <h3 class="text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-50">Communication Volume</h3>
          <div class="text-gray-400 dark:text-zinc-500 text-[9px] font-black uppercase tracking-widest">Total Messages</div>
        </div>
        <div class="h-56 w-full relative">
          <div v-if="loading" class="absolute inset-0 flex items-center justify-center z-10">
            <div class="text-[10px] font-black text-gray-300 dark:text-zinc-400 animate-pulse">Computing...</div>
          </div>
          <ChartSVG 
            v-if="stats && stats.timeseries && stats.timeseries.messages"
            :data="stats.timeseries.messages" 
            :color="isDark ? '#d4d4d8' : '#27272a'" 
            :fixed-length="chartFixedLength"
          />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, computed, watch } from 'vue';
import { fetchWorkspaceStats } from '../api';
import ChartSVG from './ChartSVG.vue';
import { useThemeStore } from '../stores/themeStore';

const props = defineProps({
  workspaceId: { type: [String, Number], required: true }
});

const themeStore = useThemeStore();
const stats = ref(null);
const loading = ref(true);
const activeRange = ref('7d');
const customFrom = ref('');
const customTo = ref('');

const isDark = computed(() =>
  themeStore.theme === 'dark' ||
  (themeStore.theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)
);

const rangeOptions = [
  { id: '1d', label: '1d' },
  { id: '7d', label: '7d' },
  { id: 'week', label: 'Wk' },
  { id: '30d', label: '30d' },
  { id: 'month', label: 'Mo' },
  { id: 'custom', label: 'Cust' }
];

async function load() {
  loading.value = true;
  try {
    let fromTs = 0;
    let toTs = 0;
    
    if (activeRange.value === 'custom') {
      if (customFrom.value) fromTs = Math.floor(new Date(customFrom.value).getTime() / 1000);
      if (customTo.value) toTs = Math.floor(new Date(customTo.value).getTime() / 1000);
    }
    
    const res = await fetchWorkspaceStats(props.workspaceId, activeRange.value, fromTs, toTs);
    stats.value = res;
  } catch (err) {
    console.error('Failed to load stats:', err);
  } finally {
    loading.value = false;
  }
}

const chartFixedLength = computed(() => {
  if (activeRange.value === '7d' || activeRange.value === 'week') return 7;
  if (activeRange.value === '30d' || activeRange.value === 'month') return 30;
  return 0;
});

function setRange(range) {
  activeRange.value = range;
  if (range !== 'custom') {
    load();
  }
}

onMounted(load);
</script>
