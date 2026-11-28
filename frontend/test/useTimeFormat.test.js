// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi, afterEach } from 'vitest';
import {
  TIME_FORMATS, browserHourCycle, loadTimeFormat, saveTimeFormat, timeFormat, withClock,
} from '../src/composables/useTimeFormat';

const AT = new Date(2026, 9, 13, 14, 5);
const time = (format) => AT.toLocaleTimeString('en-US', withClock({ hour: '2-digit', minute: '2-digit' }, format));

afterEach(() => {
  timeFormat.value = 'browser';
  vi.restoreAllMocks();
});

describe('withClock', () => {
  it('shows the account\'s 12h or 24h clock whatever the locale names', () => {
    expect(time('12h')).toBe('02:05 PM');
    expect(time('24h')).toBe('14:05');
    expect(AT.toLocaleTimeString('en-GB', withClock({ hour: '2-digit', minute: '2-digit' }, '12h'))).toMatch(/^02:05\s?pm$/i);
  });

  it('follows the browser\'s own clock for browser', () => {
    expect(withClock({ hour: 'numeric' }, 'browser')).toEqual({ hour: 'numeric', hourCycle: browserHourCycle() });
  });

  it('reads the account\'s clock when none is named', () => {
    timeFormat.value = '24h';
    expect(withClock({ minute: '2-digit' })).toEqual({ minute: '2-digit', hourCycle: 'h23' });
  });
});

describe('browserHourCycle', () => {
  it('is 12-hour where the language counts to 12 and 24-hour elsewhere', () => {
    expect(browserHourCycle('en-US')).toBe('h12');
    expect(browserHourCycle('ja-JP-u-hc-h11')).toBe('h12');
    expect(browserHourCycle('en-GB')).toBe('h23');
  });
});

describe('loadTimeFormat', () => {
  it('takes the account\'s clock from its display settings', async () => {
    await loadTimeFormat(() => Promise.resolve({ settings: { display: { timeFormat: '12h' } } }));
    expect(timeFormat.value).toBe('12h');
  });

  it('keeps the clock it has for an answer it cannot use, or a failed read', async () => {
    timeFormat.value = '24h';
    await loadTimeFormat(() => Promise.resolve({ settings: { display: { timeFormat: '13h' } } }));
    await loadTimeFormat(() => Promise.resolve(null));
    await loadTimeFormat(() => Promise.reject(new Error('network unreachable')));
    expect(timeFormat.value).toBe('24h');
  });

  it('reads through the API by default', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ settings: { display: { timeFormat: '24h' } } }), { status: 200 })
    );
    await loadTimeFormat();
    expect(fetchSpy.mock.calls[0][0]).toMatch(/\/account\/settings$/);
    expect(timeFormat.value).toBe('24h');
  });
});

describe('saveTimeFormat', () => {
  it('shows the pick at once and saves it in the display group', async () => {
    let resolve;
    const save = vi.fn(() => new Promise((r) => { resolve = r; }));
    const saving = saveTimeFormat('12h', save);
    expect(timeFormat.value).toBe('12h');
    expect(save).toHaveBeenCalledWith({ display: { timeFormat: '12h' } });
    resolve({});
    await saving;
    expect(timeFormat.value).toBe('12h');
  });

  it('puts the last clock back and throws the refusal when the server does not keep it', async () => {
    timeFormat.value = '24h';
    await expect(saveTimeFormat('12h', () => Promise.reject(new Error('database unavailable')))).rejects.toThrow('database unavailable');
    expect(timeFormat.value).toBe('24h');
  });

  it('saves through the API by default, and reads its refusal', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(JSON.stringify({ settings: { display: { timeFormat: '12h' } } }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ error: { code: 422, message: 'timeFormat "x" is not one of …' } }), { status: 422 }))
      .mockResolvedValueOnce(new Response('not json', { status: 500 }));
    await saveTimeFormat('12h');
    expect(fetchSpy.mock.calls[0][1].method).toBe('PUT');
    expect(JSON.parse(fetchSpy.mock.calls[0][1].body)).toEqual({ display: { timeFormat: '12h' } });
    await expect(saveTimeFormat('x')).rejects.toThrow('timeFormat "x" is not one of …');
    await expect(saveTimeFormat('24h')).rejects.toThrow('Failed to save account settings');
    expect(timeFormat.value).toBe('12h');
  });
});

describe('the account settings API', () => {
  it('reports a failed read', async () => {
    const api = await import('../src/api');
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('{}', { status: 500 }));
    await expect(api.fetchAccountSettings()).rejects.toThrow('Failed to fetch account settings');
  });

  it('lists the three clocks', () => {
    expect(TIME_FORMATS).toEqual(['browser', '12h', '24h']);
  });
});
