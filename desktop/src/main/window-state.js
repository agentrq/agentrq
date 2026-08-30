/**
 * Remembering where the window was.
 *
 * Restoring a window to its last position is only safe if that position still
 * exists: a laptop undocked from a second monitor, or a display resolution
 * change, can leave saved coordinates entirely off-screen — a window the user
 * cannot see, drag or close. Everything here is about avoiding that.
 *
 * Stored separately from the app settings because it changes on every move and
 * resize; rewriting the settings file that often would be needless churn on a
 * file that matters more.
 */

export const WINDOW_STATE_FILENAME = 'agentrq-window.json'

export const DEFAULT_WINDOW_STATE = {
  width: 1280,
  height: 860,
  maximized: false,
}

/** Never restore a window smaller than this; it matches the window's minimums. */
export const MIN_WIDTH = 940
export const MIN_HEIGHT = 600

function isFiniteNumber(value) {
  return typeof value === 'number' && Number.isFinite(value)
}

/**
 * Constrain saved bounds to a display that actually exists.
 *
 * @param {object} bounds  the saved `{ x, y, width, height }`
 * @param {Array<{x:number,y:number,width:number,height:number}>} displays
 *        work areas of the currently attached displays
 * @returns {object} bounds guaranteed to be visible, or size-only when there is
 *          no sensible position (letting the platform centre the window)
 */
export function clampToDisplays(bounds, displays) {
  const width = Math.max(MIN_WIDTH, isFiniteNumber(bounds?.width) ? bounds.width : DEFAULT_WINDOW_STATE.width)
  const height = Math.max(MIN_HEIGHT, isFiniteNumber(bounds?.height) ? bounds.height : DEFAULT_WINDOW_STATE.height)

  if (!Array.isArray(displays) || displays.length === 0) return { width, height }
  if (!isFiniteNumber(bounds?.x) || !isFiniteNumber(bounds?.y)) return { width, height }

  // "Visible" means a real overlap with some display, not merely a corner
  // touching it — a window one pixel on screen is a window the user has lost.
  const MIN_VISIBLE = 80
  const target = displays.find((display) => {
    const overlapX = Math.min(bounds.x + width, display.x + display.width) - Math.max(bounds.x, display.x)
    const overlapY = Math.min(bounds.y + height, display.y + display.height) - Math.max(bounds.y, display.y)
    return overlapX >= MIN_VISIBLE && overlapY >= MIN_VISIBLE
  })

  // The saved position is gone — a monitor was unplugged, most likely. Return
  // size only so the window opens centred on the primary display rather than
  // somewhere the user cannot reach it.
  if (!target) return { width, height }

  // Shrink to fit a smaller display before positioning, so a window saved on a
  // large monitor does not overflow a laptop screen.
  const fittedWidth = Math.min(width, target.width)
  const fittedHeight = Math.min(height, target.height)

  return {
    x: Math.round(Math.min(Math.max(bounds.x, target.x), target.x + target.width - fittedWidth)),
    y: Math.round(Math.min(Math.max(bounds.y, target.y), target.y + target.height - fittedHeight)),
    width: Math.round(fittedWidth),
    height: Math.round(fittedHeight),
  }
}

/** Coerce whatever was on disk into a state object. */
export function normalizeWindowState(raw) {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return { ...DEFAULT_WINDOW_STATE }

  return {
    ...(isFiniteNumber(raw.x) ? { x: raw.x } : {}),
    ...(isFiniteNumber(raw.y) ? { y: raw.y } : {}),
    width: isFiniteNumber(raw.width) ? Math.max(MIN_WIDTH, raw.width) : DEFAULT_WINDOW_STATE.width,
    height: isFiniteNumber(raw.height) ? Math.max(MIN_HEIGHT, raw.height) : DEFAULT_WINDOW_STATE.height,
    maximized: raw.maximized === true,
  }
}

/**
 * @param {object} deps
 * @param {() => Promise<string>} deps.readFile   rejects when absent
 * @param {(contents: string) => Promise<void>} deps.writeFile
 */
export function createWindowStateStore({ readFile, writeFile }) {
  return {
    async load() {
      try {
        return normalizeWindowState(JSON.parse(await readFile()))
      } catch {
        // Missing or corrupt: the defaults are a perfectly good window.
        return { ...DEFAULT_WINDOW_STATE }
      }
    },

    async save(state) {
      await writeFile(JSON.stringify(normalizeWindowState(state), null, 2))
    },
  }
}

/**
 * Capture a window's current state.
 *
 * A maximized or minimized window reports its *current* bounds, which are the
 * screen, not the size to restore to. `getNormalBounds` gives the size the
 * window had before, which is what should be remembered.
 */
export function captureWindowState(win) {
  const bounds = win.getNormalBounds ? win.getNormalBounds() : win.getBounds()
  return {
    x: bounds.x,
    y: bounds.y,
    width: bounds.width,
    height: bounds.height,
    maximized: win.isMaximized(),
  }
}

/**
 * Collapse a burst of move/resize events into one write.
 *
 * Dragging a window emits these continuously; writing the file on each would be
 * hundreds of writes for one gesture.
 */
export function debounce(fn, ms, { setTimer = setTimeout, clearTimer = clearTimeout } = {}) {
  let handle = null
  return (...args) => {
    if (handle !== null) clearTimer(handle)
    handle = setTimer(() => {
      handle = null
      fn(...args)
    }, ms)
  }
}
