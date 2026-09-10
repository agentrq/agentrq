import { describe, it, expect } from 'vitest'

import {
  DEFAULT_MEMORY,
  STATES,
  apply,
  buildPage,
  buildThread,
  collect,
  memoryName,
  readConversation,
  readWorkspace,
  thread,
} from '../../../examples/extensions/standup/index.js'
import { parseManifest } from '../../src/main/extensions/manifest.js'
import { checkShortcuts } from '../../src/main/extensions/shortcuts.js'
import { normaliseView } from '../../../frontend/src/composables/useExtensionView.js'
import manifest from '../../../examples/extensions/standup/agentrq-extension.json'

/**
 * The workspace case, and the mistake it was built on.
 *
 * This example first asked for `listTasks`, which the workspace MCP server does
 * not have and deliberately never will — an agent connected to a workspace can
 * act on the task it was given, not enumerate the board. The first test below is
 * the one that would have caught it: every tool the manifest names is checked
 * against the server's real list.
 */

/** Exactly what `backend/internal/controller/mcp/server.go` registers. */
const WORKSPACE_TOOLS = [
  'createTask',
  'updateTaskStatus',
  'reply',
  'downloadAttachment',
  'getWorkspace',
  'getTask',
  'publishEvent',
  'loadMemory',
  'saveMemory',
  'deleteMemory',
  'elicit',
]

/** The shape `getWorkspace` answers with — prose, not JSON. */
const workspaceText = (over = {}) =>
  [
    `Workspace: ${over.name ?? 'Backend'}`,
    `Description: ${over.description ?? 'the API'}`,
    '',
    'Task Statistics:',
    `- Not Started: ${over.notstarted ?? 3}`,
    `- Ongoing: ${over.ongoing ?? 1}`,
    `- Completed: ${over.completed ?? 12}`,
    `- Rejected: ${over.rejected ?? 0}`,
    `- Blocked: ${over.blocked ?? 2}`,
  ].join('\n')

/** The shape `getTask` answers with when asked for the conversation. */
const taskText = ({ total = 9, last = 'Asked on Tuesday' } = {}) =>
  `Task details:\nID: t1\nTitle: Stuck\nStatus: blocked\nDetails: body\n\nConversation:\n${JSON.stringify({
    messages: [{ id: 'm1', sender: 'human', text: last }],
    total,
    cursor: 1,
  })}`

function fakeWorkspace(over = {}) {
  const calls = []
  const wrap = (text) => ({ ok: true, result: { content: [{ text }] } })
  const answers = {
    getWorkspace: () => wrap(workspaceText()),
    loadMemory: () => wrap('# memory\n\nnotes'),
    getTask: () => wrap(taskText()),
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

const ctxWith = (fake, config = {}) => ({ config, mcp: { workspace: fake.workspace } })

describe('the manifest', () => {
  it('is one', () => {
    expect(parseManifest(manifest).ok).toBe(true)
  })

  // The test that would have caught the original bug. A manifest can name any
  // tool it likes; only a real list says whether it exists.
  it('asks only for tools the workspace server actually has', () => {
    const { manifest: parsed } = parseManifest(manifest)

    for (const tool of parsed.mcp.workspace) {
      expect(WORKSPACE_TOOLS, `"${tool}" is not on the workspace server`).toContain(tool)
    }
    expect(parsed.mcp.supervisor).toEqual([])
  })

  it('does not ask for listTasks, which does not exist there', () => {
    // Stated on its own, because this is the specific thing that was wrong and
    // the specific thing that must not come back.
    expect(parseManifest(manifest).manifest.mcp.workspace).not.toContain('listTasks')
    expect(WORKSPACE_TOOLS).not.toContain('listTasks')
  })

  it('claims a key an extension is allowed to have', () => {
    // `x s`, not `s` — the bare letters stay AgentRQ's.
    expect(checkShortcuts(parseManifest(manifest).manifest, []).ok).toBe(true)
  })
})

describe('memoryName', () => {
  it('reads the index unless told otherwise', () => {
    expect(memoryName(undefined)).toBe(DEFAULT_MEMORY)
    expect(memoryName({ memory: '  ' })).toBe(DEFAULT_MEMORY)
    expect(memoryName({ memory: 'deploys.md' })).toBe('deploys.md')
  })
})

describe('readWorkspace', () => {
  it('reads the name, the description and every count', () => {
    const read = readWorkspace(workspaceText())

    expect(read.name).toBe('Backend')
    expect(read.description).toBe('the API')
    expect(read.counts).toEqual({ blocked: 2, ongoing: 1, notstarted: 3, completed: 12, rejected: 0 })
  })

  // A workspace with no description is ordinary, and a parser that needs one
  // would break on it.
  it('holds up when a field is missing or the text is not what it expected', () => {
    expect(readWorkspace('Workspace: Backend').description).toBe('')
    expect(readWorkspace('').name).toBe('')
    expect(readWorkspace(undefined).counts.ongoing).toBe(0)
    expect(readWorkspace('nothing like the real answer').counts).toEqual({
      blocked: 0, ongoing: 0, notstarted: 0, completed: 0, rejected: 0,
    })
  })

  it('knows every state the server reports', () => {
    expect(STATES).toEqual(['blocked', 'ongoing', 'notstarted', 'completed', 'rejected'])
  })
})

describe('readConversation', () => {
  // The bug `task-stats` had, in the place it can actually be got right:
  // `total` is the thread's length, and the returned array is a page of it.
  it('reads the thread length from total, not from what was returned', () => {
    const read = readConversation(taskText({ total: 9 }))

    expect(read.total).toBe(9)
    expect(read.last).toBe('Asked on Tuesday')
  })

  it('falls back to what it was given when there is no total', () => {
    const text = `Conversation:\n${JSON.stringify({ messages: [{ text: 'one' }, { text: 'two' }] })}`
    expect(readConversation(text).total).toBe(2)
  })

  it('says nothing rather than guessing when there is no conversation', () => {
    expect(readConversation('Task details:\nID: t1')).toEqual({ total: 0, last: '' })
    expect(readConversation('Conversation:\nnot json')).toEqual({ total: 0, last: '' })
    expect(readConversation(undefined)).toEqual({ total: 0, last: '' })
    expect(readConversation('Conversation:\n{}')).toEqual({ total: 0, last: '' })
  })
})

describe('buildPage', () => {
  const workspace = () => readWorkspace(workspaceText())

  it('leads with what is blocked, and leaves out what is empty', () => {
    const page = buildPage(workspace(), { name: 'memory.md', text: 'notes' })
    const work = page.nodes.find((node) => node.label === 'Work')

    expect(work.children[0]).toMatchObject({ label: 'Blocked', value: '2', tone: 'critical' })
    // Five rows of zero is not a report.
    expect(work.children.map((row) => row.label)).not.toContain('Rejected')
  })

  it('says nothing has happened rather than showing an empty page', () => {
    const empty = readWorkspace(workspaceText({ notstarted: 0, ongoing: 0, completed: 0, rejected: 0, blocked: 0 }))

    expect(buildPage(empty, { name: 'memory.md', text: '' }).nodes[1]).toMatchObject({ type: 'empty' })
  })

  it('shows the memory under its own name, and says when there is none', () => {
    const withMemory = buildPage(workspace(), { name: 'deploys.md', text: 'how we ship' })
    const without = buildPage(workspace(), { name: 'memory.md', text: '' })

    expect(withMemory.nodes.at(-1)).toMatchObject({ label: 'deploys.md' })
    expect(withMemory.nodes.at(-1).children[0].value).toBe('how we ship')
    expect(without.nodes.at(-1).children[0]).toMatchObject({ type: 'empty' })
  })

  it('falls back to the workspace name when it has no description', () => {
    const bare = readWorkspace(workspaceText({ description: '' }))
    expect(buildPage(bare, { name: 'memory.md', text: '' }).nodes[0].value).toContain('Backend')
  })
})

describe('buildThread', () => {
  it('reports the real length of the thread and its last message', () => {
    const view = buildThread({ title: 'Stuck', status: 'blocked' }, { total: 9, last: 'Asked on Tuesday' })

    expect(view.nodes[0].items[0]).toMatchObject({ label: 'Messages', value: '9' })
    expect(view.nodes[1].children[0].value).toBe('Asked on Tuesday')
  })

  it('says so when nobody has replied', () => {
    const view = buildThread({ title: 'New', status: 'notstarted' }, { total: 0, last: '' })
    expect(view.nodes[1]).toMatchObject({ type: 'empty' })
  })

  it('holds up when handed almost nothing', () => {
    const view = buildThread(undefined, { total: 0, last: '' })
    expect(view.title).toContain('This task')
    expect(view.nodes[0].items[1].value).toBe('unknown')
  })
})

describe('collect', () => {
  it('asks the workspace where it stands, then what it remembers', async () => {
    const fake = fakeWorkspace()

    const page = await collect(ctxWith(fake), { workspaceId: 'ws1' })

    expect(fake.calls.map((call) => call.tool)).toEqual(['getWorkspace', 'loadMemory'])
    expect(fake.calls.every((call) => call.args.workspaceId === 'ws1')).toBe(true)
    expect(page.nodes.find((node) => node.label === 'Work')).toBeDefined()
  })

  it('reads the memory the user chose', async () => {
    const fake = fakeWorkspace()

    await collect(ctxWith(fake, { memory: 'deploys.md' }), { workspaceId: 'ws1' })

    expect(fake.calls[1].args.name).toBe('deploys.md')
  })

  // A memory that was never written answers with a sentence rather than an
  // error, and showing that sentence as content would be repeating the server
  // at the user.
  it('treats a memory that was never written as empty', async () => {
    const fake = fakeWorkspace({
      loadMemory: () => ({ ok: true, result: 'No memory saved under "memory.md" yet. Use saveMemory to write one.' }),
    })

    const page = await collect(ctxWith(fake), { workspaceId: 'ws1' })

    expect(page.nodes.at(-1).children[0]).toMatchObject({ type: 'empty' })
  })

  it('carries on without the memory when that one call is refused', async () => {
    const fake = fakeWorkspace({ loadMemory: () => ({ ok: false, reason: 'refused' }) })

    const page = await collect(ctxWith(fake), { workspaceId: 'ws1' })

    // The counts are still worth showing; only the memory is missing.
    expect(page.nodes.find((node) => node.label === 'Work')).toBeDefined()
  })

  // A refusal is a value, not a throw. The page says what the host said.
  it('shows the refusal it was given rather than an empty page', async () => {
    const fake = fakeWorkspace({
      getWorkspace: () => ({ ok: false, reason: 'This extension was not granted access to that workspace.' }),
    })

    const page = await collect(ctxWith(fake), { workspaceId: 'ws2' })

    expect(page.nodes[0].tone).toBe('critical')
    expect(page.nodes[0].value).toContain('not granted')
  })

  it('holds up when an answer cannot be read at all', async () => {
    const fake = fakeWorkspace({ getWorkspace: () => ({ ok: true, result: { content: [{}] } }) })

    const page = await collect(ctxWith(fake), { workspaceId: 'ws1' })

    expect(page.nodes[1]).toMatchObject({ type: 'empty' })
  })
})

describe('thread', () => {
  /** A server that paginates from the oldest, the way the real one does. */
  const paginated = (messages) =>
    fakeWorkspace({
      getTask: ({ cursor = 0, limit = 5 }) => ({
        ok: true,
        result: {
          content: [
            {
              text: `Task details:\nID: t1\n\nConversation:\n${JSON.stringify({
                messages: messages.slice(cursor, cursor + limit).map((text) => ({ text })),
                total: messages.length,
                cursor: cursor + limit,
              })}`,
            },
          ],
        },
      }),
    })

  it('asks for the task and counts what came back', async () => {
    const fake = fakeWorkspace()

    const view = await thread(ctxWith(fake), { id: 't1', workspaceId: 'ws1', title: 'Stuck', status: 'blocked' })

    expect(fake.calls[0]).toMatchObject({
      tool: 'getTask',
      args: { workspaceId: 'ws1', taskId: 't1', includeConversation: true, limit: 1 },
    })
    expect(view.nodes[0].items[0].value).toBe('9')
  })

  /**
   * Found against a real server: a window at cursor 0 is the *first* message,
   * so what appeared under "Last message" was the oldest one. Nobody would
   * catch that without a thread long enough to tell them apart.
   */
  it('reads the end of the thread, not the start of it', async () => {
    const fake = paginated(['first reply', 'second reply', 'third reply'])

    const view = await thread(ctxWith(fake), { id: 't1', workspaceId: 'ws1', title: 'Stuck' })

    expect(view.nodes[0].items[0].value).toBe('3')
    expect(view.nodes[1].children[0].value).toBe('third reply')
    // Two calls, and the second asks for the last one by name.
    expect(fake.calls.map((call) => call.args.cursor)).toEqual([0, 2])
  })

  it('asks once when there is nothing to page past', async () => {
    const one = paginated(['only reply'])
    const none = paginated([])

    expect((await thread(ctxWith(one), { id: 't1' })).nodes[1].children[0].value).toBe('only reply')
    expect(one.calls).toHaveLength(1)

    expect((await thread(ctxWith(none), { id: 't1' })).nodes[1]).toMatchObject({ type: 'empty' })
    expect(none.calls).toHaveLength(1)
  })

  it('keeps the count when the second call is refused', async () => {
    let calls = 0
    const fake = fakeWorkspace({
      getTask: (args) => {
        calls += 1
        return calls === 1
          ? { ok: true, result: { content: [{ text: taskText({ total: 9, last: 'from the first page' }) }] } }
          : { ok: false, reason: 'refused' }
      },
    })

    const view = await thread(ctxWith(fake), { id: 't1' })

    // Better than losing the whole panel over the one part that failed.
    expect(view.nodes[0].items[0].value).toBe('9')
    expect(view.nodes[1].children[0].value).toBe('from the first page')
  })

  it('shows a refusal in the same shape as the page does', async () => {
    const fake = fakeWorkspace({ getTask: () => ({ ok: false, reason: 'refused' }) })

    const view = await thread(ctxWith(fake), { id: 't1' })

    expect(view.nodes[0]).toMatchObject({ tone: 'critical', value: 'refused' })
  })
})

describe('apply', () => {
  it('registers a page, a task menu item and the shortcut', () => {
    const ui = []
    const shortcuts = []
    apply({
      config: {},
      mcp: { workspace: async () => ({ ok: true, result: '' }) },
      ui: { add: (entry) => ui.push(entry) },
      shortcuts: { add: (entry) => shortcuts.push(entry) },
    })

    expect(ui.map((entry) => entry.surface)).toEqual(['page', 'task-menu'])
    expect(shortcuts[0]).toMatchObject({ id: 'open', key: 's' })
  })

  it('wires the page and the shortcut to the same thing', async () => {
    const ui = []
    const shortcuts = []
    const fake = fakeWorkspace()
    apply({
      config: {},
      mcp: { workspace: fake.workspace },
      ui: { add: (entry) => ui.push(entry) },
      shortcuts: { add: (entry) => shortcuts.push(entry) },
    })

    const fromPage = await ui[0].view({ workspaceId: 'ws1' })
    const fromKey = await shortcuts[0].run({ workspaceId: 'ws1' })

    expect(fromPage).toEqual(fromKey)
  })

  it('wires the task menu item to the thread', async () => {
    const ui = []
    const fake = fakeWorkspace()
    apply({
      config: {},
      mcp: { workspace: fake.workspace },
      ui: { add: (entry) => ui.push(entry) },
      shortcuts: { add: () => {} },
    })

    const view = await ui[1].run({ id: 't1', workspaceId: 'ws1', title: 'Stuck' })

    expect(view.nodes[0].items[0]).toMatchObject({ label: 'Messages', value: '9' })
  })
})

/**
 * The check that catches what unit tests do not: the renderer refuses a node
 * type it does not know, so a view this builds has to survive the same
 * validator the application draws with.
 */
describe('every view it can produce', () => {
  it('is one the renderer will actually draw', () => {
    const busy = readWorkspace(workspaceText())
    const quiet = readWorkspace(workspaceText({ notstarted: 0, ongoing: 0, completed: 0, rejected: 0, blocked: 0 }))

    const views = [
      buildPage(busy, { name: 'memory.md', text: 'notes' }),
      buildPage(quiet, { name: 'memory.md', text: '' }),
      buildThread({ title: 'Stuck', status: 'blocked' }, { total: 9, last: 'Asked on Tuesday' }),
      buildThread({ title: 'New', status: 'notstarted' }, { total: 0, last: '' }),
    ]

    for (const view of views) {
      const result = normaliseView(view)
      expect(result.ok, result.reason).toBe(true)
    }
  })
})
