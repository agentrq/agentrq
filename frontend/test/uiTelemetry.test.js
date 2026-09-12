// Copyright 2026 Contextual, Inc. https://agentrq.com

import { describe, it, expect, vi } from 'vitest';

import {
  createOncePerEpisode,
  recordUiAction,
  workspaceIdFromRoute,
} from '../src/composables/useUiTelemetry';

describe('workspaceIdFromRoute', () => {
  it('reads the workspace from a workspace page', () => {
    expect(workspaceIdFromRoute({ path: '/workspaces/ws1/board', params: { id: 'ws1' } })).toBe('ws1');
  });

  it('reads it from a task opened inside a workspace', () => {
    expect(workspaceIdFromRoute({ path: '/workspaces/ws1/tasks/t1', params: { id: 'ws1', taskId: 't1' } }))
      .toBe('ws1');
  });

  it('reads it from a task opened out of a filtered list', () => {
    // That route spells the same thing `workspaceId`; missing this shape would
    // silently drop every report from half the app.
    expect(
      workspaceIdFromRoute({ path: '/tasks/ongoing/ws1/t1', params: { filter: 'ongoing', workspaceId: 'ws1', taskId: 't1' } })
    ).toBe('ws1');
  });

  it('does not mistake an event id for a workspace', () => {
    // `:id` means a workspace only under /workspaces.
    expect(workspaceIdFromRoute({ path: '/events/e1', params: { id: 'e1' } })).toBe('');
  });

  it('does not mistake a workflow id for a workspace', () => {
    expect(workspaceIdFromRoute({ path: '/workflows/wf1', params: { id: 'wf1' } })).toBe('');
  });

  it('reports nothing on a page that is not about a workspace', () => {
    expect(workspaceIdFromRoute({ path: '/tasks/ongoing', params: { filter: 'ongoing' } })).toBe('');
    expect(workspaceIdFromRoute({ path: '/', params: {} })).toBe('');
  });

  it('survives a route it was handed before one existed', () => {
    expect(workspaceIdFromRoute(undefined)).toBe('');
    expect(workspaceIdFromRoute({})).toBe('');
  });

  it('ignores a param that is not a string', () => {
    // Repeatable params arrive as arrays, which are not a workspace id.
    expect(workspaceIdFromRoute({ path: '/workspaces/x', params: { id: ['ws1', 'ws2'] } })).toBe('');
  });
});

describe('recordUiAction', () => {
  it('reports the action against the workspace in context', () => {
    const record = vi.fn();

    recordUiAction('ui_search', { path: '/workspaces/ws1/board', params: { id: 'ws1' } }, record);

    expect(record).toHaveBeenCalledWith('ui_search', 'ws1');
  });

  it('drops the report rather than guessing a workspace', () => {
    // Attributing a click to a workspace that did not generate it would
    // corrupt that workspace's numbers, which is worse than counting less.
    const record = vi.fn();

    recordUiAction('ui_search', { path: '/tasks/ongoing', params: {} }, record);

    expect(record).not.toHaveBeenCalled();
  });

  it('drops it on a page whose :id is not a workspace', () => {
    const record = vi.fn();

    recordUiAction('ui_copy_link', { path: '/events/e1', params: { id: 'e1' } }, record);

    expect(record).not.toHaveBeenCalled();
  });
});

describe('createOncePerEpisode', () => {
  it('runs the first time and not again', () => {
    // The finder searches on every keystroke; typing "deploy" is one search.
    const fn = vi.fn();
    const episode = createOncePerEpisode();

    episode.fire(fn);
    episode.fire(fn);
    episode.fire(fn);

    expect(fn).toHaveBeenCalledTimes(1);
  });

  it('runs again after a reset, which is what starts a new episode', () => {
    const fn = vi.fn();
    const episode = createOncePerEpisode();

    episode.fire(fn);
    episode.reset();
    episode.fire(fn);

    expect(fn).toHaveBeenCalledTimes(2);
  });

  it('resets cleanly before anything has fired', () => {
    const fn = vi.fn();
    const episode = createOncePerEpisode();

    episode.reset();
    episode.fire(fn);

    expect(fn).toHaveBeenCalledTimes(1);
  });

  it('keeps episodes independent of each other', () => {
    const a = vi.fn();
    const b = vi.fn();
    const first = createOncePerEpisode();
    const second = createOncePerEpisode();

    first.fire(a);
    first.fire(a);
    second.fire(b);

    expect(a).toHaveBeenCalledTimes(1);
    expect(b).toHaveBeenCalledTimes(1);
  });
});
