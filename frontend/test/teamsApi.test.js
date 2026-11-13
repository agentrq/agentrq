// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import * as api from '../src/api.js'

describe('the team API', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ id: 'x' }) }))
  })
  afterEach(() => vi.unstubAllGlobals())

  // name, arguments, method, path, body
  const CASES = [
    ['fetchTeams', [], 'GET', '/teams'],
    ['createTeam', ['Core'], 'POST', '/teams', { name: 'Core' }],
    ['getTeam', ['tm1'], 'GET', '/teams/tm1'],
    ['renameTeam', ['tm1', 'Core'], 'PATCH', '/teams/tm1', { name: 'Core' }],
    ['deleteTeam', ['tm1'], 'DELETE', '/teams/tm1'],
    ['fetchTeamMembers', ['tm1'], 'GET', '/teams/tm1/members'],
    ['setTeamMemberRole', ['tm1', 'u1', 'admin'], 'PATCH', '/teams/tm1/members/u1', { role: 'admin' }],
    ['removeTeamMember', ['tm1', 'u1'], 'DELETE', '/teams/tm1/members/u1'],
    ['leaveTeam', ['tm1'], 'POST', '/teams/tm1/leave'],
    ['fetchTeamInvitations', ['tm1'], 'GET', '/teams/tm1/invitations'],
    ['createTeamInvitation', ['tm1', { email: 'd@x.io' }], 'POST', '/teams/tm1/invitations', { email: 'd@x.io', role: 'member' }],
    ['createTeamInvitation', ['tm1', { email: 'd@x.io', role: 'admin' }], 'POST', '/teams/tm1/invitations', { email: 'd@x.io', role: 'admin' }],
    ['revokeTeamInvitation', ['tm1', 'inv1'], 'DELETE', '/teams/tm1/invitations/inv1'],
    ['fetchMyInvitations', [], 'GET', '/invitations'],
    ['previewTeamInvitation', ['tok'], 'POST', '/invitations/preview', { token: 'tok' }],
    ['acceptTeamInvitation', ['tok'], 'POST', '/invitations/accept', { token: 'tok' }],
    ['acceptTeamInvitationById', ['inv1'], 'POST', '/invitations/inv1/accept'],
    ['declineTeamInvitation', ['inv1'], 'POST', '/invitations/inv1/decline'],
    ['fetchWorkspaceShares', ['ws1'], 'GET', '/workspaces/ws1/shares'],
    ['shareWorkspace', ['ws1', 'tm1'], 'PUT', '/workspaces/ws1/shares/tm1', { group: '' }],
    ['shareWorkspace', ['ws1', 'tm1', 'Backend'], 'PUT', '/workspaces/ws1/shares/tm1', { group: 'Backend' }],
    ['unshareWorkspace', ['ws1', 'tm1'], 'DELETE', '/workspaces/ws1/shares/tm1'],
  ]

  it.each(CASES)('%s(%j) is %s %s', async (name, args, method, path, body) => {
    expect(await api[name](...args)).toEqual({ id: 'x' })
    const [url, init = {}] = fetch.mock.calls[0]
    expect(url).toBe(`/api/v1${path}`)
    expect(init.method ?? 'GET').toBe(method)
    expect(init.body === undefined ? undefined : JSON.parse(init.body)).toEqual(body)
  })

  it('answers true for an answer with no body', async () => {
    fetch.mockResolvedValueOnce({ ok: true, status: 204 })
    expect(await api.leaveTeam('tm1')).toBe(true)
  })

  it('throws the server refusal in its own words', async () => {
    fetch.mockResolvedValueOnce({
      ok: false,
      status: 429,
      json: async () => ({ error: { message: 'this team has reached its limit of 10 invitations per day', code: 429 } }),
    })
    await expect(api.createTeamInvitation('tm1', { email: 'd@x.io' })).rejects.toThrow('limit of 10 invitations per day')
  })

  it('throws its own words for a server failure', async () => {
    fetch.mockResolvedValueOnce({ ok: false, status: 500, json: async () => ({ error: { message: 'internal server error' } }) })
    await expect(api.fetchTeams()).rejects.toThrow('Failed to fetch teams')
  })
})
