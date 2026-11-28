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
});
