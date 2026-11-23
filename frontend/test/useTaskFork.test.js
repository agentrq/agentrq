// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect } from 'vitest';

import { forkNotice, forkedTaskPath } from '../src/composables/useTaskFork';

describe('forkedTaskPath', () => {
  it('swaps the task on the workspace route', () => {
    expect(forkedTaskPath('/workspaces/ws1/tasks/t1', 't2')).toBe('/workspaces/ws1/tasks/t2');
  });

  it('swaps the task on the filtered task-list route', () => {
    expect(forkedTaskPath('/tasks/ongoing/ws1/t1', 't2')).toBe('/tasks/ongoing/ws1/t2');
  });

  it('copes with a trailing slash', () => {
    expect(forkedTaskPath('/workspaces/ws1/tasks/t1/', 't2')).toBe('/workspaces/ws1/tasks/t2');
  });
});

describe('forkNotice', () => {
  it('says the agent is starting when the fork is ongoing', () => {
    expect(forkNotice({ status: 'ongoing' }).message).toMatch(/starting/);
  });

  it('says it is queued otherwise', () => {
    expect(forkNotice({ status: 'notstarted' }).message).toMatch(/^Queued/);
    expect(forkNotice(undefined).message).toMatch(/^Queued/);
  });
});
