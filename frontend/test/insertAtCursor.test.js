// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect } from 'vitest'
import { insertAtCursor } from '../src/utils/insertAtCursor'

const field = (value, start, end = start) => {
  const el = document.createElement('textarea')
  el.value = value
  el.setSelectionRange(start, end)
  return el
}

describe('insertAtCursor', () => {
  it('inserts at the cursor, spaced from the words on both sides', () => {
    const el = field('fix login page', 3)
    expect(insertAtCursor('fix login page', 'the', el)).toEqual({ value: 'fix the login page', caret: 7 })
  })

  it('adds no space where whitespace is already there', () => {
    const el = field('fix login', 4)
    expect(insertAtCursor('fix login', 'the', el)).toEqual({ value: 'fix the login', caret: 7 })
  })

  it('replaces the selected text', () => {
    const el = field('fix the page', 4, 7)
    expect(insertAtCursor('fix the page', 'a', el)).toEqual({ value: 'fix a page', caret: 5 })
  })

  it('inserts at the very start without a leading space', () => {
    const el = field('login', 0)
    expect(insertAtCursor('login', 'fix', el)).toEqual({ value: 'fix login', caret: 3 })
  })

  it('appends when the cursor is at the end', () => {
    const el = field('fix', 3)
    expect(insertAtCursor('fix', 'login', el)).toEqual({ value: 'fix login', caret: 9 })
  })

  it('appends without a field', () => {
    expect(insertAtCursor('fix', 'login', null)).toEqual({ value: 'fix login', caret: 9 })
    expect(insertAtCursor('fix\n', 'login', undefined)).toEqual({ value: 'fix\nlogin', caret: 9 })
  })

  it('treats a missing value as empty', () => {
    expect(insertAtCursor(undefined, 'hello', null)).toEqual({ value: 'hello', caret: 5 })
  })

  it('appends when the field shows something other than the value', () => {
    const el = field('stale', 1)
    expect(insertAtCursor('fix', 'login', el)).toEqual({ value: 'fix login', caret: 9 })
  })

  it('takes a collapsed cursor when the field reports no selection end', () => {
    const el = { value: 'ab', selectionStart: 1, selectionEnd: null }
    expect(insertAtCursor('ab', 'x', el)).toEqual({ value: 'a x b', caret: 3 })
  })
})
