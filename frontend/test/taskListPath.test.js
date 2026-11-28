// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect } from 'vitest';

import { taskListPath, taskPagePath } from '../src/composables/taskListPath';

describe('taskListPath', () => {
  it('goes to the filtered task list, keeping its query', () => {
    const route = { path: '/tasks/ongoing/ws1/t1', params: { filter: 'ongoing', workspaceId: 'ws1', taskId: 't1' }, query: { q: 'x' } };
    expect(taskListPath(route)).toEqual({ path: '/tasks/ongoing', query: { q: 'x' } });
  });

  it("stays in the team on a teammate's task", () => {
    const route = { path: '/teams/tm1/tasks/ongoing/ws1/t1', params: { teamId: 'tm1', filter: 'ongoing', workspaceId: 'ws1', taskId: 't1' }, query: { member: 'u1' } };
    expect(taskListPath(route)).toEqual({ path: '/teams/tm1/tasks/ongoing', query: { member: 'u1' } });
    const board = { path: '/teams/tm1/workspaces/ws1/tasks/t1', params: { teamId: 'tm1', id: 'ws1', taskId: 't1' }, query: {} };
    expect(taskListPath(board)).toEqual({ path: '/teams/tm1/workspaces/ws1', query: {} });
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

  it('goes to the Kanban board a card was opened beside, keeping its filters', () => {
    const route = { path: '/kanban/chat/ws1/t1', params: { workspaceId: 'ws1', taskId: 't1' }, query: { workspace: 'Web' } };
    expect(taskListPath(route)).toEqual({ path: '/kanban', query: { workspace: 'Web' } });
  });

  it("goes to the workspace's board a card was opened beside", () => {
    const route = { path: '/workspaces/ws1/board/chat/t1', params: { id: 'ws1', taskId: 't1' }, query: {} };
    expect(taskListPath(route)).toEqual({ path: '/workspaces/ws1/board' });
  });

  it("goes to the team's Kanban board from a card opened beside it", () => {
    const route = { path: '/teams/tm1/kanban/chat/ws1/t1', params: { teamId: 'tm1', workspaceId: 'ws1', taskId: 't1' }, query: {} };
    expect(taskListPath(route)).toEqual({ path: '/teams/tm1/kanban', query: {} });
  });
});

describe('taskPagePath', () => {
  it('is the task page for a task opened beside a board or a doc', () => {
    const p = (path, params) => taskPagePath({ path, params, query: {} });
    const page = { path: '/workspaces/ws1/tasks/t1' };
    expect(p('/kanban/chat/ws1/t1', { workspaceId: 'ws1', taskId: 't1' })).toEqual(page);
    expect(p('/workspaces/ws1/board/chat/t1', { id: 'ws1', taskId: 't1' })).toEqual(page);
    expect(p('/pano/chat/ws1/t1', { workspaceId: 'ws1', taskId: 't1' })).toEqual(page);
    expect(p('/pano/d1/chat/ws1/t1', { docId: 'd1', workspaceId: 'ws1', taskId: 't1' })).toEqual(page);
  });

  it("stays in the team on a teammate's card", () => {
    const route = { path: '/teams/tm1/kanban/chat/ws1/t1', params: { teamId: 'tm1', workspaceId: 'ws1', taskId: 't1' }, query: {} };
    expect(taskPagePath(route)).toEqual({ path: '/teams/tm1/workspaces/ws1/tasks/t1', query: {} });
  });

  it('is null on a page the task already has to itself', () => {
    expect(taskPagePath({ path: '/workspaces/ws1/tasks/t1', params: { id: 'ws1', taskId: 't1' } })).toBeNull();
    expect(taskPagePath({ path: '/tasks/active/ws1/t1', params: { filter: 'active', workspaceId: 'ws1', taskId: 't1' } })).toBeNull();
  });
});
