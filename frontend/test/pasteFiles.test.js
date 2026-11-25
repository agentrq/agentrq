// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi } from 'vitest'
import { pasteFiles } from '../src/utils/pasteFiles'

/** A paste event as a browser fires it, carrying `files` and the `types` on the clipboard. */
function pasteEvent(files, types = ['Files']) {
  const e = new Event('paste', { bubbles: true, cancelable: true })
  Object.defineProperty(e, 'clipboardData', { value: { types, files } })
  return e
}

const now = new Date(2026, 10, 5, 9, 8, 7)
const shot = () => new File(['png'], 'image.png', { type: 'image/png' })

describe('pasteFiles', () => {
  it('attaches a screenshot under the time it was pasted, and keeps it out of the text', () => {
    const attach = vi.fn()
    const e = pasteEvent([shot()])
    expect(pasteFiles(e, attach, now)).toBe(true)
    expect(e.defaultPrevented).toBe(true)
    const [files] = attach.mock.calls[0]
    expect(files.map((f) => [f.name, f.type])).toEqual([['pasted-20261105-090807.png', 'image/png']])
  })

  it('numbers several pasted at once, and keeps a file\'s own name', () => {
    const attach = vi.fn()
    const own = new File(['x'], 'design.jpg', { type: 'image/jpeg' })
    pasteFiles(pasteEvent([shot(), own, new File(['y'], '', { type: 'image/svg+xml' }), new File(['z'], '')]), attach, now)
    expect(attach.mock.calls[0][0].map((f) => f.name)).toEqual([
      'pasted-20261105-090807-1.png', 'design.jpg', 'pasted-20261105-090807-3.svg', 'pasted-20261105-090807-4.bin',
    ])
  })

  it('leaves a paste with text in it, or with no files, to the field', () => {
    const attach = vi.fn()
    for (const e of [pasteEvent([shot()], ['text/plain', 'Files']), pasteEvent([]), pasteEvent(null, null), new Event('paste')]) {
      expect(pasteFiles(e, attach)).toBe(false)
      expect(e.defaultPrevented).toBe(false)
    }
    expect(attach).not.toHaveBeenCalled()
  })
})
