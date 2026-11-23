// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect } from 'vitest'

import { ZOOM_PERCENTS, nextZoom, toggleFullScreen, viewState, zoomWindow } from '../src/main/window-view.js'

/** A window whose full-screen flag, like macOS's, only changes after the animation. */
function fakeWindow({ factor = 1, fullScreen = false } = {}) {
  const win = {
    factor,
    fullScreen,
    requested: null,
    webContents: {
      getZoomFactor: () => win.factor,
      setZoomFactor: (f) => { win.factor = f },
    },
    isFullScreen: () => win.fullScreen,
    setFullScreen: (value) => { win.requested = value },
  }
  return win
}

describe('nextZoom', () => {
  it('steps through the list', () => {
    expect(nextZoom(100, 'in')).toBe(110)
    expect(nextZoom(100, 'out')).toBe(90)
    expect(nextZoom(67, 'in')).toBe(75)
  })

  it('lands on the neighbouring step from between two', () => {
    expect(nextZoom(105, 'in')).toBe(110)
    expect(nextZoom(105, 'out')).toBe(100)
  })

  it('stops at either end', () => {
    expect(nextZoom(ZOOM_PERCENTS.at(-1), 'in')).toBe(500)
    expect(nextZoom(ZOOM_PERCENTS[0], 'out')).toBe(25)
  })

  it('resets to 100%', () => {
    expect(nextZoom(250, 'reset')).toBe(100)
  })
})

describe('viewState', () => {
  it('reads the zoom as a whole percentage', () => {
    expect(viewState(fakeWindow({ factor: 1.1000000001, fullScreen: true }))).toEqual({ zoom: 110, fullScreen: true })
  })
})

describe('zoomWindow', () => {
  it('sets the next step and answers with it', () => {
    const win = fakeWindow({ factor: 1.25 })
    expect(zoomWindow(win, 'in')).toEqual({ zoom: 150, fullScreen: false })
    expect(win.factor).toBe(1.5)
  })
})

describe('toggleFullScreen', () => {
  it('answers with the state asked for, before the window catches up', () => {
    const win = fakeWindow({ fullScreen: false })
    expect(toggleFullScreen(win)).toEqual({ zoom: 100, fullScreen: true })
    expect(win.requested).toBe(true)

    win.fullScreen = true
    expect(toggleFullScreen(win).fullScreen).toBe(false)
    expect(win.requested).toBe(false)
  })
})
