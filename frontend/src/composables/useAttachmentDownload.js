// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Saves an attachment under its own filename and type.
 *
 * A plain `<a download>` names the file only when the link is same-origin. The
 * public `/storage/` link often is not, and in the desktop app no server URL is
 * — the renderer lives on `app://` — so the file came down under its storage id
 * with whatever type the server guessed. The bytes are therefore fetched from
 * the signed-in route, which is same-origin everywhere, and saved from a Blob
 * typed with the attachment's `mimeType`.
 *
 * If the fetch fails, the link is followed as before: a file under the wrong
 * name is better than no file.
 */

/**
 * The image types the server sends as images. Any other, SVG included, comes
 * as a download (so no script in it runs on the app's origin), and an <img>
 * of it would show broken.
 */
const INLINE_IMAGES = new Set(['image/png', 'image/jpeg', 'image/gif', 'image/webp', 'image/bmp'])

/** Whether an attachment is drawn as an image rather than offered as a file. */
export const isInlineImage = (att) => INLINE_IMAGES.has(String(att?.mimeType || '').trim().toLowerCase())

/** Long enough for the browser to start reading the blob before it goes. */
export const REVOKE_DELAY_MS = 40_000

/**
 * @param {object} att                  the attachment: `{ id, filename, mimeType }`
 * @param {object} links
 * @param {string} links.source          same-origin URL to read the bytes from
 * @param {string} links.fallback        URL to follow when that read fails
 * @param {object} [deps]
 * @returns {Promise<boolean>} whether the named, typed copy was saved
 */
export async function downloadAttachment(att, { source, fallback }, deps = {}) {
  const {
    fetchImpl = (input) => fetch(input),
    doc = document,
    urls = URL,
    schedule = setTimeout,
  } = deps

  let objectUrl = null
  try {
    const res = await fetchImpl(source)
    if (!res.ok) throw new Error(`attachment fetch failed: ${res.status}`)
    const bytes = await res.blob()
    const type = att.mimeType || bytes.type || 'application/octet-stream'
    objectUrl = urls.createObjectURL(new Blob([bytes], { type }))
  } catch {
    // Followed below instead.
  }

  const a = doc.createElement('a')
  a.href = objectUrl || fallback
  a.download = att.filename || att.id
  // A public link opens in a tab of its own, as the button's link does.
  if (!objectUrl && fallback !== source) {
    a.target = '_blank'
    a.rel = 'noopener noreferrer'
  }
  doc.body.appendChild(a)
  a.click()
  a.remove()

  if (objectUrl) schedule(() => urls.revokeObjectURL(objectUrl), REVOKE_DELAY_MS)
  return Boolean(objectUrl)
}
