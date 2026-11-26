// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * A Kanban card's footer: the time worked on the left, the current state on
 * the right, from the `timing` a task list carries.
 *
 * `workedSeconds` stops at the task's latest transition; while `workingSince`
 * is set the time since then adds to it, so the card counts up live without
 * knowing when the server answered.
 */

import { formatDuration } from './useTaskTimeline';

const since = (iso, now) => Math.max(0, (now - new Date(iso).getTime()) / 1000);

/** '42m ago', '3h ago', '2d ago'. */
export function shortAgo(iso, now = Date.now()) {
  const m = Math.floor(since(iso, now) / 60);
  if (m < 1) return 'now';
  if (m < 60) return `${m}m ago`;
  if (m < 1440) return `${Math.floor(m / 60)}h ago`;
  return `${Math.floor(m / 1440)}d ago`;
}

/** Seconds worked, the running stretch included. */
export function workedSeconds(timing, now = Date.now()) {
  if (!timing) return 0;
  return (timing.workedSeconds || 0) + (timing.workingSince ? since(timing.workingSince, now) : 0);
}

/**
 * The footer for an ongoing, blocked or closed task, or null for any other,
 * or one whose timing does not say when it entered its status.
 * kind: 'working' | 'blocked' | 'completed' | 'rejected'.
 */
export function cardFooter(task, now = Date.now()) {
  const timing = task?.timing;
  if (!timing?.statusSince) return null;
  const worked = workedSeconds(timing, now);
  const total = worked > 0 ? formatDuration(worked) : '';
  switch (task.status) {
    case 'ongoing':
      return { kind: 'working', total: formatDuration(worked), state: formatDuration(since(timing.statusSince, now)) };
    case 'blocked':
      return { kind: 'blocked', total, state: formatDuration(since(timing.statusSince, now)) };
    case 'completed':
      return { kind: 'completed', total, state: `✓ ${shortAgo(timing.statusSince, now)}` };
    case 'rejected':
      return { kind: 'rejected', total, state: `✗ ${shortAgo(timing.statusSince, now)}` };
  }
  return null;
}

/**
 * The timing after a move the board learned of itself (a drag, a live
 * update), which carries none: the running stretch is banked, a move to
 * ongoing starts a new one, and the status starts at `at`. The next listing
 * replaces it with the server's.
 */
export function timingAfterMove(timing, status, at = new Date().toISOString()) {
  const atMs = new Date(at).getTime();
  const worked = (timing?.workedSeconds || 0) + (timing?.workingSince ? since(timing.workingSince, atMs) : 0);
  return {
    workedSeconds: Math.floor(worked),
    ...(status === 'ongoing' ? { workingSince: at } : {}),
    statusSince: at,
  };
}
