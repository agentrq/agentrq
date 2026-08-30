import { describe, it, expect, vi } from 'vitest'

import {
  DEFAULT_WINDOW_STATE,
  MIN_HEIGHT,
  MIN_WIDTH,
  captureWindowState,
  clampToDisplays,
  createWindowStateStore,
  debounce,
  normalizeWindowState,
} from '../src/main/window-state.js'

const laptop = { x: 0, y: 0, width: 1440, height: 900 }
const external = { x: 1440, y: 0, width: 2560, height: 1440 }

describe('clampToDisplays', () => {
  it('keeps a position that is still on a display', () => {
    expect(clampToDisplays({ x: 100, y: 100, width: 1200, height: 800 }, [laptop])).toEqual({
      x: 100,
      y: 100,
      width: 1200,
      height: 800,
    })
  })

  it('keeps a position on a secondary display', () => {
    expect(clampToDisplays({ x: 1600, y: 200, width: 1200, height: 800 }, [laptop, external])).toMatchObject({
      x: 1600,
      y: 200,
    })
  })

  it('drops a position whose display is gone', () => {
    // The classic case: saved on an external monitor, reopened after
    // undocking. Returning size only lets the platform centre the window
    // rather than placing it where the user cannot see or drag it.
    expect(clampToDisplays({ x: 2000, y: 300, width: 1200, height: 800 }, [laptop])).toEqual({
      width: 1200,
      height: 800,
    })
  })

  it('drops a position that is barely on screen', () => {
    // A window with a few pixels showing is a window the user has lost.
    expect(clampToDisplays({ x: 1400, y: 100, width: 1200, height: 800 }, [laptop])).toEqual({
      width: 1200,
      height: 800,
    })
  })

  it('pulls a partly off-screen window back into view', () => {
    const result = clampToDisplays({ x: 1000, y: 700, width: 1200, height: 800 }, [laptop])
    expect(result.x).toBe(240)
    expect(result.y).toBe(100)
  })

  it('shrinks a window saved on a larger display', () => {
    const result = clampToDisplays({ x: 1440, y: 0, width: 2400, height: 1300 }, [external])
    expect(result.width).toBe(2400)
    expect(clampToDisplays({ x: 0, y: 0, width: 2400, height: 1300 }, [laptop])).toEqual({
      x: 0,
      y: 0,
      width: 1440,
      height: 900,
    })
  })

  it('never restores a window below its minimum size', () => {
    expect(clampToDisplays({ x: 0, y: 0, width: 100, height: 100 }, [laptop])).toMatchObject({
      width: MIN_WIDTH,
      height: MIN_HEIGHT,
    })
  })

  it('falls back to defaults when there is nothing usable', () => {
    expect(clampToDisplays(null, [laptop])).toEqual({
      width: DEFAULT_WINDOW_STATE.width,
      height: DEFAULT_WINDOW_STATE.height,
    })
    expect(clampToDisplays({ width: 1200, height: 800 }, [])).toEqual({ width: 1200, height: 800 })
    expect(clampToDisplays({ width: 1200, height: 800 }, null)).toEqual({ width: 1200, height: 800 })
  })

  it('ignores a position that is not a real number', () => {
    for (const bad of [NaN, Infinity, '100', null]) {
      expect(clampToDisplays({ x: bad, y: 0, width: 1200, height: 800 }, [laptop])).toEqual({
        width: 1200,
        height: 800,
      })
    }
  })
})

describe('normalizeWindowState', () => {
  it('keeps a well-formed state', () => {
    expect(normalizeWindowState({ x: 10, y: 20, width: 1200, height: 800, maximized: true })).toEqual({
      x: 10,
      y: 20,
      width: 1200,
      height: 800,
      maximized: true,
    })
  })

  it('drops a position that is not usable but keeps the size', () => {
    expect(normalizeWindowState({ width: 1200, height: 800 })).toEqual({
      width: 1200,
      height: 800,
      maximized: false,
    })
  })

  it('raises a too-small size to the minimum', () => {
    expect(normalizeWindowState({ width: 10, height: 10 })).toMatchObject({
      width: MIN_WIDTH,
      height: MIN_HEIGHT,
    })
  })

  it('falls back to the default size when the stored one is unusable', () => {
    // A file written by an older build, or one truncated mid-write, can carry a
    // position but no size.
    expect(normalizeWindowState({ x: 10, y: 20 })).toEqual({
      x: 10,
      y: 20,
      width: DEFAULT_WINDOW_STATE.width,
      height: DEFAULT_WINDOW_STATE.height,
      maximized: false,
    })
    expect(normalizeWindowState({ width: NaN, height: '800' })).toMatchObject({
      width: DEFAULT_WINDOW_STATE.width,
      height: DEFAULT_WINDOW_STATE.height,
    })
  })

  it('falls back to defaults for anything unusable', () => {
    for (const raw of [null, undefined, 'string', 42, []]) {
      expect(normalizeWindowState(raw)).toEqual(DEFAULT_WINDOW_STATE)
    }
  })

  it('treats a non-boolean maximized as false', () => {
    expect(normalizeWindowState({ width: 1200, height: 800, maximized: 'yes' }).maximized).toBe(false)
  })
})

describe('createWindowStateStore', () => {
  function makeStore(initial = null) {
    let contents = initial
    return {
      store: createWindowStateStore({
        readFile: async () => {
          if (contents === null) throw new Error('ENOENT')
          return contents
        },
        writeFile: async (next) => {
          contents = next
        },
      }),
      read: () => contents,
    }
  }

  it('returns defaults on first run', async () => {
    const { store } = makeStore(null)
    expect(await store.load()).toEqual(DEFAULT_WINDOW_STATE)
  })

  it('returns defaults for a corrupt file rather than failing to open', async () => {
    const { store } = makeStore('{ not json')
    expect(await store.load()).toEqual(DEFAULT_WINDOW_STATE)
  })

  it('round-trips a state', async () => {
    const { store } = makeStore(null)
    await store.save({ x: 10, y: 20, width: 1200, height: 800, maximized: false })
    expect(await store.load()).toEqual({ x: 10, y: 20, width: 1200, height: 800, maximized: false })
  })

  it('normalises on the way in as well as out', async () => {
    const { store, read } = makeStore(null)
    await store.save({ width: 10, height: 10 })
    expect(JSON.parse(read())).toMatchObject({ width: MIN_WIDTH, height: MIN_HEIGHT })
  })
})

describe('captureWindowState', () => {
  it('records the restore size, not the maximized size', () => {
    // getBounds on a maximized window reports the screen; restoring to that
    // would lose the size the user actually chose.
    const win = {
      getNormalBounds: () => ({ x: 10, y: 20, width: 1200, height: 800 }),
      getBounds: () => ({ x: 0, y: 0, width: 2560, height: 1440 }),
      isMaximized: () => true,
    }

    expect(captureWindowState(win)).toEqual({ x: 10, y: 20, width: 1200, height: 800, maximized: true })
  })

  it('falls back to getBounds when normal bounds are unavailable', () => {
    const win = {
      getBounds: () => ({ x: 5, y: 6, width: 1000, height: 700 }),
      isMaximized: () => false,
    }

    expect(captureWindowState(win)).toEqual({ x: 5, y: 6, width: 1000, height: 700, maximized: false })
  })
})

describe('debounce', () => {
  it('runs once for a burst of calls', () => {
    // Dragging a window emits move events continuously; one write is enough.
    let timer = null
    const setTimer = vi.fn((fn) => {
      timer = fn
      return 1
    })
    const clearTimer = vi.fn()
    const fn = vi.fn()
    const debounced = debounce(fn, 500, { setTimer, clearTimer })

    debounced('a')
    debounced('b')
    debounced('c')
    timer()

    expect(clearTimer).toHaveBeenCalledTimes(2)
    expect(fn).toHaveBeenCalledOnce()
    expect(fn).toHaveBeenCalledWith('c')
  })

  it('runs again after the previous call settled', () => {
    let timer = null
    const setTimer = (fn) => {
      timer = fn
      return 1
    }
    const fn = vi.fn()
    const debounced = debounce(fn, 500, { setTimer, clearTimer: () => {} })

    debounced('first')
    timer()
    debounced('second')
    timer()

    expect(fn).toHaveBeenCalledTimes(2)
  })

  it('really does defer, using the default timer', async () => {
    const fn = vi.fn()
    const debounced = debounce(fn, 5)

    debounced()
    expect(fn).not.toHaveBeenCalled()
    await new Promise((resolve) => setTimeout(resolve, 20))
    expect(fn).toHaveBeenCalledOnce()
  })
})
