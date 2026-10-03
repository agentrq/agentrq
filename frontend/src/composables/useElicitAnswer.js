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

// One line for the collapsed card: the bare value when there is one field,
// "Label: value" pairs otherwise. Empty when there is nothing to show.
export function elicitAnswerSummary(m) {
  const entries = Object.entries(m.metadata?.content || {});
  if (entries.length === 1) return formatElicitAnswerValue(entries[0][1]);
  return entries
    .map(([key, value]) => `${elicitAnswerLabel(m, key)}: ${formatElicitAnswerValue(value)}`)
    .join(' · ');
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
