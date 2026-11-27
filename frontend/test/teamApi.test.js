// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The team calls on the wire, as the server's team routes take them: method,
 * path and body, and how a refusal reads.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import * as api from '../src/api.js';

describe('the team API', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ id: 'x' }) }));
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  // name, arguments, method, path, body
  const CASES = [
    ['listTeams', [], 'GET', '/teams'],
    ['createTeam', ['Core'], 'POST', '/teams', { name: 'Core' }],
    ['getTeam', ['tm1'], 'GET', '/teams/tm1'],
    ['renameTeam', ['tm1', 'Core', 4], 'PATCH', '/teams/tm1', { name: 'Core', version: 4 }],
    ['deleteTeam', ['tm1', 4], 'DELETE', '/teams/tm1?version=4'],
    ['listTeamMembers', ['tm1'], 'GET', '/teams/tm1/members'],
    ['removeTeamMember', ['tm1', 'u1', 4], 'DELETE', '/teams/tm1/members/u1?version=4'],
    ['leaveTeam', ['tm1', 4], 'POST', '/teams/tm1/leave', { version: 4 }],
    ['listTeamInvitations', ['tm1'], 'GET', '/teams/tm1/invitations'],
    ['createTeamInvitation', ['tm1', 'd@x.io', 4], 'POST', '/teams/tm1/invitations', { email: 'd@x.io', version: 4 }],
    ['revokeTeamInvitation', ['tm1', 'inv1', 4], 'DELETE', '/teams/tm1/invitations/inv1?version=4'],
    ['fetchTeamActivities', ['tm1'], 'GET', '/teams/tm1/activities?limit=20'],
    ['fetchTeamActivities', ['tm1', { limit: 5, before: 'a9' }], 'GET', '/teams/tm1/activities?limit=5&before=a9'],
    ['listMyInvitations', [], 'GET', '/invitations'],
    ['previewInvitation', ['tok'], 'POST', '/invitations/preview', { token: 'tok' }],
    ['acceptInvitation', ['tok'], 'POST', '/invitations/accept', { token: 'tok' }],
    ['acceptInvitationById', ['inv1'], 'POST', '/invitations/inv1/accept'],
    ['declineInvitation', ['inv1'], 'POST', '/invitations/inv1/decline'],
    ['listWorkspaceShares', ['ws1'], 'GET', '/workspaces/ws1/shares'],
    ['shareWorkspace', ['ws1', 'tm1', 4], 'PUT', '/workspaces/ws1/shares/tm1', { version: 4 }],
    ['shareWorkspace', ['ws1', 'tm1', 4, { tasks: true, memory: true }], 'PUT', '/workspaces/ws1/shares/tm1', { tasks: true, memory: true, version: 4 }],
    ['unshareWorkspace', ['ws1', 'tm1', 4], 'DELETE', '/workspaces/ws1/shares/tm1?version=4'],
    ['listPanoShares', ['d1'], 'GET', '/pano/d1/shares'],
    ['sharePanoDoc', ['d1', 'tm1', 4], 'PUT', '/pano/d1/shares/tm1', { version: 4 }],
    ['unsharePanoDoc', ['d1', 'tm1', 4], 'DELETE', '/pano/d1/shares/tm1?version=4'],
    ['listTeammateMemories', ['ws1'], 'GET', '/workspaces/ws1/teammate-memories'],
    ['setTeammateMemory', ['ws1', 'u1', false], 'PUT', '/workspaces/ws1/teammate-memories/u1', { included: false }],
    ['listTeammateMemoryNames', ['ws1', 'ws2'], 'GET', '/workspaces/ws1/teammate-memories/ws2/memories'],
    ['getTeammateMemory', ['ws1', 'ws2', 'release notes.md'], 'GET', '/workspaces/ws1/teammate-memories/ws2/memories/release%20notes.md'],
    ['fetchTeamWorkspaces', ['tm1'], 'GET', '/teams/tm1/workspaces'],
    ['fetchTeamTasks', ['tm1'], 'GET', '/teams/tm1/tasks?limit=10&offset=0'],
    ['fetchTeamTasks', ['tm1', { status: 'ongoing', workspace: 'Back end', member: 'u1', limit: 5, offset: 5 }], 'GET',
      '/teams/tm1/tasks?status=ongoing&workspace=Back+end&member=u1&limit=5&offset=5'],
    ['getTeamTask', ['tm1', 't1'], 'GET', '/teams/tm1/tasks/t1'],
    ['fetchTeamStats', ['tm1'], 'GET', '/teams/tm1/analytics?range=7d'],
    ['fetchTeamStats', ['tm1', 'custom', 5, 9, 'Back end'], 'GET', '/teams/tm1/analytics?range=custom&from=5&to=9&workspace=Back+end'],
    ['cloneTeamTask', ['tm1', 't1', 'ws1'], 'POST', '/teams/tm1/tasks/t1/clone', { workspaceId: 'ws1', assignee: 'human' }],
    ['cloneTeamTask', ['tm1', 't1', 'ws1', 'agent'], 'POST', '/teams/tm1/tasks/t1/clone', { workspaceId: 'ws1', assignee: 'agent' }],
    ['listTeamPanoDocs', ['tm1'], 'GET', '/teams/tm1/pano'],
    ['getTeamPanoDoc', ['tm1', 'd1'], 'GET', '/teams/tm1/pano/d1'],
    ['shareWorkspace', ['ws1', 'tm1', 4, { autoOptInSkills: false }], 'PUT', '/workspaces/ws1/shares/tm1', { autoOptInSkills: false, version: 4 }],
    ['listWorkspaceTeamSkills', ['ws1'], 'GET', '/workspaces/ws1/team-skills'],
    ['listWorkspaceTeamSkills', ['ws1', 'dep loy'], 'GET', '/workspaces/ws1/team-skills?q=dep+loy'],
    ['setWorkspaceTeamSkill', ['ws1', 's1', true], 'PUT', '/workspaces/ws1/team-skills/s1', { on: true }],
    ['setWorkspaceTeamSkill', ['ws1', 's1', false], 'PUT', '/workspaces/ws1/team-skills/s1', { on: false }],
    ['giveSkillToTeam', ['ws1', 'a b', 'tw1'], 'POST', '/workspaces/ws1/skills/a%20b/give', { teamWorkspaceId: 'tw1' }],
    ['listTeamWorkspaceSkills', ['tm1', 'tw1'], 'GET', '/teams/tm1/team-workspaces/tw1/skills'],
    ['createTeamWorkspaceSkill', ['tm1', 'tw1', '---\nname: x\n---'], 'POST', '/teams/tm1/team-workspaces/tw1/skills', { content: '---\nname: x\n---' }],
    ['importTeamWorkspaceSkills', ['tm1', 'tw1', 'https://github.com/a/b'], 'POST', '/teams/tm1/team-workspaces/tw1/skills/import', { url: 'https://github.com/a/b', overwrite: false }],
    ['importTeamWorkspaceSkills', ['tm1', 'tw1', 'u', true, ['skills/x']], 'POST', '/teams/tm1/team-workspaces/tw1/skills/import', { url: 'u', overwrite: true, skills: ['skills/x'] }],
    ['importTeamWorkspaceSkills', ['tm1', 'tw1', 'u', false, undefined, true], 'POST', '/teams/tm1/team-workspaces/tw1/skills/import', { url: 'u', overwrite: false, list: true }],
    ['getTeamWorkspaceSkill', ['tm1', 'tw1', 's1'], 'GET', '/teams/tm1/team-workspaces/tw1/skills/s1'],
    ['deleteTeamWorkspaceSkill', ['tm1', 'tw1', 's1'], 'DELETE', '/teams/tm1/team-workspaces/tw1/skills/s1'],
    ['getTeamWorkspaceSkillFile', ['tm1', 'tw1', 's1', 'scripts/a b.sh'], 'GET', '/teams/tm1/team-workspaces/tw1/skills/s1/files/scripts/a%20b.sh'],
    ['saveTeamWorkspaceSkillFile', ['tm1', 'tw1', 's1', 'SKILL.md', 'x', 3], 'PUT', '/teams/tm1/team-workspaces/tw1/skills/s1/files/SKILL.md', { content: 'x', version: 3 }],
    ['saveTeamWorkspaceSkillFile', ['tm1', 'tw1', 's1', 'new.md', 'x'], 'PUT', '/teams/tm1/team-workspaces/tw1/skills/s1/files/new.md', { content: 'x' }],
    ['deleteTeamWorkspaceSkillFile', ['tm1', 'tw1', 's1', 'new.md'], 'DELETE', '/teams/tm1/team-workspaces/tw1/skills/s1/files/new.md'],
  ];

  it.each(CASES)('%s(%j) is %s %s', async (name, args, method, path, body) => {
    expect(await api[name](...args)).toEqual({ id: 'x' });
    const [url, init = {}] = fetch.mock.calls[0];
    expect(url).toBe(`/api/v1${path}`);
    expect(init.method ?? 'GET').toBe(method);
    expect(init.body === undefined ? undefined : JSON.parse(init.body)).toEqual(body);
  });

  it('answers null for no content', async () => {
    fetch.mockResolvedValueOnce({ ok: true, status: 204 });
    expect(await api.leaveTeam('tm1', 1)).toBe(null);
  });

  it('throws the server\'s message, with its status', async () => {
    fetch.mockResolvedValueOnce({
      ok: false,
      status: 403,
      json: async () => ({ error: { message: 'this invitation is for b***@acme.com: sign in with that account', code: 403 } }),
    });
    const err = await api.acceptInvitation('tok').catch((e) => e);
    expect(err.message).toBe('this invitation is for b***@acme.com: sign in with that account');
    expect(err.status).toBe(403);
    expect(err.conflict).toBeUndefined();
  });

  it('reads a version conflict: its status, that it is one, and the team\'s version now', async () => {
    fetch.mockResolvedValueOnce({
      ok: false,
      status: 409,
      json: async () => ({ error: { message: 'someone changed this team: reload it and try again', code: 409 }, conflict: true, version: 7 }),
    });
    const err = await api.renameTeam('tm1', 'Core', 6).catch((e) => e);
    expect(err.message).toBe('someone changed this team: reload it and try again');
    expect(err.status).toBe(409);
    expect(err.conflict).toBe(true);
    expect(err.version).toBe(7);
  });

  it('throws a stale save\'s message, with its status, so the editor can offer a reload', async () => {
    fetch.mockResolvedValueOnce({
      ok: false,
      status: 409,
      json: async () => ({ error: { message: 'SKILL.md was saved since you read it', code: 409 } }),
    });
    const err = await api.saveTeamWorkspaceSkillFile('tm1', 'tw1', 's1', 'SKILL.md', 'x', 2).catch((e) => e);
    expect(err.message).toBe('SKILL.md was saved since you read it');
    expect(err.status).toBe(409);
    expect(err.conflict).toBeUndefined();
  });

  it('reads an error given as a bare string', async () => {
    fetch.mockResolvedValueOnce({ ok: false, status: 400, json: async () => ({ error: 'name is required' }) });
    const err = await api.createTeam('').catch((e) => e);
    expect(err.message).toBe('name is required');
    expect(err.status).toBe(400);
  });

  it('throws its own words when the server gives none', async () => {
    fetch.mockResolvedValueOnce({ ok: false, status: 502, json: async () => { throw new SyntaxError('not JSON'); } });
    const err = await api.listTeams().catch((e) => e);
    expect(err.message).toBe('Failed to load teams');
    expect(err.status).toBe(502);
  });
});

describe('a team file\'s link', () => {
  it('is the one the server gave, never built here', () => {
    expect(api.teamFileUrl({ id: 'a1', url: '/api/v1/teams/tm1/tasks/t1/attachments/a1' })).toBe('/api/v1/teams/tm1/tasks/t1/attachments/a1');
  });
});
