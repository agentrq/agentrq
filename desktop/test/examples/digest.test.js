import { describe, it, expect, vi } from 'vitest'

import {
  DEFAULT_CRON,
  MAX_TASKS,
  apply,
  buildPage,
  buildText,
  collect,
  mirror,
  scheduleBody,
  scheduleFor,
  summarise,
  view,
} from '../../../examples/extensions/digest/index.js'
import { parseManifest } from '../../src/main/extensions/manifest.js'
import { validateSchedule } from '../../src/main/extensions/schedules.js'
import { normaliseView } from '../../../frontend/src/composables/useExtensionView.js'
import { SCOPE, describeAsk, availableScopes, toGrant, validateGrant } from '../../../frontend/src/composables/useExtensionGrant.js'
import manifest from '../../../examples/extensions/digest/agentrq-extension.json'

/**
 * The full case: supervisor scope, a secret, a declared host, a schedule and
 * three UI surfaces. This is the example that tests the grant ladder at its top
 * rung, where "all workspaces, including ones you create later" is what the user
 * is actually agreeing to.
 */

const workspaces = [
  { id: 'ws1', name: 'Backend' },
  { id: 'ws2', name: 'Frontend' },
]

const tasks = [
  { workspaceId: 'ws1', title: 'Ship it', status: 'completed' },
  { workspaceId: 'ws1', title: 'Waiting on legal', status: 'blocked' },
  { workspaceId: 'ws2', title: 'Refactor', status: 'ongoing' },
]

function fakeSupervisor(over = {}) {
  const calls = []
  const wrap = (payload) => ({ ok: true, result: { content: [{ text: JSON.stringify(payload) }] } })
  const answers = {
    listWorkspaces: () => wrap({ workspaces }),
    listAllTasks: () => wrap({ tasks }),
    ...over,
  }
  return {
    calls,
    supervisor: async (tool, args) => {
      calls.push({ tool, args })
      return answers[tool](args)
    },
  }
}

const ctxWith = (fake, config = {}) => ({
  config,
  mcp: { supervisor: fake.supervisor },
  logger: { warn: vi.fn(), info: vi.fn() },
})

describe('the manifest', () => {
  it('is one', () => {
    expect(parseManifest(manifest).ok).toBe(true)
  })

  it('asks for the account, a secret, and names a host', () => {
    const { manifest: parsed } = parseManifest(manifest)

    expect(parsed.mcp.supervisor).toEqual(['listWorkspaces', 'listAllTasks', 'createTask'])
    expect(parsed.net).toEqual(['hooks.slack.com'])
    expect(parsed.config.find((field) => field.key === 'webhookToken').type).toBe('secret')
  })
})

describe('the grant ladder at its top rung', () => {
  const ask = () => describeAsk(parseManifest(manifest).manifest)

  it('offers all three rungs when there is a workspace in context', () => {
    expect(availableScopes(ask(), { workspaceId: 'ws1' })).toEqual([SCOPE.workspace, SCOPE.selected, SCOPE.supervisor])
  })

  // The screen extensions are installed from belongs to no workspace, so
  // "this workspace only" has no referent there and is not offered — it used to
  // be, and produced a grant with an empty list that refused everything.
  it('drops the rung whose name means nothing on the install screen', () => {
    expect(availableScopes(ask())).toEqual([SCOPE.selected, SCOPE.supervisor])
  })

  // "Supervisor, but only this workspace" is the same grant with a misleading
  // label: listAllTasks spans the platform whatever list sits beside it.
  it('carries no workspace list at the supervisor rung', () => {
    const grant = toGrant(ask(), { scope: SCOPE.supervisor, workspaces: ['ws1'], workspaceId: 'ws1' })

    expect(grant.workspaces).toEqual([])
    expect(grant.includesFutureWorkspaces).toBe(true)
    expect(grant.tools.supervisor).toEqual(['listWorkspaces', 'listAllTasks', 'createTask'])
  })

  it('is a coherent choice at every rung it offers, wherever it is offered from', () => {
    for (const workspaceId of ['ws1', '']) {
      for (const scope of availableScopes(ask(), { workspaceId })) {
        const choice = { scope, workspaces: ['ws1'], workspaceId }
        expect(validateGrant(ask(), choice).ok, `${scope} from ${workspaceId || 'the sidebar'}`).toBe(true)
      }
    }
  })

  // Not a permission, and must never be rendered as one.
  it('presents the host as something its author says, not something enforced', () => {
    expect(ask().networkClaim).toBe('Its author says it contacts hooks.slack.com.')
    expect(ask().machineAccess).toContain('full access to your computer')
  })
})

describe('summarise', () => {
  it('groups by workspace and puts the most stuck first', () => {
    const summary = summarise(workspaces, tasks)

    expect(summary[0]).toMatchObject({ workspace: 'Backend', completed: 1, ongoing: 0 })
    expect(summary[0].blocked).toEqual(['Waiting on legal'])
    expect(summary[1].workspace).toBe('Frontend')
  })

  it('sorts by name when nothing is blocked, so the order does not wander', () => {
    const quiet = [
      { workspaceId: 'ws2', title: 'a', status: 'ongoing' },
      { workspaceId: 'ws1', title: 'b', status: 'ongoing' },
    ]
    expect(summarise(workspaces, quiet).map((entry) => entry.workspace)).toEqual(['Backend', 'Frontend'])
  })

  // Dropping the group would silently lose the tasks in it.
  it('keeps tasks whose workspace has since been deleted', () => {
    const summary = summarise([], [{ workspaceId: 'gone', title: 'x', status: 'ongoing' }])
    expect(summary[0].workspace).toContain('no longer exists')
  })

  it('is empty when nothing happened', () => {
    expect(summarise(workspaces, [])).toEqual([])
  })
})

describe('buildPage and buildText', () => {
  it('says the same thing two ways', () => {
    const summary = summarise(workspaces, tasks)

    expect(buildPage(summary).nodes[0].label).toBe('Backend')
    expect(buildText(summary)).toContain('*Backend* — 1 completed, 0 ongoing')
    expect(buildText(summary)).toContain('Blocked: Waiting on legal')
  })

  it('says nothing happened rather than showing an empty page', () => {
    expect(buildPage([]).nodes[0]).toMatchObject({ type: 'empty' })
    expect(buildText([])).toContain('Nothing happened')
  })

  it('marks a clean workspace as clean rather than leaving a gap', () => {
    const summary = summarise(workspaces, [tasks[0]])

    expect(buildPage(summary).nodes[0].children.at(-1)).toMatchObject({ type: 'text', tone: 'positive' })
    expect(buildText(summary)).not.toContain('Blocked')
  })

  it('produces views the renderer will actually draw', () => {
    for (const summary of [[], summarise(workspaces, tasks), summarise(workspaces, [tasks[0]])]) {
      const result = normaliseView(buildPage(summary))
      expect(result.ok, result.reason).toBe(true)
    }
  })
})

describe('collect', () => {
  it('asks for the workspaces and every recent task', async () => {
    const fake = fakeSupervisor()

    const summary = await collect(ctxWith(fake))

    expect(fake.calls.map((call) => call.tool)).toEqual(['listWorkspaces', 'listAllTasks'])
    expect(fake.calls[1].args.limit).toBe(MAX_TASKS)
    expect(summary[0].workspace).toBe('Backend')
  })

  it('carries a refusal back rather than throwing', async () => {
    const refused = fakeSupervisor({
      listWorkspaces: () => ({ ok: false, reason: 'This extension was not granted access to all workspaces.' }),
    })
    expect((await collect(ctxWith(refused))).refused).toContain('not granted')

    const halfway = fakeSupervisor({ listAllTasks: () => ({ ok: false, reason: 'refused' }) })
    expect((await collect(ctxWith(halfway))).refused).toBe('refused')
  })

  it('holds up when an answer cannot be read', async () => {
    const fake = fakeSupervisor({ listWorkspaces: () => ({ ok: true, result: 'not json' }) })
    expect(await collect(ctxWith(fake))).toEqual(summarise([], tasks))
  })
})

describe('view', () => {
  it('draws the digest', async () => {
    const page = await view(ctxWith(fakeSupervisor()))
    expect(page.nodes[0].label).toBe('Backend')
  })

  it('draws the refusal, in the same shape', async () => {
    const fake = fakeSupervisor({ listWorkspaces: () => ({ ok: false, reason: 'refused' }) })

    const page = await view(ctxWith(fake))

    expect(page.nodes[0]).toMatchObject({ tone: 'critical', value: 'refused' })
    expect(normaliseView(page).ok).toBe(true)
  })
})

describe('mirror', () => {
  const config = { webhookPath: 'T000/B000', webhookToken: 'xoxb-secret' }

  it('posts the digest to the configured webhook', async () => {
    const fetch = vi.fn(async () => ({ ok: true, status: 200 }))

    const result = await mirror({ config }, 'the digest', { fetch })

    expect(result.ok).toBe(true)
    expect(fetch.mock.calls[0][0]).toBe('https://hooks.slack.com/services/T000/B000/xoxb-secret')
    expect(JSON.parse(fetch.mock.calls[0][1].body).text).toBe('the digest')
  })

  // Half-configured looks configured, which is worse than not sending.
  it('refuses to send to half an address', async () => {
    const fetch = vi.fn()

    expect((await mirror({ config: { webhookPath: 'T000/B000' } }, 'x', { fetch })).ok).toBe(false)
    expect((await mirror({ config: { webhookToken: 'xoxb' } }, 'x', { fetch })).ok).toBe(false)
    expect((await mirror({ config: {} }, 'x', { fetch })).ok).toBe(false)
    expect((await mirror({}, 'x', { fetch })).ok).toBe(false)
    expect(fetch).not.toHaveBeenCalled()
  })

  // The URL is the secret, so no failure path may repeat it.
  it('never puts the token in a failure message', async () => {
    const failed = await mirror({ config }, 'x', { fetch: async () => ({ ok: false, status: 403 }) })
    const threw = await mirror({ config }, 'x', {
      fetch: async () => {
        throw new Error('connect ECONNREFUSED https://hooks.slack.com/services/T000/B000/xoxb-secret')
      },
    })

    expect(failed.reason).toBe('The webhook answered 403.')
    expect(threw.reason).toBe('The webhook could not be reached.')
    for (const { reason } of [failed, threw]) expect(reason).not.toContain('xoxb-secret')
  })
})

describe('scheduleFor', () => {
  it('declares a cron task in the configured workspace', () => {
    const schedule = scheduleFor({ workspaceId: 'ws1', cron: '0 6 * * *' })

    expect(schedule).toMatchObject({ kind: 'task', workspaceId: 'ws1', cron: '0 6 * * *', assignee: 'agent' })
    // And is one the reconciler will accept, which is the only test that counts.
    expect(validateSchedule({ id: 'daily', value: schedule }).ok).toBe(true)
  })

  it('falls back to a sensible hour rather than to nothing', () => {
    expect(scheduleFor({ workspaceId: 'ws1' }).cron).toBe(DEFAULT_CRON)
    expect(scheduleFor({ workspaceId: 'ws1', cron: '  ' }).cron).toBe(DEFAULT_CRON)
  })

  it('declares nothing at all without a workspace to post in', () => {
    expect(scheduleFor({})).toBeNull()
    expect(scheduleFor(undefined)).toBeNull()
  })

  it('tells the agent to reply on the task when there is no webhook', () => {
    expect(scheduleBody({})).toContain('Post the summary as your reply')
    expect(scheduleBody({ webhookPath: 'T000/B000' })).not.toContain('Post the summary as your reply')
  })
})

describe('apply', () => {
  const spy = () => {
    const ui = []
    const schedules = []
    const logger = { warn: vi.fn(), info: vi.fn() }
    return { ui, schedules, logger, ctx: { ui: { add: (e) => ui.push(e) }, schedules: { add: (e) => schedules.push(e) }, logger, config: {}, mcp: { supervisor: async () => ({ ok: true, result: '{}' }) } } }
  }

  it('registers a page, a header action, a menu item and the schedule', () => {
    const { ui, schedules, ctx } = spy()

    apply(ctx, { workspaceId: 'ws1' })

    expect(ui.map((entry) => entry.surface)).toEqual(['page', 'workspace-action', 'task-menu'])
    expect(schedules[0]).toMatchObject({ id: 'daily' })
  })

  // A permanent row on every task is a menu nobody reads.
  it('shows its task item only where it means something', () => {
    const { ui, ctx } = spy()
    apply(ctx, { workspaceId: 'ws1' })
    const item = ui.find((entry) => entry.surface === 'task-menu')

    expect(item.when({ status: 'completed' })).toBe(true)
    expect(item.when({ status: 'ongoing' })).toBe(false)
    expect(item.when(undefined)).toBe(false)
  })

  it('says why it registered no schedule rather than registering none quietly', () => {
    const { schedules, logger, ctx } = spy()

    apply(ctx, {})

    expect(schedules).toEqual([])
    expect(logger.warn).toHaveBeenCalledWith(expect.stringContaining('No workspace is configured'))
  })

  it('wires the page and the action to the same digest', async () => {
    const fake = fakeSupervisor()
    const { ui, ctx } = spy()
    ctx.mcp = { supervisor: fake.supervisor }

    apply(ctx, { workspaceId: 'ws1' })
    const page = await ui.find((entry) => entry.surface === 'page').view()
    const action = await ui.find((entry) => entry.surface === 'workspace-action').run()

    expect(page).toEqual(action)
  })
})
