<template>
  <div class="flex flex-col gap-6">
    <!-- Filters -->
    <div class="flex flex-wrap items-center gap-2 bg-gray-50 p-2 border-2 border-black shadow-[2px_2px_0px_0px_rgba(0,0,0,1)]">
      <button 
        v-for="opt in rangeOptions" 
        :key="opt.id"
        @click="setRange(opt.id)"
        class="px-3 py-1.5 text-[10px] font-black uppercase tracking-widest border-2 transition-all"
        :class="activeRange === opt.id ? 'bg-black text-white border-black' : 'bg-white text-black border-transparent hover:border-black'"
      >
        {{ opt.label }}
      </button>
      
      <!-- Custom Date Inputs (only if custom is selected) -->
      <div v-if="activeRange === 'custom'" class="flex items-center gap-2 ml-auto">
        <input type="date" v-model="customFrom" class="text-[10px] bg-white border-2 border-black px-2 py-1 uppercase font-bold" />
        <span class="text-[10px] font-black">-</span>
        <input type="date" v-model="customTo" class="text-[10px] bg-white border-2 border-black px-2 py-1 uppercase font-bold" />
        <button @click="load" class="p-1 text-black hover:bg-[#00FF88] border-2 border-black transition-all">
          <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path d="M5 13l4 4L19 7" /></svg>
        </button>
      </div>
    </div>

    <!-- Summary Cards -->
    <div v-if="stats && stats.summary" class="grid grid-cols-2 md:grid-cols-6 gap-4">
      <div class="bg-white border-2 border-black p-4 shadow-[4px_4px_0px_0px_rgba(0,0,0,1)] flex flex-col gap-1">
        <span class="text-[10px] font-black text-gray-400 uppercase tracking-widest">Tasks Completed</span>
        <span class="text-3xl font-black text-black">{{ stats.summary.tasks_completed }}</span>
      </div>
      <div class="bg-white border-2 border-black p-4 shadow-[4px_4px_0px_0px_rgba(0,0,0,1)] flex flex-col gap-1">
        <span class="text-[10px] font-black text-gray-400 uppercase tracking-widest">Scheduled Tasks</span>
        <span class="text-3xl font-black text-black">{{ stats.summary.tasks_scheduled }}</span>
      </div>
      <div class="bg-white border-2 border-black p-4 shadow-[4px_4px_0px_0px_rgba(0,0,0,1)] flex flex-col gap-1">
        <span class="text-[10px] font-black text-gray-400 uppercase tracking-widest">Messages</span>
        <span class="text-3xl font-black text-black">{{ stats.summary.messages }}</span>
      </div>
      <div class="bg-white border-2 border-black p-4 shadow-[4px_4px_0px_0px_rgba(0,0,0,1)] flex flex-col gap-1">
        <span class="text-[10px] font-black text-gray-400 uppercase tracking-widest">Manual Appr.</span>
        <span class="text-3xl font-black text-black">{{ stats.summary.manual_approvals }}</span>
      </div>
      <div class="bg-white border-2 border-black p-4 shadow-[4px_4px_0px_0px_rgba(0,0,0,1)] flex flex-col gap-1">
        <span class="text-[10px] font-black text-gray-400 uppercase tracking-widest">Auto Appr.</span>
        <span class="text-3xl font-black text-black">{{ stats.summary.auto_approvals }}</span>
      </div>
      <div class="bg-white border-2 border-black p-4 shadow-[4px_4px_0px_0px_rgba(0,0,0,1)] flex flex-col gap-1">
        <span class="text-[10px] font-black text-gray-400 uppercase tracking-widest">Denies</span>
        <span class="text-3xl font-black text-black">{{ stats.summary.denies }}</span>
      </div>
    </div>

    <!-- Charts Section -->
    <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
      <!-- Task Chart -->
      <div class="bg-white border-2 border-black p-6 shadow-[4px_4px_0px_0px_rgba(0,0,0,1)]">
        <div class="flex items-center justify-between mb-6">
          <h3 class="text-xs font-black uppercase tracking-tighter">Task Completion Velocity</h3>
          <div class="px-2 py-0.5 bg-[#00FF88] border border-black text-[9px] font-black uppercase tracking-widest shadow-[1px_1px_0px_0px_rgba(0,0,0,1)]">Daily Trend</div>
        </div>
        <div class="h-48 w-full border-l-2 border-b-2 border-gray-100 relative">
          <div v-if="loading" class="absolute inset-0 flex items-center justify-center bg-white/50 z-10">
            <div class="text-[10px] font-black uppercase animate-pulse">Computing...</div>
          </div>
          <ChartSVG 
            v-if="stats && stats.timeseries && stats.timeseries.tasks_completed"
            :data="stats.timeseries.tasks_completed" 
            color="#00FF88" 
          />
        </div>
      </div>

      <!-- Message Chart -->
      <div class="bg-white border-2 border-black p-6 shadow-[4px_4px_0px_0px_rgba(0,0,0,1)]">
        <div class="flex items-center justify-between mb-6">
          <h3 class="text-xs font-black uppercase tracking-tighter">Communication Volume</h3>
          <div class="px-2 py-0.5 bg-black border border-black text-[9px] font-black uppercase tracking-widest shadow-[1px_1px_0px_0px_rgba(0,0,0,1)] text-white">Total Messages</div>
        </div>
        <div class="h-48 w-full border-l-2 border-b-2 border-gray-100 relative">
          <div v-if="loading" class="absolute inset-0 flex items-center justify-center bg-white/50 z-10">
            <div class="text-[10px] font-black uppercase animate-pulse">Computing...</div>
          </div>
          <ChartSVG 
            v-if="stats && stats.timeseries && stats.timeseries.messages"
            :data="stats.timeseries.messages" 
            color="#000000" 
          />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, computed, watch } from 'vue';
import { fetchWorkspaceStats } from '../api';
import ChartSVG from './ChartSVG.vue'; // New component we'll create

const props = defineProps({
  workspaceId: { type: [String, Number], required: true }
});

const stats = ref(null);
const loading = ref(true);
const activeRange = ref('7d');
const customFrom = ref('');
const customTo = ref('');

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

function setRange(range) {
  activeRange.value = range;
  if (range !== 'custom') {
    load();
  }
}

onMounted(load);
</script>
