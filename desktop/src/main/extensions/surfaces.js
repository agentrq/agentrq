// Copyright 2026 Contextual, Inc. https://agentrq.com

/**
 * Getting what an extension contributed across to the window that draws it.
 *
 * A registry entry is not a message. It holds functions — `when(task)` decides
 * whether a menu row belongs on *this* task, `run(task)` and `view(context)`
 * produce what to draw — and a function cannot cross a process boundary. So the
 * renderer never receives an entry; it receives a *description* of one, and asks
 * this side to run it.
 *
 * That is not only a serialisation detail. It is what keeps extension code out
 * of the renderer, which is on a privileged `app://` origin with a bridge to
 * files, the clipboard and the shell. Third-party code next to that bridge would
 * have the whole machine through an API built for the app's own UI.
 *
 * ## The predicate is evaluated here, not shipped over
 *
 * The renderer sends the task and gets back the rows that apply to it. The
 * alternative — sending every row with a flag — would mean either running the
 * predicate somewhere it cannot run, or dropping `when` entirely and giving
 * every installed extension a permanent row on every task.
 *
 * ## Everything an extension returns is caught
 *
 * `run` and `view` are somebody else's code called from an IPC handler. An
 * exception there would reject a bridge call and surface in the renderer as a
 * broken promise with a stack trace from a file the user has never heard of. A
 * refusal with the extension's name on it is the useful form.
 */

const fail = (reason) => ({ ok: false, reason })

/**
 * Which registry a surface's entries live in.
 *
 * `shortcut` is the odd one: a key sequence is registered in its own registry
 * because it has to be unique across every extension, while a page only has to
 * be unique within one. The renderer asks for surfaces rather than registries —
 * it should not have to know which is which — so the mapping is here.
 */
export const REGISTRY_FOR = Object.freeze({
  page: 'ui',
  'workspace-action': 'ui',
  'task-menu': 'ui',
  shortcut: 'shortcuts',
  // A renderer claims a fenced-code language and is asked to draw one block.
  'code-block': 'renderers',
})

export function registryFor(surface) {
  return REGISTRY_FOR[surface] ?? 'ui'
}

/** The fields that survive the crossing. Anything callable is deliberately not here. */
export function serialise(entry) {
  return {
    owner: entry.owner,
    id: entry.id,
    surface: entry.surface ?? '',
    label: entry.label ?? entry.id,
    order: entry.order,
    // Only shortcuts have one, and the help sheet needs it.
    ...(entry.key ? { key: entry.key } : {}),
    // Only renderers have one, and the message body needs it to know which
    // fences are worth asking about at all.
    ...(entry.language ? { language: entry.language } : {}),
  }
}

/**
 * Whether one entry applies to what the renderer is asking about.
 *
 * A predicate that throws is treated as "no". One extension deciding badly must
 * not empty a menu that other extensions are also in, and a thrown error is not
 * a reason to show a row whose own author could not say whether it belonged.
 */
export function applies(entry, context, onError = () => {}) {
  if (typeof entry.when !== 'function') return true
  try {
    return Boolean(entry.when(context))
  } catch (error) {
    onError(entry.owner, error)
    return false
  }
}

/**
 * Everything contributed to one surface that applies right now.
 *
 * @param {object[]} entries  Already resolved from the registry.
 * @param {string} surface    'page' | 'workspace-action' | 'task-menu'
 */
export function entriesFor(entries, surface, context, { onError = () => {} } = {}) {
  // Everything in the shortcuts registry is a shortcut, so there is nothing for
  // an entry to declare — asking authors to write `surface: 'shortcut'` next to
  // the key they are binding would be a field with one legal value.
  const belongs = (entry) => registryFor(surface) !== 'ui' || (entry.surface ?? '') === surface

  return entries
    .filter(belongs)
    .filter((entry) => applies(entry, context, onError))
    .map(serialise)
}

/**
 * Run one entry and answer with what it drew.
 *
 * `run` and `view` are the same thing under two names — an action produces a
 * panel, a page produces a page — so either is accepted rather than making an
 * author remember which surface takes which.
 */
export async function invokeEntry(entries, { owner, id, surface }, context) {
  const bySurface = registryFor(surface) === 'ui'
  const entry = entries.find(
    (candidate) =>
      candidate.owner === owner &&
      candidate.id === id &&
      (!bySurface || (candidate.surface ?? '') === (surface ?? '')),
  )
  // Ordinary rather than exceptional: an extension can be uninstalled between
  // a menu opening and something on it being clicked.
  if (!entry) return fail('That extension is no longer available.')

  const run = typeof entry.run === 'function' ? entry.run : entry.view
  if (typeof run !== 'function') return fail(`${owner} registered "${id}" with nothing to run.`)

  try {
    const view = await run(context)
    // A view is what this surface is for. An entry that returns nothing has
    // done something else — refreshed itself, opened a window — and saying so
    // is better than drawing an empty panel that looks like a failure.
    return view ? { ok: true, view } : { ok: true, view: null }
  } catch (error) {
    return fail(`${owner} failed: ${error?.message || String(error)}`)
  }
}
