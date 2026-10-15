// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi, afterEach } from 'vitest';

import { listTaskToolCalls } from '../src/api';

function serve(body, status = 200) {
  const seen = [];
  vi.stubGlobal('fetch', vi.fn((url) => {
    seen.push(url);
    return Promise.resolve(new Response(JSON.stringify(body), { status }));
  }));
  return seen;
}

afterEach(() => vi.unstubAllGlobals());

// Relative, so the desktop app's proxy carries it, with only what was given.
describe('listTaskToolCalls', () => {
  it('pages before a call, or reads the latest', async () => {
    const seen = serve({ toolCalls: [{ id: 'c1' }] });

    expect(await listTaskToolCalls('ws1', 't1', { before: 'c9', limit: 50 })).toEqual({ toolCalls: [{ id: 'c1' }] });
    await listTaskToolCalls('ws1', 't1');

    expect(seen).toEqual([
      '/api/v1/workspaces/ws1/tasks/t1/tool-calls?before=c9&limit=50',
      '/api/v1/workspaces/ws1/tasks/t1/tool-calls',
    ]);
  });

  it('throws on a refusal', async () => {
    serve({ error: 'not found' }, 404);

    await expect(listTaskToolCalls('ws1', 't1')).rejects.toThrow('Failed to fetch tool calls');
  });
});
