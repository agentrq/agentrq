/**
 * Standup — the workspace case.
 *
 * Where this workspace stands: how much work sits in each state, what the
 * workspace remembers, and — from a task's own right-click menu — how long that
 * task's thread has actually run.
 *
 * ## This example was wrong, and the way it was wrong is the lesson
 *
 * It first asked for `listTasks`, built a page listing everything that moved
 * today, and would never have worked: **the workspace MCP server has no
 * `listTasks`, deliberately.** An agent connected to a workspace can act on the
 * task it was given and read what the workspace remembers; it cannot enumerate
 * the whole board. That is a decision about what a connected agent is allowed to
 * see, and an extension does not get to route around it.
 *
 * Nothing stopped the manifest naming a tool that does not exist, either —
 * `checkCompatibility` would have refused the install with "the workspace server
 * does not offer listTasks", but only once a real tool list was in front of it.
 * So the first rule of writing one of these: **read the server's actual tool
 * list before you write the manifest.** For the workspace server that is
 * `createTask`, `updateTaskStatus`, `reply`, `downloadAttachment`,
 * `getWorkspace`, `getTask`, `publishEvent`, `loadMemory`, `saveMemory`,
 * `deleteMemory` and `elicit` — no more.
 *
 * What that constraint produced is a better extension. `getWorkspace` already
 * answers with counts per status, which is what a standup is actually for; the
 * per-task detail belongs on the task, where the person asking has one in front
 * of them.
 *
 * ## Everything it reaches goes through the broker
 *
 * `ctx.mcp.workspace(tool, args)` is a request, not a call. The extension never
 * holds the workspace token — the host attaches it on the way out, checks the
 * tool against the manifest's allowlist and the workspace against the grant, and
 * answers `{ ok: false, reason }` rather than throwing when either says no.
 *
 * ## The workspace server answers in prose, not JSON
 *
 * It is written for agents to read, so these responses are text. Parsing it is
 * a few lines and they are below — worth knowing before you write your own,
 * because the supervisor server does the opposite and answers with the REST
 * API's JSON.
 */

export const name = 'standup'

export const inject = ['ui', 'shortcuts']

/** The index every workspace's memory hangs off, and the default here. */
export const DEFAULT_MEMORY = 'memory.md'

/** The states a workspace's work can be in, in the order a standup reads them. */
export const STATES = ['blocked', 'ongoing', 'notstarted', 'completed', 'rejected']

/** Blocked leads, because it is the only one that needs somebody. */
const TONES = {
  blocked: 'critical',
  ongoing: 'warning',
  completed: 'positive',
  notstarted: 'default',
  rejected: 'muted',
}

const LABELS = {
  blocked: 'Blocked',
  ongoing: 'Ongoing',
  notstarted: 'Not started',
  completed: 'Completed',
  rejected: 'Rejected',
}

/** Whichever memory the user named, or the index. */
export function memoryName(config) {
  return String(config?.memory ?? '').trim() || DEFAULT_MEMORY
}

/**
 * Reads `getWorkspace`'s answer.
 *
 * The shape is fixed by the server and looks like:
 *
 *     Workspace: Backend
 *     Description: the API
 *
 *     Task Statistics:
 *     - Not Started: 3
 *     - Ongoing: 1
 *     …
 *
 * Every field is optional here on purpose: a workspace with no description is
 * ordinary, and a parser that needs one would break on it.
 */
export function readWorkspace(text) {
  const body = String(text ?? '')
  const field = (label) => body.match(new RegExp(`^${label}:[ \\t]*(.*)$`, 'm'))?.[1]?.trim() ?? ''

  const counts = {}
  for (const state of STATES) {
    // The server writes them in title case with a space; match on that rather
    // than on the status word, which is what the API uses.
    const printed = LABELS[state]
    const found = body.match(new RegExp(`^- ${printed}:[ \\t]*(\\d+)$`, 'mi'))
    counts[state] = found ? Number(found[1]) : 0
  }

  return { name: field('Workspace'), description: field('Description'), counts }
}

/**
 * Reads the conversation out of `getTask`'s answer.
 *
 * The text carries a `Conversation:` section holding JSON, and `total` in it is
 * the real length of the thread — not the number of messages returned, which is
 * paginated and defaults to five. Counting the returned array is how you end up
 * reporting five for every long task, which is the same class of mistake as
 * `task-stats` reporting one.
 *
 * The window itself starts at the *oldest* message, so `messages.at(-1)` is the
 * newest of whatever came back and not the newest in the thread. Which one that
 * is depends entirely on the cursor the caller asked for.
 */
export function readConversation(text) {
  const body = String(text ?? '')
  const at = body.indexOf('Conversation:')
  if (at === -1) return { total: 0, last: '' }

  try {
    const payload = JSON.parse(body.slice(at + 'Conversation:'.length).trim())
    const messages = payload?.messages ?? []
    return { total: Number(payload?.total ?? messages.length), last: messages.at(-1)?.text ?? '' }
  } catch {
    // An answer that will not parse is not a thread of length zero, but there
    // is nothing better to say and the page reads the same either way.
    return { total: 0, last: '' }
  }
}

/** How the state counts read on the page. */
export function buildPage(workspace, memory) {
  const total = STATES.reduce((sum, state) => sum + workspace.counts[state], 0)

  const nodes = [
    {
      type: 'text',
      tone: 'muted',
      value: workspace.description || `Where ${workspace.name || 'this workspace'} stands.`,
    },
  ]

  if (total === 0) {
    // A state, not a failure — and worth saying in words, because an empty page
    // with a heading looks like something that failed to load.
    nodes.push({ type: 'empty', value: 'No tasks here yet.' })
  } else {
    nodes.push({
      type: 'group',
      label: 'Work',
      children: STATES
        // Only what is actually there: five rows of zero is not a report.
        .filter((state) => workspace.counts[state] > 0)
        .map((state) => ({
          type: 'row',
          label: LABELS[state],
          value: String(workspace.counts[state]),
          tone: TONES[state],
        })),
    })
  }

  nodes.push({
    type: 'group',
    label: memory.name,
    children: memory.text
      ? [{ type: 'text', value: memory.text }]
      : [{ type: 'empty', value: 'Nothing remembered here yet.' }],
  })

  return { title: 'Standup', nodes }
}

/** One task's thread, from the task menu where a task is actually in hand. */
export function buildThread(task, conversation) {
  return {
    title: `"${task?.title ?? 'This task'}"`,
    nodes: [
      {
        type: 'rows',
        items: [
          { type: 'row', label: 'Messages', value: String(conversation.total) },
          { type: 'row', label: 'Status', value: String(task?.status ?? 'unknown') },
        ],
      },
      conversation.total === 0
        ? { type: 'empty', value: 'Nobody has replied to this task yet.' }
        : { type: 'group', label: 'Last message', children: [{ type: 'text', value: conversation.last }] },
    ],
  }
}

/** What the broker handed back, as text. */
function textOf(result) {
  let value = result
  if (value && typeof value === 'object' && Array.isArray(value.content)) value = value.content[0]?.text
  return typeof value === 'string' ? value : ''
}

/** A refusal, drawn the way the host worded it. */
const refusal = (reason) => ({ title: 'Standup', nodes: [{ type: 'text', tone: 'critical', value: reason }] })

/**
 * The page: where the workspace stands, and what it remembers.
 */
export async function collect(ctx, { workspaceId } = {}) {
  const workspace = await ctx.mcp.workspace('getWorkspace', { workspaceId })
  if (!workspace.ok) {
    // The reason the host gave, shown as it came. An author debugging a refused
    // grant needs the sentence that explains it, not a generic failure.
    return refusal(workspace.reason)
  }

  const name = memoryName(ctx.config)
  const memory = await ctx.mcp.workspace('loadMemory', { workspaceId, name })
  // A memory that was never written answers with a sentence rather than an
  // error, so a refusal is the only thing worth treating as missing.
  const text = memory.ok ? textOf(memory.result) : ''

  return buildPage(readWorkspace(textOf(workspace.result)), {
    name,
    text: text.startsWith('No memory saved under') ? '' : text,
  })
}

/**
 * The task menu item: how long this thread has run.
 *
 * This is the half `task-stats` cannot do. The board's task list carries only
 * the last message per task, so an extension with no permissions can never
 * count a thread — and one that asks for `getTask`, as this does at install,
 * can.
 */
export async function thread(ctx, task) {
  // `workspaceId` addresses the call; the host turns it into an endpoint and
  // keeps it out of the arguments, because the per-workspace server's tools take
  // no such parameter and refuse one.
  const ask = (over) =>
    ctx.mcp.workspace('getTask', {
      workspaceId: task?.workspaceId,
      taskId: task?.id,
      includeConversation: true,
      ...over,
    })

  // The first call is for `total`, which is the thread's real length whatever
  // window came back with it.
  const first = await ask({ limit: 1, cursor: 0 })
  if (!first.ok) return refusal(first.reason)

  const conversation = readConversation(textOf(first.result))

  // Pagination starts at the oldest, so a window at cursor 0 is the *first*
  // message however small it is — showing that under a heading saying "last"
  // was wrong in a way nobody would catch without a thread to read. The end of
  // it needs a second call, and only when there is more than one message.
  if (conversation.total > 1) {
    const last = await ask({ limit: 1, cursor: conversation.total - 1 })
    if (last.ok) conversation.last = readConversation(textOf(last.result)).last
  }

  return buildThread(task, conversation)
}

export function apply(ctx) {
  ctx.ui.add({
    id: 'today',
    surface: 'page',
    label: 'Standup',
    order: 20,
    view: (context) => collect(ctx, context),
  })

  ctx.ui.add({
    id: 'thread',
    surface: 'task-menu',
    label: 'Standup: this thread',
    order: 20,
    run: (task) => thread(ctx, task),
  })

  ctx.shortcuts.add({
    id: 'open',
    key: 's',
    label: 'Standup: where this workspace stands',
    // The prefix is the application's; the second key is the extension's. `x`
    // then `s` opens this page, and the two-key scheme is what keeps the bare
    // single letters entirely AgentRQ's.
    run: (context) => collect(ctx, context),
  })
}
