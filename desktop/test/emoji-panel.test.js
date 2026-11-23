// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, expect, it, vi } from 'vitest'

import { showEmojiPanel } from '../src/main/emoji-panel.js'

describe('showEmojiPanel', () => {
  it('opens the emoji keyboard where the system has one', () => {
    const app = { isEmojiPanelSupported: () => true, showEmojiPanel: vi.fn() }
    expect(showEmojiPanel(app)).toBe(true)
    expect(app.showEmojiPanel).toHaveBeenCalledOnce()
  })

  it('answers false without trying where it has none', () => {
    const app = { isEmojiPanelSupported: () => false, showEmojiPanel: vi.fn() }
    expect(showEmojiPanel(app)).toBe(false)
    expect(app.showEmojiPanel).not.toHaveBeenCalled()
  })
})
