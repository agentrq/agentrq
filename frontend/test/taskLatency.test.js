// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, h, nextTick, ref, effectScope } from 'vue';
import { createPinia, setActivePinia } from 'pinia';
import {
  LATENCY_SERIES,
  latencyColors,
  toMinutes,
  formatMinutes,
  niceScale,
  bucketLabel,
  buildLatencyChart,
  bucketAt,
  latencyTooltip,
  latencyCards,
  useTaskLatency,
} from '../src/composables/useTaskLatency';
import TaskLatencyPanel from '../src/components/TaskLatencyPanel.vue';
import AccountStats from '../src/components/AccountStats.vue';
import { useThemeStore } from '../src/stores/themeStore';
import * as api from '../src/api';
import { timeFormat } from '../src/composables/useTimeFormat';

const DAY = 86400;
const T0 = Date.UTC(2026, 8, 20) / 1000; // 2026-09-20 00:00 UTC

const val = (minutes, count = 1) => ({ seconds: minutes == null ? null : minutes * 60, count });

function point(i, { s2c = null, worked = null, blocked = null, needsInput = null, closed = 0 } = {}) {
  return {
    periodStart: T0 + i * DAY,
    closed,
    startToClose: val(s2c),
    worked: val(worked),
    blocked: val(blocked),
    needsInput: val(needsInput),
  };
}

const settle = () => new Promise((r) => setTimeout(r, 20));

describe('formatting', () => {
  it('converts and formats minutes, keeping a gap a gap', () => {
    expect(toMinutes(null)).toBe(null);
    expect(toMinutes(undefined)).toBe(null);
    expect(toMinutes(90)).toBe(1.5);
    expect(formatMinutes(null)).toBe('—');
    expect(formatMinutes(0)).toBe('0');
    expect(formatMinutes(2.46)).toBe('2.5');
    expect(formatMinutes(12.6)).toBe('13');
    expect(formatMinutes(12345)).toBe('12,345');
  });

  it('picks a round axis of at most five steps', () => {
    expect(niceScale(0)).toEqual({ max: 1, step: 0.25 });
    expect(niceScale(-3).max).toBe(1);
    expect(niceScale(NaN).max).toBe(1);
    expect(niceScale(203)).toEqual({ max: 250, step: 50 });
    expect(niceScale(50)).toEqual({ max: 50, step: 10 });
    expect(niceScale(7)).toEqual({ max: 8, step: 2 });
    expect(niceScale(430)).toEqual({ max: 500, step: 100 });
    expect(niceScale(1)).toEqual({ max: 1, step: 0.2 });
  });

  it('labels hours locally, on the account\'s clock, and days and months in UTC', () => {
    const at = T0 + 14 * 3600;
    const hour = new Date(at * 1000).getHours();
    timeFormat.value = '24h';
    expect(bucketLabel(at, 'hour')).toBe(`${String(hour).padStart(2, '0')}:00`);
    timeFormat.value = '12h';
    expect(bucketLabel(at, 'hour')).toBe(`${String(hour % 12 || 12).padStart(2, '0')}:00 ${hour < 12 ? 'AM' : 'PM'}`);
    timeFormat.value = 'browser';
    expect(bucketLabel(T0, 'day')).toBe('Sep 20');
    expect(bucketLabel(T0, 'month')).toBe('Sep 2026');
  });

  it('gives each line its colour in each theme', () => {
    // The analytics screen's own pair; the second two lines are dashed.
    expect(latencyColors(false)).toEqual({ startToClose: '#27272a', worked: '#71717a', blocked: '#27272a', needsInput: '#71717a' });
    expect(latencyColors(true)).toEqual({ startToClose: '#aec477', worked: '#a8a3d9', blocked: '#aec477', needsInput: '#a8a3d9' });
    const dashes = buildLatencyChart([point(0)], 'day').series.map((s) => s.dash);
    expect(dashes).toEqual([null, null, '6 4', '6 4']);
    expect(LATENCY_SERIES.map((s) => s.key)).toEqual(['startToClose', 'worked', 'blocked', 'needsInput']);
  });
});

describe('buildLatencyChart', () => {
  it('breaks a line at a gap and draws a lone point as a dot', () => {
    const points = [
      point(0, { worked: 10 }),
      point(1, { worked: 20 }),
      point(2),
      point(3, { worked: 40 }),
      point(4),
    ];
    const chart = buildLatencyChart(points, 'day');
    const worked = chart.series.find((s) => s.key === 'worked');
    expect(chart.max).toBe(40);
    expect(worked.path.match(/M/g)).toHaveLength(1); // one two-point segment
    expect(worked.dots).toHaveLength(1);
    expect(worked.dots[0].index).toBe(3);
    expect(worked.end.value).toBe(40);
    expect(chart.series.find((s) => s.key === 'blocked').end).toBe(null);
    expect(chart.hasData).toBe(true);
    // Buckets are centred in their slot.
    expect(chart.xs[0]).toBe(10);
    expect(chart.ticks.map((t) => t.label)).toEqual(['0', '10', '20', '30', '40']);
  });

  it('spreads direct labels that would collide, inside the plot', () => {
    const points = [point(0, { s2c: 100, worked: 99, blocked: 1, needsInput: 0 })];
    const chart = buildLatencyChart(points, 'day');
    const ys = chart.series.map((s) => s.end.labelY).sort((a, b) => a - b);
    for (let i = 1; i < ys.length; i++) expect(ys[i] - ys[i - 1]).toBeGreaterThanOrEqual(9 - 1e-9);
    expect(Math.max(...ys)).toBeLessThanOrEqual(100);
  });

  it('is empty without points and keeps the x labels sparse', () => {
    const empty = buildLatencyChart();
    expect(empty.hasData).toBe(false);
    expect(empty.max).toBe(1);
    const many = buildLatencyChart(Array.from({ length: 24 }, (_, i) => point(i)), 'day');
    expect(many.xLabels.length).toBeLessThanOrEqual(7);
    expect(many.xLabels.at(-1).i).toBe(23);
  });

  it('finds the bucket under the pointer', () => {
    expect(bucketAt(0.5, 0)).toBe(-1);
    expect(bucketAt(-1, 4)).toBe(0);
    expect(bucketAt(0.3, 4)).toBe(1);
    expect(bucketAt(1, 4)).toBe(3);
  });
});

describe('tooltip and cards', () => {
  const colors = latencyColors(false);

  it('lists every line for the hovered bucket', () => {
    expect(latencyTooltip(undefined, 'day', colors)).toBe(null);
    const tip = latencyTooltip(point(0, { worked: 5, closed: 2 }), 'day', colors);
    expect(tip.title).toBe('Sep 20');
    expect(tip.closed).toBe(2);
    expect(tip.rows.map((r) => r.value)).toEqual(['—', '5', '—', '—']);
    expect(latencyTooltip({ periodStart: T0 }, 'day', colors).closed).toBe(0);
  });

  it('makes a card per line from the summary', () => {
    const cards = latencyCards({ worked: val(30, 4) }, colors);
    expect(cards.map((c) => [c.key, c.value, c.count])).toEqual([
      ['startToClose', '—', 0],
      ['worked', '30', 4],
      ['blocked', '—', 0],
      ['needsInput', '—', 0],
    ]);
    expect(latencyCards(undefined, colors)[0].value).toBe('—');
  });
});

describe('useTaskLatency', () => {
  function setup(fetchLatency, init = {}) {
    const deps = {
      activeRange: ref(init.range ?? '7d'),
      customFrom: ref(init.from ?? ''),
      customTo: ref(init.to ?? ''),
      scope: ref('ws-1'),
      isDark: ref(false),
    };
    const scope = effectScope();
    const state = scope.run(() => useTaskLatency({ fetchLatency, ...deps, onError: init.onError }));
    return { state, deps, stop: () => scope.stop() };
  }

  it('loads for the range and aggregate and reloads on a change', async () => {
    const fetchLatency = vi.fn(async (range, from, to, aggregate) => ({
      granularity: 'hour',
      points: [point(0, { worked: aggregate === 'max' ? 9 : 3, closed: 1 })],
      summary: { closed: 1, worked: val(3) },
    }));
    const { state, deps, stop } = setup(fetchLatency);
    await state.load();
    expect(fetchLatency).toHaveBeenLastCalledWith('7d', 0, 0, 'p50');
    expect(state.granularity.value).toBe('hour');
    expect(state.closed.value).toBe(1);
    expect(state.cards.value[1].value).toBe('3');

    state.setAggregate('p50'); // unchanged: no reload
    expect(fetchLatency).toHaveBeenCalledTimes(1);
    state.setAggregate('max');
    await settle();
    expect(fetchLatency).toHaveBeenLastCalledWith('7d', 0, 0, 'max');
    expect(state.chart.value.series[1].end.value).toBe(9);

    deps.activeRange.value = '30d';
    await settle();
    expect(fetchLatency).toHaveBeenLastCalledWith('30d', 0, 0, 'max');

    state.hover(0.5);
    expect(state.hovered.value).toBe(0);
    expect(state.tooltip.value.closed).toBe(1);
    state.leave();
    expect(state.tooltip.value).toBe(null);
    stop();
  });

  it('waits for both ends of a custom range', async () => {
    const fetchLatency = vi.fn(async () => ({ points: [], summary: {} }));
    const { state, deps, stop } = setup(fetchLatency, { range: 'custom', from: '2026-09-01' });
    await state.load();
    expect(fetchLatency).not.toHaveBeenCalled();
    deps.customTo.value = '2026-09-10';
    await settle();
    expect(fetchLatency).toHaveBeenCalledTimes(1);
    expect(fetchLatency.mock.calls[0][0]).toBe('custom');
    expect(fetchLatency.mock.calls[0][1]).toBeGreaterThan(0);
    // No granularity in the answer reads as days; hovering nothing is nothing.
    expect(state.granularity.value).toBe('day');
    state.hover(0.5);
    expect(state.hovered.value).toBe(-1);
    stop();
  });

  it('drops a stale answer and clears on failure', async () => {
    const resolvers = [];
    const fetchLatency = vi.fn(() => new Promise((resolve, reject) => resolvers.push({ resolve, reject })));
    const onError = vi.fn();
    const { state, stop } = setup(fetchLatency, { onError });
    const first = state.load();
    const second = state.load();
    resolvers[1].resolve({ points: [point(0, { worked: 2 })], summary: {} });
    await second;
    resolvers[0].resolve({ points: [point(0, { worked: 99 })], summary: {} });
    await first;
    expect(state.chart.value.series[1].end.value).toBe(2);
    expect(state.loading.value).toBe(false);

    const third = state.load();
    const fourth = state.load();
    resolvers[2].reject(new Error('superseded request failed'));
    await third;
    expect(onError).toHaveBeenCalledTimes(1);
    expect(state.data.value).not.toBe(null); // the stale failure did not clear it
    resolvers[3].reject(new Error('latest request failed'));
    await fourth;
    expect(state.data.value).toBe(null);
    stop();
  });

  it('logs to the console without an error handler', async () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {});
    const { state, stop } = setup(async () => { throw new Error('network unreachable'); });
    await state.load();
    expect(spy).toHaveBeenCalled();
    spy.mockRestore();
    stop();
  });
});

describe('TaskLatencyPanel', () => {
  let container;
  let app;
  let pinia;

  beforeEach(() => {
    pinia = createPinia();
    setActivePinia(pinia);
    container = document.createElement('div');
    document.body.appendChild(container);
  });

  afterEach(() => {
    app?.unmount();
    app = null;
    container.remove();
    vi.restoreAllMocks();
  });

  function mount(props) {
    app = createApp({ render: () => h(TaskLatencyPanel, props) });
    app.use(pinia);
    app.mount(container);
  }

  it('shows the cards, the lines and a tooltip, and switches aggregate', async () => {
    useThemeStore(pinia).setTheme('dark');
    const fetchLatency = vi.fn(async () => ({
      granularity: 'day',
      points: [point(0, { s2c: 60, worked: 30, blocked: 0, closed: 2 }), point(1, { s2c: 90, worked: 45, closed: 1 })],
      summary: { closed: 3, startToClose: val(75, 3), worked: val(40, 3), blocked: val(0, 3), needsInput: val(null) },
    }));
    mount({ fetchLatency, activeRange: '7d', scope: 'ws-1' });
    await settle();

    expect(fetchLatency).toHaveBeenCalledWith('7d', 0, 0, 'p50');
    expect(container.textContent).toContain('Task Latency');
    expect(container.textContent).toContain('3 closed');
    expect(container.querySelector('[data-card="startToClose"]').textContent).toContain('75');
    expect(container.querySelector('[data-card="needsInput"]').textContent).toContain('—');
    const [solid, dashed] = container.querySelectorAll('path[stroke="#aec477"]');
    expect(solid.getAttribute('d')).toMatch(/^M .* L /);
    expect(solid.getAttribute('stroke-dasharray')).toBe(null);
    expect(dashed.getAttribute('stroke-dasharray')).toBe('6 4');
    expect(container.querySelector('table.sr-only').textContent).toContain('Sep 21');

    const svg = container.querySelector('svg[role="img"]');
    svg.getBoundingClientRect = () => ({ left: 0, width: 200 });
    svg.dispatchEvent(new MouseEvent('mousemove', { clientX: 150, bubbles: true }));
    await nextTick();
    expect(container.textContent).toContain('Sep 21 · 1 closed');
    svg.dispatchEvent(new MouseEvent('mouseleave'));
    await nextTick();
    expect(container.textContent).not.toContain('Sep 21 · 1 closed');

    // A zero-width box (not laid out yet) still resolves to a bucket.
    svg.getBoundingClientRect = () => ({ left: 0, width: 0 });
    svg.dispatchEvent(new MouseEvent('mousemove', { clientX: 10, bubbles: true }));
    await nextTick();
    expect(container.textContent).toContain('Sep 20 · 2 closed');

    const max = [...container.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Max');
    max.click();
    await settle();
    expect(fetchLatency).toHaveBeenLastCalledWith('7d', 0, 0, 'max');
    expect(max.getAttribute('aria-pressed')).toBe('true');
  });

  it('says so when nothing closed, and shows a lone point', async () => {
    mount({ fetchLatency: async () => ({ points: [point(0)], summary: {} }), activeRange: '1d' });
    await settle();
    expect(container.textContent).toContain('No closed tasks in this range');
    expect(container.textContent).toContain('0 tasks');

    app.unmount();
    mount({ fetchLatency: async () => ({ points: [point(0, { worked: 1, closed: 1 })], summary: { worked: val(1) } }), activeRange: '1d' });
    await settle();
    expect(container.querySelectorAll('span.rounded-full.ring-2')).toHaveLength(1);
    expect(container.textContent).toContain('1 task');
  });
});

describe('AccountStats', () => {
  it('shows the account-wide latency panel', async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    vi.spyOn(api, 'fetchUserStats').mockResolvedValue({ summary: null, workspaces: [] });
    const latency = vi.spyOn(api, 'fetchUserTaskLatency').mockResolvedValue({ points: [], summary: { closed: 0 } });
    const container = document.createElement('div');
    document.body.appendChild(container);
    const app = createApp({ render: () => h(AccountStats) });
    app.use(pinia);
    app.component('RouterLink', { render: () => null });
    app.mount(container);
    await settle();
    expect(latency).toHaveBeenCalledWith('7d', 0, 0, 'p50');
    expect(container.textContent).toContain('Task Latency');
    app.unmount();
    container.remove();
    vi.restoreAllMocks();
  });
});
