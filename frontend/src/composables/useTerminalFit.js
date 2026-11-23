// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * When a terminal is sized to its box, and what to do when it cannot be.
 *
 * This is here rather than in `SessionTerminal.vue` because it is a policy with
 * three rules that fight each other, and all three have a bug behind them. A
 * component is where it was, and where none of them could be tested.
 *
 * ## A fit that did not happen looks exactly like one that did
 *
 * `FitAddon.fit()` returns nothing and throws nothing. Read its source: it asks
 * `proposeDimensions()` first, and that answers `undefined` whenever the
 * renderer has not measured a character cell yet —
 * `dimensions.css.cell.width === 0`. `fit()` then returns having done nothing
 * at all, and a caller that reads `term.cols` back gets **80×24**, xterm's
 * default, with no way to know it is not a measurement.
 *
 * So a fit is *attempted*, the attempt can fail, and a failed attempt is
 * retried on the next frame rather than recorded as a size.
 *
 * **How far that is actually established.** The no-op path is real and is
 * quoted from the addon's source. What is *not* established is that it happens
 * here: measured in headless Chromium against this app's own
 * `TERMINAL_OPTIONS`, `proposeDimensions()` answered a real size synchronously,
 * in the same tick as `open()`, on every attempt. So treat this file as a
 * guard against a documented hazard, not as the proven cure for a reported
 * symptom. If you are here because terminals are still mis-shaped on first
 * open, this is probably not where the cause lives — look for a
 * wrong-but-plausible measurement, which every guard in here would let past.
 *
 * ## Nothing else was going to retry
 *
 * The only other thing that fits is the resize observer, and an observer fires
 * when the box *changes*. A box mis-measured once and then left alone never
 * changes, so a bad measurement would survive until somebody resized something
 * by hand.
 *
 * Retries are bounded all the same. A terminal in a panel nobody has opened
 * measures zero every frame, and spinning a render-frame loop forever to
 * discover that is worse than waiting: when it is shown, the observer fires.
 *
 * ## It must never fit to a box that depends on its own height
 *
 * Two rules from the growing-terminal bug, and both are kept here rather than
 * relaxed — see the note in `docs/agents/machines-and-daemon.md`:
 *
 * - **A measurement that has not changed is not acted on.** Fitting makes the
 *   terminal taller, which is a box change, which fits again. The comparison
 *   is what breaks that loop, so `onSize` is called for a size that is new and
 *   never merely for a fit that ran.
 * - **A box too small to mean anything is not fitted to.** Fitting to zero
 *   throws away the dimensions the terminal needs for when it comes back.
 *   Unlike the loop guard, this one *is* transient, so it counts as a failed
 *   attempt and is retried.
 */

/**
 * How many frames a fit is retried for before waiting for the observer.
 *
 * Twenty is about a third of a second at 60Hz, which is far longer than a
 * renderer takes to measure a cell and still short enough that a hidden
 * terminal stops asking. It is not a timeout anybody waits on: the resize
 * observer picks up anything slower.
 */
export const FIT_ATTEMPTS = 20

/** The smallest box worth fitting to, in pixels. Below this, measuring is a lie. */
export const MIN_BOX = 2

/**
 * True when a proposal from the fit addon is a real measurement.
 *
 * `undefined` is the renderer not being ready. NaN is a box with no computed
 * size — `parseInt('')` on an element that has not been laid out. Neither is a
 * number of columns, and `isNaN` alone would let Infinity through.
 */
export function usableProposal(proposed) {
  if (!proposed) return false
  const { cols, rows } = proposed
  return Number.isFinite(cols) && Number.isFinite(rows) && cols >= 1 && rows >= 1
}

/**
 * Drive the sizing of one terminal.
 *
 * @param {object} deps
 * @param {() => {width: number, height: number}|null} deps.measure
 *        the host box, or null when there is no host to measure.
 * @param {() => {cols: number, rows: number}|undefined} deps.propose
 *        `FitAddon.proposeDimensions()`. Asked *before* fitting, because it is
 *        the only way to know whether a fit would do anything.
 * @param {() => {cols: number, rows: number}} deps.apply
 *        performs the fit and answers the size the terminal ended up at.
 * @param {(cols: number, rows: number) => void} [deps.onSize]
 *        a size that is new. Never called for a repeat, which is what stops a
 *        fit from being a reason to fit again.
 * @param {(fn: Function) => any} [deps.schedule]
 * @param {(handle: any) => void} [deps.cancel]
 * @param {number} [deps.attempts]
 */
export function useTerminalFit({
  measure,
  propose,
  apply,
  onSize = () => {},
  schedule = requestAnimationFrame,
  cancel = cancelAnimationFrame,
  attempts = FIT_ATTEMPTS,
}) {
  let frame = 0
  let left = 0
  let last = ''
  let stopped = false

  /**
   * One attempt, right now.
   *
   * Synchronous so the caller can have a real size *before* it tells anything
   * else how big the terminal is. The first size sent to the machine used to
   * be the pre-fit default, and an agent that painted its opening screen for
   * 80 columns keeps that screen however the terminal is resized afterwards.
   *
   * @returns {boolean} whether the terminal is now sized to its box.
   */
  function attempt() {
    if (stopped) return false

    const box = measure()
    // No host, or one too small to be a measurement. Transient in both cases —
    // a panel that is not on screen yet — so the caller is told the attempt
    // failed and it will be tried again.
    if (!box || box.width < MIN_BOX || box.height < MIN_BOX) return false

    // Asked before fitting rather than inferred after it. This is the whole
    // point of the file: `fit()` is a no-op when this is undefined, and
    // reading `term.cols` back afterwards cannot tell the difference.
    if (!usableProposal(propose())) return false

    const size = apply()
    if (!usableProposal(size)) return false

    const key = `${size.cols}x${size.rows}`
    if (key === last) return true
    last = key
    onSize(size.cols, size.rows)
    return true
  }

  /**
   * Ask for a fit, at most once per frame, and keep asking while the attempt
   * cannot be made.
   *
   * Coalescing is what keeps a window drag from fitting on every pixel. The
   * retry is what recovers from a renderer that was not ready, and it stops on
   * the first attempt that succeeds so that a settled terminal is not fitting
   * every frame forever.
   */
  function request() {
    if (stopped) return
    left = attempts
    if (frame) return
    frame = schedule(run)
  }

  function run() {
    frame = 0
    if (stopped) return
    if (attempt()) return
    if (--left <= 0) return
    frame = schedule(run)
  }

  /**
   * Fit now, and keep trying on the coming frames if it could not be done.
   *
   * What a caller reaches for on mount: the synchronous attempt is usually
   * enough, and when it is not, the retries are already scheduled rather than
   * left to whoever resizes something next.
   */
  function settle() {
    // One less than a bare request, because the synchronous attempt below is
    // the first of them. `attempts` is a count of tries, not of frames.
    left = attempts - 1
    if (attempt()) return true
    if (!frame) frame = schedule(run)
    return false
  }

  /** Give up on any scheduled attempt. For unmount. */
  function stop() {
    stopped = true
    if (frame) cancel(frame)
    frame = 0
  }

  return { attempt, request, settle, stop }
}

/**
 * Re-fit once the terminal's webfont is really there.
 *
 * xterm measures a character cell when the terminal opens. Before this repo
 * shipped a font that measurement was safe, because whatever the stack resolved
 * to was already installed — the cell it measured was the cell it would keep.
 * A webfont breaks that: the first measurement is of the *fallback*, the real
 * face arrives a moment later, and from then on every glyph is a different
 * width from the cell it is drawn into. Nothing re-measures on its own, so the
 * terminal stays that shape until somebody resizes something.
 *
 * ## `document.fonts.ready` alone is the wrong answer
 *
 * It reads like the whole fix and it is not, because **a font nothing has asked
 * for is not loading, and `ready` does not wait for it.** A face is only
 * fetched when a glyph needs it, so `ready` read in the same tick as `open()`
 * can resolve against whatever else was in flight — the UI font, say — and say
 * nothing about the terminal's. That version passes review and re-fits at the
 * wrong moment.
 *
 * So each face is `load()`ed first, which is what starts the request, and only
 * then is `ready` awaited. Measured: with `ready` alone the webfont had not
 * been applied by the time the terminal was measured.
 *
 * A face that fails to load is *not* a reason to skip the re-fit — the cell
 * then still holds the fallback that was measured at open, and `useTerminalFit`
 * drops a size that has not changed anyway.
 *
 * ## ...and re-fitting on its own changes nothing
 *
 * The second half, and the one that looks like it should not be needed: the fit
 * addon divides the host box by the cell xterm **cached when it opened**, so a
 * fit after the font lands returns the columns it already had. `remeasure` is
 * what makes the terminal measure the cell again, and without it this whole
 * file is an elaborate no-op. See `remeasureCell` in `useTerminalView`, which
 * is what the component passes and where the awkward part lives.
 *
 * @param {object} deps
 * @param {FontFaceSet} deps.fonts `document.fonts`, or nothing where there is
 *        no font loading API — jsdom, so the caller needs no guard of its own.
 * @param {string[]} deps.specs CSS font shorthands, one per face to wait for.
 * @param {() => void} deps.refit `useTerminalFit`'s `request`.
 * @param {() => void} [deps.remeasure] makes xterm measure a cell again, before
 *        the fit that reads it.
 * @returns {{cancel: () => void, done: Promise<boolean>}} `cancel` for unmount;
 *        `done` resolves to whether a re-fit was asked for, which is what a
 *        test can wait on.
 */
export function refitWhenFontsLoad({ fonts, specs, refit, remeasure = () => {} }) {
  let cancelled = false
  const cancel = () => {
    cancelled = true
  }

  if (typeof fonts?.load !== 'function') return { cancel, done: Promise.resolve(false) }

  const done = Promise.all(specs.map((spec) => fonts.load(spec)))
    .then(() => fonts.ready)
    // Swallowed on purpose: see above. The terminal is still showing something.
    .catch(() => {})
    .then(() => {
      // The terminal is gone. Touching a disposed one throws, and this promise
      // outlives the component that started it by design.
      if (cancelled) return false
      // Before the fit, never after: the fit is what reads the new cell.
      remeasure()
      refit()
      return true
    })

  return { cancel, done }
}
