// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

const pad = (n) => String(n).padStart(2, '0')

/**
 * Hands the files of a paste to `attach`, such as a screenshot, and keeps
 * them out of the text. A paste carrying text is left alone: a spreadsheet
 * or a document puts a picture of the selection beside its text, and the
 * text is what was meant.
 *
 * A screenshot comes named `image.png`, so each is renamed after the time it
 * was pasted, and two in one message stay apart.
 *
 * @param {ClipboardEvent} event
 * @param {(files: File[]) => void} attach
 * @param {Date} [now]
 * @returns {boolean} whether it attached anything
 */
export function pasteFiles(event, attach, now = new Date()) {
  const data = event.clipboardData
  if (!data || Array.from(data.types || []).includes('text/plain')) return false
  const files = Array.from(data.files || [])
  if (files.length === 0) return false
  event.preventDefault()

  const stamp = `${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}-${pad(now.getHours())}${pad(now.getMinutes())}${pad(now.getSeconds())}`
  attach(files.map((file, i) => {
    if (file.name && !/^image\.\w+$/i.test(file.name)) return file
    const ext = (file.type.split('/')[1] || 'bin').replace(/\W.*$/, '')
    const name = `pasted-${stamp}${files.length > 1 ? `-${i + 1}` : ''}.${ext}`
    return new File([file], name, { type: file.type })
  }))
  return true
}
