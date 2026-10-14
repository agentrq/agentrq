// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Once resolved, an elicitation's metadata.content holds the answer keyed by
// schema property name — look up that property's own title for a label.
export function elicitAnswerLabel(m, key) {
  return m.metadata?.requestedSchema?.properties?.[key]?.title || key;
}

export function formatElicitAnswerValue(value) {
  if (Array.isArray(value)) return value.length ? value.join(', ') : '(none)';
  if (typeof value === 'boolean') return value ? 'Yes' : 'No';
  if (value === undefined || value === null || value === '') return '(empty)';
  return String(value);
}

// How a closed question ended. The server closes one nobody answered in time
// as "cancel", and a form offers no Cancel button, so a cancelled form was
// missed rather than cancelled by the human.
export function elicitOutcome(m) {
  const { status, mode, content } = m.metadata || {};
  if (status === 'accept') return Object.keys(content || {}).length ? 'answered' : 'done';
  if (status === 'decline') return 'declined';
  return mode === 'form' ? 'missed' : 'cancelled';
}

// One row per question: what was answered, or, for a missed form, each field
// it asked with a null answer.
export function elicitRows(m) {
  if (elicitOutcome(m) === 'answered') {
    return Object.entries(m.metadata.content)
      .map(([key, value]) => ({ key, label: elicitAnswerLabel(m, key), answer: formatElicitAnswerValue(value) }));
  }
  return Object.keys(m.metadata?.requestedSchema?.properties || {})
    .map((key) => ({ key, label: elicitAnswerLabel(m, key), answer: null }));
}

// The longest the server waits for an answer. Questions asked before they
// carried an expiresAt are past their deadline once this has passed.
export const ELICIT_MAX_WAIT_MS = 60 * 60 * 1000;

// A question still pending after its deadline has nobody waiting for its
// answer: the server closes it, but one it could not close (a restart) would
// otherwise offer buttons that can only fail.
export function isElicitExpired(m, now) {
  if (m.metadata?.status !== 'pending') return false;
  const expiresAt = Date.parse(m.metadata.expiresAt);
  const deadline = Number.isNaN(expiresAt) ? Date.parse(m.createdAt) + ELICIT_MAX_WAIT_MS : expiresAt;
  return now >= deadline;
}
