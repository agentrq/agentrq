// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi, afterEach } from 'vitest';

import { importWorkspaceSkills, searchWorkspaceSkills, setWorkspaceSkillEnabled } from '../src/api';

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

// The skills chosen from a repository too large to import whole go in the
// body; without a choice the body is what it always was.
describe('importWorkspaceSkills', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('names the chosen skills only when there are some', async () => {
    const bodies = [];
    vi.stubGlobal('fetch', vi.fn((_url, init) => {
      bodies.push(JSON.parse(init.body));
      return Promise.resolve(new Response(JSON.stringify({ imported: [], skipped: [] }), { status: 200 }));
    }));

    await importWorkspaceSkills('ws1', 'https://github.com/a/b');
    await importWorkspaceSkills('ws1', 'https://github.com/a/b', true, []);
    await importWorkspaceSkills('ws1', 'https://github.com/a/b', false, ['ship', 'tools/guard']);
    expect(bodies).toEqual([
      { url: 'https://github.com/a/b', overwrite: false },
      { url: 'https://github.com/a/b', overwrite: true },
      { url: 'https://github.com/a/b', overwrite: false, skills: ['ship', 'tools/guard'] },
    ]);
  });
});

// Turning a skill on or off is a PATCH of the skill, always naming the state.
describe('setWorkspaceSkillEnabled', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('sends the state asked for, and returns the skill', async () => {
    const seen = [];
    vi.stubGlobal('fetch', vi.fn((url, init) => {
      seen.push([url, init.method, JSON.parse(init.body)]);
      return Promise.resolve(new Response(JSON.stringify({ skill: { name: 'tdd', enabled: false } }), { status: 200 }));
    }));

    await expect(setWorkspaceSkillEnabled('ws1', 'tdd', false)).resolves.toEqual({ skill: { name: 'tdd', enabled: false } });
    await setWorkspaceSkillEnabled('ws1', 'a b', true);
    expect(seen).toEqual([
      ['/api/v1/workspaces/ws1/skills/tdd', 'PATCH', { enabled: false }],
      ['/api/v1/workspaces/ws1/skills/a%20b', 'PATCH', { enabled: true }],
    ]);
  });

  it('passes the server\'s refusal on, or says which way it failed', async () => {
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve(new Response(JSON.stringify({ error: { message: 'read-only here' } }), { status: 403 })),
    ));
    await expect(setWorkspaceSkillEnabled('ws1', 'tdd', false)).rejects.toMatchObject({ message: 'read-only here', status: 403 });
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response('', { status: 500 }))));
    await expect(setWorkspaceSkillEnabled('ws1', 'tdd', false)).rejects.toMatchObject({ message: 'Failed to turn skill off' });
    await expect(setWorkspaceSkillEnabled('ws1', 'tdd', true)).rejects.toMatchObject({ message: 'Failed to turn skill on' });
  });
});
