// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect } from 'vitest';

import { elicitAnswerLabel, formatElicitAnswerValue, elicitOutcome, elicitRows, isElicitExpired } from '../src/composables/useElicitAnswer';

const msg = (content, properties) => ({ metadata: { content, requestedSchema: properties && { properties } } });

describe('elicitAnswerLabel', () => {
  it('uses the property title, falling back to its name', () => {
    const m = msg({}, { transport: { title: 'Transport' } });
    expect(elicitAnswerLabel(m, 'transport')).toBe('Transport');
    expect(elicitAnswerLabel(m, 'other')).toBe('other');
    expect(elicitAnswerLabel({}, 'x')).toBe('x');
  });
});

describe('formatElicitAnswerValue', () => {
  it('formats each kind of value', () => {
    expect(formatElicitAnswerValue(['a', 'b'])).toBe('a, b');
    expect(formatElicitAnswerValue([])).toBe('(none)');
    expect(formatElicitAnswerValue(true)).toBe('Yes');
    expect(formatElicitAnswerValue(false)).toBe('No');
    expect(formatElicitAnswerValue('')).toBe('(empty)');
    expect(formatElicitAnswerValue(null)).toBe('(empty)');
    expect(formatElicitAnswerValue(undefined)).toBe('(empty)');
    expect(formatElicitAnswerValue(3)).toBe('3');
  });
});

describe('elicitOutcome', () => {
  const closed = (metadata) => ({ metadata });
  it('names how a closed question ended', () => {
    expect(elicitOutcome(closed({ status: 'accept', content: { a: 1 } }))).toBe('answered');
    expect(elicitOutcome(closed({ status: 'accept', content: {} }))).toBe('done');
    expect(elicitOutcome(closed({ status: 'accept', mode: 'url' }))).toBe('done');
    expect(elicitOutcome(closed({ status: 'decline', mode: 'form' }))).toBe('declined');
    expect(elicitOutcome(closed({ status: 'cancel', mode: 'form' }))).toBe('missed');
    expect(elicitOutcome(closed({ status: 'cancel', mode: 'url' }))).toBe('cancelled');
    expect(elicitOutcome({})).toBe('cancelled');
  });
});

describe('elicitRows', () => {
  const props = { transport: { title: 'Transport' }, retry: {} };
  it('lists what was answered, labelled and formatted', () => {
    const m = { metadata: { status: 'accept', content: { transport: 'http', retry: true }, requestedSchema: { properties: props } } };
    expect(elicitRows(m)).toEqual([
      { key: 'transport', label: 'Transport', answer: 'http' },
      { key: 'retry', label: 'retry', answer: 'Yes' },
    ]);
  });

  it('lists every question of a missed form with no answer', () => {
    const m = { metadata: { status: 'cancel', mode: 'form', requestedSchema: { properties: props } } };
    expect(elicitRows(m)).toEqual([
      { key: 'transport', label: 'Transport', answer: null },
      { key: 'retry', label: 'retry', answer: null },
    ]);
  });

  it('is empty when a missed form carried no schema', () => {
    expect(elicitRows({ metadata: { status: 'cancel', mode: 'form' } })).toEqual([]);
  });
});

describe('isElicitExpired', () => {
  const now = Date.parse('2026-10-03T05:00:00Z');
  const pending = (metadata, createdAt = '2026-10-03T04:59:00Z') => ({ createdAt, metadata: { status: 'pending', ...metadata } });

  it('is past its expiresAt', () => {
    expect(isElicitExpired(pending({ expiresAt: '2026-10-03T04:59:59Z' }), now)).toBe(true);
    expect(isElicitExpired(pending({ expiresAt: '2026-10-03T05:00:00Z' }), now)).toBe(true);
    expect(isElicitExpired(pending({ expiresAt: '2026-10-03T05:00:01Z' }), now)).toBe(false);
  });

  // Asked before questions carried a deadline: the longest wait applies.
  it('falls back to an hour after it was asked', () => {
    expect(isElicitExpired(pending({}, '2026-10-03T03:59:59Z'), now)).toBe(true);
    expect(isElicitExpired(pending({}, '2026-10-03T04:00:01Z'), now)).toBe(false);
    expect(isElicitExpired(pending({ expiresAt: 'soon' }, '2026-10-03T03:00:00Z'), now)).toBe(true);
  });

  it('is never true of a question already resolved', () => {
    const answered = { createdAt: '2026-10-01T00:00:00Z', metadata: { status: 'accept' } };
    expect(isElicitExpired(answered, now)).toBe(false);
    expect(isElicitExpired({}, now)).toBe(false);
  });
});
