// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ref } from 'vue'
import { lineEnding, quoteText, appendQuote, useSelectionQuote } from '../src/composables/useSelectionQuote'

describe('lineEnding', () => {
  it('is CRLF on Windows, from the desktop shell or the browser', () => {
    expect(lineEnding({ os: 'win32' }, {})).toBe('\r\n')
    expect(lineEnding({}, { userAgentData: { platform: 'Windows' } })).toBe('\r\n')
    expect(lineEnding({}, { platform: 'Win32' })).toBe('\r\n')
  })

  it('is LF everywhere else, macOS included', () => {
    expect(lineEnding({ os: 'darwin' }, {})).toBe('\n')
    expect(lineEnding({}, { platform: 'MacIntel' })).toBe('\n')
    expect(lineEnding({}, { platform: 'Linux x86_64' })).toBe('\n')
    expect(lineEnding({}, undefined)).toBe('\n')
  })

  it('defaults to the platform it runs on', () => {
    vi.stubGlobal('navigator', { platform: 'Win32' })
    try {
      expect(lineEnding()).toBe('\r\n')
    } finally {
      vi.unstubAllGlobals()
    }
  })
})

describe('quoteText', () => {
  it('puts every line behind "> " and ends with a blank line', () => {
    expect(quoteText('selected text', '\n')).toBe('> selected text\n\n')
    expect(quoteText('one\n\ntwo', '\n')).toBe('> one\n>\n> two\n\n')
  })

  it('uses the given line ending throughout, whatever the selection had', () => {
    expect(quoteText('one\r\ntwo\rthree', '\r\n')).toBe('> one\r\n> two\r\n> three\r\n\r\n')
  })

  it('drops blank lines at either end', () => {
    expect(quoteText('\n\nhello\n', '\n')).toBe('> hello\n\n')
  })
})

describe('appendQuote', () => {
  it('is the quote alone in an empty reply', () => {
    expect(appendQuote('', '> a\n\n', '\n')).toBe('> a\n\n')
  })

  it('starts the quote on its own line after a draft', () => {
    expect(appendQuote('draft', '> a\r\n\r\n', '\r\n')).toBe('draft\r\n> a\r\n\r\n')
    expect(appendQuote('draft\n', '> a\n\n', '\n')).toBe('draft\n> a\n\n')
  })
})

describe('useSelectionQuote', () => {
  let root
  let rect

  beforeEach(() => {
    document.body.innerHTML = `
      <div id="root">
        <div data-quote-source id="one"><p id="p1">first message</p></div>
        <div data-quote-source id="two"><p id="p2">second message</p></div>
        <p id="plain">not marked</p>
      </div>
      <div data-quote-source id="outside">outside</div>`
    root = document.getElementById('root')
    root.getBoundingClientRect = () => ({ top: 100, left: 20 })
    root.scrollTop = 50
    // jsdom lays nothing out, and a Range has no getBoundingClientRect at all.
    rect = Object.getOwnPropertyDescriptor(Range.prototype, 'getBoundingClientRect')
    Range.prototype.getBoundingClientRect = () => ({ bottom: 160, right: 140 })
  })

  afterEach(() => {
    if (rect) Object.defineProperty(Range.prototype, 'getBoundingClientRect', rect)
    else delete Range.prototype.getBoundingClientRect
    document.getSelection().removeAllRanges()
  })

  const select = (startNode, start, endNode, end) => {
    const range = document.createRange()
    range.setStart(startNode, start)
    range.setEnd(endNode, end)
    const sel = document.getSelection()
    sel.removeAllRanges()
    sel.addRange(range)
  }
  const text = (id) => document.getElementById(id).firstChild

  const setup = (container = ref(root), doc) => {
    const hooks = {}
    const api = useSelectionQuote(container, {
      ...(doc ? { doc } : {}),
      onMounted: (f) => { hooks.mounted = f },
      onUnmounted: (f) => { hooks.unmounted = f },
    })
    return { ...api, hooks }
  }

  it('shows a toolbar under text selected in one marked element', () => {
    const { toolbar, update } = setup()
    select(text('p1'), 0, text('p1'), 5)
    update()
    expect(toolbar.value).toEqual({ text: 'first', top: 110, right: 120 })
  })

  it("goes under the end of the selection's last line, kept inside the pane", () => {
    const { toolbar, update } = setup()
    select(text('p1'), 0, text('p1'), 5)
    Range.prototype.getClientRects = () => [{ bottom: 140, right: 300 }, { bottom: 160, right: 90 }]
    try {
      update()
      expect(toolbar.value).toEqual({ text: 'first', top: 110, right: 70 })

      Range.prototype.getClientRects = () => [{ bottom: 160, right: 30 }]
      update()
      expect(toolbar.value.right).toBe(64)

      Range.prototype.getClientRects = () => []
      update()
      expect(toolbar.value.right).toBe(120)
    } finally {
      delete Range.prototype.getClientRects
    }
  })

  it('follows selectionchange between mount and unmount', () => {
    const add = vi.spyOn(document, 'addEventListener')
    const remove = vi.spyOn(document, 'removeEventListener')
    const { toolbar, hooks } = setup()
    hooks.mounted()
    select(text('p1'), 0, text('p1'), 5)
    document.dispatchEvent(new Event('selectionchange'))
    expect(toolbar.value.text).toBe('first')
    hooks.unmounted()
    expect(remove).toHaveBeenCalledWith('selectionchange', add.mock.calls.find(([n]) => n === 'selectionchange')[1])
    add.mockRestore()
    remove.mockRestore()
  })

  it('offers nothing for a selection across messages, unmarked text, or outside the container', () => {
    const { toolbar, update } = setup()
    select(text('p1'), 0, text('p2'), 3)
    update()
    expect(toolbar.value).toBeNull()

    select(text('plain'), 0, text('plain'), 3)
    update()
    expect(toolbar.value).toBeNull()

    select(text('outside'), 0, text('outside'), 3)
    update()
    expect(toolbar.value).toBeNull()
  })

  it('offers nothing for an empty, blank or missing selection, or no container', () => {
    const { toolbar, update } = setup()
    select(text('p1'), 2, text('p1'), 2)
    update()
    expect(toolbar.value).toBeNull()

    text('p1').data = 'a   b'
    select(text('p1'), 1, text('p1'), 4)
    update()
    expect(toolbar.value).toBeNull()

    // A selection anchored in an element node, as a triple click gives.
    select(document.getElementById('p1'), 0, document.getElementById('p1'), 1)
    update()
    expect(toolbar.value.text).toBe('a   b')

    const none = setup(ref(root), { getSelection: () => null, addEventListener() {}, removeEventListener() {} })
    none.update()
    expect(none.toolbar.value).toBeNull()

    const noRanges = setup(ref(root), { getSelection: () => ({ isCollapsed: false, rangeCount: 0 }) })
    noRanges.update()
    expect(noRanges.toolbar.value).toBeNull()

    const detached = setup(ref(null))
    detached.update()
    expect(detached.toolbar.value).toBeNull()
  })

  it('clear drops the selection and the toolbar', () => {
    const { toolbar, update, clear } = setup()
    select(text('p1'), 0, text('p1'), 5)
    update()
    clear()
    expect(toolbar.value).toBeNull()
    expect(document.getSelection().rangeCount).toBe(0)

    const none = setup(ref(root), { getSelection: () => null })
    none.clear()
    expect(none.toolbar.value).toBeNull()
  })

  it('defaults to the document and Vue lifecycle hooks', () => {
    const { toolbar, update } = useSelectionQuote(ref(root))
    select(text('p1'), 0, text('p1'), 5)
    update()
    expect(toolbar.value.text).toBe('first')
  })
})
