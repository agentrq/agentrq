// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * A task's state transitions, as the timeline under its description shows
 * them.
 *
 * The server records every status change (`stateTransitions` on the single-task
 * GET). A state lasts until the next transition, and the one the task is in now
 * lasts until `now`, so the timeline keeps growing while it is open. The totals
 * are worked out here from the same segments rather than read from the
 * response's `timing`, so the numbers and the bar never disagree while the
 * current state ticks.
 */

import { taskAccentClass } from './useTaskStatusStyle';
import { withClock } from './useTimeFormat';

const CLOSED = new Set(['completed', 'rejected']);

/**
 * @typedef {object} TimelineSegment
 * @property {string} state
 * @property {Date} start
 * @property {Date} end
 * @property {number} seconds
 * @property {boolean} current  the state the task is in now
 * @property {boolean} closed   completed or rejected: an end, not a span
 */

/**
 * @param {Array<{toState: string, createdAt: string}>} transitions  oldest first
 * @param {Date} now
 * @returns {TimelineSegment[]}
 */
export function timelineSegments(transitions, now) {
  if (!Array.isArray(transitions)) return [];
  return transitions.map((tr, i) => {
    const start = new Date(tr.createdAt);
    const next = transitions[i + 1];
    const end = next ? new Date(next.createdAt) : now;
    return {
      state: tr.toState,
      start,
      end,
      seconds: Math.max(0, Math.floor((end - start) / 1000)),
      current: !next,
      closed: CLOSED.has(tr.toState),
    };
  });
}

/**
 * The totals the timeline states: time worked (ongoing), time blocked, time
 * waiting on the person's input, and from the first start to the close — the
 * last only while the task is closed.
 *
 * @param {TimelineSegment[]} segments
 * @returns {{workedSeconds: number, blockedSeconds: number, needsInputSeconds: number, startToCloseSeconds: number|null, totalSeconds: number}}
 */
export function timelineTotals(segments) {
  let workedSeconds = 0;
  let blockedSeconds = 0;
  let needsInputSeconds = 0;
  let startedAt = null;
  for (const s of segments) {
    if (s.state === 'ongoing') {
      workedSeconds += s.seconds;
      if (!startedAt) startedAt = s.start;
    } else if (s.state === 'blocked') {
      blockedSeconds += s.seconds;
    } else if (s.state === 'needsinput') {
      needsInputSeconds += s.seconds;
    }
  }
  const last = segments[segments.length - 1];
  const startToCloseSeconds =
    startedAt && last?.closed ? Math.max(0, Math.floor((last.start - startedAt) / 1000)) : null;
  // From the first start to the close, or to now while the task is open.
  const totalSeconds = startedAt
    ? startToCloseSeconds ?? Math.max(0, Math.floor((last.end - startedAt) / 1000))
    : 0;
  return { workedSeconds, blockedSeconds, needsInputSeconds, startToCloseSeconds, totalSeconds };
}

/**
 * What the worked total is called: the agent that last changed the task's
 * status, as it names itself, and its model when it reported one — "claude-code
 * worked", "gemini (Gemini 3 Flash) working" — or plain "Worked" when no agent
 * has. "Working" while the task is still ongoing.
 *
 * @param {Array<{agent?: string, agentModel?: string}>} transitions  oldest first
 * @param {TimelineSegment[]} segments
 * @returns {string}
 */
export function timelineWorkedLabel(transitions, segments) {
  const last = segments[segments.length - 1];
  const verb = last?.state === 'ongoing' ? 'working' : 'worked';
  const by = Array.isArray(transitions) ? [...transitions].reverse().find((tr) => tr?.agent) : null;
  if (!by) return verb === 'working' ? 'Working' : 'Worked';
  return `${by.agent}${by.agentModel ? ` (${by.agentModel})` : ''} ${verb}`;
}

/**
 * When a transition happened, as short as it can be said: the time alone on
 * the day it is, the date as well on any other.
 *
 * @param {Date} at
 * @param {Date} now
 * @returns {string}
 */
export function formatTransitionTime(at, now) {
  const time = at.toLocaleTimeString('en-US', withClock({ hour: '2-digit', minute: '2-digit' }));
  if (at.toDateString() === now.toDateString()) return time;
  return `${at.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })}, ${time}`;
}

/**
 * A duration at the precision a glance needs: `45s`, `12m`, `3h 5m`, `2d 4h`.
 *
 * @param {number} seconds
 * @returns {string}
 */
export function formatDuration(seconds) {
  const s = Math.max(0, Math.floor(seconds || 0));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 24) return m % 60 ? `${h}h ${m % 60}m` : `${h}h`;
  const d = Math.floor(h / 24);
  return h % 24 ? `${d}d ${h % 24}h` : `${d}d`;
}

/** What each state is called on the timeline. */
export function timelineStateLabel(state) {
  if (state === 'notstarted') return 'not started';
  if (state === 'needsinput' || state === 'pending') return 'needs input';
  return state || 'unknown';
}

/**
 * The colour a segment is drawn in: its own state's, and 'pending' — the
 * yellow of "waiting on you" — for a stretch that needed the person's input.
 * The server records those as `needsinput`; the current segment also turns
 * pending while the task shows as waiting for any other reason taskStatusTone
 * knows (a not-started task assigned to the person).
 *
 * @param {TimelineSegment} segment
 * @param {string} currentTone  taskStatusTone of the task as it is now
 */
export function timelineTone(segment, currentTone) {
  if (segment.state === 'needsinput') return 'pending';
  if (segment.current && !segment.closed && currentTone === 'pending') return 'pending';
  return segment.state;
}

/** The dot's fill: the status colours every task list uses. */
export function timelineDotClass(tone) {
  return tone === 'pending' ? 'bg-yellow-400' : taskAccentClass(tone);
}

/** A label or duration: blocked reads red and needs-input yellow, as the dots do. */
export function timelineTextClass(tone, emphasised) {
  if (tone === 'pending') return 'text-yellow-600 dark:text-yellow-400';
  if (tone === 'blocked') return 'text-red-600 dark:text-red-400';
  return emphasised ? 'text-gray-800 dark:text-zinc-100' : 'text-gray-500 dark:text-zinc-400';
}

/** The line to the next dot: a blocked or needs-input stretch is coloured along its whole length. */
export function timelineLineClass(tone) {
  if (tone === 'blocked') return 'h-0.5 bg-red-400/70 dark:bg-red-500/60';
  if (tone === 'pending') return 'h-0.5 bg-yellow-400/80 dark:bg-yellow-400/60';
  return 'h-px bg-gray-200 dark:bg-zinc-700';
}
