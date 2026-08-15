<template>
  <div class="w-full h-full flex flex-col gap-1.5">
    <div v-if="columns.length === 0" class="flex-1 flex items-center justify-center">
      <span class="text-[10px] font-black text-gray-300 dark:text-zinc-500 uppercase tracking-widest italic">No data points</span>
    </div>

    <div v-else class="flex-1 flex flex-col gap-1.5 min-h-0">
      <!-- Column labels (months, or days) -->
      <div class="flex gap-[3px] pl-6">
        <div
          v-for="(col, i) in columns"
          :key="'lbl-' + i"
          class="relative flex-1 min-w-0 text-[9px] font-black text-gray-400 dark:text-zinc-500 uppercase tracking-widest leading-none"
        >
          <span
            v-if="col.label"
            class="absolute top-0 whitespace-nowrap"
            :class="i >= columns.length - 2 ? 'right-0' : 'left-0'"
          >{{ col.label }}</span>
        </div>
      </div>

      <!-- Grid body -->
      <div class="flex flex-1 gap-[3px] min-h-0">
        <!-- Row labels -->
        <div class="flex flex-col justify-between gap-[3px] w-6 flex-shrink-0">
          <span
            v-for="(lbl, i) in rowLabels"
            :key="'row-' + i"
            class="flex-1 text-[9px] font-black text-gray-400 dark:text-zinc-500 uppercase tracking-widest leading-none flex items-center"
          >{{ lbl }}</span>
        </div>

        <!-- Columns of cells -->
        <div class="flex-1 flex gap-[3px]">
          <div v-for="(col, ci) in columns" :key="'col-' + ci" class="flex-1 min-w-0 flex flex-col gap-[3px]">
            <div
              v-for="(cell, ri) in col.cells"
              :key="'cell-' + ri"
              class="rounded-sm transition-colors"
              :class="[levelClass(cell), cell.dimmed ? 'opacity-30' : 'cursor-pointer flex-1']"
              :style="{ flex: cell.dimmed ? '1 1 0%' : undefined }"
              @mouseenter="!cell.dimmed && onCellEnter($event, cell)"
              @mouseleave="hovered = null"
            />
          </div>
        </div>
      </div>
    </div>

    <Teleport to="body">
      <div
        v-if="hovered"
        class="fixed z-50 bg-black dark:bg-white text-white dark:text-black px-2 py-1 text-[10px] font-black uppercase tracking-widest pointer-events-none rounded shadow-lg whitespace-nowrap"
        :style="{ left: hovered.x + 'px', top: hovered.y + 'px', transform: `translate(-50%, ${hovered.showBelow ? '0' : '-100%'})` }"
      >{{ hovered.label }}: {{ hovered.count }}</div>
    </Teleport>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue';

const props = defineProps({
  data: { type: Array, default: () => [] }, // [{ bucket, count }]
  granularity: { type: String, default: 'day' }, // 'hour' | 'day'
  rangeStart: { type: Number, default: 0 }, // unix seconds
  rangeEnd: { type: Number, default: 0 }, // unix seconds
  metricLabel: { type: String, default: 'Count' },
  weekdayColumnLabels: { type: Boolean, default: false } // show "Mon"/"Tue" column headers instead of dates
});

const hovered = ref(null);

function onCellEnter(e, cell) {
  const rect = e.currentTarget.getBoundingClientRect();
  const showBelow = rect.top < 80;
  hovered.value = {
    label: cell.label,
    count: cell.count,
    x: rect.left + rect.width / 2,
    y: showBelow ? rect.bottom + 6 : rect.top - 6,
    showBelow
  };
}

const LEVEL_CLASSES = [
  'bg-gray-100 dark:bg-zinc-800/60',
  'bg-gray-300 dark:bg-zinc-600',
  'bg-gray-500 dark:bg-zinc-500',
  'bg-gray-700 dark:bg-zinc-300',
  'bg-black dark:bg-zinc-50'
];

const MONTH_NAMES = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
const DAY_NAMES = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

function pad2(n) {
  return String(n).padStart(2, '0');
}

function toISODate(d) {
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`;
}

function startOfDay(d) {
  const r = new Date(d);
  r.setHours(0, 0, 0, 0);
  return r;
}

const maxCount = computed(() => {
  if (!props.data || props.data.length === 0) return 0;
  return Math.max(...props.data.map(d => d.count), 0);
});

function levelForCount(count) {
  const max = maxCount.value;
  if (!count || max <= 0) return 0;
  const ratio = count / max;
  if (ratio > 0.75) return 4;
  if (ratio > 0.5) return 3;
  if (ratio > 0.25) return 2;
  return 1;
}

function levelClass(cell) {
  return LEVEL_CLASSES[levelForCount(cell.count)];
}

const rowLabels = computed(() => {
  if (props.granularity === 'hour') {
    // 24 rows (hours), label every 6 hours
    return Array.from({ length: 24 }, (_, h) => {
      if (h === 0) return '12a';
      if (h === 6) return '6a';
      if (h === 12) return '12p';
      if (h === 18) return '6p';
      return '';
    });
  }
  // 7 rows (Sun..Sat), label Mon/Wed/Fri like a GitHub-style contribution graph
  return DAY_NAMES.map((name, i) => ([1, 3, 5].includes(i) ? name : ''));
});

const dataMap = computed(() => {
  const m = new Map();
  for (const d of props.data || []) {
    m.set(d.bucket, d.count);
  }
  return m;
});

// Hour x Day grid: columns = days in the window, rows = hours 0-23
const hourColumns = computed(() => {
  if (!props.rangeStart || !props.rangeEnd) return [];
  const start = startOfDay(new Date(props.rangeStart * 1000));
  const end = new Date(props.rangeEnd * 1000);

  const days = [];
  let cur = new Date(start);
  while (cur <= end) {
    days.push(toISODate(cur));
    cur.setDate(cur.getDate() + 1);
  }

  return days.map(dateStr => {
    const d = new Date(`${dateStr}T00:00:00`);
    const cells = [];
    for (let h = 0; h < 24; h++) {
      const bucket = `${dateStr} ${pad2(h)}:00`;
      const bucketTs = Math.floor(new Date(`${dateStr}T${pad2(h)}:00:00`).getTime() / 1000);
      const dimmed = bucketTs < props.rangeStart || bucketTs > props.rangeEnd;
      cells.push({
        count: dataMap.value.get(bucket) || 0,
        label: `${MONTH_NAMES[d.getMonth()]} ${d.getDate()} ${pad2(h)}:00`,
        dimmed
      });
    }
    return {
      label: props.weekdayColumnLabels ? DAY_NAMES[d.getDay()] : `${MONTH_NAMES[d.getMonth()]} ${d.getDate()}`,
      cells
    };
  });
});

// Day x Week grid: columns = weeks, rows = day-of-week (Sun..Sat)
const dayColumns = computed(() => {
  if (!props.rangeStart || !props.rangeEnd) return [];
  const rangeStartDate = startOfDay(new Date(props.rangeStart * 1000));
  const rangeEndDate = startOfDay(new Date(props.rangeEnd * 1000));

  const gridStart = new Date(rangeStartDate);
  gridStart.setDate(gridStart.getDate() - gridStart.getDay());

  const weeks = [];
  let cur = new Date(gridStart);
  while (cur <= rangeEndDate) {
    const week = { label: '', cells: [] };
    for (let d = 0; d < 7; d++) {
      const dateStr = toISODate(cur);
      const dimmed = cur < rangeStartDate || cur > rangeEndDate;
      // Label the first column of each month (even if the month name repeats
      // for ranges spanning more than a year), skipping a month that only has
      // its very first day inside the window — a boundary sliver with no
      // real data, e.g. a trailing "Jan" when the range ends exactly on Jan 1st.
      if (cur.getDate() === 1 && cur >= rangeStartDate && cur < rangeEndDate) {
        week.label = MONTH_NAMES[cur.getMonth()];
      }
      week.cells.push({
        count: dataMap.value.get(dateStr) || 0,
        label: `${MONTH_NAMES[cur.getMonth()]} ${cur.getDate()}`,
        dimmed
      });
      cur.setDate(cur.getDate() + 1);
    }
    weeks.push(week);
  }
  return weeks;
});

const columns = computed(() => (props.granularity === 'hour' ? hourColumns.value : dayColumns.value));
</script>
