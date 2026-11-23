// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * Showing the person the screen a browser agent's tool acts on.
 *
 * A write tool declares the page it changes (`screen` in `src/webmcp/tools.js`).
 * This moves the person there — before the tool runs when the page follows from
 * its arguments, so a prompt to approve something destructive appears over the
 * screen it would change; after, when the page exists only in the result — and
 * says in a toast what the agent did.
 *
 * Unsaved input is never navigated away from: with a draft on the page the move
 * is held, and the toast carries a "Show" link instead.
 */

// Inputs that hold text somebody typed, not a choice or a control.
const TEXT_INPUTS = 'textarea, input:not([type=checkbox], [type=radio], [type=hidden], [type=search], [type=button], [type=submit], [type=range], [type=file], [type=color])'

/** Whether the current page holds text the person typed and has not sent. */
export function hasUnsavedInput(doc = globalThis.document) {
  return [...(doc?.querySelectorAll(TEXT_INPUTS) ?? [])].some(
    (el) => !el.readOnly && !el.disabled && el.value.trim() !== '',
  )
}

/** "restartDaemon" → "restart daemon" */
const words = (name) => name.replace(/([a-z0-9])([A-Z])/g, '$1 $2').toLowerCase()

/**
 * Wrap a write tool so it shows its screen. A tool without a `screen` is
 * returned as it is; one with a `screen` loses it from the descriptor, since it
 * is not something an agent is told.
 *
 * @param {object} tool a descriptor from `createToolCatalogue`
 * @param {object} deps
 * @param {(path: string) => Promise<unknown>} deps.go move the person
 * @param {(path: string) => boolean} deps.isCurrent whether they are already there
 * @param {() => boolean} deps.hasUnsavedInput
 * @param {(message: string, link: { path: string, label: string }|null) => void} deps.notify
 */
export function withScreen(tool, { go, isCurrent, hasUnsavedInput: hasDraft, notify }) {
  const { screen, ...rest } = tool
  if (!screen) return tool

  let held = null
  const show = async (path) => {
    if (!path || isCurrent(path)) return
    if (hasDraft()) {
      held = path
      return
    }
    try {
      await go(path)
    } catch {
      // The tool's work does not depend on the person having moved.
    }
  }

  return {
    ...rest,
    execute: async (args, ...more) => {
      held = null
      const input = args ?? {}
      await show(screen.before?.(input))
      const result = await tool.execute(args, ...more)
      await show(screen.after?.(input, result))
      notify(`Browser agent ran: ${words(tool.name)}`, held && { path: held, label: 'Show' })
      return result
    },
  }
}
