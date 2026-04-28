<template>
  <!-- Mini SVG Line Chart -->
  <div v-if="hasData" class="h-full w-full relative overflow-hidden group/spark">
    <svg class="w-full h-full" viewBox="0 0 100 32" preserveAspectRatio="none">
      <path
        :d="pathData"
        fill="none"
        stroke="black"
        stroke-width="2"
        stroke-linecap="round"
        stroke-linejoin="round"
        class="group-hover/spark:stroke-black transition-colors"
      />
    </svg>
  </div>
</template>

<script setup>
import { computed } from 'vue';

const props = defineProps({
  data: { type: Array, default: () => [] }
});

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
    y: height - (count / max) * height
  }));
});

const pathData = computed(() => {
  if (points.value.length < 2) return '';
  return points.value.reduce((acc, p, i) => {
    return i === 0 ? `M ${p.x} ${p.y}` : `${acc} L ${p.x} ${p.y}`;
  }, '');
});
</script>

<style scoped>
</style>
