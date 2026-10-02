// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  DEFAULT_WIDTH,
  MIN_MAIN,
  MIN_WIDTH,
  SIDE_PANEL_STORAGE_KEY,
  clampWidth,
  isPanelUrl,
  normaliseAddress,
  panelWidth,
  readSaved,
  resetSidePanelForTests,
  useSidePanel,
  writeSaved,
} from '../src/composables/useSidePanel'

/** A Storage that keeps what it is given, and can be told to refuse. */
function memoryStorage(initial = {}) {
  const held = new Map(Object.entries(initial))
  return {
    held,
    getItem: (key) => (held.has(key) ? held.get(key) : null),
    setItem: (key, value) => { held.set(key, String(value)) },
  }
}

const refusing = {
  getItem: () => { throw new Error('SecurityError: storage is disabled') },
  setItem: () => { throw new Error('QuotaExceededError: storage is full') },
}

beforeEach(() => resetSidePanelForTests())
afterEach(() => resetSidePanelForTests())

describe('clampWidth', () => {
  it('keeps a width that fits', () => {
    expect(clampWidth(500, 1400)).toBe(500)
  })

  it('never goes below the minimum', () => {
    expect(clampWidth(100, 1400)).toBe(MIN_WIDTH)
  })

  it('leaves the main column its minimum when there is room for both', () => {
    expect(clampWidth(1200, 1400)).toBe(1400 - MIN_MAIN)
  })

  it('keeps the panel usable when the window is too small for both', () => {
    // 700px is narrower than 480 + 320. The panel keeps its minimum and the
    // main column gives way, rather than either going below zero.
    expect(clampWidth(600, 700)).toBe(MIN_WIDTH)
  })

  it('uses the default for a width that is not a number', () => {
    expect(clampWidth(Number.NaN, 1400)).toBe(DEFAULT_WIDTH)
    expect(clampWidth(undefined)).toBe(DEFAULT_WIDTH)
  })

  it('rounds to whole pixels', () => {
    expect(clampWidth(400.6, 1400)).toBe(401)
  })
})

describe('panelWidth', () => {
  it('fills the room the main column leaves when it was never resized', () => {
    expect(panelWidth(null, 1400)).toBe(1400 - MIN_MAIN)
    expect(panelWidth(undefined, 1400)).toBe(1400 - MIN_MAIN)
  })

  it('keeps its minimum in a window too small to fill', () => {
    expect(panelWidth(null, 700)).toBe(MIN_WIDTH)
  })

  it('falls back to the default before the window is measured', () => {
    expect(panelWidth(null)).toBe(DEFAULT_WIDTH)
  })

  it('draws a dragged width, clamped to the room there is', () => {
    expect(panelWidth(500, 1400)).toBe(500)
    expect(panelWidth(1300, 1400)).toBe(1400 - MIN_MAIN)
  })
})

describe('normaliseAddress', () => {
  it('keeps a full web or Extension address', () => {
    expect(normaliseAddress('https://example.com/docs')).toBe('https://example.com/docs')
    expect(normaliseAddress('  http://example.com  ')).toBe('http://example.com/')
    expect(normaliseAddress('agentrq-ext://notes/panel/index.html')).toBe('agentrq-ext://notes/panel/index.html')
  })

  it('gives a bare host https, as a browser would', () => {
    expect(normaliseAddress('example.com')).toBe('https://example.com/')
    expect(normaliseAddress('example.com/path?q=1')).toBe('https://example.com/path?q=1')
  })

  it('gives the local machine plain http, where nothing has a certificate', () => {
    expect(normaliseAddress('localhost:3000')).toBe('http://localhost:3000/')
    expect(normaliseAddress('127.0.0.1:5173/tasks')).toBe('http://127.0.0.1:5173/tasks')
    expect(normaliseAddress('localhost')).toBe('http://localhost/')
  })

  it('refuses every scheme the panel does not show', () => {
    for (const text of ['javascript:alert(1)', 'file:///etc/passwd', 'data:text/html,hi', 'mailto:a@b.c', 'about:blank', 'app://agentrq/']) {
      expect(normaliseAddress(text), text).toBe('')
    }
  })

  it('refuses nothing at all, words with spaces, and addresses with no host', () => {
    expect(normaliseAddress('')).toBe('')
    expect(normaliseAddress(null)).toBe('')
    expect(normaliseAddress('two words')).toBe('')
    expect(normaliseAddress('https://')).toBe('')
    expect(normaliseAddress('https://[bad')).toBe('')
    expect(normaliseAddress('agentrq-ext://')).toBe('')
  })
})

describe('isPanelUrl', () => {
  it('accepts what the panel shows and nothing else', () => {
    expect(isPanelUrl('https://example.com')).toBe(true)
    expect(isPanelUrl('agentrq-ext://notes/panel/index.html')).toBe(true)
    expect(isPanelUrl('javascript:alert(1)')).toBe(false)
    expect(isPanelUrl('file:///etc/passwd')).toBe(false)
    expect(isPanelUrl('')).toBe(false)
    expect(isPanelUrl(undefined)).toBe(false)
  })
})

describe('readSaved and writeSaved', () => {
  it('round-trips the state', () => {
    const storage = memoryStorage()
    writeSaved({ open: true, full: true, width: 500, url: 'https://example.com/' }, storage)
    expect(JSON.parse(storage.held.get(SIDE_PANEL_STORAGE_KEY))).toEqual({ open: true, full: true, width: 500, url: 'https://example.com/' })
    expect(readSaved(storage)).toEqual({ open: true, full: true, width: 500, url: 'https://example.com/' })
  })

  it('starts closed at the default width with nothing saved', () => {
    expect(readSaved(memoryStorage())).toEqual({ open: false, full: false, width: null, url: '' })
    expect(readSaved(null)).toEqual({ open: false, full: false, width: null, url: '' })
  })

  it('never restores an address the panel would not show', () => {
    const storage = memoryStorage({ [SIDE_PANEL_STORAGE_KEY]: JSON.stringify({ open: true, width: 400, url: 'javascript:alert(1)' }) })
    expect(readSaved(storage).url).toBe('')
  })

  it('treats saved junk as nothing saved', () => {
    expect(readSaved(memoryStorage({ [SIDE_PANEL_STORAGE_KEY]: '{not json' }))).toEqual({ open: false, full: false, width: null, url: '' })
    expect(readSaved(memoryStorage({ [SIDE_PANEL_STORAGE_KEY]: 'null' }))).toEqual({ open: false, full: false, width: null, url: '' })
    expect(readSaved(memoryStorage({ [SIDE_PANEL_STORAGE_KEY]: JSON.stringify({ open: 'yes', width: 'wide' }) }))).toEqual({ open: false, full: false, width: null, url: '' })
    // A width that was saved is clamped to the panel's minimum.
    expect(readSaved(memoryStorage({ [SIDE_PANEL_STORAGE_KEY]: JSON.stringify({ width: 10 }) })).width).toBe(MIN_WIDTH)
  })

  it('carries on when storage refuses to read or write', () => {
    expect(readSaved(refusing)).toEqual({ open: false, full: false, width: null, url: '' })
    expect(() => writeSaved({ open: true, width: 400, url: '' }, refusing)).not.toThrow()
  })

  it('uses localStorage by default', () => {
    writeSaved({ open: true, full: false, width: 360, url: '' })
    expect(readSaved()).toEqual({ open: true, full: false, width: 360, url: '' })
    localStorage.removeItem(SIDE_PANEL_STORAGE_KEY)
  })

  it('copes with no localStorage at all', () => {
    const descriptor = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
    Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: undefined })
    try {
      expect(readSaved()).toEqual({ open: false, full: false, width: null, url: '' })
    } finally {
      Object.defineProperty(globalThis, 'localStorage', descriptor)
    }
  })

  it('copes with a localStorage that cannot even be reached', () => {
    const descriptor = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
    Object.defineProperty(globalThis, 'localStorage', { configurable: true, get: () => { throw new Error('SecurityError: access denied') } })
    try {
      expect(readSaved()).toEqual({ open: false, full: false, width: null, url: '' })
    } finally {
      Object.defineProperty(globalThis, 'localStorage', descriptor)
    }
  })
})

describe('useSidePanel', () => {
  it('opens, closes and toggles, remembering each change', () => {
    const storage = memoryStorage()
    const onOpen = vi.fn()
    const panel = useSidePanel({ storage, onOpen })

    panel.open()
    expect(panel.state.open).toBe(true)
    expect(readSaved(storage).open).toBe(true)

    panel.toggle()
    expect(panel.state.open).toBe(false)
    expect(readSaved(storage).open).toBe(false)

    panel.toggle()
    expect(panel.state.open).toBe(true)
    expect(onOpen).toHaveBeenCalledTimes(2)
  })

  it('reports an open only when a closed panel opens', () => {
    const onOpen = vi.fn()
    const panel = useSidePanel({ storage: memoryStorage(), onOpen })
    panel.open('https://example.com/a')
    panel.open('https://example.com/b')
    expect(onOpen).toHaveBeenCalledTimes(1)
    expect(panel.state.url).toBe('https://example.com/b')
  })

  it('ignores an address it would not show', () => {
    const panel = useSidePanel({ storage: memoryStorage() })
    panel.open('https://example.com/')
    panel.open('file:///etc/passwd')
    expect(panel.state.url).toBe('https://example.com/')
    panel.setUrl('javascript:alert(1)')
    expect(panel.state.url).toBe('https://example.com/')
  })

  it('remembers where the panel navigated to, once', () => {
    const storage = memoryStorage()
    const setItem = vi.spyOn(storage, 'setItem')
    const panel = useSidePanel({ storage })
    panel.setUrl('https://example.com/next')
    panel.setUrl('https://example.com/next')
    expect(readSaved(storage).url).toBe('https://example.com/next')
    expect(setItem).toHaveBeenCalledTimes(1)
  })

  it('clamps a dragged width, and resets to filling the window', () => {
    const storage = memoryStorage()
    const panel = useSidePanel({ storage })
    panel.setWidth(2000, 1400)
    expect(panel.state.width).toBe(1400 - MIN_MAIN)
    panel.resetWidth()
    expect(panel.state.width).toBeNull()
    expect(readSaved(storage).width).toBeNull()
  })

  it('reports an open made by any caller to whoever listens', () => {
    const storage = memoryStorage()
    const onOpen = vi.fn()
    useSidePanel({ storage, onOpen })
    // The task view's button asks for the panel without a listener of its own.
    useSidePanel({ storage }).open()
    expect(onOpen).toHaveBeenCalledOnce()
  })

  it('is one panel however many callers ask for it', () => {
    const storage = memoryStorage()
    const first = useSidePanel({ storage })
    const second = useSidePanel({ storage })
    first.open()
    expect(second.state.open).toBe(true)
  })

  it('starts from what was remembered', () => {
    const storage = memoryStorage({ [SIDE_PANEL_STORAGE_KEY]: JSON.stringify({ open: true, width: 600, url: 'https://example.com/' }) })
    expect(useSidePanel({ storage }).state).toEqual({ open: true, full: false, width: 600, url: 'https://example.com/' })
  })

  it('expands to the whole window and collapses back, remembering which', () => {
    const storage = memoryStorage()
    const panel = useSidePanel({ storage })
    panel.setWidth(500, 1400)
    panel.toggleFull()
    expect(panel.state.full).toBe(true)
    expect(readSaved(storage).full).toBe(true)
    panel.toggleFull()
    expect(panel.state.full).toBe(false)
    // Collapsing goes back to the width it had.
    expect(panel.state.width).toBe(500)
    panel.setFull('yes')
    expect(panel.state.full).toBe(true)
  })

  it('works with no options at all', () => {
    const panel = useSidePanel()
    panel.open()
    expect(panel.state.open).toBe(true)
    panel.close()
    localStorage.removeItem(SIDE_PANEL_STORAGE_KEY)
  })
})
