// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

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
    ['renameTeam', ['tm1', 'Core'], 'PATCH', '/teams/tm1', { name: 'Core' }],
    ['deleteTeam', ['tm1'], 'DELETE', '/teams/tm1'],
    ['listTeamMembers', ['tm1'], 'GET', '/teams/tm1/members'],
    ['setTeamMemberRole', ['tm1', 'u1', 'admin'], 'PATCH', '/teams/tm1/members/u1', { role: 'admin' }],
    ['removeTeamMember', ['tm1', 'u1'], 'DELETE', '/teams/tm1/members/u1'],
    ['leaveTeam', ['tm1'], 'POST', '/teams/tm1/leave'],
    ['listTeamInvitations', ['tm1'], 'GET', '/teams/tm1/invitations'],
    ['createTeamInvitation', ['tm1', 'd@x.io', 'admin'], 'POST', '/teams/tm1/invitations', { email: 'd@x.io', role: 'admin' }],
    ['revokeTeamInvitation', ['tm1', 'inv1'], 'DELETE', '/teams/tm1/invitations/inv1'],
    ['listMyInvitations', [], 'GET', '/invitations'],
    ['previewInvitation', ['tok'], 'POST', '/invitations/preview', { token: 'tok' }],
    ['acceptInvitation', ['tok'], 'POST', '/invitations/accept', { token: 'tok' }],
    ['acceptInvitationById', ['inv1'], 'POST', '/invitations/inv1/accept'],
    ['declineInvitation', ['inv1'], 'POST', '/invitations/inv1/decline'],
    ['listWorkspaceShares', ['ws1'], 'GET', '/workspaces/ws1/shares'],
    ['shareWorkspace', ['ws1', 'tm1', 'Backend'], 'PUT', '/workspaces/ws1/shares/tm1', { group: 'Backend' }],
    ['unshareWorkspace', ['ws1', 'tm1'], 'DELETE', '/workspaces/ws1/shares/tm1'],
  ];

  it.each(CASES)('%s is %s %s', async (name, args, method, path, body) => {
    expect(await api[name](...args)).toEqual({ id: 'x' });
    const [url, init = {}] = fetch.mock.calls[0];
    expect(url).toBe(`/api/v1${path}`);
    expect(init.method ?? 'GET').toBe(method);
    expect(init.body === undefined ? undefined : JSON.parse(init.body)).toEqual(body);
  });

  it('answers null for no content', async () => {
    fetch.mockResolvedValueOnce({ ok: true, status: 204 });
    expect(await api.leaveTeam('tm1')).toBe(null);
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
  });

  it('throws its own words when the server gives none', async () => {
    fetch.mockResolvedValueOnce({ ok: false, status: 502, json: async () => { throw new SyntaxError('not JSON'); } });
    const err = await api.listTeams().catch((e) => e);
    expect(err.message).toBe('Failed to load teams');
    expect(err.status).toBe(502);
  });
});
