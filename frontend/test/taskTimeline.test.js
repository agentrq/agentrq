// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { afterEach, describe, expect, it } from 'vitest';
import { createApp, h, nextTick, ref } from 'vue';
import TaskTimeline from '../src/components/TaskTimeline.vue';
import {
  formatDuration,
  formatTransitionTime,
  timelineDotClass,
  timelineLineClass,
  timelineSegments,
  timelineStateLabel,
  timelineTextClass,
  timelineTone,
  timelineTotals,
  timelineWorkedLabel,
} from '../src/composables/useTaskTimeline';

const t0 = new Date('2026-09-27T10:00:00Z');
const at = (minutes) => new Date(t0.getTime() + minutes * 60000);
const tr = (fromState, toState, minutes) => ({ fromState, toState, createdAt: at(minutes).toISOString() });

describe('timelineSegments', () => {
  it('is empty without a list', () => {
    expect(timelineSegments(undefined, t0)).toEqual([]);
  });

  it('lasts each state until the next transition, and the current one until now', () => {
    const segs = timelineSegments([tr('', 'ongoing', 0), tr('ongoing', 'completed', 10)], at(30));
    expect(segs.map((s) => [s.state, s.seconds, s.current, s.closed])).toEqual([
      ['ongoing', 600, false, false],
      ['completed', 1200, true, true],
    ]);
  });

  it('never goes negative on a clock that runs behind', () => {
    expect(timelineSegments([tr('', 'ongoing', 10)], at(0))[0].seconds).toBe(0);
  });
});

describe('timelineTotals', () => {
  it('adds up worked, blocked and needs-input time, and start to close', () => {
    const segs = timelineSegments([
      tr('', 'notstarted', 0),
      tr('notstarted', 'ongoing', 5),
      tr('ongoing', 'needsinput', 10),
      tr('needsinput', 'ongoing', 12),
      tr('ongoing', 'blocked', 20),
      tr('blocked', 'ongoing', 50),
      tr('ongoing', 'completed', 60),
    ], at(90));
    expect(timelineTotals(segs)).toEqual({
      workedSeconds: (5 + 8 + 10) * 60,
      blockedSeconds: 30 * 60,
      needsInputSeconds: 2 * 60,
      startToCloseSeconds: 55 * 60,
      totalSeconds: 55 * 60,
    });
  });

  it('has no close time while open, nor without a start', () => {
    expect(timelineTotals(timelineSegments([tr('', 'ongoing', 0)], at(5))).startToCloseSeconds).toBeNull();
    expect(timelineTotals(timelineSegments([tr('', 'notstarted', 0), tr('notstarted', 'rejected', 5)], at(9))).startToCloseSeconds).toBeNull();
    expect(timelineTotals([])).toEqual({ workedSeconds: 0, blockedSeconds: 0, needsInputSeconds: 0, startToCloseSeconds: null, totalSeconds: 0 });
    // Open, the total runs from the first start to now.
    expect(timelineTotals(timelineSegments([tr('', 'notstarted', 0), tr('notstarted', 'ongoing', 5)], at(25))).totalSeconds).toBe(20 * 60);
  });
});

describe('formatting', () => {
  it('says a duration at a glance', () => {
    expect([0, undefined, -5, 45, 60, 12 * 60, 3600, 3 * 3600 + 5 * 60, 86400, 2 * 86400 + 4 * 3600].map(formatDuration))
      .toEqual(['0s', '0s', '0s', '45s', '1m', '12m', '1h', '3h 5m', '1d', '2d 4h']);
  });

  it('gives the date only for another day', () => {
    const today = formatTransitionTime(at(0), at(60));
    expect(today).not.toContain(',');
    expect(formatTransitionTime(at(0), at(3 * 24 * 60))).toMatch(/^Sep 27, /);
  });

  it('names the states', () => {
    expect(['notstarted', 'needsinput', 'pending', 'blocked', '', undefined].map(timelineStateLabel))
      .toEqual(['not started', 'needs input', 'needs input', 'blocked', 'unknown', 'unknown']);
  });
});

describe('colour', () => {
  const seg = (state, current = false, closed = false) => ({ state, current, closed });

  it('draws needs-input yellow, and the current open segment while the task waits on you', () => {
    expect(timelineTone(seg('needsinput'), '')).toBe('pending');
    expect(timelineTone(seg('ongoing', true), 'pending')).toBe('pending');
    expect(timelineTone(seg('ongoing', false), 'pending')).toBe('ongoing');
    expect(timelineTone(seg('completed', true, true), 'pending')).toBe('completed');
    expect(timelineTone(seg('blocked', true), 'blocked')).toBe('blocked');
  });

  it('maps a tone to its classes', () => {
    expect(timelineDotClass('pending')).toBe('bg-yellow-400');
    expect(timelineDotClass('blocked')).toContain('bg-red-500');
    expect(timelineTextClass('pending')).toContain('yellow');
    expect(timelineTextClass('blocked')).toContain('red');
    expect(timelineTextClass('ongoing', true)).toContain('text-gray-800');
    expect(timelineTextClass('ongoing', false)).toContain('text-gray-500');
    expect(timelineLineClass('blocked')).toContain('red');
    expect(timelineLineClass('pending')).toContain('yellow');
    expect(timelineLineClass('ongoing')).toContain('h-px');
  });
});

describe('timelineWorkedLabel', () => {
  const label = (transitions) => timelineWorkedLabel(transitions, timelineSegments(transitions, at(60)));

  it('names the agent that last changed the status, and its model', () => {
    expect(label([
      tr('', 'notstarted', 0),
      { ...tr('notstarted', 'ongoing', 1), agent: 'codex' },
      { ...tr('ongoing', 'completed', 5), agent: 'gemini', agentModel: 'Gemini 3 Flash' },
    ])).toBe('gemini (Gemini 3 Flash) worked');
    expect(label([tr('', 'notstarted', 0), { ...tr('notstarted', 'ongoing', 1), agent: 'claude-code' }, tr('ongoing', 'completed', 5)]))
      .toBe('claude-code worked');
  });

  it('says working while the task is ongoing', () => {
    expect(label([tr('', 'notstarted', 0), { ...tr('notstarted', 'ongoing', 1), agent: 'claude-code' }])).toBe('claude-code working');
    expect(label([tr('', 'ongoing', 0)])).toBe('Working');
  });

  it('keeps the plain label when no agent is named', () => {
    expect(label([tr('', 'ongoing', 0), tr('ongoing', 'completed', 5)])).toBe('Worked');
    expect(timelineWorkedLabel(null, [])).toBe('Worked');
  });
});

describe('TaskTimeline', () => {
  let app;
  afterEach(() => { app?.unmount(); app = null; });

  function mount(props) {
    const el = document.createElement('div');
    document.body.appendChild(el);
    const state = ref(props);
    app = createApp({ setup: () => () => h(TaskTimeline, state.value) });
    app.mount(el);
    return { el, state };
  }

  it('renders nothing without history', () => {
    const { el } = mount({ transitions: [] });
    expect(el.querySelector('[data-testid=task-timeline]')).toBeNull();
  });

  it('labels every dot and states the totals', async () => {
    const now = Date.now();
    const ago = (m) => new Date(now - m * 60000).toISOString();
    const { el } = mount({
      transitions: [
        { fromState: '', toState: 'ongoing', createdAt: ago(60) },
        { fromState: 'ongoing', toState: 'blocked', createdAt: ago(40) },
        { fromState: 'blocked', toState: 'needsinput', createdAt: ago(30) },
        { fromState: 'needsinput', toState: 'completed', createdAt: ago(20), agent: 'claude-code' },
      ],
    });
    await nextTick();
    const text = el.textContent;
    expect(el.querySelectorAll('li')).toHaveLength(4);
    for (const s of ['ongoing', 'blocked', 'needs input', 'completed', 'claude-code worked', '20m', 'Blocked', '10m', 'Needs input', 'Start→close', '40m']) {
      expect(text).toContain(s);
    }
  });

  it('keeps a phone to the latest dots, with no labels', async () => {
    const now = Date.now();
    const transitions = Array.from({ length: 9 }, (_, i) => ({
      fromState: i ? 'ongoing' : '', toState: i % 2 ? 'blocked' : 'ongoing', createdAt: new Date(now - (9 - i) * 60000).toISOString(),
    }));
    const { el } = mount({ transitions, compact: true, currentTone: 'pending' });
    await nextTick();
    expect(el.querySelectorAll('li')).toHaveLength(6);
    expect(el.textContent).not.toContain('Start→close');
    expect(el.textContent).not.toContain('Worked');
    expect(el.textContent).toContain('Total');
    expect(el.querySelector('li:last-child').getAttribute('title')).toMatch(/^needs input/);
  });
});
