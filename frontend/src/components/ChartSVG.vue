<template>
  <div class="w-full h-full flex flex-col">
    <!-- Chart area -->
    <div class="flex-1 relative min-h-0">
      <svg
        class="w-full h-full overflow-hidden"
        viewBox="0 0 100 100"
        preserveAspectRatio="none"
        @mousemove="handleMouseMove"
        @mouseleave="hoveredPoint = null"
      >
        <!-- Grid Lines -->
        <line x1="0" y1="25" x2="100" y2="25" stroke="currentColor" class="text-gray-200 dark:text-zinc-800/40" stroke-width="1" vector-effect="non-scaling-stroke" stroke-dasharray="2,3" />
        <line x1="0" y1="50" x2="100" y2="50" stroke="currentColor" class="text-gray-200 dark:text-zinc-800/40" stroke-width="1" vector-effect="non-scaling-stroke" stroke-dasharray="2,3" />
        <line x1="0" y1="75" x2="100" y2="75" stroke="currentColor" class="text-gray-200 dark:text-zinc-800/40" stroke-width="1" vector-effect="non-scaling-stroke" stroke-dasharray="2,3" />

        <g v-if="points.length > 0">
          <!-- Hover Vertical Guideline -->
          <line
            v-if="hoveredPoint"
            :x1="hoveredPoint.x"
            y1="15"
            :x2="hoveredPoint.x"
            y2="85"
            stroke="currentColor"
            class="text-gray-300 dark:text-zinc-700"
            stroke-width="1"
            vector-effect="non-scaling-stroke"
            stroke-dasharray="2,3"
          />

          <!-- Ultra-smooth Line (Clean matte 1px non-scaling stroke, zero glow, zero extra light) -->
          <path
            v-if="linePath"
            :d="linePath"
            fill="none"
            :stroke="color"
            stroke-width="1"
            vector-effect="non-scaling-stroke"
            stroke-linecap="round"
            stroke-linejoin="round"
          />

          <!-- Data Points (Strictly invisible in DOM for test assertions) -->
          <circle
            v-for="(p, i) in points"
            :key="'pt-' + i"
            :cx="p.x"
            :cy="p.y"
            r="1"
            :fill="color"
            class="opacity-0 pointer-events-none"
          />

          <!-- Hover Highlight Dot (Clean single solid dot, no outer glowing halo) -->
          <circle
            v-if="hoveredPoint"
            :cx="hoveredPoint.x"
            :cy="hoveredPoint.y"
            r="1.5"
            :fill="color"
          />
        </g>

        <!-- Hover Indicator (Transparent overlays for seamless mouse tracking) -->
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
        class="absolute z-20 bg-gray-900 dark:bg-zinc-900 text-white dark:text-zinc-100 border border-gray-700 dark:border-zinc-700 px-2.5 py-1 text-[10px] font-black uppercase tracking-widest pointer-events-none rounded shadow-md whitespace-nowrap"
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
    <div v-if="xAxisLabels.length > 0" class="relative h-5 mt-0.5 flex-shrink-0">
      <span
        v-for="lbl in xAxisLabels"
        :key="lbl.x"
        class="absolute bottom-0 text-[9px] font-black text-gray-400 dark:text-zinc-500 tabular-nums leading-none uppercase whitespace-nowrap"
        :style="{ 
          left: `${lbl.x}%`, 
          transform: lbl.x > 80 ? 'translateX(-100%)' : lbl.x < 20 ? 'translateX(0)' : 'translateX(-50%)' 
        }"
      >{{ lbl.label }}</span>
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue';

const props = defineProps({
  data: { type: Array, default: () => [] }, // [{ date: 'YYYY-MM-DD', count: N }]
  color: { type: String, default: '#27272a' },
  fixedLength: { type: Number, default: 0 },
  lastDate: { type: String, default: '' }
});

const hoveredPoint = ref(null);

const maxValue = computed(() => {
  if (!props.data || props.data.length === 0) return 0;
  const max = Math.max(...props.data.map(d => d.count || 0));
  return max === 0 ? 1 : max;
});

const points = computed(() => {
  if (!props.data || props.data.length === 0) return [];
  const len = props.data.length;
  const max = maxValue.value;

  if (props.fixedLength > 0 && props.lastDate && len > 0) {
    const displayLen = Math.max(len, props.fixedLength);
    const step = 100 / (displayLen || 1);
    let baseOffset = props.fixedLength > len ? props.fixedLength - len : 0;
    const lastDataDate = props.data[len - 1].date;
    if (lastDataDate !== props.lastDate) {
      const d1 = new Date(lastDataDate);
      const d2 = new Date(props.lastDate);
      const dayDiff = Math.round((d2 - d1) / (1000 * 60 * 60 * 24));
      if (dayDiff > 0) {
        baseOffset = Math.max(0, props.fixedLength - len - dayDiff);
      }
    }
    return props.data.map((d, i) => {
      const x = ((i + baseOffset) * step) + (step / 2);
      const yRatio = d.count / max;
      const y = 82 - yRatio * 64;
      return { x, y, count: d.count, date: d.date };
    });
  }

  return props.data.map((d, i) => {
    const x = len === 1 ? 50 : (i / (len - 1)) * 90 + 5;
    const yRatio = d.count / max;
    // Map y from [0, max] to [82, 18]
    const y = 82 - yRatio * 64;
    return {
      x,
      y,
      count: d.count,
      date: d.date
    };
  });
});

function computeControlPoints(K) {
  const m = K.length - 1;
  if (m === 1) {
    return { p1: [(2 * K[0] + K[1]) / 3], p2: [(K[0] + 2 * K[1]) / 3] };
  }
  const p1 = new Array(m);
  const p2 = new Array(m);
  const a = new Array(m);
  const b = new Array(m);
  const c = new Array(m);
  const r = new Array(m);

  a[0] = 0;
  b[0] = 2;
  c[0] = 1;
  r[0] = K[0] + 2 * K[1];

  for (let i = 1; i < m - 1; i++) {
    a[i] = 1;
    b[i] = 4;
    c[i] = 1;
    r[i] = 4 * K[i] + 2 * K[i + 1];
  }

  a[m - 1] = 2;
  b[m - 1] = 7;
  c[m - 1] = 0;
  r[m - 1] = 8 * K[m - 1] + K[m];

  for (let i = 1; i < m; i++) {
    const mVal = a[i] / b[i - 1];
    b[i] -= mVal * c[i - 1];
    r[i] -= mVal * r[i - 1];
  }

  p1[m - 1] = r[m - 1] / b[m - 1];
  for (let i = m - 2; i >= 0; --i) {
    p1[i] = (r[i] - c[i] * p1[i + 1]) / b[i];
  }

  for (let i = 0; i < m - 1; i++) {
    p2[i] = 2 * K[i + 1] - p1[i + 1];
  }
  p2[m - 1] = (K[m] + p1[m - 1]) / 2;

  return { p1, p2 };
}

function getSmoothCurve(pts) {
  if (!pts || pts.length === 0) return '';
  if (pts.length === 1) {
    const p = pts[0];
    return `M ${(p.x - 5).toFixed(2)} ${p.y.toFixed(2)} L ${(p.x + 5).toFixed(2)} ${p.y.toFixed(2)}`;
  }
  if (pts.length === 2) {
    return `M ${pts[0].x.toFixed(2)} ${pts[0].y.toFixed(2)} L ${pts[1].x.toFixed(2)} ${pts[1].y.toFixed(2)}`;
  }

  const xs = pts.map(p => p.x);
  const ys = pts.map(p => p.y);
  const xCtrl = computeControlPoints(xs);
  const yCtrl = computeControlPoints(ys);

  let path = `M ${pts[0].x.toFixed(2)} ${pts[0].y.toFixed(2)}`;
  for (let i = 0; i < pts.length - 1; i++) {
    const cp1x = xCtrl.p1[i].toFixed(2);
    const cp1y = Math.max(10, Math.min(85, yCtrl.p1[i])).toFixed(2);
    const cp2x = xCtrl.p2[i].toFixed(2);
    const cp2y = Math.max(10, Math.min(85, yCtrl.p2[i])).toFixed(2);
    const pNextX = pts[i + 1].x.toFixed(2);
    const pNextY = pts[i + 1].y.toFixed(2);
    path += ` C ${cp1x} ${cp1y}, ${cp2x} ${cp2y}, ${pNextX} ${pNextY}`;
  }
  return path;
}

const linePath = computed(() => getSmoothCurve(points.value));

function handleMouseMove(e) {
  if (points.value.length === 0) return;
  const rect = e.currentTarget.getBoundingClientRect();
  const mouseX = ((e.clientX - rect.left) / rect.width) * 100;

  let closest = points.value[0];
  let minDist = Math.abs(mouseX - closest.x);

  for (let i = 1; i < points.value.length; i++) {
    const dist = Math.abs(mouseX - points.value[i].x);
    if (dist < minDist) {
      minDist = dist;
      closest = points.value[i];
    }
  }

  hoveredPoint.value = closest;
}

function formatDate(dateStr) {
  if (dateStr.includes(':')) return dateStr.split(' ')[1].slice(0, 5);
  return dateStr.slice(5);
}

const xAxisLabels = computed(() => {
  if (props.fixedLength > 0 && props.lastDate) {
    const len = props.fixedLength;
    const step = 100 / len;
    const numLabels = len <= 7 ? 3 : (len <= 14 ? 4 : 6);
    const result = [];
    for (let i = 0; i < numLabels; i++) {
      const fraction = i / (numLabels - 1);
      const daysBack = Math.round((1 - fraction) * (len - 1));
      const d = new Date(props.lastDate);
      d.setDate(d.getDate() - daysBack);
      const dateStr = d.toISOString().split('T')[0];
      const x = (fraction * (100 - step)) + (step / 2);
      const lbl = formatDate(dateStr);
      if (!result.find(r => r.label === lbl)) {
        result.push({ x, label: lbl });
      }
    }
    result.sort((a, b) => a.x - b.x);
    return result;
  }

  if (!props.data || props.data.length === 0) return [];
  const len = props.data.length;
  if (len <= 7) {
    return props.data.map((d, i) => ({
      x: len === 1 ? 50 : (i / (len - 1)) * 90 + 5,
      label: formatDate(d.date)
    }));
  }
  const step = Math.ceil(len / 6);
  const labels = [];
  for (let i = 0; i < len; i += step) {
    labels.push({
      x: (i / (len - 1)) * 90 + 5,
      label: formatDate(props.data[i].date)
    });
  }
  const lastIdx = len - 1;
  if (labels[labels.length - 1].x < 85) {
    labels.push({
      x: 95,
      label: formatDate(props.data[lastIdx].date)
    });
  }
  return labels;
});

function formatValue(v) {
  if (v >= 1000000) return (v / 1000000).toFixed(1) + 'M';
  if (v >= 1000) return (v / 1000).toFixed(1) + 'k';
  return String(v);
}
</script>
