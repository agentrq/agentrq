// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { computed, ref } from 'vue';

/**
 * The time-range control shared by the analytics dashboards.
 *
 * There are two of these screens — one workspace, and the whole account — and
 * they differ only in which endpoint they call. Everything else about them (the
 * range buttons, what "this week" means, how many columns the charts reserve,
 * which colours the SVGs are handed) has to stay identical, or the same numbers
 * appear to disagree depending on which screen you are looking at. So it lives
 * here once and both screens read it, rather than being copied and drifting.
 *
 * The window arithmetic mirrors `statsWindow` in the Go controller. The backend
 * is authoritative — it is what actually filters the rows — but the chart needs
 * to know how many buckets to reserve before the response arrives, so the shape
 * of the window is computed on both sides.
 */

/** The range buttons, in the order they are shown. */
export const STATS_RANGE_OPTIONS = Object.freeze([
  { id: '1d', label: '1d' },
  { id: '7d', label: '7d' },
  { id: 'week', label: 'Wk' },
  { id: '30d', label: '30d' },
  { id: 'month', label: 'Mo' },
  { id: 'custom', label: 'Cust' },
]);

/** The range a dashboard opens on. */
export const DEFAULT_STATS_RANGE = '7d';

/**
 * How many day-columns the line chart should reserve.
 *
 * A fixed length is what stops a quiet week from being drawn as a full-width
 * chart of three points: the axis stays the length of the range asked for, and
 * missing days read as missing.
 *
 * 0 means "as many as the data has" — the honest answer for the calendar ranges,
 * whose length depends on today's date, and for an incomplete custom range.
 *
 * @param {string} range
 * @param {string} customFrom ISO date, `YYYY-MM-DD`
 * @param {string} customTo ISO date, `YYYY-MM-DD`
 * @returns {number}
 */
export function chartFixedLengthFor(range, customFrom = '', customTo = '') {
  if (range === '7d' || range === 'week') return 7;
  if (range === '30d' || range === 'month') return 30;
  if (range === 'custom' && customFrom && customTo) {
    const from = new Date(customFrom);
    const to = new Date(customTo);
    const days = Math.ceil((to - from) / (1000 * 60 * 60 * 24));
    // An inverted range is not a negative number of columns; it is no range.
    return days >= 0 ? days + 1 : 0;
  }
  return 0;
}

/**
 * The date the chart's last column stands for, as `YYYY-MM-DD`.
 *
 * Built from the local date parts rather than `toISOString`, which would shift
 * the label by a day for anyone east or west of UTC at the wrong hour.
 *
 * @param {string} range
 * @param {string} customTo ISO date
 * @param {Date} [now]
 * @returns {string}
 */
export function chartEndDateFor(range, customTo = '', now = new Date()) {
  if (range === 'custom' && customTo) return customTo;
  const y = now.getFullYear();
  const m = String(now.getMonth() + 1).padStart(2, '0');
  const d = String(now.getDate()).padStart(2, '0');
  return `${y}-${m}-${d}`;
}

/**
 * The `from`/`to` the request should carry, as Unix seconds.
 *
 * Only a custom range sends them; every other range is a name the backend
 * resolves itself, and sending a window alongside it would invite the two to
 * disagree. 0 means "not sent".
 *
 * @param {string} range
 * @param {string} customFrom ISO date
 * @param {string} customTo ISO date
 * @returns {{from: number, to: number}}
 */
export function customWindowFor(range, customFrom = '', customTo = '') {
  if (range !== 'custom') return { from: 0, to: 0 };
  return {
    from: customFrom ? Math.floor(new Date(customFrom).getTime() / 1000) : 0,
    to: customTo ? Math.floor(new Date(customTo).getTime() / 1000) : 0,
  };
}

/**
 * The colours handed to the chart and heatmap components.
 *
 * These are props rather than CSS because both components draw SVG and pick
 * their own scales, so the value has to exist in JavaScript. The light theme
 * deliberately uses one neutral for both series — the distinction is carried by
 * the panel headings there — while dark mode separates them with the matte
 * green and violet the rest of the analytics screen uses.
 *
 * @param {boolean} isDark
 */
export function statsPalette(isDark) {
  return {
    taskChart: isDark ? '#aec477' : '#27272a',
    messageChart: isDark ? '#a8a3d9' : '#27272a',
    taskHeatmap: isDark ? 'yellow' : 'gray',
    messageHeatmap: isDark ? 'violet' : 'gray',
  };
}

/**
 * Range state plus the fetch it drives.
 *
 * `fetchStats` is injected rather than imported so the two dashboards can point
 * at their own endpoint, and so this is testable without a server.
 *
 * A custom range does not load on every keystroke: it waits until both ends are
 * set, and `apply` is what the tick button calls to re-run an already-complete
 * range.
 *
 * @param {object} deps
 * @param {(range: string, from: number, to: number) => Promise<object>} deps.fetchStats
 * @param {(err: Error) => void} [deps.onError] defaults to console.error
 */
export function useStatsRange({ fetchStats, onError }) {
  const stats = ref(null);
  const loading = ref(true);
  const activeRange = ref(DEFAULT_STATS_RANGE);
  const customFrom = ref('');
  const customTo = ref('');

  async function load() {
    loading.value = true;
    try {
      const { from, to } = customWindowFor(activeRange.value, customFrom.value, customTo.value);
      stats.value = await fetchStats(activeRange.value, from, to);
    } catch (err) {
      // The panels render nothing rather than the previous range's numbers,
      // which would otherwise sit there looking like the answer.
      stats.value = null;
      (onError ?? ((e) => console.error('Failed to load stats:', e)))(err);
    } finally {
      loading.value = false;
    }
  }

  /** Switch range. A custom range waits for both dates before it loads. */
  function setRange(range) {
    activeRange.value = range;
    if (range !== 'custom') load();
  }

  /** Re-run the current custom range, once both ends are set. */
  function apply() {
    if (activeRange.value === 'custom' && customFrom.value && customTo.value) load();
  }

  return {
    stats,
    loading,
    activeRange,
    customFrom,
    customTo,
    load,
    setRange,
    apply,
    rangeOptions: STATS_RANGE_OPTIONS,
    chartFixedLength: computed(() =>
      chartFixedLengthFor(activeRange.value, customFrom.value, customTo.value)
    ),
    chartEndDate: computed(() => chartEndDateFor(activeRange.value, customTo.value)),
    heatmapGranularity: computed(() => stats.value?.heatmap?.granularity || 'day'),
  };
}
