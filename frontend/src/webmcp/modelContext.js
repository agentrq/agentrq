// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * The seam between AgentRQ and the browser's WebMCP implementation.
 *
 * WebMCP lets a page hand an agent running *in the browser* a set of typed
 * tools, so the agent drives the application the way a person does rather than
 * guessing at its HTTP API. The catalogue of tools is `./tools.js`; this module
 * is only concerned with reaching the browser API at all, and with doing it in
 * a way that cannot break the app when the API is absent, moved, or refuses.
 *
 * Three things about that API make a seam worth having:
 *
 * - **It moved.** The accessor was `navigator.modelContext` when WebMCP first
 *   shipped and is `document.modelContext` since the May 2026 draft, with the
 *   old one deprecated rather than removed. Both are checked, newest first.
 * - **It is usually not there.** Only some browsers implement it, and only in a
 *   secure context. Absence is the ordinary case, not an error, so it is
 *   reported as a reason rather than thrown.
 * - **Registration can be refused.** A permissions policy, a duplicate name or
 *   a malformed schema rejects the promise. One bad tool must not take the
 *   others down with it, so each is registered on its own.
 *
 * Nothing here imports Vue: this is the part that talks to the browser, and
 * keeping it free of the framework is what makes it testable without one.
 */

/** Why the page has no tools registered. */
export const WebMCPStatus = {
  /** Tools are registered and the agent can see them. */
  Registered: 'registered',
  /** The browser does not implement WebMCP, or the page is not a secure context. */
  Unsupported: 'unsupported',
  /** Supported, but every tool was refused. */
  Refused: 'refused',
}

/**
 * The page's model context, or null when this browser has none.
 *
 * `document` first: `navigator.modelContext` is the deprecated spelling, kept
 * only so a browser that has not yet moved is still served.
 *
 * @param {{ document?: object, navigator?: object }} [globals] injectable for tests
 * @returns {object|null}
 */
export function getModelContext({ document: doc = globalThis.document, navigator: nav = globalThis.navigator } = {}) {
  const context = doc?.modelContext ?? nav?.modelContext ?? null
  // A stub that cannot register a tool is no more useful than no API at all,
  // and treating it as present would mean reporting tools that do not exist.
  return context && typeof context.registerTool === 'function' ? context : null
}

/**
 * Register a catalogue of tools with the browser.
 *
 * Every tool is registered against the same `AbortSignal`, which is how WebMCP
 * spells "unregister": aborting the controller withdraws them all at once. That
 * matters here because the tools act as the signed-in user — when the session
 * ends they have to stop being offered.
 *
 * Registration is per-tool and failures are collected rather than thrown: a
 * schema the browser dislikes should cost the app that one tool, not the whole
 * catalogue and not the page.
 *
 * @param {Array<object>} tools from `./tools.js`
 * @param {{ context?: object|null, signal?: AbortSignal }} [options]
 * @returns {Promise<{ status: string, registered: string[], refused: Array<{ name: string, reason: string }> }>}
 */
export async function registerTools(tools, { context = getModelContext(), signal } = {}) {
  if (!context) return { status: WebMCPStatus.Unsupported, registered: [], refused: [] }

  const registered = []
  const refused = []

  for (const tool of tools) {
    try {
      // `signal` goes in the options bag, not the descriptor: it is how the
      // registration is withdrawn, not something the agent sees.
      await context.registerTool(tool, signal ? { signal } : {})
      registered.push(tool.name)
    } catch (err) {
      refused.push({ name: tool.name, reason: err?.message || String(err) })
    }
  }

  // Nothing registered out of a non-empty catalogue means the browser said no
  // to all of it — a permissions policy, most likely. Worth distinguishing from
  // "this browser has no WebMCP", because only one of the two is fixable.
  const status = registered.length === 0 && tools.length > 0 ? WebMCPStatus.Refused : WebMCPStatus.Registered
  return { status, registered, refused }
}
