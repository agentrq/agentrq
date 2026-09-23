// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, it, expect, vi, afterEach } from 'vitest';

import { searchWorkspaceSkills } from '../src/api';

// The search query travels in the URL, relative so the desktop app's proxy
// carries it, with only the parameters that were given.
describe('searchWorkspaceSkills', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('sends only what was asked for', async () => {
    const seen = [];
    vi.stubGlobal('fetch', vi.fn((url) => {
      seen.push(url);
      return Promise.resolve(new Response(JSON.stringify({ skills: [], total: 0 }), { status: 200 }));
    }));

    await searchWorkspaceSkills('ws1');
    await searchWorkspaceSkills('ws1', { q: 'pull request', limit: 20, offset: 40 });
    expect(seen).toEqual([
      '/api/v1/workspaces/ws1/skills',
      '/api/v1/workspaces/ws1/skills?q=pull+request&limit=20&offset=40',
    ]);
  });

  it('passes the server\'s refusal on, with its status', async () => {
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve(new Response(JSON.stringify({ error: { message: 'search query "ab" is too short' } }), { status: 422 })),
    ));
    await expect(searchWorkspaceSkills('ws1', { q: 'ab' })).rejects.toMatchObject({ message: 'search query "ab" is too short', status: 422 });
  });
});
