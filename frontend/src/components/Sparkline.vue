<template>
  <!-- Mini SVG Line Chart -->
  <div v-if="hasData" class="h-full w-full relative group/spark">
    <svg class="w-full h-full overflow-visible" viewBox="0 0 100 32" preserveAspectRatio="none">
      <defs>
        <filter :id="'glow-spark-' + uid" x="-20%" y="-20%" width="140%" height="140%">
          <feGaussianBlur stdDeviation="1.5" result="blur" />
          <feComposite in="SourceGraphic" in2="blur" operator="over" />
        </filter>
      </defs>
      <path
        :d="pathData"
        fill="none"
        stroke="currentColor"
        stroke-width="1.5"
        stroke-linecap="round"
        stroke-linejoin="round"
        class="text-zinc-800 dark:text-zinc-300 opacity-20"
        :filter="'url(#glow-spark-' + uid + ')'"
      />
      <path
        :d="pathData"
        fill="none"
        stroke="currentColor"
        stroke-width="1"
        stroke-linecap="round"
        stroke-linejoin="round"
        class="text-zinc-800 dark:text-zinc-300"
      />
    </svg>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue';

const props = defineProps({
  data: { type: Array, default: () => [] }
});

const uid = ref(Math.random().toString(36).substring(2, 9));

const hasData = computed(() => {
  return props.data && props.data.length > 1 && props.data.some(d => d.count > 0);
});

const points = computed(() => {
  if (!hasData.value) return [];
  
  const counts = props.data.map(d => d.count);
  const max = Math.max(...counts, 1);
  const width = 100;
  const height = 32;
  
  return counts.map((count, i) => ({
    x: (i / (counts.length - 1)) * width,
    // Add 6px vertical padding to prevent overflow
    y: (height - 6) - (count / max) * (height - 12)
  }));
});

const pathData = computed(() => {
  if (points.value.length < 2) return '';
  const pts = points.value;

  let d = `M ${pts[0].x} ${pts[0].y}`;
  for (let i = 0; i < pts.length - 1; i++) {
    const p0 = pts[i === 0 ? i : i - 1];
    const p1 = pts[i];
    const p2 = pts[i + 1];
    const p3 = i + 2 >= pts.length ? p2 : pts[i + 2];

    const cp1x = p1.x + (p2.x - p0.x) / 6;
    const cp1y = p1.y + (p2.y - p0.y) / 6;
    const cp2x = p2.x - (p3.x - p1.x) / 6;
    const cp2y = p2.y - (p3.y - p1.y) / 6;

    d += ` C ${cp1x} ${cp1y}, ${cp2x} ${cp2y}, ${p2.x} ${p2.y}`;
  }
  return d;
});
</script>

<style scoped>
</style>
