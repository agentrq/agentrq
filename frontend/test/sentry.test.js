// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, describe, expect, it, vi } from 'vitest';

import { startSentry } from '../src/utils/sentry.js';

// @sentry/vue, for the default loader alone; the other cases pass their own.
const realInit = vi.hoisted(() => vi.fn());
vi.mock('@sentry/vue', () => ({ init: realInit }));

// The server's GET /sentry, answering with `body` and `ok`.
function serve(body, ok = true) {
  const fetch = vi.fn().mockResolvedValue({ ok, json: async () => body });
  vi.stubGlobal('fetch', fetch);
  return fetch;
}

function sdk() {
  const Sentry = { init: vi.fn() };
  return { Sentry, load: vi.fn().mockResolvedValue(Sentry) };
}

afterEach(() => vi.unstubAllGlobals());

describe('startSentry', () => {
  it('starts Sentry with the server\'s DSN, reporting through the server', async () => {
    const fetch = serve({ dsn: 'https://key@sentry.example.com/1', environment: 'staging' });
    const { Sentry, load } = sdk();
    const app = {};

    await expect(startSentry(app, { load })).resolves.toBe(true);

    expect(fetch).toHaveBeenCalledWith('/api/v1/sentry');
    expect(Sentry.init).toHaveBeenCalledWith({
      app,
      dsn: 'https://key@sentry.example.com/1',
      environment: 'staging',
      release: __APP_VERSION__,
      tunnel: '/api/v1/sentry',
    });
  });

  it('loads nothing when frontend Sentry is off', async () => {
    serve(null, false);
    const off = sdk();
    await expect(startSentry({}, { load: off.load })).resolves.toBe(false);
    expect(off.load).not.toHaveBeenCalled();
  });

  it('loads nothing without a DSN', async () => {
    for (const body of [null, {}, { dsn: '' }]) {
      serve(body);
      const { load } = sdk();
      await expect(startSentry({}, { load })).resolves.toBe(false);
      expect(load).not.toHaveBeenCalled();
    }
  });

  it('loads nothing when the server does not answer', async () => {
    const { load } = sdk();
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')));
    await expect(startSentry({}, { load })).resolves.toBe(false);
  });

  it('never throws, even when the SDK fails to load', async () => {
    serve({ dsn: 'https://key@sentry.example.com/1' });
    await expect(startSentry({}, { load: () => Promise.reject(new Error('chunk')) })).resolves.toBe(false);
  });

  it('loads @sentry/vue by default', async () => {
    serve({ dsn: 'https://key@sentry.example.com/1', environment: 'test' });
    await expect(startSentry({})).resolves.toBe(true);
    expect(realInit).toHaveBeenCalledWith(expect.objectContaining({ environment: 'test' }));
  });
});
