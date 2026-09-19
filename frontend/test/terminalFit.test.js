// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * When a terminal is sized to its box.
 *
 * The hazard behind this file: `FitAddon.fit()` does nothing at all until the
 * renderer has measured a character cell, and it says nothing about having
 * done nothing — so a fit can leave the terminal at xterm's default 80×24 and
 * the caller cannot tell. Nothing would try again either, because the only
 * other thing that fits is an observer that fires on a box *change*.
 *
 * These tests drive that state deliberately, through the fake below. Worth
 * knowing: in a real browser it was never observed — headless Chromium had a
 * usable proposal synchronously after `open()` every time it was measured. So
 * these cover a documented failure mode of the addon, not a reproduction of
 * the report that prompted them.
 */
import { describe, it, expect, vi } from 'vitest'
import { useTerminalFit, usableProposal, FIT_ATTEMPTS, MIN_BOX } from '../src/composables/useTerminalFit.js'

/**
 * A terminal that behaves the way xterm does.
 *
 * `ready` is the renderer having measured a cell. While it is false,
 * `propose()` answers undefined and `fit()` is a no-op that leaves the size at
 * the default — which is precisely the state a caller cannot detect.
 */
function fakeTerminal({ box = { width: 800, height: 400 }, ready = false, cell = 8 } = {}) {
  const t = {
    box,
    ready,
    cols: 80,
    rows: 24,
    fits: 0,
    sizes: [],
    frames: [],
  }

  t.deps = {
    measure: () => t.box,
    propose: () => {
      if (!t.ready || !t.box) return undefined
      return { cols: Math.floor(t.box.width / cell), rows: Math.floor(t.box.height / (cell * 2)) }
    },
    apply: () => {
      t.fits += 1
      // What fit() actually does: nothing, unless there is a proposal.
      const proposed = t.deps.propose()
      if (proposed) {
        t.cols = proposed.cols
        t.rows = proposed.rows
      }
      return { cols: t.cols, rows: t.rows }
    },
    onSize: (cols, rows) => t.sizes.push(`${cols}x${rows}`),
    schedule: (fn) => {
      t.frames.push(fn)
      return t.frames.length
    },
    cancel: vi.fn(),
  }

  t.fitter = useTerminalFit(t.deps)
  /** Run one scheduled frame, the way the browser would. */
  t.frame = () => {
    const fn = t.frames.shift()
    if (fn) fn()
    return Boolean(fn)
  }
  return t
}

describe('usableProposal', () => {
  it('accepts a real measurement', () => {
    expect(usableProposal({ cols: 100, rows: 30 })).toBe(true)
  })

  // Each of these is a way the fit addon says "not yet", and every one of them
  // read as a size before this existed.
  it('refuses everything that is not one', () => {
    // The renderer has not measured a cell: proposeDimensions() returns this.
    expect(usableProposal(undefined)).toBe(false)
    expect(usableProposal(null)).toBe(false)
    // An unlaid-out box: parseInt('') inside the addon.
    expect(usableProposal({ cols: NaN, rows: 30 })).toBe(false)
    expect(usableProposal({ cols: 100, rows: NaN })).toBe(false)
    // isNaN alone — which is what the addon itself checks — lets these past.
    expect(usableProposal({ cols: Infinity, rows: 30 })).toBe(false)
    expect(usableProposal({ cols: 0, rows: 30 })).toBe(false)
  })
})

describe('the opening fit', () => {
  it('fits synchronously when the renderer is ready', () => {
    const t = fakeTerminal({ ready: true })

    expect(t.fitter.settle()).toBe(true)
    // Synchronously, with no frame run: the caller sends this size to the
    // machine on the next line, and a size that arrives a frame later is the
    // default going out instead.
    expect(t.cols).toBe(100)
    expect(t.sizes).toEqual(['100x25'])
  })

  // The bug. The renderer is not ready on the first attempt, which is the
  // normal state right after `open()`.
  it('keeps trying when the renderer has not measured a cell yet', () => {
    const t = fakeTerminal({ ready: false })

    expect(t.fitter.settle()).toBe(false)
    // Nothing recorded, and nothing claimed: 80×24 is not a measurement.
    expect(t.cols).toBe(80)
    expect(t.sizes).toEqual([])
    // A retry is already scheduled — it is not left to whoever resizes
    // something next, which was the whole problem.
    expect(t.frames).toHaveLength(1)

    t.ready = true
    t.frame()

    expect(t.cols).toBe(100)
    expect(t.sizes).toEqual(['100x25'])
  })

  it('stops retrying once one succeeds', () => {
    const t = fakeTerminal({ ready: false })
    t.fitter.settle()
    t.ready = true
    t.frame()

    // A settled terminal must not be fitting on every frame for the rest of
    // the session.
    expect(t.frames).toHaveLength(0)
  })

  // A terminal in a panel nobody opened measures zero forever. Retrying
  // forever to discover that costs a render-frame loop for nothing — and the
  // observer will fit it the moment it is shown.
  it('gives up after a bounded number of frames', () => {
    const t = fakeTerminal({ ready: false })
    t.fitter.settle()

    let frames = 0
    while (t.frame()) frames += 1

    expect(frames).toBe(FIT_ATTEMPTS - 1)
    expect(t.sizes).toEqual([])
  })
})

describe('a fit that ran and still produced nothing', () => {
  // Belt and braces over the addon's private-API reach: `fit()` uses
  // `term._core._renderService`, so a version bump that changes that shape
  // could leave the terminal reporting a size that is not one. Treated as a
  // failed attempt, so it is retried rather than recorded.
  it('is not recorded as a size', () => {
    const t = fakeTerminal({ ready: true })
    t.deps.apply = () => {
      t.fits += 1
      return { cols: NaN, rows: NaN }
    }
    const fitter = useTerminalFit(t.deps)

    expect(fitter.settle()).toBe(false)
    expect(t.sizes).toEqual([])
    expect(t.frames).toHaveLength(1)
  })
})

describe('a box not worth measuring', () => {
  it('is not fitted to, and is retried', () => {
    // Fitting to zero throws away the dimensions the terminal needs for when
    // it comes back, so this is a failed attempt rather than a size.
    const t = fakeTerminal({ ready: true, box: { width: 800, height: MIN_BOX - 1 } })

    expect(t.fitter.settle()).toBe(false)
    expect(t.fits).toBe(0)
    expect(t.sizes).toEqual([])

    t.box = { width: 800, height: 400 }
    t.frame()
    expect(t.sizes).toEqual(['100x25'])
  })

  it('covers a zero width as well as a zero height', () => {
    const t = fakeTerminal({ ready: true, box: { width: 0, height: 400 } })
    expect(t.fitter.settle()).toBe(false)
    expect(t.fits).toBe(0)
  })

  // The host is a template ref, and it is null after unmount.
  it('is not fitted to when there is no host at all', () => {
    const t = fakeTerminal({ ready: true })
    t.box = null
    expect(t.fitter.settle()).toBe(false)
    expect(t.fits).toBe(0)
  })
})

describe('requesting a fit', () => {
  it('coalesces to one attempt a frame', () => {
    const t = fakeTerminal({ ready: true })
    // What a window drag looks like: an observer callback per pixel.
    t.fitter.request()
    t.fitter.request()
    t.fitter.request()

    expect(t.frames).toHaveLength(1)
    t.frame()
    expect(t.fits).toBe(1)
  })

  // The loop guard from the growing-terminal bug, which must survive this
  // change: fitting makes the terminal taller, a taller terminal is a box
  // change, and a box change asks for a fit. Reporting a size that has not
  // changed is what closes that circle.
  it('reports a size once, however many times it is asked', () => {
    const t = fakeTerminal({ ready: true })
    t.fitter.settle()
    t.fitter.request()
    t.frame()
    t.fitter.request()
    t.frame()

    expect(t.fits).toBe(3)
    expect(t.sizes).toEqual(['100x25'])
  })

  // `settle` is called on mount and `request` from the observer, and the
  // observer's first callback lands in the same tick. Two schedules for one
  // frame would run the retry chain twice over.
  it('does not schedule twice when one is already pending', () => {
    const t = fakeTerminal({ ready: false })
    t.fitter.request()
    expect(t.frames).toHaveLength(1)

    expect(t.fitter.settle()).toBe(false)
    expect(t.frames).toHaveLength(1)
  })

  it('reports a size that really changed', () => {
    const t = fakeTerminal({ ready: true })
    t.fitter.settle()

    // Somebody collapsed the sidebar.
    t.box = { width: 1200, height: 400 }
    t.fitter.request()
    t.frame()

    expect(t.sizes).toEqual(['100x25', '150x25'])
  })

  it('retries a request that could not be made, not just the first one', () => {
    const t = fakeTerminal({ ready: true })
    t.fitter.settle()

    // The panel is hidden — a real box change, to a box not worth fitting to.
    t.box = { width: 0, height: 0 }
    t.fitter.request()
    t.frame()
    expect(t.frames).toHaveLength(1)

    t.box = { width: 1200, height: 400 }
    t.frame()
    expect(t.sizes).toEqual(['100x25', '150x25'])
  })
})

describe('stopping', () => {
  it('cancels a scheduled attempt and refuses later ones', () => {
    const t = fakeTerminal({ ready: false })
    t.fitter.settle()
    expect(t.frames).toHaveLength(1)

    t.fitter.stop()
    expect(t.deps.cancel).toHaveBeenCalled()

    // A frame already queued with the browser still runs, and must do nothing:
    // the terminal it would fit has been disposed.
    t.ready = true
    t.frame()
    expect(t.fits).toBe(0)

    // And nothing new is scheduled or attempted.
    t.fitter.request()
    expect(t.fitter.attempt()).toBe(false)
    expect(t.frames).toHaveLength(0)
    expect(t.sizes).toEqual([])
  })

  it('is safe with nothing scheduled', () => {
    const t = fakeTerminal({ ready: true })
    t.fitter.stop()
    expect(t.deps.cancel).not.toHaveBeenCalled()
  })
})

describe('the defaults', () => {
  it('schedules on animation frames, and reports nothing to nobody', () => {
    // The real timers and a default onSize, exercised once: a caller that
    // passes neither must still fit.
    const raf = vi.spyOn(window, 'requestAnimationFrame').mockReturnValue(7)
    const cancelled = vi.spyOn(window, 'cancelAnimationFrame').mockImplementation(() => {})

    const fitter = useTerminalFit({
      measure: () => ({ width: 800, height: 400 }),
      propose: () => undefined,
      apply: () => ({ cols: 80, rows: 24 }),
    })
    expect(fitter.settle()).toBe(false)
    expect(raf).toHaveBeenCalled()

    fitter.stop()
    expect(cancelled).toHaveBeenCalledWith(7)

    raf.mockRestore()
    cancelled.mockRestore()
  })

  it('reports a size with no onSize given', () => {
    const fitter = useTerminalFit({
      measure: () => ({ width: 800, height: 400 }),
      propose: () => ({ cols: 100, rows: 25 }),
      apply: () => ({ cols: 100, rows: 25 }),
      schedule: () => 0,
    })
    expect(fitter.settle()).toBe(true)
  })
})
