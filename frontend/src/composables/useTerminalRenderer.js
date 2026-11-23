// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * Which of xterm's renderers draws the terminal, and what happens when it dies.
 *
 * ## Why a renderer addon at all
 *
 * Without one xterm uses its **DOM renderer**, which asks the *font* for box
 * drawing and block elements. The WebGL renderer draws those glyphs itself
 * (xterm's `customGlyphs`, which its own typings note "doesn't work with the
 * DOM renderer"), so they join up regardless of what the font has — and the
 * shipped font's subset has no box drawing at all. An agent's framed interface
 * is most of what arrives here, so this is not a polish change.
 *
 * ## Why it is a list rather than an addon
 *
 * **WebGL is not always there.** Some Linux boxes, most VMs, a Chromium started
 * without a GPU, and the Electron build on a machine with a blocked driver all
 * fail it — and they fail it *late*, inside `activate()`, not in the
 * constructor, so `new WebglAddon()` succeeding proves nothing. Each candidate
 * is therefore tried by loading it, and a throw moves to the next one. The last
 * rung is no addon at all, which is the DOM renderer: a terminal that renders
 * imperfectly beats one that renders nothing.
 *
 * Today the caller passes one candidate. The list is here because the fallback
 * *chain* is the thing being tested, and because `@xterm/addon-canvas` is the
 * obvious next rung the day its release catches up with the engine — see the
 * note where the candidates are built.
 *
 * ## Context loss is not an error, it is an event
 *
 * A GPU can take its context away at any time — a driver reset, a laptop
 * switching cards, too many live contexts in other tabs. The addon says so
 * through `onContextLoss` and then **draws nothing ever again**: the terminal
 * goes blank and stays blank, which looks exactly like an agent that stopped
 * talking. So the addon is disposed and rebuilt when it fires.
 *
 * Rebuilding is bounded. A machine whose context is lost the moment it is
 * granted would otherwise rebuild forever, burning a GPU context each time, and
 * the thing it is fighting for is a renderer it evidently cannot keep. After
 * `recreations` attempts it drops to the next candidate and stays there.
 */

/** What it is called when no addon could be loaded. */
export const DOM_RENDERER = 'dom'

/**
 * How many times a renderer is rebuilt after losing its context before the
 * next one down is used instead.
 *
 * One, because a context lost twice in a session is a machine saying no. It is
 * not a retry budget for a transient failure — the first rebuild covers the
 * driver reset that everybody actually hits.
 */
export const RECREATIONS = 1

/**
 * Attach the best renderer that works.
 *
 * @param {object} deps
 * @param {import('@xterm/xterm').Terminal} deps.term the opened terminal.
 *        **Must already be `open()`ed** — a renderer addon has nothing to
 *        attach to before there is an element, and fails if asked early.
 * @param {{name: string, make: () => object}[]} deps.candidates in order of
 *        preference. `make` is called at most once per attempt, so a failed
 *        addon is never reused.
 * @param {(name: string) => void} [deps.onRenderer] told what ended up drawing,
 *        including `'dom'` when nothing would load, and told again after a
 *        context loss changes the answer.
 * @param {number} [deps.recreations]
 */
export function useTerminalRenderer({
  term,
  candidates = [],
  onRenderer = () => {},
  recreations = RECREATIONS,
}) {
  /** The loaded addon, or null when the DOM renderer is drawing. */
  let addon = null
  /** Unsubscribes the context-loss listener. */
  let unwatch = null
  let name = DOM_RENDERER
  let left = recreations
  let stopped = false

  /**
   * Let go of whatever is attached.
   *
   * `dispose()` is allowed to throw here and is caught: a WebGL addon whose
   * context has just been taken away is exactly the case this is called for,
   * and it has no context left to tear down cleanly.
   */
  function detach() {
    unwatch?.()
    unwatch = null
    try {
      addon?.dispose()
    } catch {
      // Nothing to do and nothing to say. The addon is being dropped either
      // way, and a renderer that cannot die quietly must not take the terminal
      // down with it.
    }
    addon = null
  }

  /** Watch for the GPU taking the context back, where the addon reports it. */
  function watchContextLoss(from) {
    // Only the WebGL addon has this, so a candidate without it is not a
    // candidate that forgot — there is simply no context for it to lose.
    if (typeof addon.onContextLoss !== 'function') return

    const sub = addon.onContextLoss(() => {
      if (stopped) return
      // Rebuild the same renderer while there is budget, then stop asking.
      if (left > 0) {
        left -= 1
        attachFrom(from)
        return
      }
      attachFrom(from + 1)
    })
    unwatch = () => sub?.dispose?.()
  }

  /**
   * Load the first candidate at or after `from` that works.
   *
   * @returns {string} what is drawing now.
   */
  function attachFrom(from) {
    detach()

    for (let i = from; i < candidates.length; i += 1) {
      let made = null
      try {
        made = candidates[i].make()
        // The real test. A WebGL context that cannot be had fails in here,
        // not in the constructor above.
        term.loadAddon(made)
      } catch {
        // This machine cannot have this renderer. Drop whatever was half-built
        // and try the next one; `detach` is not used because `made` was never
        // the attached addon.
        try {
          made?.dispose()
        } catch {
          // Already broken. It is being thrown away regardless.
        }
        continue
      }

      addon = made
      name = candidates[i].name
      watchContextLoss(i)
      onRenderer(name)
      return name
    }

    name = DOM_RENDERER
    onRenderer(name)
    return name
  }

  return {
    /** Attach the best renderer available. Call once, after `term.open()`. */
    attach: () => attachFrom(0),
    /** What is drawing. */
    current: () => name,
    /** For unmount. Stops any rebuild already in flight. */
    dispose() {
      stopped = true
      detach()
      name = DOM_RENDERER
    },
  }
}
