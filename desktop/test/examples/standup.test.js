import { describe, it, expect } from 'vitest'

import {
  DEFAULT_HOURS,
  apply,
  buildPage,
  collect,
  hoursFrom,
  movedSince,
  toneFor,
} from '../../../examples/extensions/standup/index.js'
import { parseManifest } from '../../src/main/extensions/manifest.js'
import { checkShortcuts } from '../../src/main/extensions/shortcuts.js'
import { normaliseView } from '../../../frontend/src/composables/useExtensionView.js'
import manifest from '../../../examples/extensions/standup/agentrq-extension.json'

/**
 * The workspace case: two tools, one config field, a page and a shortcut.
 *
 * The test that earns its place here is the last one — every view this extension
 * produces is run through the renderer's own validator. An extension that
 * describes a page the renderer refuses to draw is a bug nobody finds until
 * somebody installs it, and the vocabulary is closed precisely so that this can
 * be checked ahead of time.
 */

const NOW = Date.parse('2026-03-10T12:00:00Z')
const ago = (hours) => new Date(NOW - hours * 3600_000).toISOString()

/** A workspace server that answers the way the broker hands answers back. */
function fakeWorkspace(over = {}) {
  const calls = []
  const wrap = (payload) => ({ ok: true, result: { content: [{ text: JSON.stringify(payload) }] } })
  const answers = {
    listTasks: () => wrap({ tasks: [] }),
    getTask: () => wrap({ task: { messages: [] } }),
    ...over,
  }
  return {
    calls,
    workspace: async (tool, args) => {
      calls.push({ tool, args })
      return answers[tool](args)
    },
  }
}

describe('the manifest', () => {
  it('is one, and asks only for the workspace', () => {
    const { ok, manifest: parsed } = parseManifest(manifest)

    expect(ok).toBe(true)
    expect(parsed.mcp.workspace).toEqual(['listTasks', 'getTask'])
    expect(parsed.mcp.supervisor).toEqual([])
  })

  it('claims a key an extension is allowed to have', () => {
    // `x s`, not `s` — the bare letters stay AgentRQ's.
    expect(checkShortcuts(parseManifest(manifest).manifest, []).ok).toBe(true)
  })
})

describe('hoursFrom', () => {
  it('falls back rather than looking at nothing', () => {
    expect(hoursFrom(undefined)).toBe(DEFAULT_HOURS)
    expect(hoursFrom({ since: '' })).toBe(DEFAULT_HOURS)
    expect(hoursFrom({ since: 'soon' })).toBe(DEFAULT_HOURS)
  })

  // A negative window silently lists nothing while looking like it worked.
  it('refuses a window that would list nothing', () => {
    expect(hoursFrom({ since: -3 })).toBe(DEFAULT_HOURS)
    expect(hoursFrom({ since: 0 })).toBe(DEFAULT_HOURS)
  })

  it('takes a number a person typed, up to a fortnight', () => {
    expect(hoursFrom({ since: '8' })).toBe(8)
    expect(hoursFrom({ since: 10_000 })).toBe(24 * 14)
  })
})

describe('movedSince', () => {
  const cutoff = NOW - 24 * 3600_000

  it('prefers when a task last changed over when it was made', () => {
    expect(movedSince({ createdAt: ago(100), updatedAt: ago(2) }, cutoff)).toBe(true)
    expect(movedSince({ createdAt: ago(2) }, cutoff)).toBe(true)
  })

  it('leaves out what has not moved', () => {
    expect(movedSince({ updatedAt: ago(48) }, cutoff)).toBe(false)
    expect(movedSince({}, cutoff)).toBe(false)
  })
})

describe('toneFor', () => {
  it('reads a status as what it means rather than as a colour', () => {
    expect(toneFor('blocked')).toBe('critical')
    expect(toneFor('completed')).toBe('positive')
    expect(toneFor('ongoing')).toBe('warning')
    expect(toneFor('notstarted')).toBe('default')
  })
})

describe('buildPage', () => {
  const tasks = [
    { title: 'Ship it', status: 'completed' },
    { title: 'Waiting on legal', status: 'blocked', lastMessage: 'Asked on Tuesday' },
  ]

  it('puts what is stuck first, and in its own group', () => {
    const page = buildPage(tasks, { hours: 24, workspaceName: 'Backend' })

    expect(page.nodes[1].label).toBe('Waiting on somebody')
    expect(page.nodes[1].children[0].value).toBe('Asked on Tuesday')
    expect(page.nodes[2].label).toBe('Everything else')
  })

  it('counts in words a person reads', () => {
    expect(buildPage([tasks[0]], { hours: 24 }).nodes[0].value).toContain('1 task moved')
    expect(buildPage(tasks, { hours: 24 }).nodes[0].value).toContain('2 tasks moved')
  })

  it('says nothing happened rather than showing an empty page', () => {
    // An empty page with a heading looks like something that failed to load.
    const page = buildPage([], { hours: 24 })
    expect(page.nodes.at(-1)).toMatchObject({ type: 'empty' })
  })

  it('leaves out the blocked group entirely when nothing is', () => {
    const page = buildPage([tasks[0]], { hours: 24 })
    expect(page.nodes.map((node) => node.label)).not.toContain('Waiting on somebody')
  })
})

describe('collect', () => {
  const ctx = (fake, config = {}) => ({ config, mcp: { workspace: fake.workspace } })

  it('asks the workspace for its tasks and builds the page', async () => {
    const fake = fakeWorkspace({
      listTasks: () => ({
        ok: true,
        result: JSON.stringify({ tasks: [{ id: 't1', title: 'Ship it', status: 'completed', updatedAt: ago(1) }] }),
      }),
    })

    const page = await collect(ctx(fake), { workspaceId: 'ws1', workspaceName: 'Backend', now: NOW })

    expect(fake.calls[0]).toMatchObject({ tool: 'listTasks', args: { workspaceId: 'ws1' } })
    expect(page.nodes[0].value).toContain('Backend')
  })

  it('reads the last message on anything blocked, and only on those', async () => {
    const fake = fakeWorkspace({
      listTasks: () => ({
        ok: true,
        result: JSON.stringify({
          tasks: [
            { id: 't1', title: 'Stuck', status: 'blocked', updatedAt: ago(1) },
            { id: 't2', title: 'Fine', status: 'ongoing', updatedAt: ago(1) },
          ],
        }),
      }),
      getTask: () => ({ ok: true, result: JSON.stringify({ task: { messages: [{ text: 'Asked on Tuesday' }] } }) }),
    })

    const page = await collect(ctx(fake), { workspaceId: 'ws1', now: NOW })

    // One extra call, for the one task that needed it.
    expect(fake.calls.filter((call) => call.tool === 'getTask')).toHaveLength(1)
    expect(page.nodes[1].children[0].value).toBe('Asked on Tuesday')
  })

  // A refusal is a value, not a throw. The page says what the host said.
  it('shows the refusal it was given rather than an empty page', async () => {
    const fake = fakeWorkspace({
      listTasks: () => ({ ok: false, reason: 'This extension may not call "listTasks" on the workspace server.' }),
    })

    const page = await collect(ctx(fake), { workspaceId: 'ws1', now: NOW })

    expect(page.nodes[0].tone).toBe('critical')
    expect(page.nodes[0].value).toContain('may not call')
  })

  it('carries on when one task detail is refused', async () => {
    const fake = fakeWorkspace({
      listTasks: () => ({
        ok: true,
        result: JSON.stringify({ tasks: [{ id: 't1', title: 'Stuck', status: 'blocked', updatedAt: ago(1) }] }),
      }),
      getTask: () => ({ ok: false, reason: 'refused' }),
    })

    const page = await collect(ctx(fake), { workspaceId: 'ws1', now: NOW })

    expect(page.nodes[1].children[0].value).toBe('No reply yet')
  })

  it('holds up when an answer cannot be read at all', async () => {
    const fake = fakeWorkspace({ listTasks: () => ({ ok: true, result: 'not json' }) })

    const page = await collect(ctx(fake), { workspaceId: 'ws1', now: NOW })

    expect(page.nodes.at(-1)).toMatchObject({ type: 'empty' })
  })

  it('says so when a blocked task has no messages at all', async () => {
    const fake = fakeWorkspace({
      listTasks: () => ({
        ok: true,
        result: JSON.stringify({ tasks: [{ id: 't1', title: 'Stuck', status: 'blocked', updatedAt: ago(1) }] }),
      }),
      getTask: () => ({ ok: true, result: JSON.stringify({}) }),
    })

    const page = await collect(ctx(fake), { workspaceId: 'ws1', now: NOW })

    expect(page.nodes[1].children[0].value).toBe('No reply yet')
  })

  it('uses the configured window', async () => {
    const fake = fakeWorkspace({
      listTasks: () => ({
        ok: true,
        result: JSON.stringify({
          tasks: [
            { id: 't1', title: 'Recent', status: 'ongoing', updatedAt: ago(2) },
            { id: 't2', title: 'Yesterday', status: 'ongoing', updatedAt: ago(20) },
          ],
        }),
      }),
    })

    const page = await collect(ctx(fake, { since: 4 }), { workspaceId: 'ws1', now: NOW })

    expect(page.nodes[0].value).toContain('1 task moved')
    expect(page.nodes[0].value).toContain('last 4 hours')
  })
})

describe('apply', () => {
  it('registers a page and the shortcut that opens it', () => {
    const ui = []
    const shortcuts = []
    apply({
      config: {},
      mcp: { workspace: async () => ({ ok: true, result: '{"tasks":[]}' }) },
      ui: { add: (entry) => ui.push(entry) },
      shortcuts: { add: (entry) => shortcuts.push(entry) },
    })

    expect(ui[0]).toMatchObject({ id: 'today', surface: 'page', label: 'Standup' })
    expect(shortcuts[0]).toMatchObject({ id: 'open', key: 's' })
  })
})

/**
 * The check that catches what unit tests do not: the renderer refuses a node
 * type it does not know, so a view this builds has to survive the same
 * validator the application draws with.
 */
describe('every view it can produce', () => {
  it('is one the renderer will actually draw', () => {
    const pages = [
      buildPage([], { hours: 24 }),
      buildPage([{ title: 'Ship it', status: 'completed' }], { hours: 24 }),
      buildPage(
        [
          { title: 'Stuck', status: 'blocked', lastMessage: 'Asked on Tuesday' },
          { title: 'Fine', status: 'ongoing' },
        ],
        { hours: 24, workspaceName: 'Backend' },
      ),
    ]

    for (const page of pages) {
      const result = normaliseView(page)
      expect(result.ok, result.reason).toBe(true)
    }
  })
})
