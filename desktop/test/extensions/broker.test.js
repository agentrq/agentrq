// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi } from 'vitest'

import { SCOPE, createBroker, permits } from '../../src/main/extensions/broker.js'

/**
 * The one real boundary in this feature, and a narrow one: it does not contain
 * the extension — this is trusted Node — it keeps the credential out of
 * extension code. That matters because a workspace token and a supervisor
 * session outlive any single extension and reach the whole account.
 */

const grant = (over = {}) => ({
  scope: SCOPE.workspace,
  workspaces: ['ws1'],
  tools: { workspace: ['task.get', 'task.reply'], supervisor: [] },
  ...over,
})

function build(over = {}) {
  const record = vi.fn()
  const logger = { warn: vi.fn() }
  const broker = createBroker({
    callWorkspace: vi.fn(async () => ({ id: 't1' })),
    callSupervisor: vi.fn(async () => ({ tasks: [] })),
    record,
    logger,
    ...over,
  })
  return { broker, record, logger }
}

describe('permits', () => {
  it('allows a granted tool on a granted workspace', () => {
    expect(permits(grant(), { surface: 'workspace', tool: 'task', action: 'get', workspaceId: 'ws1' }).ok).toBe(true)
  })

  it('names the surface when refusing a tool', () => {
    // The same tool name can exist on both, so "not allowed" would leave an
    // author guessing which one they asked for.
    const { ok, reason } = permits(grant(), { surface: 'workspace', tool: 'deleteTask', workspaceId: 'ws1' })

    expect(ok).toBe(false)
    expect(reason).toBe('This extension may not call "deleteTask" on the workspace server.')
  })

  /**
   * The two are different situations for whoever reads them, and saying the
   * first when the second is true sends them looking at the wrong thing.
   *
   * An empty list came from the install screen offering "this workspace only"
   * on a screen belonging to no workspace: the grant was written with nothing
   * in it, and every call was then refused as though a particular workspace had
   * been left out.
   */
  it('treats a grant that never recorded a list as an empty one', () => {
    // A record written by an older version has no `workspaces` at all.
    const legacy = permits({ scope: SCOPE.workspace, tools: { workspace: ['task.get'], supervisor: [] } }, {
      surface: 'workspace',
      tool: 'task',
      action: 'get',
      workspaceId: 'ws1',
    })

    expect(legacy.reason).toContain('not granted access to any workspace')
  })

  it('tells an empty grant apart from a workspace left out of one', () => {
    const nothing = permits(grant({ workspaces: [] }), { surface: 'workspace', tool: 'task', action: 'get', workspaceId: 'ws1' })
    const elsewhere = permits(grant(), { surface: 'workspace', tool: 'task', action: 'get', workspaceId: 'ws2' })

    expect(nothing.reason).toContain('not granted access to any workspace')
    // And says what to do about it, because the user cannot widen a grant.
    expect(nothing.reason).toContain('install it again')
    expect(elsewhere.reason).toBe('This extension was not granted access to that workspace.')
  })

  it('refuses a workspace outside the grant', () => {
    // A tool being allowed does not make it allowed everywhere.
    const denied = permits(grant(), { surface: 'workspace', tool: 'task', action: 'get', workspaceId: 'ws2' })

    expect(denied.reason).toBe('This extension was not granted access to that workspace.')
  })

  it('refuses a workspace call that names no workspace', () => {
    expect(permits(grant(), { surface: 'workspace', tool: 'task', action: 'get' }).reason).toBe(
      'This call names no workspace.',
    )
  })

  it('refuses the supervisor to anything below that rung', () => {
    const asked = { surface: 'supervisor', tool: 'task', action: 'listAll' }
    const withTool = grant({ tools: { workspace: [], supervisor: ['task.listAll'] } })

    expect(permits(withTool, asked).reason).toBe('This extension was not granted access to all workspaces.')
    expect(permits(grant({ ...withTool, scope: SCOPE.selected }), asked).ok).toBe(false)
  })

  it('lets the supervisor rung reach a workspace no list mentions', () => {
    // Because that rung *is* every workspace — task.listAll spans the platform
    // and task.create(workspaceId) reaches anywhere.
    const supervisor = grant({ scope: SCOPE.supervisor, workspaces: [] })

    expect(permits(supervisor, { surface: 'workspace', tool: 'task', action: 'get', workspaceId: 'anything' }).ok).toBe(
      true,
    )
  })

  it('still checks the tool list at the supervisor rung', () => {
    // The widest scope is not a blanket permission.
    const supervisor = grant({ scope: SCOPE.supervisor, tools: { workspace: [], supervisor: [] } })

    expect(permits(supervisor, { surface: 'supervisor', tool: 'task', action: 'listAll' }).ok).toBe(false)
  })

  it('refuses when there is no grant to consult', () => {
    expect(permits(undefined, { surface: 'workspace', tool: 'task', action: 'get' }).ok).toBe(false)
  })

  it('holds an action tool to the action a call names, and says which', () => {
    const { ok, reason } = permits(grant(), { surface: 'workspace', tool: 'task', action: 'create', workspaceId: 'ws1' })

    expect(ok).toBe(false)
    expect(reason).toBe('This extension may not call "task.create" on the workspace server.')
  })

  it('holds a tool that takes no action to its own name', () => {
    // A tool this build does not know as an action tool, like one a newer
    // server added, is granted by name.
    const named = grant({ tools: { workspace: ['newTool'], supervisor: [] } })

    expect(permits(named, { surface: 'workspace', tool: 'newTool', workspaceId: 'ws1' }).ok).toBe(true)
  })

  it('lets the bare tool grant every action', () => {
    const everything = grant({ tools: { workspace: ['task'], supervisor: [] } })

    expect(permits(everything, { surface: 'workspace', tool: 'task', action: 'create', workspaceId: 'ws1' }).ok).toBe(true)
  })

  it('allows the help tool to any extension granted something on the surface, since it only reads', () => {
    const help = { surface: 'workspace', tool: 'help', workspaceId: 'ws1' }

    expect(permits(grant(), help).ok).toBe(true)
    expect(permits(grant({ tools: { workspace: [], supervisor: ['task.listAll'] } }), help).reason).toBe(
      'This extension may not call "help" on the workspace server.',
    )
    // Still only in a workspace it was granted.
    expect(permits(grant(), { ...help, workspaceId: 'ws2' }).ok).toBe(false)
  })

  it('answers help to an extension granted any action of the tool, and to no other', () => {
    const help = { surface: 'workspace', tool: 'task', action: 'help', workspaceId: 'ws1' }

    expect(permits(grant(), help).ok).toBe(true)
    expect(permits(grant({ tools: { workspace: ['pano.read'], supervisor: [] } }), help).ok).toBe(false)
  })
})

describe('createBroker', () => {
  /**
   * Found against a real server, which is the only place it could be.
   *
   * The per-workspace endpoint already names the workspace, so its tools take
   * no `workspaceId` — and the go-sdk validates strictly, refusing the whole
   * call with `unexpected additional properties ["workspaceId"]`. An extension
   * author would have had no way to explain that: they were doing the only
   * thing this broker's shape allowed.
   */
  it('keeps the workspace out of the arguments the workspace server sees', async () => {
    const callWorkspace = vi.fn(async () => ({}))
    const { broker } = build({ callWorkspace })
    broker.setGrant('linear', grant())

    await broker
      .clientFor('linear')
      .workspace('task', { workspaceId: 'ws1', action: 'get', payload: { workspaceId: 'ws1', taskId: 't1' } })

    // Out of the payload as well, where an action tool's fields are.
    expect(callWorkspace.mock.calls[0][0].args).toEqual({ action: 'get', payload: { taskId: 't1' } })
    // And is still what decided the call was allowed, and where it goes.
    expect(callWorkspace.mock.calls[0][0].workspaceId).toBe('ws1')
  })

  // One endpoint for the whole account, so there it is a real argument:
  // `task.list(workspaceId)` means exactly what it says. An action tool takes
  // it in its payload, so that is where it goes.
  it('keeps it for the supervisor, where it is an argument', async () => {
    const callSupervisor = vi.fn(async () => ({}))
    const { broker } = build({ callSupervisor })
    broker.setGrant('digest', {
      scope: SCOPE.supervisor,
      workspaces: [],
      tools: { workspace: [], supervisor: ['task.list'] },
    })
    const client = broker.clientFor('digest')

    await client.supervisor('task', { workspaceId: 'ws1', action: 'list', payload: { limit: 5 } })
    await client.supervisor('help', { workspaceId: 'ws1', action: 'task' })

    expect(callSupervisor).toHaveBeenNthCalledWith(1, {
      tool: 'task',
      args: { action: 'list', payload: { limit: 5, workspaceId: 'ws1' } },
    })
    // help is no action tool: its own arguments, the workspace beside them.
    expect(callSupervisor).toHaveBeenNthCalledWith(2, { tool: 'help', args: { workspaceId: 'ws1', action: 'task' } })
  })

  it('makes the call and hands back the result', async () => {
    const callWorkspace = vi.fn(async () => ({ id: 't1' }))
    const { broker } = build({ callWorkspace })
    broker.setGrant('linear', grant())

    const result = await broker.clientFor('linear').workspace('getTask', { workspaceId: 'ws1', taskId: 't1' })

    expect(result).toEqual({ ok: true, result: { id: 't1' } })
    // The workspace goes to the *caller*, which turns it into an endpoint, and
    // not to the tool — the per-workspace server has no such parameter and
    // refuses one. See the note at the top of broker.js.
    // Called by its old name, as an extension written before `task` does, and
    // made as the action it became.
    expect(callWorkspace).toHaveBeenCalledWith({
      workspaceId: 'ws1',
      tool: 'task',
      args: { action: 'get', payload: { taskId: 't1' } },
    })
  })

  it('sends an action with no payload an empty one', async () => {
    const callWorkspace = vi.fn(async () => ({}))
    const { broker } = build({ callWorkspace })
    broker.setGrant('linear', grant())

    await broker.clientFor('linear').workspace('task', { workspaceId: 'ws1', action: 'help' })

    expect(callWorkspace.mock.calls[0][0].args).toEqual({ action: 'help', payload: {} })
  })

  it('knows no action tools on a surface that does not exist', () => {
    expect(permits(grant(), { surface: 'elsewhere', tool: 'task', action: 'get' }).reason).toBe(
      'This extension may not call "task" on the elsewhere server.',
    )
  })

  it('calls an old reply as task.reply, its chatId as the taskId', async () => {
    const callWorkspace = vi.fn(async () => ({}))
    const { broker } = build({ callWorkspace })
    broker.setGrant('linear', grant({ tools: { workspace: ['reply'], supervisor: [] } }))

    const result = await broker.clientFor('linear').workspace('reply', { workspaceId: 'ws1', chatId: 't1', text: 'hi' })

    expect(result.ok).toBe(true)
    expect(callWorkspace.mock.calls[0][0].args).toEqual({ action: 'reply', payload: { taskId: 't1', text: 'hi' } })
  })

  it('hands the extension nothing that could be a credential', async () => {
    // The whole point: an extension cannot leak a key it was never given.
    const { broker } = build()
    broker.setGrant('linear', grant())

    const client = broker.clientFor('linear')

    expect(Object.keys(client).sort()).toEqual(['supervisor', 'workspace'])
    expect(JSON.stringify(client)).not.toMatch(/token|secret|bearer|session/i)
  })

  it('refuses before calling anything', async () => {
    const callWorkspace = vi.fn()
    const { broker } = build({ callWorkspace })
    broker.setGrant('linear', grant())

    const denied = await broker.clientFor('linear').workspace('deleteTask', { workspaceId: 'ws1' })

    expect(denied.ok).toBe(false)
    expect(callWorkspace).not.toHaveBeenCalled()
  })

  it('records refusals as well as calls', async () => {
    // A refusal is the interesting half of an audit trail: it is what says an
    // extension is asking for more than it was given.
    const { broker, record } = build()
    broker.setGrant('linear', grant())
    const client = broker.clientFor('linear')

    await client.workspace('getTask', { workspaceId: 'ws1' })
    await client.workspace('deleteTask', { workspaceId: 'ws1' })

    expect(record).toHaveBeenNthCalledWith(1, expect.objectContaining({ allowed: true, tool: 'task', action: 'get' }))
    expect(record).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({ allowed: false, tool: 'deleteTask', name: 'linear' }),
    )
  })

  it('refuses an extension with no grant at all', async () => {
    const { broker } = build()

    const denied = await broker.clientFor('stranger').workspace('getTask', { workspaceId: 'ws1' })

    expect(denied.reason).toBe('This extension has not been granted any access.')
  })

  it('closes over the extension, so one cannot ask on another’s behalf', async () => {
    const { broker } = build()
    broker.setGrant('linear', grant())
    broker.setGrant('standup', grant({ tools: { workspace: [], supervisor: [] } }))

    const denied = await broker.clientFor('standup').workspace('getTask', { workspaceId: 'ws1' })

    expect(denied.ok).toBe(false)
  })

  it('passes the server’s own message through when a call fails', async () => {
    // An author debugging a malformed call needs what the server said, not a
    // sentence invented here.
    const { broker } = build({
      callWorkspace: vi.fn(async () => {
        throw new Error('task not found')
      }),
    })
    broker.setGrant('linear', grant())

    expect((await broker.clientFor('linear').workspace('getTask', { workspaceId: 'ws1' })).reason).toBe(
      'task not found',
    )
  })

  it('says something when a failure carries no message', async () => {
    const { broker } = build({ callWorkspace: vi.fn(async () => { throw {} }) })
    broker.setGrant('linear', grant())

    expect((await broker.clientFor('linear').workspace('getTask', { workspaceId: 'ws1' })).reason).toBe(
      'The call failed.',
    )
  })

  it('calls the supervisor without a workspace, at that rung', async () => {
    const callSupervisor = vi.fn(async () => ({ tasks: [] }))
    const { broker } = build({ callSupervisor })
    broker.setGrant('linear', grant({ scope: SCOPE.supervisor, tools: { workspace: [], supervisor: ['task.listAll'] } }))

    const result = await broker.clientFor('linear').supervisor('listAllTasks', {})

    expect(result.ok).toBe(true)
    expect(callSupervisor).toHaveBeenCalledWith({ tool: 'task', args: { action: 'listAll', payload: {} } })
  })

  it('takes a copy of the grant, so a caller cannot widen it afterwards', () => {
    const { broker } = build()
    const original = grant()
    broker.setGrant('linear', original)

    original.tools.workspace.push('deleteTask')
    original.workspaces.push('ws2')

    expect(broker.grantFor('linear').tools.workspace).toEqual(['task.get', 'task.reply'])
    expect(broker.grantFor('linear').workspaces).toEqual(['ws1'])
  })

  it('reads a grant saved before the task tools merged as the actions they became', () => {
    const { broker } = build()
    broker.setGrant('digest', grant({ tools: { workspace: ['getTask', 'task.get'], supervisor: ['listAllTasks', 'listWorkspaces'] } }))

    expect(broker.grantFor('digest').tools).toEqual({ workspace: ['task.get'], supervisor: ['task.listAll', 'workspace.list'] })
  })

  it('hands out a copy of the grant too', () => {
    const { broker } = build()
    broker.setGrant('linear', grant())

    broker.grantFor('linear').workspaces.push('ws2')

    expect(broker.grantFor('linear').workspaces).toEqual(['ws1'])
  })

  it('defaults a sparse grant to the narrowest thing', async () => {
    const { broker } = build()
    broker.setGrant('linear', {})

    expect(broker.grantFor('linear')).toEqual({
      scope: SCOPE.workspace,
      workspaces: [],
      tools: { workspace: [], supervisor: [] },
      // The narrowest thing here is being asked nothing at all: a grant that
      // says nothing about reviewing tool calls has not consented to it.
      hooks: { toolCall: 'none' },
    })
  })

  it('forgets a grant on revoke', async () => {
    const { broker } = build()
    broker.setGrant('linear', grant())

    broker.revoke('linear')

    expect(broker.grantFor('linear')).toBeNull()
    expect((await broker.clientFor('linear').workspace('getTask', { workspaceId: 'ws1' })).ok).toBe(false)
  })

  it('logs through a bare console when given no logger', async () => {
    const broker = createBroker({ callWorkspace: vi.fn(), callSupervisor: vi.fn() })
    broker.setGrant('linear', grant())

    expect((await broker.clientFor('linear').workspace('nope', { workspaceId: 'ws1' })).ok).toBe(false)
  })
})
