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
  tools: { workspace: ['getTask', 'reply'], supervisor: [] },
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
    expect(permits(grant(), { surface: 'workspace', tool: 'getTask', workspaceId: 'ws1' }).ok).toBe(true)
  })

  it('names the surface when refusing a tool', () => {
    // The same tool name can exist on both, so "not allowed" would leave an
    // author guessing which one they asked for.
    const { ok, reason } = permits(grant(), { surface: 'workspace', tool: 'deleteTask', workspaceId: 'ws1' })

    expect(ok).toBe(false)
    expect(reason).toBe('This extension may not call "deleteTask" on the workspace server.')
  })

  it('refuses a workspace outside the grant', () => {
    // A tool being allowed does not make it allowed everywhere.
    const denied = permits(grant(), { surface: 'workspace', tool: 'getTask', workspaceId: 'ws2' })

    expect(denied.reason).toBe('This extension was not granted access to that workspace.')
  })

  it('refuses a workspace call that names no workspace', () => {
    expect(permits(grant(), { surface: 'workspace', tool: 'getTask' }).reason).toBe(
      'This call names no workspace.',
    )
  })

  it('refuses the supervisor to anything below that rung', () => {
    const asked = { surface: 'supervisor', tool: 'listAllTasks' }
    const withTool = grant({ tools: { workspace: [], supervisor: ['listAllTasks'] } })

    expect(permits(withTool, asked).reason).toBe('This extension was not granted access to all workspaces.')
    expect(permits(grant({ ...withTool, scope: SCOPE.selected }), asked).ok).toBe(false)
  })

  it('lets the supervisor rung reach a workspace no list mentions', () => {
    // Because that rung *is* every workspace — listAllTasks spans the platform
    // and createTask(workspaceId) reaches anywhere.
    const supervisor = grant({ scope: SCOPE.supervisor, workspaces: [] })

    expect(permits(supervisor, { surface: 'workspace', tool: 'getTask', workspaceId: 'anything' }).ok).toBe(
      true,
    )
  })

  it('still checks the tool list at the supervisor rung', () => {
    // The widest scope is not a blanket permission.
    const supervisor = grant({ scope: SCOPE.supervisor, tools: { workspace: [], supervisor: [] } })

    expect(permits(supervisor, { surface: 'supervisor', tool: 'listAllTasks' }).ok).toBe(false)
  })

  it('refuses when there is no grant to consult', () => {
    expect(permits(undefined, { surface: 'workspace', tool: 'getTask' }).ok).toBe(false)
  })
})

describe('createBroker', () => {
  it('makes the call and hands back the result', async () => {
    const callWorkspace = vi.fn(async () => ({ id: 't1' }))
    const { broker } = build({ callWorkspace })
    broker.setGrant('linear', grant())

    const result = await broker.clientFor('linear').workspace('getTask', { workspaceId: 'ws1', taskId: 't1' })

    expect(result).toEqual({ ok: true, result: { id: 't1' } })
    expect(callWorkspace).toHaveBeenCalledWith({
      workspaceId: 'ws1',
      tool: 'getTask',
      args: { workspaceId: 'ws1', taskId: 't1' },
    })
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

    expect(record).toHaveBeenNthCalledWith(1, expect.objectContaining({ allowed: true, tool: 'getTask' }))
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
    broker.setGrant('linear', grant({ scope: SCOPE.supervisor, tools: { workspace: [], supervisor: ['listAllTasks'] } }))

    const result = await broker.clientFor('linear').supervisor('listAllTasks', {})

    expect(result.ok).toBe(true)
    expect(callSupervisor).toHaveBeenCalledWith({ tool: 'listAllTasks', args: {} })
  })

  it('takes a copy of the grant, so a caller cannot widen it afterwards', () => {
    const { broker } = build()
    const original = grant()
    broker.setGrant('linear', original)

    original.tools.workspace.push('deleteTask')
    original.workspaces.push('ws2')

    expect(broker.grantFor('linear').tools.workspace).toEqual(['getTask', 'reply'])
    expect(broker.grantFor('linear').workspaces).toEqual(['ws1'])
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
