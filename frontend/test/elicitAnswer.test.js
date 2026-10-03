// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect } from 'vitest';

import { elicitAnswerLabel, formatElicitAnswerValue, elicitAnswerSummary, isElicitExpired } from '../src/composables/useElicitAnswer';

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

describe('elicitAnswerSummary', () => {
  it('shows a single answer bare', () => {
    expect(elicitAnswerSummary(msg({ transport: 'stdio' }, { transport: { title: 'Transport' } }))).toBe('stdio');
  });

  it('labels several answers', () => {
    const m = msg({ transport: 'http', retry: true }, { transport: { title: 'Transport' } });
    expect(elicitAnswerSummary(m)).toBe('Transport: http · retry: Yes');
  });

  it('is empty with no content', () => {
    expect(elicitAnswerSummary({ metadata: {} })).toBe('');
    expect(elicitAnswerSummary({})).toBe('');
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
