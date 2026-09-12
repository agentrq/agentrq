// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * Daily Digest — the full case, and the top of the grant ladder.
 *
 * It looks at every workspace, writes a summary, and posts it back as a task on
 * a schedule. It declares supervisor tools, a secret, a host it contacts, a
 * schedule, a workspace-header action and a task menu item — which makes it the
 * example that exercises everything at once.
 *
 * ## Why it asks for the account rather than a workspace
 *
 * "Across every workspace" is not a wide version of "in this workspace"; it is a
 * different question. `listAllTasks` spans the platform and `createTask` reaches
 * anywhere, so the supervisor surface *is* every workspace. The grant ladder
 * makes "supervisor, but only this one" unrepresentable on purpose: it would be
 * the same grant wearing a narrower label.
 *
 * The install screen says so in those words, and the user can decline. An
 * extension that genuinely only needs one workspace should ask for workspace
 * tools instead — see the `standup` example, which does.
 *
 * ## The secret
 *
 * `webhookToken` is declared `secret`, which means three things: it is stored
 * encrypted through the OS keychain, it is never shown back to the user or to
 * the catalogue, and **it is refused rather than written in the clear** if the
 * machine has no secure storage. This code receives the decrypted value in
 * `ctx.config` and does the only correct thing with it — sends it and keeps no
 * copy. It is never logged, not even at a length.
 *
 * ## The network claim
 *
 * The manifest says `"net": ["hooks.slack.com"]`. Nothing enforces that: this is
 * trusted Node and could contact anything. It is the author saying what the
 * extension is for, and the install screen presents it as a claim rather than a
 * restriction, because a list of hosts under a permission heading would be read
 * as a boundary that nobody is holding.
 *
 * ## The schedule is the reason any of this works
 *
 * The desktop app is not running at 3am. The digest still happens, because what
 * is registered here is a *cron task on the server*, reconciled by the host: it
 * is created when this loads, revised when the setting changes, and removed when
 * the extension is uninstalled.
 */

export const name = 'digest'

export const inject = ['ui', 'schedules']

/** Where the digest lands if the setting is empty. Nowhere — it is required. */
export const DEFAULT_CRON = '0 9 * * *'

/** How many tasks are worth reading before the summary stops being a summary. */
export const MAX_TASKS = 200

/**
 * The task body the scheduled run leaves behind.
 *
 * Written as an instruction to the agent that picks the task up rather than as a
 * finished report, because the scheduled task runs server-side where this code
 * is not. That is the shape of every schedule an extension declares: the
 * extension decides *what should happen*, and something else is there when it
 * does.
 */
export function scheduleBody(config) {
  return [
    'Summarise the last day across every workspace on this account.',
    '',
    'Group by workspace. For each one, say what completed, what is still ongoing,',
    'and name anything that has been blocked for more than a day — that last part',
    'is the reason anybody reads this.',
    config?.webhookPath ? '' : 'Post the summary as your reply on this task.',
  ]
    .filter((line) => line !== undefined)
    .join('\n')
}

/** Grouped by workspace, because a flat list of forty tasks is not a digest. */
export function summarise(workspaces, tasks) {
  const names = new Map(workspaces.map((workspace) => [workspace.id, workspace.name]))
  const byWorkspace = new Map()

  for (const task of tasks) {
    const id = task.workspaceId
    if (!byWorkspace.has(id)) byWorkspace.set(id, [])
    byWorkspace.get(id).push(task)
  }

  return [...byWorkspace.entries()]
    .map(([id, group]) => ({
      // A workspace that has since been deleted still has tasks in the answer,
      // and dropping the group would silently lose them.
      workspace: names.get(id) ?? 'A workspace that no longer exists',
      completed: group.filter((task) => task.status === 'completed').length,
      ongoing: group.filter((task) => task.status === 'ongoing').length,
      blocked: group.filter((task) => task.status === 'blocked').map((task) => task.title),
    }))
    .sort((a, b) => b.blocked.length - a.blocked.length || a.workspace.localeCompare(b.workspace))
}

/** The summary as a page. Blocked work leads, because it is what needs somebody. */
export function buildPage(summary) {
  if (summary.length === 0) {
    return { title: 'Daily Digest', nodes: [{ type: 'empty', value: 'Nothing has happened across your workspaces today.' }] }
  }

  return {
    title: 'Daily Digest',
    nodes: summary.map((entry) => ({
      type: 'group',
      label: entry.workspace,
      children: [
        { type: 'row', label: 'Completed', value: String(entry.completed) },
        { type: 'row', label: 'Ongoing', value: String(entry.ongoing) },
        entry.blocked.length === 0
          ? { type: 'text', tone: 'positive', value: 'Nothing blocked.' }
          : {
              type: 'group',
              label: `Blocked (${entry.blocked.length})`,
              children: entry.blocked.map((title) => ({ type: 'text', tone: 'critical', value: title })),
            },
      ],
    })),
  }
}

/** The same summary as the plain text a webhook wants. */
export function buildText(summary) {
  if (summary.length === 0) return 'Nothing happened across your workspaces today.'
  return summary
    .map((entry) => {
      const head = `*${entry.workspace}* — ${entry.completed} completed, ${entry.ongoing} ongoing`
      if (entry.blocked.length === 0) return head
      return `${head}\nBlocked: ${entry.blocked.join('; ')}`
    })
    .join('\n\n')
}

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

/**
 * Reads every workspace and every recent task, and summarises them.
 *
 * A refusal from either call becomes a page saying so. Both are asked for at
 * install, so a refusal here means the user declined or later narrowed the
 * grant — which is a thing they did on purpose and deserves a sentence, not a
 * retry.
 */
export async function collect(ctx) {
  const workspaces = await ctx.mcp.supervisor('listWorkspaces', {})
  if (!workspaces.ok) return refusal(workspaces.reason)

  const tasks = await ctx.mcp.supervisor('listAllTasks', { limit: MAX_TASKS })
  if (!tasks.ok) return refusal(tasks.reason)

  return summarise(payload(workspaces.result)?.workspaces ?? [], payload(tasks.result)?.tasks ?? [])
}

function refusal(reason) {
  return { refused: reason }
}

/** What the header action and the menu item both show. */
export async function view(ctx) {
  const summary = await collect(ctx)
  if (summary.refused) {
    return { title: 'Daily Digest', nodes: [{ type: 'text', tone: 'critical', value: summary.refused }] }
  }
  return buildPage(summary)
}

/**
 * Mirrors the digest to a webhook, if one is configured.
 *
 * Both halves are required together: a path with no token, or a token with no
 * path, is a setting somebody started filling in. Sending to an incomplete
 * address is worse than not sending, because it looks configured.
 *
 * The token goes in the URL because that is where Slack puts it. It is
 * therefore never logged — not the URL, not a prefix of it, and the failure
 * message below deliberately says the status and nothing else.
 */
export async function mirror(ctx, text, { fetch: doFetch = fetch } = {}) {
  const path = String(ctx.config?.webhookPath ?? '').trim()
  const token = String(ctx.config?.webhookToken ?? '').trim()
  if (!path || !token) return { ok: false, reason: 'No webhook is configured.' }

  try {
    const response = await doFetch(`https://hooks.slack.com/services/${path}/${token}`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ text }),
    })
    if (!response.ok) return { ok: false, reason: `The webhook answered ${response.status}.` }
    return { ok: true }
  } catch {
    // The error is not passed on: a fetch failure message can carry the URL,
    // and the URL is the secret.
    return { ok: false, reason: 'The webhook could not be reached.' }
  }
}

/**
 * The schedule this extension wants to exist.
 *
 * Declared, not created. The host reconciles it against what is already on the
 * server — making it when it is missing, revising it when this changes, and
 * removing it when the extension goes away.
 */
export function scheduleFor(config) {
  const workspaceId = String(config?.workspaceId ?? '').trim()
  if (!workspaceId) return null

  return {
    kind: 'task',
    workspaceId,
    title: 'Daily digest',
    body: scheduleBody(config),
    assignee: 'agent',
    cron: String(config?.cron ?? '').trim() || DEFAULT_CRON,
  }
}

export function apply(ctx, config) {
  ctx.ui.add({
    id: 'digest',
    surface: 'page',
    label: 'Daily Digest',
    order: 30,
    view: () => view(ctx),
  })

  ctx.ui.add({
    id: 'now',
    surface: 'workspace-action',
    label: 'Digest now',
    order: 30,
    run: () => view(ctx),
  })

  ctx.ui.add({
    id: 'context',
    surface: 'task-menu',
    label: 'Digest since this task',
    order: 30,
    // Earns its row rather than taking one on every task: a digest anchored to
    // a task only means something for one that is finished.
    when: (task) => task?.status === 'completed',
    run: () => view(ctx),
  })

  const schedule = scheduleFor(config)
  if (schedule) {
    ctx.schedules.add({ id: 'daily', value: schedule })
  } else {
    // Said out loud rather than skipped silently. An extension that quietly
    // registers nothing because a setting is blank is one nobody can debug.
    ctx.logger.warn('No workspace is configured, so the daily digest is not scheduled.')
  }
}
