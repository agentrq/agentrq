// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * Zoom and full screen, for the title bar's window menu on macOS.
 *
 * The View menu already does both through Electron's roles; this is the same
 * thing reachable from a button, answering with the state the menu should
 * show afterwards.
 */

/** The steps a browser zooms through, so − and + land where people expect. */
export const ZOOM_PERCENTS = [25, 33, 50, 67, 75, 80, 90, 100, 110, 125, 150, 175, 200, 250, 300, 400, 500]

/**
 * The zoom one step in `direction` from `percent`.
 *
 * A zoom between two steps — set by ⌘ and the scroll wheel, which do not use
 * this list — moves to the neighbouring step rather than skipping one.
 *
 * @param {number} percent
 * @param {'in'|'out'|'reset'} direction
 * @returns {number}
 */
export function nextZoom(percent, direction) {
  if (direction === 'in') return ZOOM_PERCENTS.find((p) => p > percent) ?? ZOOM_PERCENTS.at(-1)
  if (direction === 'out') return ZOOM_PERCENTS.findLast((p) => p < percent) ?? ZOOM_PERCENTS[0]
  return 100
}

/**
 * @param {import('electron').BrowserWindow} win
 * @returns {{ zoom: number, fullScreen: boolean }}
 */
export function viewState(win) {
  return { zoom: Math.round(win.webContents.getZoomFactor() * 100), fullScreen: win.isFullScreen() }
}

/** @returns {{ zoom: number, fullScreen: boolean }} */
export function zoomWindow(win, direction) {
  win.webContents.setZoomFactor(nextZoom(viewState(win).zoom, direction) / 100)
  return viewState(win)
}

/**
 * Answers with the state asked for, not the window's: on macOS the switch is
 * animated, and `isFullScreen()` still reports the old value until it ends.
 *
 * @returns {{ zoom: number, fullScreen: boolean }}
 */
export function toggleFullScreen(win) {
  const fullScreen = !win.isFullScreen()
  win.setFullScreen(fullScreen)
  return { ...viewState(win), fullScreen }
}
