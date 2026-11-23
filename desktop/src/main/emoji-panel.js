// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * Open the system's emoji keyboard, which types into whatever has focus.
 *
 * Electron can only do this on macOS and Windows. Elsewhere it answers false,
 * and the renderer says how to open the system's own picker instead.
 *
 * @param {{ isEmojiPanelSupported: () => boolean, showEmojiPanel: () => void }} app
 * @returns {boolean} whether the keyboard was opened
 */
export function showEmojiPanel(app) {
  if (!app.isEmojiPanelSupported()) return false
  app.showEmojiPanel()
  return true
}
