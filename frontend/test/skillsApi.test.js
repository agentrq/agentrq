// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi, afterEach } from 'vitest';

import {
  deleteSkill,
  getSkill,
  getSkillFile,
  getWorkspaceSkill,
  getWorkspaceSkillFile,
  importSkills,
  searchSkills,
  searchWorkspaceSkills,
  setSkillEnabled,
  setWorkspaceSkillEnabled,
} from '../src/api';

/** Answers every call with `body` and `status`, and records what was asked. */
function serve(body, status = 200) {
  const seen = [];
  vi.stubGlobal('fetch', vi.fn((url, init = {}) => {
    seen.push([url, init.method || 'GET', init.body ? JSON.parse(init.body) : undefined]);
    return Promise.resolve(new Response(status === 204 ? null : JSON.stringify(body), { status }));
  }));
  return seen;
}

afterEach(() => vi.unstubAllGlobals());

// The search query travels in the URL, relative so the desktop app's proxy
// carries it, with only the parameters that were given.
describe('searching skills', () => {
  it('sends only what was asked for, for the account and for a workspace', async () => {
    const seen = serve({ skills: [], total: 0 });

    await searchSkills();
    await searchSkills({ q: 'pull request', limit: 20, offset: 40 });
    await searchWorkspaceSkills('ws1');
    await searchWorkspaceSkills('ws1', { q: 'pull request', limit: 20, offset: 40 });
    expect(seen.map(([url]) => url)).toEqual([
      '/api/v1/skills',
      '/api/v1/skills?q=pull+request&limit=20&offset=40',
      '/api/v1/workspaces/ws1/skills',
      '/api/v1/workspaces/ws1/skills?q=pull+request&limit=20&offset=40',
    ]);
  });

  it('passes the server\'s refusal on, with its status, or says what failed', async () => {
    serve({ error: { message: 'search query "ab" is too short' } }, 422);
    await expect(searchSkills({ q: 'ab' })).rejects.toMatchObject({ message: 'search query "ab" is too short', status: 422 });
    await expect(searchWorkspaceSkills('ws1', { q: 'ab' })).rejects.toMatchObject({ message: 'search query "ab" is too short', status: 422 });
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response('', { status: 500 }))));
    await expect(searchSkills()).rejects.toMatchObject({ message: 'Failed to fetch skills', status: 500 });
    await expect(searchWorkspaceSkills('ws1')).rejects.toMatchObject({ message: 'Failed to fetch workspace skills' });
  });
});

// A name is one segment and a path several, each encoded on its own.
describe('reading a skill', () => {
  it('reads it and its files from the account, or through a workspace', async () => {
    const seen = serve({ skill: { name: 'a b' } });

    await expect(getSkill('a b')).resolves.toEqual({ skill: { name: 'a b' } });
    await getSkillFile('a b', 'references/x y.md');
    await getWorkspaceSkill('ws1', 'a b');
    await getWorkspaceSkillFile('ws1', 'a b', 'references/x y.md');
    expect(seen.map(([url]) => url)).toEqual([
      '/api/v1/skills/a%20b',
      '/api/v1/skills/a%20b/files/references/x%20y.md',
      '/api/v1/workspaces/ws1/skills/a%20b',
      '/api/v1/workspaces/ws1/skills/a%20b/files/references/x%20y.md',
    ]);
  });

  it('says a skill is missing with the status that says so', async () => {
    serve({ error: { message: 'skill tdd not found' } }, 404);
    await expect(getSkill('tdd')).rejects.toMatchObject({ message: 'skill tdd not found', status: 404 });
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response('', { status: 500 }))));
    await expect(getSkill('tdd')).rejects.toMatchObject({ message: 'Failed to fetch skill' });
    await expect(getSkillFile('tdd', 'SKILL.md')).rejects.toMatchObject({ message: 'Failed to fetch skill file' });
    await expect(getWorkspaceSkill('ws1', 'tdd')).rejects.toMatchObject({ message: 'Failed to fetch skill' });
    await expect(getWorkspaceSkillFile('ws1', 'tdd', 'SKILL.md')).rejects.toMatchObject({ message: 'Failed to fetch skill file' });
  });
});

// The skills chosen from a repository too large to import whole, and the
// workspaces to turn them on in, go in the body only when there are some.
describe('importSkills', () => {
  it('names the chosen skills and workspaces only when there are some', async () => {
    const seen = serve({ imported: [], skipped: [] });

    await importSkills('https://github.com/a/b');
    await importSkills('https://github.com/a/b', true, [], []);
    await importSkills('https://github.com/a/b', false, ['ship', 'tools/guard']);
    await importSkills('https://github.com/a/b', false, undefined, ['ws1', 'ws2']);
    expect(seen).toEqual([
      ['/api/v1/skills/import', 'POST', { url: 'https://github.com/a/b', overwrite: false }],
      ['/api/v1/skills/import', 'POST', { url: 'https://github.com/a/b', overwrite: true }],
      ['/api/v1/skills/import', 'POST', { url: 'https://github.com/a/b', overwrite: false, skills: ['ship', 'tools/guard'] }],
      ['/api/v1/skills/import', 'POST', { url: 'https://github.com/a/b', overwrite: false, workspaceIds: ['ws1', 'ws2'] }],
    ]);
  });

  it('passes the server\'s refusal on, or says it failed', async () => {
    serve({ error: { message: 'not a GitHub repository' } }, 422);
    await expect(importSkills('https://example.com')).rejects.toMatchObject({ message: 'not a GitHub repository', status: 422 });
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response('', { status: 502 }))));
    await expect(importSkills('https://github.com/a/b')).rejects.toMatchObject({ message: 'Failed to import skills', status: 502 });
  });
});

describe('deleteSkill', () => {
  it('deletes it from the account', async () => {
    const seen = serve(null, 204);
    await expect(deleteSkill('a b')).resolves.toBe(true);
    expect(seen).toEqual([['/api/v1/skills/a%20b', 'DELETE', undefined]]);
  });

  it('passes the server\'s refusal on, or says it failed', async () => {
    serve({ error: { message: 'skill tdd not found' } }, 404);
    await expect(deleteSkill('tdd')).rejects.toMatchObject({ message: 'skill tdd not found', status: 404 });
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response('', { status: 500 }))));
    await expect(deleteSkill('tdd')).rejects.toMatchObject({ message: 'Failed to delete skill' });
  });
});

// Each switch is a PATCH of the skill, always naming the state: the account's
// on its own route, a workspace's through that workspace.
describe('the two switches', () => {
  it('sends the state asked for, and returns the skill', async () => {
    const seen = serve({ skill: { name: 'tdd', enabled: false } });

    await expect(setSkillEnabled('tdd', false)).resolves.toEqual({ skill: { name: 'tdd', enabled: false } });
    await setSkillEnabled('a b', 1);
    await setWorkspaceSkillEnabled('ws1', 'tdd', false);
    await setWorkspaceSkillEnabled('ws1', 'a b', true);
    expect(seen).toEqual([
      ['/api/v1/skills/tdd', 'PATCH', { enabled: false }],
      ['/api/v1/skills/a%20b', 'PATCH', { enabled: true }],
      ['/api/v1/workspaces/ws1/skills/tdd', 'PATCH', { enabled: false }],
      ['/api/v1/workspaces/ws1/skills/a%20b', 'PATCH', { enabled: true }],
    ]);
  });

  it('passes the server\'s refusal on, or says which way it failed', async () => {
    serve({ error: { message: 'skill tdd not found' } }, 404);
    await expect(setSkillEnabled('tdd', false)).rejects.toMatchObject({ message: 'skill tdd not found', status: 404 });
    await expect(setWorkspaceSkillEnabled('ws1', 'tdd', false)).rejects.toMatchObject({ message: 'skill tdd not found', status: 404 });
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response('', { status: 500 }))));
    await expect(setSkillEnabled('tdd', false)).rejects.toMatchObject({ message: 'Failed to turn skill off' });
    await expect(setSkillEnabled('tdd', true)).rejects.toMatchObject({ message: 'Failed to turn skill on' });
    await expect(setWorkspaceSkillEnabled('ws1', 'tdd', false)).rejects.toMatchObject({ message: 'Failed to turn skill off' });
    await expect(setWorkspaceSkillEnabled('ws1', 'tdd', true)).rejects.toMatchObject({ message: 'Failed to turn skill on' });
  });
});
