// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi, afterEach } from 'vitest';

import { exportAccountData } from '../src/api';

function serve(response) {
  const seen = [];
  vi.stubGlobal('fetch', vi.fn((url) => {
    seen.push(url);
    return Promise.resolve(response);
  }));
  return seen;
}

describe('exportAccountData', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('reads the export at a relative URL, under the filename the server gives', async () => {
    const seen = serve(new Response('gz', {
      status: 200,
      headers: { 'Content-Type': 'application/gzip', 'Content-Disposition': 'attachment; filename="agentrq-export-2026-11-28.json.gz"' },
    }));
    const { blob, filename } = await exportAccountData();
    expect(seen).toEqual(['/api/v1/account/export']);
    expect(filename).toBe('agentrq-export-2026-11-28.json.gz');
    expect(await blob.text()).toBe('gz');
  });

  it('names the file itself when the server does not', async () => {
    serve(new Response('gz', { status: 200 }));
    await expect(exportAccountData()).resolves.toMatchObject({ filename: 'agentrq-export.json.gz' });
  });

  it('throws the server\'s sentence, or its own', async () => {
    serve(new Response(JSON.stringify({ error: { message: 'database unavailable' } }), { status: 500 }));
    await expect(exportAccountData()).rejects.toThrow('database unavailable');
    serve(new Response('nope', { status: 500 }));
    await expect(exportAccountData()).rejects.toThrow('Failed to export your data');
  });

  it('says when the next export is allowed, past the daily cap', async () => {
    const before = Date.now();
    serve(new Response(JSON.stringify({ error: { message: 'Your data can be exported 2 times a day' } }), { status: 429, headers: { 'Retry-After': '5400' } }));
    const err = await exportAccountData().catch((e) => e);
    expect(err.message).toBe('Your data can be exported 2 times a day');
    expect(err.retryAt.getTime()).toBeGreaterThanOrEqual(before + 5400 * 1000);
    expect(err.retryAt.getTime()).toBeLessThanOrEqual(Date.now() + 5400 * 1000);
  });

  it('names no retry time without a usable Retry-After, or for another refusal', async () => {
    serve(new Response('{}', { status: 429 }));
    expect((await exportAccountData().catch((e) => e)).retryAt).toBeUndefined();
    serve(new Response('{}', { status: 500, headers: { 'Retry-After': '60' } }));
    expect((await exportAccountData().catch((e) => e)).retryAt).toBeUndefined();
  });
});
