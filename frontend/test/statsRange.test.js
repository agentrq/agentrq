// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, it, expect, vi } from 'vitest';
import { nextTick } from 'vue';

import {
  DEFAULT_STATS_RANGE,
  STATS_RANGE_OPTIONS,
  chartEndDateFor,
  chartFixedLengthFor,
  customWindowFor,
  statsPalette,
  useStatsRange,
} from '../src/composables/useStatsRange';

describe('STATS_RANGE_OPTIONS', () => {
  it('offers the six ranges both dashboards share, in order', () => {
    expect(STATS_RANGE_OPTIONS.map((o) => o.id)).toEqual([
      '1d',
      '7d',
      'week',
      '30d',
      'month',
      'custom',
    ]);
  });

  it('opens on the last 7 days', () => {
    expect(DEFAULT_STATS_RANGE).toBe('7d');
    expect(STATS_RANGE_OPTIONS.map((o) => o.id)).toContain(DEFAULT_STATS_RANGE);
  });

  it('is frozen, so one dashboard cannot mutate the other\'s options', () => {
    expect(Object.isFrozen(STATS_RANGE_OPTIONS)).toBe(true);
  });
});

describe('chartFixedLengthFor', () => {
  it('reserves a week of columns for the seven-day ranges', () => {
    expect(chartFixedLengthFor('7d')).toBe(7);
    expect(chartFixedLengthFor('week')).toBe(7);
  });

  it('reserves thirty columns for the month-length ranges', () => {
    expect(chartFixedLengthFor('30d')).toBe(30);
    expect(chartFixedLengthFor('month')).toBe(30);
  });

  it('counts a custom range inclusively', () => {
    // The 1st through the 7th is seven days, not six.
    expect(chartFixedLengthFor('custom', '2026-09-01', '2026-09-07')).toBe(7);
    expect(chartFixedLengthFor('custom', '2026-09-01', '2026-09-01')).toBe(1);
  });

  it('treats an inverted custom range as no range rather than negative columns', () => {
    expect(chartFixedLengthFor('custom', '2026-09-07', '2026-09-01')).toBe(0);
  });

  it('reserves nothing until a custom range has both ends', () => {
    expect(chartFixedLengthFor('custom', '2026-09-01', '')).toBe(0);
    expect(chartFixedLengthFor('custom', '', '2026-09-07')).toBe(0);
    expect(chartFixedLengthFor('custom')).toBe(0);
  });

  it('lets the shorter ranges size themselves to the data', () => {
    expect(chartFixedLengthFor('1d')).toBe(0);
    expect(chartFixedLengthFor('anything-else')).toBe(0);
  });
});

describe('chartEndDateFor', () => {
  it('uses the end of a custom range', () => {
    expect(chartEndDateFor('custom', '2026-09-07')).toBe('2026-09-07');
  });

  it('falls back to today when a custom range has no end yet', () => {
    const now = new Date(2026, 8, 9, 12, 0, 0);
    expect(chartEndDateFor('custom', '', now)).toBe('2026-09-09');
  });

  it('uses today for every relative range', () => {
    const now = new Date(2026, 8, 9, 12, 0, 0);
    expect(chartEndDateFor('7d', '', now)).toBe('2026-09-09');
    expect(chartEndDateFor('month', '', now)).toBe('2026-09-09');
  });

  it('zero-pads month and day', () => {
    const now = new Date(2026, 0, 5, 12, 0, 0);
    expect(chartEndDateFor('7d', '', now)).toBe('2026-01-05');
  });

  it('reads the local date, not the UTC one', () => {
    // Late evening local; `toISOString` would already have rolled over to the
    // next day for anyone west of UTC, labelling the last column tomorrow.
    const now = new Date(2026, 8, 9, 23, 30, 0);
    expect(chartEndDateFor('7d', '', now)).toBe('2026-09-09');
  });

  it('defaults to the current date with no clock passed', () => {
    const today = new Date();
    const expected = [
      today.getFullYear(),
      String(today.getMonth() + 1).padStart(2, '0'),
      String(today.getDate()).padStart(2, '0'),
    ].join('-');
    expect(chartEndDateFor('7d')).toBe(expected);
  });
});

describe('customWindowFor', () => {
  it('sends nothing for a named range, which the backend resolves itself', () => {
    expect(customWindowFor('7d', '2026-09-01', '2026-09-07')).toEqual({ from: 0, to: 0 });
    expect(customWindowFor('week')).toEqual({ from: 0, to: 0 });
  });

  it('converts a custom range to Unix seconds', () => {
    const { from, to } = customWindowFor('custom', '2026-09-01', '2026-09-07');
    expect(from).toBe(Math.floor(new Date('2026-09-01').getTime() / 1000));
    expect(to).toBe(Math.floor(new Date('2026-09-07').getTime() / 1000));
  });

  it('leaves a missing end of a custom range at zero', () => {
    expect(customWindowFor('custom', '', '')).toEqual({ from: 0, to: 0 });
    const partial = customWindowFor('custom', '2026-09-01', '');
    expect(partial.from).toBeGreaterThan(0);
    expect(partial.to).toBe(0);
  });
});

describe('statsPalette', () => {
  it('separates the two series in dark mode', () => {
    const dark = statsPalette(true);
    expect(dark.taskChart).toBe('#aec477');
    expect(dark.messageChart).toBe('#a8a3d9');
    expect(dark.taskHeatmap).toBe('yellow');
    expect(dark.messageHeatmap).toBe('violet');
  });

  it('uses one neutral for both series in light mode', () => {
    const light = statsPalette(false);
    expect(light.taskChart).toBe('#27272a');
    expect(light.messageChart).toBe('#27272a');
    expect(light.taskHeatmap).toBe('gray');
    expect(light.messageHeatmap).toBe('gray');
  });
});

describe('useStatsRange', () => {
  /** A fetcher that records how it was called and resolves with a fixture. */
  function recordingFetch(response = { summary: {}, heatmap: { granularity: 'hour' } }) {
    const calls = [];
    return {
      calls,
      fetchStats: vi.fn((range, from, to) => {
        calls.push({ range, from, to });
        return Promise.resolve(response);
      }),
    };
  }

  it('opens on 7d and starts out loading', () => {
    const { fetchStats } = recordingFetch();
    const s = useStatsRange({ fetchStats });
    expect(s.activeRange.value).toBe('7d');
    expect(s.loading.value).toBe(true);
    expect(s.stats.value).toBe(null);
  });

  it('loads the active range and clears loading', async () => {
    const { fetchStats, calls } = recordingFetch({ summary: { tasksCompleted: 3 } });
    const s = useStatsRange({ fetchStats });

    await s.load();

    expect(calls).toEqual([{ range: '7d', from: 0, to: 0 }]);
    expect(s.stats.value).toEqual({ summary: { tasksCompleted: 3 } });
    expect(s.loading.value).toBe(false);
  });

  it('loads immediately when switching to a named range', async () => {
    const { fetchStats, calls } = recordingFetch();
    const s = useStatsRange({ fetchStats });

    s.setRange('30d');
    await nextTick();

    expect(s.activeRange.value).toBe('30d');
    expect(calls).toEqual([{ range: '30d', from: 0, to: 0 }]);
  });

  it('does not load when switching to custom, which has no dates yet', async () => {
    const { fetchStats, calls } = recordingFetch();
    const s = useStatsRange({ fetchStats });

    s.setRange('custom');
    await nextTick();

    expect(s.activeRange.value).toBe('custom');
    expect(calls).toEqual([]);
  });

  it('applies a custom range only once both ends are set', async () => {
    const { fetchStats, calls } = recordingFetch();
    const s = useStatsRange({ fetchStats });
    s.setRange('custom');

    s.customFrom.value = '2026-09-01';
    s.apply();
    await nextTick();
    expect(calls).toEqual([]);

    s.customTo.value = '2026-09-07';
    s.apply();
    await nextTick();

    expect(calls).toHaveLength(1);
    expect(calls[0].range).toBe('custom');
    expect(calls[0].from).toBe(Math.floor(new Date('2026-09-01').getTime() / 1000));
    expect(calls[0].to).toBe(Math.floor(new Date('2026-09-07').getTime() / 1000));
  });

  it('ignores apply outside a custom range', async () => {
    const { fetchStats, calls } = recordingFetch();
    const s = useStatsRange({ fetchStats });
    s.customFrom.value = '2026-09-01';
    s.customTo.value = '2026-09-07';

    s.apply();
    await nextTick();

    expect(calls).toEqual([]);
  });

  it('drops stale numbers when a load fails, rather than leaving them on screen', async () => {
    const boom = new Error('nope');
    const onError = vi.fn();
    let shouldFail = false;
    const s = useStatsRange({
      fetchStats: () => (shouldFail ? Promise.reject(boom) : Promise.resolve({ summary: { tasksCompleted: 1 } })),
      onError,
    });

    await s.load();
    expect(s.stats.value).toEqual({ summary: { tasksCompleted: 1 } });

    shouldFail = true;
    await s.load();

    expect(s.stats.value).toBe(null);
    expect(s.loading.value).toBe(false);
    expect(onError).toHaveBeenCalledWith(boom);
  });

  it('reports a failure through console.error when given no handler', async () => {
    const boom = new Error('nope');
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {});
    const s = useStatsRange({ fetchStats: () => Promise.reject(boom) });

    await s.load();

    expect(spy).toHaveBeenCalledWith('Failed to load stats:', boom);
    expect(s.stats.value).toBe(null);
    spy.mockRestore();
  });

  it('exposes the derived chart inputs for the active range', async () => {
    const { fetchStats } = recordingFetch({ heatmap: { granularity: 'hour' } });
    const s = useStatsRange({ fetchStats });

    expect(s.chartFixedLength.value).toBe(7);
    expect(s.rangeOptions).toBe(STATS_RANGE_OPTIONS);

    s.activeRange.value = '30d';
    expect(s.chartFixedLength.value).toBe(30);

    s.activeRange.value = 'custom';
    s.customTo.value = '2026-09-07';
    expect(s.chartEndDate.value).toBe('2026-09-07');
  });

  it('takes the heatmap granularity from the response, defaulting to day', async () => {
    const { fetchStats } = recordingFetch({ heatmap: { granularity: 'hour' } });
    const s = useStatsRange({ fetchStats });

    expect(s.heatmapGranularity.value).toBe('day');

    await s.load();
    expect(s.heatmapGranularity.value).toBe('hour');

    s.stats.value = { heatmap: {} };
    expect(s.heatmapGranularity.value).toBe('day');
    s.stats.value = {};
    expect(s.heatmapGranularity.value).toBe('day');
  });
});
