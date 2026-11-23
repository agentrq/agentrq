// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The terminal size a launch sends, worked out from the box it will actually
 * render into.
 *
 * Both launch panels — the machine page's form and the workspace's own —
 * navigate to `/sessions/<id>` the moment the daemon has been asked, and the
 * daemon creates the pty at the size in that request, before anything has
 * attached to resize it. Sending a hardcoded size here is what makes an
 * agent's first screen wrap wrong on a small window and waste the rest of a
 * large one; the fix is not a better constant, because no single constant is
 * right for both.
 *
 * `App.vue` never unmounts the element `CONTENT_AREA_SELECTOR` marks — Vue
 * Router swaps the routed page underneath it — so its size while a launch
 * panel is open is the size the terminal page is about to get. Only the
 * terminal page's own chrome on top of that (its heading row, the terminal
 * card's header bar, border and padding) has to be estimated, because that
 * page is not mounted yet to measure.
 */

import { usableProposal } from './useTerminalFit'
import { TERMINAL_OPTIONS } from './useTerminalView'

/**
 * Sent when the real box cannot be measured at all — no browser, no content
 * element yet, or one too small to mean anything. Large enough that an
 * agent's first output is not immediately wrapped; corrected the moment a
 * viewer attaches and sends its own size.
 */
export const FALLBACK_COLS = 120
export const FALLBACK_ROWS = 40

/** Marks the app shell's content box, in `App.vue`. */
export const CONTENT_AREA_SELECTOR = '[data-terminal-launch-area]'

/**
 * The terminal page's own chrome on top of the content box: `SessionTerminalView`'s
 * heading row and gap, plus `SessionTerminal`'s border, header bar and host
 * padding. Fixed regardless of window size, so — unlike the columns and rows
 * below — a constant is the honest answer for it rather than a stand-in for a
 * measurement that cannot be taken yet.
 */
const CHROME_WIDTH = 20
const CHROME_HEIGHT = 128

/**
 * Cols/rows for a box of this size, using the exact font the real terminal
 * renders with: a temporary, invisible `Terminal` and `FitAddon`, not a
 * hand-rolled "characters are about N px wide" — that guess is exactly the
 * kind of constant this file replaces, and it would drift the moment
 * `TERMINAL_OPTIONS` changed.
 *
 * `@xterm/xterm` and `@xterm/addon-fit` are imported dynamically so that
 * mounting a launch panel does not pull the terminal engine into that page's
 * chunk — it is only needed for the moment of launching.
 */
export async function fitToBox(width, height) {
  const [{ Terminal }, { FitAddon }] = await Promise.all([
    import('@xterm/xterm'),
    import('@xterm/addon-fit'),
  ])
  await import('@xterm/xterm/css/xterm.css')

  const host = document.createElement('div')
  host.style.cssText =
    `position:fixed; top:0; left:0; visibility:hidden; pointer-events:none;` +
    `width:${width}px; height:${height}px;`
  document.body.appendChild(host)

  const term = new Terminal(TERMINAL_OPTIONS)
  try {
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(host)
    return fit.proposeDimensions()
  } finally {
    term.dispose()
    host.remove()
  }
}

/**
 * What a launch should send for the box `query` finds, or the fallback when
 * there is nothing to measure.
 *
 * @param {object} [deps]
 * @param {() => Element|null} [deps.query] finds the content box. Defaults to
 *        `CONTENT_AREA_SELECTOR` in the live document.
 * @param {(width: number, height: number) => Promise<{cols:number,rows:number}|undefined>} [deps.fit]
 *        proposes cols/rows for a box of that size. Swapped in tests, which
 *        cannot lay out a real terminal in jsdom any more than the terminal
 *        page's own tests can.
 */
export async function launchTerminalSize({
  query = () => document.querySelector(CONTENT_AREA_SELECTOR),
  fit = fitToBox,
} = {}) {
  const area = query()
  if (!area) return { cols: FALLBACK_COLS, rows: FALLBACK_ROWS }

  const width = area.clientWidth - CHROME_WIDTH
  const height = area.clientHeight - CHROME_HEIGHT
  if (width < 1 || height < 1) return { cols: FALLBACK_COLS, rows: FALLBACK_ROWS }

  let proposed
  try {
    proposed = await fit(width, height)
  } catch {
    // A hidden probe failing is not a reason to refuse the launch itself.
    proposed = undefined
  }
  if (!usableProposal(proposed)) return { cols: FALLBACK_COLS, rows: FALLBACK_ROWS }
  return { cols: proposed.cols, rows: proposed.rows }
}
