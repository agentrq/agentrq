// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createApp, h, nextTick } from 'vue';
import { createPinia, setActivePinia } from 'pinia';
import HeatmapChart from '../src/components/HeatmapChart.vue';
import ChartSVG from '../src/components/ChartSVG.vue';
import WorkspaceStats from '../src/components/WorkspaceStats.vue';
import { useThemeStore } from '../src/stores/themeStore';
import * as api from '../src/api';

describe('HeatmapChart', () => {
  let container;

  beforeEach(() => {
    container = document.createElement('div');
    document.body.appendChild(container);
  });

  it('renders empty state when data is empty', () => {
    const app = createApp({
      render() {
        return h(HeatmapChart, { data: [], rangeStart: 0, rangeEnd: 0 });
      }
    });
    app.mount(container);
    expect(container.textContent).toContain('No data points');
    app.unmount();
  });

  it('renders hour granularity columns and labels', async () => {
    const now = Math.floor(Date.now() / 1000);
    const dayAgo = now - 86400;

    const app = createApp({
      render() {
        return h(HeatmapChart, {
          data: [{ bucket: '2026-09-06 14:00', count: 5 }],
          granularity: 'hour',
          rangeStart: dayAgo,
          rangeEnd: now,
          color: 'yellow'
        });
      }
    });
    app.mount(container);
    await nextTick();

    expect(container.textContent).toContain('12a');
    expect(container.textContent).toContain('12p');
    app.unmount();
  });

  it('renders with yellow variant color levels', async () => {
    const now = Math.floor(Date.now() / 1000);
    const dayAgo = now - 86400 * 2;
    const d = new Date(dayAgo * 1000);
    const dateStr = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;

    const app = createApp({
      render() {
        return h(HeatmapChart, {
          data: [{ bucket: dateStr, count: 10 }],
          granularity: 'day',
          rangeStart: dayAgo,
          rangeEnd: now,
          color: 'yellow'
        });
      }
    });
    app.mount(container);
    await nextTick();

    const cells = container.querySelectorAll('.rounded-sm');
    expect(cells.length).toBeGreaterThan(0);
    const yellowCell = Array.from(cells).find(c => c.className.includes('bg-[#aec477]'));
    expect(yellowCell).toBeTruthy();
    app.unmount();
  });

  it('renders with violet variant color levels', async () => {
    const now = Math.floor(Date.now() / 1000);
    const dayAgo = now - 86400 * 2;
    const d = new Date(dayAgo * 1000);
    const dateStr = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;

    const app = createApp({
      render() {
        return h(HeatmapChart, {
          data: [{ bucket: dateStr, count: 15 }],
          granularity: 'day',
          rangeStart: dayAgo,
          rangeEnd: now,
          color: 'violet'
        });
      }
    });
    app.mount(container);
    await nextTick();

    const cells = container.querySelectorAll('.rounded-sm');
    expect(cells.length).toBeGreaterThan(0);
    const violetCell = Array.from(cells).find(c => c.className.includes('bg-[#a8a3d9]'));
    expect(violetCell).toBeTruthy();
    app.unmount();
  });

  it('renders with gray variant color levels by default', async () => {
    const now = Math.floor(Date.now() / 1000);
    const dayAgo = now - 86400 * 2;
    const d = new Date(dayAgo * 1000);
    const dateStr = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;

    const app = createApp({
      render() {
        return h(HeatmapChart, {
          data: [{ bucket: dateStr, count: 20 }],
          granularity: 'day',
          rangeStart: dayAgo,
          rangeEnd: now,
          color: 'gray'
        });
      }
    });
    app.mount(container);
    await nextTick();

    const cells = container.querySelectorAll('.rounded-sm');
    expect(cells.length).toBeGreaterThan(0);
    const grayCell = Array.from(cells).find(c => c.className.includes('bg-black') || c.className.includes('bg-zinc-50'));
    expect(grayCell).toBeTruthy();
    app.unmount();
  });
});

describe('ChartSVG', () => {
  let container;

  beforeEach(() => {
    container = document.createElement('div');
    document.body.appendChild(container);
  });

  it('renders empty state when data is empty', () => {
    const app = createApp({
      render() {
        return h(ChartSVG, { data: [] });
      }
    });
    app.mount(container);
    expect(container.textContent).toContain('No data points');
    app.unmount();
  });

  it('renders lines and points with supplied color', async () => {
    const app = createApp({
      render() {
        return h(ChartSVG, {
          data: [
            { date: '2026-09-01', count: 5 },
            { date: '2026-09-02', count: 12 }
          ],
          color: '#aec477'
        });
      }
    });
    app.mount(container);
    await nextTick();

    const lines = container.querySelectorAll('path[stroke="#aec477"]');
    expect(lines.length).toBeGreaterThan(0);
    const points = container.querySelectorAll('circle[fill="#aec477"]');
    expect(points.length).toBe(2);
    app.unmount();
  });

  it('renders smooth spline curves using cubic bezier commands', async () => {
    const app = createApp({
      render() {
        return h(ChartSVG, {
          data: [
            { date: '2026-09-01', count: 5 },
            { date: '2026-09-02', count: 12 },
            { date: '2026-09-03', count: 8 },
            { date: '2026-09-04', count: 20 }
          ],
          color: '#aec477'
        });
      }
    });
    app.mount(container);
    await nextTick();

    const line = container.querySelector('path[stroke="#aec477"]');
    expect(line).toBeTruthy();
    const d = line.getAttribute('d');
    expect(d).toContain('C');
    app.unmount();
  });

  it('formats large values in Y-axis labels', async () => {
    const app = createApp({
      render() {
        return h(ChartSVG, {
          data: [
            { date: '2026-09-01', count: 2500000 }
          ],
          color: '#a8a3d9'
        });
      }
    });
    app.mount(container);
    await nextTick();

    expect(container.textContent).toContain('2.5M');
    app.unmount();
  });
});

describe('WorkspaceStats', () => {
  let container;
  let pinia;

  beforeEach(() => {
    pinia = createPinia();
    setActivePinia(pinia);
    container = document.createElement('div');
    document.body.appendChild(container);

    vi.spyOn(api, 'fetchWorkspaceStats').mockResolvedValue({
      summary: {
        tasksCompleted: 42,
        tasksScheduled: 10,
        messages: 120,
        manualApprovals: 5,
        autoApprovals: 15,
        denies: 1
      },
      heatmap: {
        granularity: 'day',
        rangeStart: 1725600000,
        rangeEnd: 1725700000,
        tasksCompleted: [{ bucket: '2026-09-06', count: 10 }],
        messages: [{ bucket: '2026-09-06', count: 25 }]
      },
      timeseries: {
        tasksCompleted: [{ date: '2026-09-06', count: 10 }],
        messages: [{ date: '2026-09-06', count: 25 }]
      }
    });
  });

  it('renders summary cards with indicators and values', async () => {
    const app = createApp({
      render() {
        return h(WorkspaceStats, { workspaceId: 'ws-123' });
      }
    });
    app.use(pinia);
    app.mount(container);
    await nextTick();
    await new Promise(r => setTimeout(r, 10));
    await nextTick();

    expect(container.textContent).toContain('Completed');
    expect(container.textContent).toContain('42');
    expect(container.textContent).toContain('Messages');
    expect(container.textContent).toContain('120');
    expect(container.textContent).toContain('Task Activity Heatmap');
    expect(container.textContent).toContain('Message Activity Heatmap');
    expect(container.textContent).toContain('Task Completion Velocity');
    expect(container.textContent).toContain('Communication Volume');

    app.unmount();
  });

  it('adjusts chart color based on dark mode theme setting', async () => {
    const themeStore = useThemeStore(pinia);
    themeStore.setTheme('dark');

    const app = createApp({
      render() {
        return h(WorkspaceStats, { workspaceId: 'ws-123' });
      }
    });
    app.use(pinia);
    app.mount(container);
    await nextTick();
    await new Promise(r => setTimeout(r, 10));
    await nextTick();

    // In dark mode, matte yellow-green (#aec477) is used for tasks, violet (#a8a3d9) for messages
    const yellowLines = container.querySelectorAll('path[stroke="#aec477"]');
    const violetLines = container.querySelectorAll('path[stroke="#a8a3d9"]');
    expect(yellowLines.length).toBeGreaterThan(0);
    expect(violetLines.length).toBeGreaterThan(0);

    app.unmount();
  });

  it('adjusts chart color based on light mode theme setting', async () => {
    const themeStore = useThemeStore(pinia);
    themeStore.setTheme('light');

    const app = createApp({
      render() {
        return h(WorkspaceStats, { workspaceId: 'ws-123' });
      }
    });
    app.use(pinia);
    app.mount(container);
    await nextTick();
    await new Promise(r => setTimeout(r, 10));
    await nextTick();

    // In light mode, original #27272a (dark charcoal) is used for both charts
    const lines = container.querySelectorAll('path[stroke="#27272a"]');
    expect(lines.length).toBeGreaterThan(0);

    app.unmount();
  });
});
