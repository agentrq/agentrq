/**
 * Task Stats — the smallest extension there is.
 *
 * It asks for **nothing**: no workspace tools, no supervisor tools, no network.
 * Everything it shows, it works out from the task it was handed.
 *
 * That is the point of it as an example. An extension that needs no permissions
 * should not have to pretend to, and the install screen for this one shows no
 * permission list at all — only the sentence saying that an extension runs with
 * full access to your computer, which is true here as it is everywhere else.
 * That screen is easy to get wrong in the direction that matters: an empty
 * permission list reads as safety, and the emptiest screen must not be the one
 * describing the least-restrained code.
 */

export const name = 'task-stats'

/**
 * Only the registry it uses.
 *
 * `inject` is not decorative — the host builds a context carrying exactly what
 * is asked for here, so an extension reaching for something it did not declare
 * finds it undefined rather than working by accident.
 */
export const inject = ['ui']

/** Rounded down, and plural-aware, because "1 days ago" is a bug people notice. */
export function describeAge(createdAt, now = Date.now()) {
  const created = new Date(createdAt ?? 0).getTime()
  if (!Number.isFinite(created) || created <= 0) return 'unknown'

  const minutes = Math.max(0, Math.floor((now - created) / 60000))
  if (minutes < 60) return count(minutes, 'minute')
  const hours = Math.floor(minutes / 60)
  if (hours < 48) return count(hours, 'hour')
  return count(Math.floor(hours / 24), 'day')
}

function count(value, noun) {
  return `${value} ${noun}${value === 1 ? '' : 's'}`
}

/** Words, not characters: a body is prose, and prose is measured in words. */
export function wordCount(text) {
  return String(text ?? '')
    .split(/\s+/)
    .filter(Boolean).length
}

/**
 * What the panel says about one task.
 *
 * A pure function of the task, which is what makes it worth testing on its own —
 * and what makes this extension work with no server involved at all.
 */
export function statsFor(task, now = Date.now()) {
  const messages = task?.messages ?? []
  const words = wordCount(task?.body) + messages.reduce((total, message) => total + wordCount(message?.text), 0)

  return {
    title: `Stats for "${task?.title ?? 'this task'}"`,
    nodes: [
      { type: 'rows', items: [
        { type: 'row', label: 'Age', value: describeAge(task?.createdAt, now) },
        { type: 'row', label: 'Messages', value: String(messages.length) },
        { type: 'row', label: 'Words', value: String(words) },
        { type: 'row', label: 'Status', value: String(task?.status ?? 'unknown') },
      ] },
      // Said rather than left blank: a panel of zeroes on a task nobody has
      // replied to looks broken, and one sentence is the difference.
      messages.length === 0
        ? { type: 'text', tone: 'muted', value: 'Nobody has replied to this task yet.' }
        : { type: 'text', tone: 'muted', value: 'Counted from the task body and every message in the thread.' },
    ],
  }
}

export function apply(ctx) {
  ctx.ui.add({
    id: 'stats',
    surface: 'task-menu',
    label: 'Task Stats',
    order: 10,
    // Every task has a title, so there is nothing to be conditional about. An
    // entry with no `when` is assumed to always apply, which is right here and
    // rare elsewhere — ten installed extensions each adding a permanent row
    // make a menu nobody reads.
    run: (task) => statsFor(task),
  })
}
