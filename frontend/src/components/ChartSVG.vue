<template>
  <div class="w-full h-full relative group/chart">
    <svg 
      class="w-full h-full overflow-visible" 
      viewBox="0 0 100 100" 
      preserveAspectRatio="none"
      @mousemove="handleMouseMove"
      @mouseleave="hoveredPoint = null"
    >
      <defs>
        <linearGradient :id="gradientId" x1="0%" y1="0%" x2="0%" y2="100%">
          <stop offset="0%" :stop-color="color" stop-opacity="0.2" />
          <stop offset="100%" :stop-color="color" stop-opacity="0" />
        </linearGradient>
      </defs>

      <!-- Grid Lines -->
      <line x1="0" y1="0" x2="100" y2="0" stroke="currentColor" class="text-gray-100 dark:text-zinc-400" stroke-width="0.5" />
      <line x1="0" y1="25" x2="100" y2="25" stroke="currentColor" class="text-gray-100 dark:text-zinc-400" stroke-width="0.5" />
      <line x1="0" y1="50" x2="100" y2="50" stroke="currentColor" class="text-gray-100 dark:text-zinc-400" stroke-width="0.5" />
      <line x1="0" y1="75" x2="100" y2="75" stroke="currentColor" class="text-gray-100 dark:text-zinc-400" stroke-width="0.5" />

      <!-- Area -->
      <path 
        v-if="areaPath"
        :d="areaPath"
        :fill="`url(#${gradientId})`"
      />

      <!-- Line -->
      <path 
        v-if="linePath"
        :d="linePath"
        :stroke="color"
        stroke-width="2"
        fill="none"
        stroke-linecap="round"
        stroke-linejoin="round"
      />

      <!-- Points -->
      <circle 
        v-for="(p, i) in points" 
        :key="i"
        :cx="p.x" 
        :cy="p.y" 
        r="0.8"
        :fill="color"
        stroke="white"
        class="dark:stroke-zinc-900"
        stroke-width="0.5"
      />

      <!-- Hover Indicator -->
      <line 
        v-if="hoveredPoint"
        :x1="hoveredPoint.x" y1="0" :x2="hoveredPoint.x" y2="100"
        stroke="currentColor" class="text-gray-300 dark:text-zinc-400" stroke-width="0.5" stroke-dasharray="2,2"
      />
    </svg>

    <!-- Label / Tooltip -->
    <div 
      v-if="hoveredPoint" 
      class="absolute z-20 bg-gray-900 dark:bg-white text-white dark:text-gray-900 px-2.5 py-1.5 rounded-lg shadow-xl text-[10px] font-black pointer-events-none transition-all duration-75 border border-transparent dark:border-zinc-200"
      :style="{ left: `${hoveredPoint.x}%`, top: `${hoveredPoint.y - 12}%`, transform: 'translate(-50%, -100%)' }"
    >
      <div class="flex flex-col gap-0.5 text-center">
        <span class="text-[8px] opacity-60 leading-none">{{ hoveredPoint.date }}</span>
        <span class="leading-none">{{ hoveredPoint.count.toLocaleString() }}</span>
      </div>
    </div>

    <!-- Empty State -->
    <div v-if="points.length === 0" class="absolute inset-0 flex items-center justify-center">
      <span class="text-[10px] font-black text-gray-300 dark:text-zinc-400 italic">No Data</span>
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue';

const props = defineProps({
  data: { type: Array, default: () => [] },
  color: { type: String, default: '#000' }
});

const hoveredPoint = ref(null);
const gradientId = `gradient-${Math.random().toString(36).substr(2, 9)}`;

const points = computed(() => {
  if (!props.data || props.data.length === 0) return [];
  
  const counts = props.data.map(d => d.count);
  const max = Math.max(...counts, 1);
  const len = props.data.length;
  
  return props.data.map((d, i) => {
    const x = len > 1 ? (i / (len - 1)) * 100 : 50;
    const y = 100 - (d.count / max) * 100;
    return { x, y, date: d.date, count: d.count };
  });
});

const linePath = computed(() => {
  if (points.value.length < 1) return '';
  if (points.value.length === 1) return `M ${points.value[0].x} ${points.value[0].y} L ${points.value[0].x+0.1} ${points.value[0].y}`;
  
  return points.value.reduce((acc, p, i) => {
    return i === 0 ? `M ${p.x} ${p.y}` : `${acc} L ${p.x} ${p.y}`;
  }, '');
});

const areaPath = computed(() => {
  if (points.value.length < 2) return '';
  const first = points.value[0];
  const last = points.value[points.value.length - 1];
  return `${linePath.value} L ${last.x} 100 L ${first.x} 100 Z`;
});

function handleMouseMove(e) {
  if (points.value.length === 0) return;
  
  const svg = e.currentTarget;
  const rect = svg.getBoundingClientRect();
  const xPercent = ((e.clientX - rect.left) / rect.width) * 100;
  
  // Find closest point
  let closest = points.value[0];
  let minDiff = Math.abs(xPercent - points.value[0].x);
  
  for (const p of points.value) {
    const diff = Math.abs(xPercent - p.x);
    if (diff < minDiff) {
      minDiff = diff;
      closest = p;
    }
  }
  
  hoveredPoint.value = closest;
}
</script>
