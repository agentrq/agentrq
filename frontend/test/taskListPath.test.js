// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect } from 'vitest';

import { taskListPath } from '../src/composables/taskListPath';

describe('taskListPath', () => {
  it('goes to the filtered task list, keeping its query', () => {
    const route = { path: '/tasks/ongoing/ws1/t1', params: { filter: 'ongoing', workspaceId: 'ws1', taskId: 't1' }, query: { q: 'x' } };
    expect(taskListPath(route)).toEqual({ path: '/tasks/ongoing', query: { q: 'x' } });
  });

  it("goes to the workspace's task list", () => {
    const route = { path: '/workspaces/ws1/tasks/t1', params: { id: 'ws1', taskId: 't1' }, query: {} };
    expect(taskListPath(route)).toEqual({ path: '/workspaces/ws1' });
  });

  it('goes to the doc a Pano task was opened beside', () => {
    const route = { path: '/pano/d1/chat/ws1/t1', params: { docId: 'd1', workspaceId: 'ws1', taskId: 't1' }, query: {} };
    expect(taskListPath(route)).toEqual({ path: '/pano/d1' });
  });

  it('goes to the Pano board from a supervisor chat', () => {
    const route = { path: '/pano/chat/ws1/t1', params: { workspaceId: 'ws1', taskId: 't1' }, query: {} };
    expect(taskListPath(route)).toEqual({ path: '/pano' });
  });
});
