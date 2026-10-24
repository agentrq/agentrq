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

  it('sums up folded forks, one or many', () => {
    expect(activityTooltip(quiet, [busy])).toBe('docs\n1 fork · 1 working · 1 needs you');
    expect(activityTooltip(quiet, [quiet, quiet])).toBe('docs\n2 forks');
  });

  it('counts its own and its folded forks\' tasks that need you', () => {
    expect(needsYouCount(busy, [busy, quiet])).toBe(2);
    expect(needsYouCount(quiet)).toBe(0);
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
