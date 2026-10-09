// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi } from 'vitest'
import { TERMINAL_KEYS, keyBytes, pressKey } from '../src/composables/useTerminalKeys'

const key = (id) => TERMINAL_KEYS.find((k) => k.id === id)

describe('the on-screen terminal keys', () => {
  it('offers Esc and Ctrl+C together, then Tab, Shift+Tab, the four arrows and Enter last', () => {
    expect(TERMINAL_KEYS.map((k) => k.id)).toEqual([
      'esc',
      'ctrl-c',
      'tab',
      'shift-tab',
      'up',
      'down',
      'left',
      'right',
      'enter',
    ])
  })

  it('sends the bytes a real keyboard would', () => {
    expect(keyBytes(key('esc'))).toBe('\x1b')
    expect(keyBytes(key('tab'))).toBe('\t')
    expect(keyBytes(key('shift-tab'))).toBe('\x1b[Z')
    expect(keyBytes(key('ctrl-c'))).toBe('\x03')
    expect(keyBytes(key('enter'))).toBe('\r')
  })

  it('sends arrows as CSI in normal cursor mode', () => {
    expect(['up', 'down', 'right', 'left'].map((id) => keyBytes(key(id)))).toEqual([
      '\x1b[A',
      '\x1b[B',
      '\x1b[C',
      '\x1b[D',
    ])
  })

  it('sends arrows as SS3 when the program asked for application cursor mode', () => {
    expect(keyBytes(key('up'), { applicationCursor: true })).toBe('\x1bOA')
    expect(keyBytes(key('down'), { applicationCursor: true })).toBe('\x1bOB')
  })

  it('leaves non-arrow keys alone in application cursor mode', () => {
    expect(keyBytes(key('esc'), { applicationCursor: true })).toBe('\x1b')
  })

  it('types the key into the terminal as user input', () => {
    const term = { input: vi.fn(), modes: { applicationCursorKeysMode: false } }
    pressKey(term, key('up'))
    expect(term.input).toHaveBeenCalledWith('\x1b[A', true)
  })

  it("follows the terminal's cursor mode", () => {
    const term = { input: vi.fn(), modes: { applicationCursorKeysMode: true } }
    pressKey(term, key('down'))
    expect(term.input).toHaveBeenCalledWith('\x1bOB', true)
  })

  it('treats a terminal without modes as normal cursor mode', () => {
    const term = { input: vi.fn() }
    pressKey(term, key('left'))
    expect(term.input).toHaveBeenCalledWith('\x1b[D', true)
  })

  it('does nothing before the terminal exists', () => {
    expect(() => pressKey(null, key('esc'))).not.toThrow()
  })
})
