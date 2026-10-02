// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * Notes — an extension with a page of its own, in the desktop side panel.
 *
 * Every other surface is a view spec AgentRQ draws. A panel page is not: it is
 * this extension's own HTML, CSS and JS (`panel/`), served from its own origin
 * (`agentrq-ext://panel-notes/…`) into the side panel. It can draw anything.
 *
 * It cannot reach anything, though. The page has no Node, no AgentRQ session
 * and no MCP — it has `window.agentrq.panel`, a line to *this* extension and no
 * other, and it asks for what it needs. This file answers. That split is the
 * pattern: the page draws, the extension decides.
 *
 * It asks for no permissions. What it keeps, it keeps in `ctx.storage`.
 */

export const name = 'panel-notes'

/** `panel` is not a registry: it is how the extension opens its page and talks to it. */
export const inject = ['ui', 'panel']

/** Where the notes live, in this extension's own storage. */
export const NOTES_KEY = 'text'

/** A task, as a line in the notes. */
export function noteLine(task) {
  const title = String(task?.title ?? '').trim() || 'Untitled task'
  return `- ${title}`
}

/** Notes with one more line, without a blank line between them. */
export function appendLine(text, line) {
  const current = String(text ?? '')
  if (!current) return line
  return current.endsWith('\n') ? `${current}${line}` : `${current}\n${line}`
}

/**
 * What the page asks, answered.
 *
 * `load` when it opens, `save` as somebody types. Anything else is refused by
 * throwing, which reaches the page as a rejected `send` with this extension's
 * name on it.
 */
export async function answer(storage, message) {
  if (message?.type === 'load') return { text: (await storage.get(NOTES_KEY)) ?? '' }
  if (message?.type === 'save') {
    await storage.set(NOTES_KEY, String(message.text ?? ''))
    return { saved: true }
  }
  throw new Error(`Notes does not know "${message?.type ?? 'nothing'}".`)
}

export function apply(ctx) {
  ctx.panel.onMessage((message) => answer(ctx.storage, message))

  ctx.ui.add({
    id: 'add-to-notes',
    surface: 'task-menu',
    label: 'Add to notes',
    // Returns nothing: this opens a page beside the task rather than drawing a
    // panel, and nothing is the answer for "did something else".
    async run(task) {
      const text = appendLine(await ctx.storage.get(NOTES_KEY), noteLine(task))
      await ctx.storage.set(NOTES_KEY, text)
      // An open page is told at once. A closed one loads the new text when it
      // opens, so the post being dropped then loses nothing.
      ctx.panel.post({ type: 'changed', text })
      await ctx.panel.open({ page: 'notes' })
    },
  })
}
