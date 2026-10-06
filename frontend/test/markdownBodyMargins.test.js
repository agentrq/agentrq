// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, afterEach } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

// The rules as written, less Tailwind's directives and the :has() rule, which
// jsdom cannot use.
const css = readFileSync(resolve(__dirname, '../src/style.css'), 'utf8')
  .split('\n').filter((line) => line.startsWith('.md-body') && !line.includes(':has(')).join('\n')

let style
afterEach(() => { style?.remove(); document.body.innerHTML = '' })

describe('a message body', () => {
  it('has no margin past its first and last paragraph, though each segment is wrapped', () => {
    style = document.createElement('style')
    style.textContent = css
    document.head.appendChild(style)
    document.body.innerHTML = '<div class="md-body"><div><p>one</p><p>two</p></div><div><p>three</p></div></div>'
    const [one, two, three] = document.querySelectorAll('p')

    expect(getComputedStyle(one).marginTop).toBe('0px')
    expect(getComputedStyle(three).marginBottom).toBe('0px')
    // Between paragraphs the spacing stays.
    expect(getComputedStyle(two).marginBottom).toBe('8.8px')
  })
})
