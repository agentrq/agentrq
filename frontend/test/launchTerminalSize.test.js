// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The size a launch sends, worked out from a box rather than hardcoded.
 *
 * `fit` is faked throughout, the same way `terminalFit.test.js` fakes the fit
 * addon: jsdom does no layout, so a real `Terminal` mounted here would measure
 * nothing meaningful either way, and these tests are about the arithmetic and
 * the fallback, not about xterm's own sizing.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import {
  launchTerminalSize,
  fitToBox,
  FALLBACK_COLS,
  FALLBACK_ROWS,
  CONTENT_AREA_SELECTOR,
} from '../src/composables/useLaunchTerminalSize.js'

describe('launchTerminalSize', () => {
  it('falls back when there is no content box to measure', async () => {
    const size = await launchTerminalSize({ query: () => null, fit: vi.fn() })
    expect(size).toEqual({ cols: FALLBACK_COLS, rows: FALLBACK_ROWS })
  })

  it('falls back when the box is too small to mean anything, once chrome is removed', async () => {
    const fit = vi.fn()
    const size = await launchTerminalSize({
      query: () => ({ clientWidth: 10, clientHeight: 10 }),
      fit,
    })
    expect(size).toEqual({ cols: FALLBACK_COLS, rows: FALLBACK_ROWS })
    expect(fit).not.toHaveBeenCalled()
  })

  it('fits the box with the chrome around the terminal removed first', async () => {
    const fit = vi.fn().mockResolvedValue({ cols: 214, rows: 61 })
    const size = await launchTerminalSize({
      query: () => ({ clientWidth: 1200, clientHeight: 800 }),
      fit,
    })
    expect(size).toEqual({ cols: 214, rows: 61 })
    const [width, height] = fit.mock.calls[0]
    // The exact allowance is chrome, not a rendering measurement, so this
    // only pins that some of the box was set aside for it rather than the
    // whole box being handed to the fit.
    expect(width).toBeLessThan(1200)
    expect(height).toBeLessThan(800)
  })

  it('falls back when the fit addon has not measured a cell yet', async () => {
    const size = await launchTerminalSize({
      query: () => ({ clientWidth: 1200, clientHeight: 800 }),
      fit: vi.fn().mockResolvedValue(undefined),
    })
    expect(size).toEqual({ cols: FALLBACK_COLS, rows: FALLBACK_ROWS })
  })

  it('falls back on a proposal with no real measurement in it', async () => {
    for (const bad of [{ cols: NaN, rows: 40 }, { cols: 0, rows: 40 }, { cols: 120, rows: 0 }]) {
      const size = await launchTerminalSize({
        query: () => ({ clientWidth: 1200, clientHeight: 800 }),
        fit: vi.fn().mockResolvedValue(bad),
      })
      expect(size, JSON.stringify(bad)).toEqual({ cols: FALLBACK_COLS, rows: FALLBACK_ROWS })
    }
  })

  it('falls back rather than refusing the launch when the probe throws', async () => {
    const size = await launchTerminalSize({
      query: () => ({ clientWidth: 1200, clientHeight: 800 }),
      fit: vi.fn().mockRejectedValue(new Error('no GPU here')),
    })
    expect(size).toEqual({ cols: FALLBACK_COLS, rows: FALLBACK_ROWS })
  })

  it('finds the box by the marker App.vue puts on its content area', async () => {
    const area = document.createElement('div')
    area.setAttribute('data-terminal-launch-area', '')
    Object.defineProperty(area, 'clientWidth', { value: 1000 })
    Object.defineProperty(area, 'clientHeight', { value: 700 })
    document.body.appendChild(area)

    try {
      const fit = vi.fn().mockResolvedValue({ cols: 100, rows: 30 })
      const size = await launchTerminalSize({ fit })
      expect(document.querySelector(CONTENT_AREA_SELECTOR)).toBe(area)
      expect(size).toEqual({ cols: 100, rows: 30 })
    } finally {
      area.remove()
    }
  })
})

describe('fitToBox', () => {
  const openedOn = { current: null }
  let disposed = false

  beforeEach(() => {
    disposed = false
    vi.resetModules()
    vi.doMock('@xterm/xterm/css/xterm.css', () => ({}))
    vi.doMock('@xterm/xterm', () => ({
      Terminal: class {
        constructor() {}
        loadAddon() {}
        open(el) {
          openedOn.current = el
        }
        dispose() {
          disposed = true
        }
      },
    }))
    vi.doMock('@xterm/addon-fit', () => ({
      FitAddon: class {
        proposeDimensions() {
          return { cols: 88, rows: 27 }
        }
      },
    }))
  })

  afterEach(() => {
    vi.doUnmock('@xterm/xterm')
    vi.doUnmock('@xterm/addon-fit')
    vi.doUnmock('@xterm/xterm/css/xterm.css')
    vi.resetModules()
  })

  it('opens a hidden terminal sized to the box, reads its proposal, and cleans up', async () => {
    const { fitToBox: fresh } = await import('../src/composables/useLaunchTerminalSize.js')
    const before = document.body.childElementCount

    const proposed = await fresh(300, 150)

    expect(proposed).toEqual({ cols: 88, rows: 27 })
    expect(openedOn.current).toBeTruthy()
    expect(openedOn.current.style.width).toBe('300px')
    expect(openedOn.current.style.height).toBe('150px')
    expect(disposed).toBe(true)
    // The probe host is removed again rather than left in the page.
    expect(document.body.childElementCount).toBe(before)
  })
})
