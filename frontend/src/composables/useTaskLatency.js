// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { computed, ref, unref, watch } from 'vue';
import { customWindowFor } from './useStatsRange';
import { withClock } from './useTimeFormat';

/**
 * The task latency panel on both analytics screens: how long the tasks closed
 * in the selected range took, as one chart of four lines in minutes, with a
 * summary card per line.
 *
 * Everything here is the logic, so the component is only markup: the fetch
 * and its aggregate, the chart's geometry, the tooltip and the cards. The
 * server does the arithmetic — merging rollups and reading p50 out of their
 * histograms — so this never recomputes an aggregate, it only draws one.
 */

/**
 * The four lines, in the analytics screen's own colours: the task and message
 * charts' pair (see statsPalette), which in light mode is a charcoal and a
 * zinc grey. Two colours for four lines, so the second pair is dashed — and
 * every line also has a legend entry and a direct label.
 */
export const LATENCY_SERIES = Object.freeze([
  { key: 'startToClose', label: 'Start → close', light: '#27272a', dark: '#aec477', dashed: false },
  { key: 'worked', label: 'Worked', light: '#71717a', dark: '#a8a3d9', dashed: false },
  { key: 'blocked', label: 'Blocked', light: '#27272a', dark: '#aec477', dashed: true },
  { key: 'needsInput', label: 'Needs input', light: '#71717a', dark: '#a8a3d9', dashed: true },
]);

/** The dash pattern of a dashed line, in pixels (strokes do not scale). */
export const LATENCY_DASH = '6 4';

/** The aggregate toggle. p50 is the default; there is no average by design. */
export const LATENCY_AGGREGATES = Object.freeze([
  { id: 'p50', label: 'p50' },
  { id: 'min', label: 'Min' },
  { id: 'max', label: 'Max' },
]);

export const DEFAULT_LATENCY_AGGREGATE = 'p50';

/** @param {boolean} isDark */
export function latencyColors(isDark) {
  return Object.fromEntries(LATENCY_SERIES.map((s) => [s.key, isDark ? s.dark : s.light]));
}

/** Seconds to minutes; null (a gap) stays null. */
export function toMinutes(seconds) {
  return seconds == null ? null : seconds / 60;
}

/**
 * Minutes for display: one decimal under ten, whole minutes above, grouped.
 * A gap reads as a dash, never as 0.
 */
export function formatMinutes(minutes) {
  if (minutes == null) return '—';
  if (minutes < 10) return String(Math.round(minutes * 10) / 10);
  return Math.round(minutes).toLocaleString('en-US');
}

/**
 * A round axis for values up to max: a step of 1, 2 or 5 × 10ⁿ giving at most
 * five intervals, and the top tick the first step at or above max.
 */
export function niceScale(max) {
  if (!(max > 0)) return { max: 1, step: 0.25 };
  const raw = max / 5;
  const exp = 10 ** Math.floor(Math.log10(raw));
  const step = [1, 2, 5, 10].map((m) => m * exp).find((v) => v >= raw);
  return { max: Math.ceil(max / step - 1e-9) * step, step };
}

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

/**
 * A bucket's label. Hours read in local time, the zone the reader lives in;
 * days and months are UTC buckets, so they are labelled in UTC — a local label
 * would put a UTC day under the previous date west of Greenwich.
 */
export function bucketLabel(periodStart, granularity) {
  const d = new Date(periodStart * 1000);
  if (granularity === 'hour') return d.toLocaleTimeString('en-US', withClock({ hour: '2-digit', minute: '2-digit' }));
  if (granularity === 'month') return `${MONTHS[d.getUTCMonth()]} ${d.getUTCFullYear()}`;
  return `${MONTHS[d.getUTCMonth()]} ${d.getUTCDate()}`;
}

/** The minimum gap between two direct labels, in % of the plot's height. */
const LABEL_GAP = 9;

/**
 * Pushes labels apart so none overlaps the one above it, then back up if the
 * last one fell off the bottom. Mutates labelY on the entries.
 */
function spreadLabels(ends) {
  const sorted = [...ends].sort((a, b) => a.y - b.y);
  for (let i = 0; i < sorted.length; i++) {
    sorted[i].labelY = i === 0 ? sorted[i].y : Math.max(sorted[i].y, sorted[i - 1].labelY + LABEL_GAP);
  }
  for (let i = sorted.length - 1; i >= 0; i--) {
    const limit = i === sorted.length - 1 ? 100 : sorted[i + 1].labelY - LABEL_GAP;
    sorted[i].labelY = Math.min(sorted[i].labelY, limit);
  }
}

/**
 * The chart's geometry, in a 0–100 box on both axes (the SVG stretches it;
 * strokes are non-scaling). Each series is split into segments at gaps, so a
 * bucket with no closed task breaks the line rather than dipping to zero; a
 * point with no neighbour on either side becomes a dot, or it would not show.
 *
 * @param {Array<object>} points the API's points
 * @param {string} granularity 'hour' | 'day' | 'month'
 */
export function buildLatencyChart(points = [], granularity = 'day') {
  const n = points.length;
  const xs = points.map((_, i) => ((i + 0.5) / n) * 100);
  const values = LATENCY_SERIES.map((s) => points.map((p) => toMinutes(p[s.key]?.seconds)));
  const { max, step } = niceScale(Math.max(0, ...values.flat().filter((v) => v != null)));
  const yOf = (v) => 100 - (v / max) * 100;

  const series = LATENCY_SERIES.map((s, si) => {
    const segments = [];
    let current = [];
    values[si].forEach((v, i) => {
      if (v == null) {
        if (current.length) segments.push(current);
        current = [];
        return;
      }
      current.push({ x: xs[i], y: yOf(v), value: v, index: i });
    });
    if (current.length) segments.push(current);
    const last = segments.at(-1)?.at(-1) ?? null;
    return {
      key: s.key,
      label: s.label,
      dash: s.dashed ? LATENCY_DASH : null,
      path: segments
        .filter((seg) => seg.length > 1)
        .map((seg) => seg.map((p, i) => `${i ? 'L' : 'M'} ${p.x.toFixed(2)} ${p.y.toFixed(2)}`).join(' '))
        .join(' '),
      dots: segments.filter((seg) => seg.length === 1).map((seg) => seg[0]),
      end: last && { x: last.x, y: last.y, value: last.value, labelY: last.y },
    };
  });
  spreadLabels(series.map((s) => s.end).filter(Boolean));

  const ticks = [];
  for (let i = 0; i * step <= max + 1e-9; i++) {
    const value = i * step;
    ticks.push({ value, y: yOf(value), label: formatMinutes(value) });
  }

  // About six labels whatever the length, always including the last bucket.
  const every = Math.max(1, Math.ceil(n / 6));
  const xLabels = points
    .map((p, i) => ({ x: xs[i], label: bucketLabel(p.periodStart, granularity), i }))
    .filter(({ i }) => (n - 1 - i) % every === 0);

  return {
    xs,
    max,
    ticks,
    series,
    xLabels,
    hasData: values.some((vs) => vs.some((v) => v != null)),
  };
}

/**
 * Which bucket a pointer at fraction fx (0–1) of the plot's width is over.
 * -1 for an empty chart.
 */
export function bucketAt(fx, count) {
  if (!count) return -1;
  return Math.min(count - 1, Math.max(0, Math.floor(fx * count)));
}

/** The hover tooltip for one bucket: its label and a row per line. */
export function latencyTooltip(point, granularity, colors) {
  if (!point) return null;
  return {
    title: bucketLabel(point.periodStart, granularity),
    closed: point.closed || 0,
    rows: LATENCY_SERIES.map((s) => ({
      key: s.key,
      label: s.label,
      color: colors[s.key],
      value: formatMinutes(toMinutes(point[s.key]?.seconds)),
    })),
  };
}

/** One card per line, from the response's summary. */
export function latencyCards(summary, colors) {
  return LATENCY_SERIES.map((s) => {
    const v = summary?.[s.key];
    return {
      key: s.key,
      label: s.label,
      color: colors[s.key],
      value: formatMinutes(toMinutes(v?.seconds)),
      count: v?.count || 0,
    };
  });
}

/**
 * Latency state and the fetch it drives, following the dashboard's range.
 *
 * @param {object} deps
 * @param {(range: string, from: number, to: number, aggregate: string) => Promise<object>} deps.fetchLatency
 * @param {import('vue').Ref<string>} deps.activeRange
 * @param {import('vue').Ref<string>} deps.customFrom
 * @param {import('vue').Ref<string>} deps.customTo
 * @param {import('vue').Ref<boolean>} deps.isDark
 * @param {import('vue').Ref<any>} [deps.scope] reloads when it changes (the workspace)
 * @param {(err: Error) => void} [deps.onError]
 */
export function useTaskLatency({ fetchLatency, activeRange, customFrom, customTo, isDark, scope, onError }) {
  const data = ref(null);
  const loading = ref(false);
  const aggregate = ref(DEFAULT_LATENCY_AGGREGATE);
  const hovered = ref(-1);
  let seq = 0;

  async function load() {
    const range = unref(activeRange);
    if (range === 'custom' && !(unref(customFrom) && unref(customTo))) return;
    const mine = ++seq;
    loading.value = true;
    try {
      const { from, to } = customWindowFor(range, unref(customFrom), unref(customTo));
      const res = await fetchLatency(range, from, to, aggregate.value);
      // A slower, older answer must not overwrite a newer range's.
      if (mine === seq) data.value = res;
    } catch (err) {
      if (mine === seq) data.value = null;
      (onError ?? ((e) => console.error('Failed to load task latency:', e)))(err);
    } finally {
      if (mine === seq) loading.value = false;
    }
  }

  function setAggregate(id) {
    if (id === aggregate.value) return;
    aggregate.value = id;
    load();
  }

  watch([activeRange, customFrom, customTo, () => unref(scope)], load);

  const colors = computed(() => latencyColors(unref(isDark)));
  const granularity = computed(() => data.value?.granularity || 'day');
  const chart = computed(() => buildLatencyChart(data.value?.points || [], granularity.value));
  const cards = computed(() => latencyCards(data.value?.summary, colors.value));
  const closed = computed(() => data.value?.summary?.closed || 0);
  const tooltip = computed(() =>
    latencyTooltip(data.value?.points?.[hovered.value], granularity.value, colors.value)
  );

  function hover(fx) {
    hovered.value = bucketAt(fx, data.value?.points?.length || 0);
  }
  function leave() {
    hovered.value = -1;
  }

  return {
    data,
    loading,
    aggregate,
    aggregates: LATENCY_AGGREGATES,
    series: LATENCY_SERIES,
    setAggregate,
    load,
    colors,
    granularity,
    chart,
    cards,
    closed,
    hovered,
    tooltip,
    hover,
    leave,
  };
}
