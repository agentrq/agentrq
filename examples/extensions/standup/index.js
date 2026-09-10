/**
 * Standup — the workspace case.
 *
 * A page in the sidebar listing what moved in one workspace today, plus
 * <kbd>x</kbd> <kbd>s</kbd> to open it. It asks for two workspace tools and
 * nothing else, which is the middle rung of the grant ladder: the install screen
 * shows exactly `listTasks` and `getTask`, and the workspace they apply to.
 *
 * ## Everything it reaches goes through the broker
 *
 * `ctx.mcp.workspace(tool, args)` is a request, not a call. The extension never
 * holds the workspace token — the host attaches it on the way out, checks the
 * tool against the manifest's allowlist and the workspace against the grant, and
 * answers `{ ok: false, reason }` rather than throwing when either says no.
 *
 * That shape is why nothing here has a try/catch around a network call: a
 * refusal is a value, and the page renders it as a sentence rather than becoming
 * an unhandled rejection somewhere in the host.
 *
 * ## Why the page is a description rather than markup
 *
 * The renderer is on a privileged origin with a bridge to files, the clipboard
 * and the shell. Extensions describe what they want drawn in a closed
 * vocabulary and AgentRQ draws it — so extension UI looks like the rest of the
 * product instead of approximating it, and no third-party markup is ever in that
 * origin.
 */

export const name = 'standup'

export const inject = ['ui', 'shortcuts']

/** Looked back this far when the setting is empty. A working day. */
export const DEFAULT_HOURS = 24

/**
 * How far back to look.
 *
 * Clamped rather than trusted: the field is a number a person typed, and a
 * negative one would silently list nothing while looking like it worked.
 */
export function hoursFrom(config) {
  const hours = Number(config?.since)
  if (!Number.isFinite(hours) || hours <= 0) return DEFAULT_HOURS
  return Math.min(hours, 24 * 14)
}

/** Whether a task changed inside the window. */
export function movedSince(task, cutoff) {
  const at = new Date(task?.updatedAt ?? task?.createdAt ?? 0).getTime()
  return Number.isFinite(at) && at >= cutoff
}

/** The tones the page uses, chosen from what a task's status actually means. */
export function toneFor(status) {
  if (status === 'blocked') return 'critical'
  if (status === 'completed') return 'positive'
  if (status === 'ongoing') return 'warning'
  return 'default'
}

/**
 * The page, built from what the workspace answered.
 *
 * Kept apart from the fetching so it can be tested as what it is: a pure
 * function from a list of tasks to a description of a screen.
 */
export function buildPage(tasks, { hours, workspaceName = 'this workspace' } = {}) {
  const blocked = tasks.filter((task) => task.status === 'blocked')
  const rest = tasks.filter((task) => task.status !== 'blocked')

  const nodes = []
  nodes.push({
    type: 'text',
    tone: 'muted',
    value: `${tasks.length === 1 ? '1 task' : `${tasks.length} tasks`} moved in ${workspaceName} in the last ${hours} hours.`,
  })

  if (tasks.length === 0) {
    // A state, not a failure — and worth saying in words, because an empty page
    // with a heading looks like something that failed to load.
    nodes.push({ type: 'empty', value: 'Nothing has moved yet today.' })
    return { title: 'Standup', nodes }
  }

  if (blocked.length > 0) {
    // First, and its own group: the whole reason somebody opens this page is to
    // find what is stuck, and burying it in date order hides it.
    nodes.push({
      type: 'group',
      label: 'Waiting on somebody',
      children: blocked.map((task) => ({
        type: 'row',
        label: task.title,
        // `||`, not `??`: a blocked task whose last message is empty is the
        // same situation as one with no messages, and the difference between
        // the two operators here is a row that renders blank.
        value: task.lastMessage || 'No reply yet',
      })),
    })
  }

  if (rest.length > 0) {
    nodes.push({
      type: 'group',
      label: 'Everything else',
      children: rest.map((task) => ({
        type: 'row',
        label: task.title,
        value: task.status,
      })),
    })
  }

  return { title: 'Standup', nodes }
}

/**
 * Fetches and builds, in one place.
 *
 * The extra `getTask` per blocked task is deliberate and bounded: a standup is
 * read to find out *why* something is stuck, and the list alone cannot say. It
 * is only asked for the blocked ones, so an ordinary day costs one call.
 */
export async function collect(ctx, { workspaceId, workspaceName, now = Date.now() } = {}) {
  const hours = hoursFrom(ctx.config)
  const cutoff = now - hours * 3600_000

  const listed = await ctx.mcp.workspace('listTasks', { workspaceId, limit: 50 })
  if (!listed.ok) {
    // The reason the host gave, shown as it came. An author debugging a refused
    // grant needs the sentence that explains it, not a generic failure.
    return { title: 'Standup', nodes: [{ type: 'text', tone: 'critical', value: listed.reason }] }
  }

  const tasks = (readTasks(listed.result) ?? []).filter((task) => movedSince(task, cutoff))

  for (const task of tasks) {
    if (task.status !== 'blocked') continue
    const detail = await ctx.mcp.workspace('getTask', { workspaceId, taskId: task.id })
    if (!detail.ok) continue
    const messages = readTask(detail.result)?.messages ?? []
    task.lastMessage = messages.at(-1)?.text ?? ''
  }

  return buildPage(tasks, { hours, workspaceName })
}

/**
 * Unwraps whatever the broker handed back.
 *
 * MCP answers carry the same JSON the REST API sends, as text inside a content
 * array. Which layer of that arrives depends on the transport, so this unwraps
 * rather than assuming — and an answer it cannot read becomes an empty list,
 * which the page already knows how to say something about.
 */
function payload(result) {
  let value = result
  if (value && typeof value === 'object' && Array.isArray(value.content)) value = value.content[0]?.text
  if (typeof value === 'string') {
    try {
      value = JSON.parse(value)
    } catch {
      return null
    }
  }
  return value ?? null
}

function readTasks(result) {
  return payload(result)?.tasks ?? []
}

function readTask(result) {
  return payload(result)?.task ?? null
}

export function apply(ctx) {
  ctx.ui.add({
    id: 'today',
    surface: 'page',
    label: 'Standup',
    order: 20,
    view: (context) => collect(ctx, context),
  })

  ctx.shortcuts.add({
    id: 'open',
    key: 's',
    label: 'Standup: today',
    // The prefix is the application's; the second key is the extension's. `x`
    // then `s` opens this page, and the two-key scheme is what keeps the bare
    // single letters entirely AgentRQ's.
    run: (context) => collect(ctx, context),
  })
}
