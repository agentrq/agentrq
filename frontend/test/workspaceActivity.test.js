// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi } from 'vitest';
import {
  TASK_ACTIVITY_EVENTS,
  activityOf,
  activityTooltip,
  createActivityRefresher,
  forkProgressLabel,
  forkStatusLabel,
  forkTooltip,
  sortForks,
  needsYouCount,
  needsYouLabel,
  workingLabel,
} from '../src/composables/useWorkspaceActivity';

const current = { id: 't1', title: 'Fix the build' };
const busy = { name: 'api', agentConnected: true, taskSummary: { ongoing: 2, needsYou: 1, done: 3, total: 6, current } };

describe('activityOf', () => {
  it('works only with an agent connected and a task ongoing', () => {
    expect(activityOf(busy)).toEqual({ working: true, ongoing: 2, needsYou: 1, done: 3, total: 6, current });
    expect(activityOf({ ...busy, agentConnected: false }).working).toBe(false);
    expect(activityOf({ agentConnected: true, taskSummary: { ongoing: 0 } }).working).toBe(false);
  });

  it('reads a workspace with no summary as doing nothing', () => {
    const idle = { working: false, ongoing: 0, needsYou: 0, done: 0, total: 0, current: null };
    expect(activityOf({ name: 'old' })).toEqual(idle);
    expect(activityOf(undefined)).toEqual(idle);
  });
});

describe('labels', () => {
  it('counts what needs you', () => {
    expect(needsYouLabel(1)).toBe('1 needs you');
    expect(needsYouLabel(3)).toBe('3 need you');
  });

  it('names the task at work, and how many more', () => {
    expect(workingLabel(activityOf(busy))).toBe('Working on Fix the build (+1 more)');
    expect(workingLabel({ ongoing: 1, current })).toBe('Working on Fix the build');
    expect(workingLabel({ ongoing: 1, current: null })).toBe('Working');
  });

  it('says what a fork is doing, what needs you first', () => {
    expect(forkStatusLabel(busy)).toBe('1 needs you');
    expect(forkStatusLabel({ ...busy, taskSummary: { ...busy.taskSummary, needsYou: 0 } })).toBe('Working on Fix the build (+1 more)');
    expect(forkStatusLabel({ agentConnected: true })).toBe('Idle');
    expect(forkStatusLabel({})).toBe('Agent offline');
  });

  it('shows progress only once a fork has tasks', () => {
    expect(forkProgressLabel(busy)).toBe('3/6 done');
    expect(forkProgressLabel({})).toBe('');
  });
});

describe('the sidebar row', () => {
  const quiet = { name: 'docs', agentConnected: true };

  it('says what is going on under the name', () => {
    expect(activityTooltip(busy)).toBe('api\nWorking on Fix the build (+1 more)\n1 needs you');
    expect(activityTooltip(quiet)).toBe('docs');
    expect(activityTooltip(undefined)).toBe('');
  });

  it('names its busy folded forks, busiest first, and counts the idle ones', () => {
    const working = { name: 'pg', agentConnected: true, taskSummary: { ongoing: 1, current } };
    expect(activityTooltip(quiet, [quiet, working, busy])).toBe(
      'docs\napi — 1 needs you\npg — Working on Fix the build\n1 idle'
    );
    expect(activityTooltip(quiet, [working])).toBe('docs\npg — Working on Fix the build');
    expect(activityTooltip(quiet, [quiet])).toBe('docs\n1 fork idle');
    const done = { name: 'old', taskSummary: { done: 1, total: 1 } };
    expect(activityTooltip(quiet, [done, quiet])).toBe('docs\n1 ready to merge · 1 idle');
    expect(activityTooltip(quiet, [done])).toBe('docs\n1 ready to merge');
    expect(activityTooltip(quiet, [quiet, quiet])).toBe('docs\n2 forks idle');
  });

  it('names six busy forks at most, and counts the rest', () => {
    const many = Array.from({ length: 9 }, (_, i) => ({ ...busy, name: `f${i}` }));
    const lines = activityTooltip(quiet, [...many, quiet]).split('\n');
    expect(lines).toHaveLength(8);
    expect(lines.slice(1, 7)).toEqual(['f0', 'f1', 'f2', 'f3', 'f4', 'f5'].map((n) => `${n} — 1 needs you`));
    expect(lines[7]).toBe('+3 more · 1 idle');
  });

  it('counts its own and its folded forks\' tasks that need you', () => {
    expect(needsYouCount(busy, [busy, quiet])).toBe(2);
    expect(needsYouCount(quiet)).toBe(0);
  });
});

describe('the forks bar', () => {
  it('puts what needs you first, then work, then what can merge, then the rest', () => {
    const idle = { name: 'idle', agentConnected: true };
    const mergeable = { name: 'done', taskSummary: { done: 2, total: 2 } };
    const unfinished = { name: 'waiting', unfinishedTasks: 1, taskSummary: { done: 1, total: 2 } };
    const working = { name: 'pg', agentConnected: true, taskSummary: { ongoing: 1 } };
    expect(sortForks([idle, mergeable, unfinished, working, busy]).map((f) => f.name)).toEqual(['api', 'pg', 'done', 'idle', 'waiting']);
    expect(sortForks()).toEqual([]);
  });

  it('says a fork\'s state and progress on hover', () => {
    expect(forkTooltip(busy)).toBe('api\n1 needs you\n3/6 done');
    expect(forkTooltip({ name: 'new', agentConnected: true })).toBe('new\nIdle');
    expect(forkTooltip(undefined)).toBe('Agent offline');
  });
});

describe('createActivityRefresher', () => {
  function fakeTimers() {
    const pending = new Map();
    let next = 1;
    return {
      pending,
      setTimeout: vi.fn((fn) => {
        pending.set(next, fn);
        return next++;
      }),
      clearTimeout: vi.fn((id) => pending.delete(id)),
      fire() {
        const fns = [...pending.values()];
        pending.clear();
        fns.forEach((fn) => fn());
      },
    };
  }

  it('reads the list once for a burst of task events, and again for the next burst', () => {
    const timers = fakeTimers();
    const refresh = vi.fn();
    const r = createActivityRefresher({ refresh, delay: 50, timers });
    for (const type of TASK_ACTIVITY_EVENTS) r.onEvent({ type });
    expect(timers.setTimeout).toHaveBeenCalledTimes(1);
    expect(timers.setTimeout.mock.calls[0][1]).toBe(50);
    timers.fire();
    expect(refresh).toHaveBeenCalledTimes(1);
    r.onEvent({ type: 'reply.received' });
    timers.fire();
    expect(refresh).toHaveBeenCalledTimes(2);
  });

  it('ignores events that change no task, and stops a pending read', () => {
    const timers = fakeTimers();
    const refresh = vi.fn();
    const r = createActivityRefresher({ refresh, timers });
    r.onEvent({ type: 'agent.connected' });
    r.onEvent(undefined);
    expect(timers.setTimeout).not.toHaveBeenCalled();
    r.stop();
    r.onEvent({ type: 'task.updated' });
    expect(timers.setTimeout.mock.calls[0][1]).toBe(2000);
    r.stop();
    expect(timers.pending.size).toBe(0);
    timers.fire();
    expect(refresh).not.toHaveBeenCalled();
  });

  it('runs on the real timers by default', () => {
    vi.useFakeTimers();
    try {
      const refresh = vi.fn();
      createActivityRefresher({ refresh }).onEvent({ type: 'task.created' });
      vi.advanceTimersByTime(2000);
      expect(refresh).toHaveBeenCalledTimes(1);
      expect(createActivityRefresher()).toHaveProperty('onEvent');
    } finally {
      vi.useRealTimers();
    }
  });
});
