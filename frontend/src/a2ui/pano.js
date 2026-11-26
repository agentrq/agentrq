// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// A Pano doc's a2ui blocks: fenced code tagged a2ui, each one A2UI card the
// doc draws in its own colours. The markdown is the doc, so a block names
// its media as attachment:<filename> of the doc's files, resolved here.

import { applyMessages } from './surface.js'
import { validateMessages } from './validate.js'

/** What a block may draw, as the server's crud.PanoA2UIComponents. */
export const PANO_COMPONENTS = ['Text', 'Row', 'Column', 'Card', 'Tabs', 'Divider', 'Icon', 'Image', 'Video', 'AudioPlayer', 'Chart']

const MEDIA = { Image: ['url'], Video: ['url', 'posterUrl'], AudioPlayer: ['url'] }
const ATTACHMENT = 'attachment:'

// Each media reference to one of files becomes link(file); any other is
// left, for the check to refuse.
function resolveMedia(messages, files, link) {
  const byName = new Map((files ?? []).map((f) => [f.filename, f]))
  for (const m of messages) {
    const body = m?.createSurface ?? m?.updateComponents
    for (const c of Array.isArray(body?.components) ? body.components : []) {
      for (const prop of MEDIA[c?.component] ?? []) {
        const v = c[prop]
        if (typeof v === 'string' && v.startsWith(ATTACHMENT) && byName.has(v.slice(ATTACHMENT.length))) {
          c[prop] = link(byName.get(v.slice(ATTACHMENT.length)))
        }
      }
    }
  }
  return messages
}

/**
 * The surface a block's source draws, its media the doc's files, or null
 * when it is not one a doc may draw: the block then stays as code. A doc's
 * file is checked as a stored file, whichever route serves it: a teammate
 * reads it on the team's.
 */
export function panoSurface(source, files) {
  let messages
  try {
    messages = JSON.parse(source)
  } catch {
    return null
  }
  if (!Array.isArray(messages)) return null
  const checked = resolveMedia(structuredClone(messages), files, () => '/storage/pano')
  if (!validateMessages(checked, { components: PANO_COMPONENTS }).ok) return null
  const state = applyMessages(resolveMedia(messages, files, (f) => f.url))
  return state.deleted ? null : state
}

// The angles a piece leans at, in degrees: half a note's, as it is smaller.
const TILTS = [-1, -0.6, -0.3, 0.4, 0.7, 1]

/** How far a card or a photo leans, picked from its key so it keeps its angle. */
export function panoPieceTilt(key) {
  let h = 0
  for (const ch of String(key ?? '')) h = (h * 31 + ch.charCodeAt(0)) >>> 0
  return TILTS[h % TILTS.length]
}

/** A media's time as m:ss. */
export function panoTime(seconds) {
  const s = Number.isFinite(seconds) && seconds > 0 ? Math.floor(seconds) : 0
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
}
