<template>
  <div class="w-full h-full flex flex-col group/chart">
    <!-- Chart area -->
    <div class="flex-1 relative min-h-0">
      <svg
        class="w-full h-full overflow-hidden"
        viewBox="0 0 100 100"
        preserveAspectRatio="none"
        @mousemove="handleMouseMove"
        @mouseleave="hoveredPoint = null"
      >
        <defs>
          <filter :id="'glow-' + uid" x="-20%" y="-20%" width="140%" height="140%">
            <feGaussianBlur stdDeviation="1.2" result="blur" />
            <feComposite in="SourceGraphic" in2="blur" operator="over" />
          </filter>
        </defs>

        <!-- Grid Lines -->
        <line x1="0" y1="25" x2="100" y2="25" stroke="currentColor" class="text-gray-100 dark:text-zinc-800/30" stroke-width="0.5" stroke-dasharray="1,2" />
        <line x1="0" y1="50" x2="100" y2="50" stroke="currentColor" class="text-gray-100 dark:text-zinc-800/30" stroke-width="0.5" stroke-dasharray="1,2" />
        <line x1="0" y1="75" x2="100" y2="75" stroke="currentColor" class="text-gray-100 dark:text-zinc-800/30" stroke-width="0.5" stroke-dasharray="1,2" />

        <!-- Bars -->
        <g v-if="points.length > 0">
          <rect
            v-for="(p, i) in points"
            :key="i"
            :x="p.x - (barWidth / 2)"
            :y="p.y"
            :width="barWidth"
            :height="Math.max(90 - p.y, 2)"
            :fill="color"
            :fill-opacity="1"
            rx="2"
            class="transition-all duration-200"
            :class="{'brightness-90': hoveredPoint === p}"
            :filter="hoveredPoint === p ? 'url(#glow-' + uid + ')' : ''"
          />
        </g>

        <!-- Hover Indicator (Transparent overlay for better mouse tracking) -->
        <rect
          v-for="(p, i) in points"
          :key="'hover-'+i"
          :x="p.x - (100 / (points.length || 1) / 2)"
          y="0"
          :width="100 / (points.length || 1)"
          height="100"
          fill="transparent"
          class="cursor-pointer"
          @mouseenter="hoveredPoint = p"
        />
      </svg>

      <!-- Y-axis labels -->
      <div v-if="maxValue > 0" class="absolute pointer-events-none flex flex-col justify-between" style="top: 15%; bottom: 15%; left: 0; right: 0;">
        <span class="text-[9px] font-black text-gray-400 dark:text-zinc-500 tabular-nums leading-none">{{ formatValue(maxValue) }}</span>
        <span class="absolute text-[9px] font-black text-gray-400 dark:text-zinc-500 tabular-nums leading-none" style="top:50%;transform:translateY(-50%)">{{ formatValue(Math.round(maxValue / 2)) }}</span>
        <span class="text-[9px] font-black text-gray-400 dark:text-zinc-500 tabular-nums leading-none">0</span>
      </div>

      <!-- Tooltip -->
      <div
        v-if="hoveredPoint"
        class="absolute z-20 bg-black dark:bg-white text-white dark:text-black px-2 py-1 text-[10px] font-black uppercase tracking-widest pointer-events-none rounded shadow-lg whitespace-nowrap"
        :style="{ 
          left: `${hoveredPoint.x}%`, 
          top: `${hoveredPoint.y}%`, 
          transform: `translate(${hoveredPoint.x > 80 ? '-100%' : hoveredPoint.x < 20 ? '0%' : '-50%'}, ${hoveredPoint.y < 20 ? '10%' : '-110%'})` 
        }"
      >
        {{ hoveredPoint.date }}: {{ hoveredPoint.count }}
      </div>

      <!-- Empty State -->
      <div v-if="points.length === 0" class="absolute inset-0 flex items-center justify-center">
        <span class="text-[10px] font-black text-gray-300 dark:text-zinc-500 uppercase tracking-widest italic">No data points</span>
      </div>
    </div>

    <!-- X-axis labels -->
    <div v-if="points.length > 1" class="relative h-5 mt-0.5 flex-shrink-0">
      <span
        v-for="lbl in xAxisLabels"
        :key="lbl.x"
        class="absolute bottom-0 text-[9px] font-black text-gray-400 dark:text-zinc-500 tabular-nums leading-none uppercase"
        :style="{ left: `${lbl.x}%`, transform: 'translateX(-50%)' }"
      >{{ lbl.label }}</span>
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue';

const props = defineProps({
  data: { type: Array, default: () => [] },
  color: { type: String, default: 'currentColor' },
  fixedLength: { type: Number, default: 0 }
});

const uid = ref(Math.random().toString(36).substring(2, 9));
const hoveredPoint = ref(null);

const maxValue = computed(() => {
  if (!props.data || props.data.length === 0) return 0;
  return Math.max(...props.data.map(d => d.count), 1);
});

const points = computed(() => {
  if (!props.data || props.data.length === 0) return [];

  const max = maxValue.value;
  const len = props.data.length;

  const displayLen = Math.max(len, props.fixedLength);
  const step = 100 / (displayLen || 1);
  
  // If we have fixed length but fewer data points, they should be at the end (most recent)
  const offset = props.fixedLength > len ? props.fixedLength - len : 0;

  return props.data.map((d, i) => {
    // Center the bar in its slot, offset by missing slots
    const x = ((i + offset) * step) + (step / 2);
    // Add 15% vertical padding for better clearance
    const y = 85 - (d.count / max) * 70;
    return { x, y, date: d.date, count: d.count };
  });
});

const barWidth = computed(() => {
  const len = props.data.length;
  const displayLen = Math.max(len, props.fixedLength);
  if (displayLen === 0) return 0;
  const step = 100 / displayLen;
  return step * 0.7; // 70% width, 30% gap
});

// Unused in bar chart mode
const linePath = computed(() => '');
const areaPath = computed(() => '');

const xAxisLabels = computed(() => {
  if (points.value.length < 1) return [];
  const all = points.value;

  // If we only have one point (centered at 50%), show it
  if (all.length === 1) {
    return [{ x: all[0].x, label: formatDate(all[0].date) }];
  }

  const maxLabels = 6;
  if (all.length <= maxLabels) {
    const res = [];
    all.forEach(p => {
      if (!res.find(r => r.label === formatDate(p.date))) {
        res.push({ x: p.x, label: formatDate(p.date) });
      }
    });
    return res;
  }

  const result = [];
  const step = (all.length - 1) / (maxLabels - 1);
  for (let i = 0; i < maxLabels; i++) {
    const idx = Math.min(Math.round(i * step), all.length - 1);
    const lbl = { x: all[idx].x, label: formatDate(all[idx].date) };
    if (!result.find(r => r.label === lbl.label)) {
      result.push(lbl);
    }
  }
  return result;
});

function formatDate(dateStr) {
  if (dateStr.includes(':')) return dateStr.split(' ')[1].slice(0, 5);
  return dateStr.slice(5);
}

function formatValue(n) {
  if (n >= 1000000) return (n / 1000000).toFixed(1).replace(/\.0$/, '') + 'M';
  if (n >= 1000) return (n / 1000).toFixed(1).replace(/\.0$/, '') + 'K';
  return String(n);
}

function handleMouseMove(e) {
  if (points.value.length === 0) return;

  const svg = e.currentTarget;
  const rect = svg.getBoundingClientRect();
  const xPercent = ((e.clientX - rect.left) / rect.width) * 100;

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
