// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// What the card's components draw with: their classes, the media check and
// the text's markdown. Kept out of the .vue files so every branch is tested.

import DOMPurify from 'dompurify'

import { renderMarkdown } from '../utils/markdown.js'
import { resolveValue } from './resolve.js'

const JUSTIFY = {
  start: 'justify-start',
  center: 'justify-center',
  end: 'justify-end',
  spaceBetween: 'justify-between',
  spaceAround: 'justify-around',
  spaceEvenly: 'justify-evenly',
  stretch: 'justify-stretch',
}
const ALIGN = { start: 'items-start', center: 'items-center', end: 'items-end', stretch: 'items-stretch' }

/** A Row's or Column's justify and align classes, the spec's defaults when unset. */
export function flexClasses(c) {
  return [JUSTIFY[c.justify] ?? JUSTIFY.start, ALIGN[c.align] ?? ALIGN.stretch]
}

/** A List's classes: its direction and cross-axis alignment. */
export function listClasses(c) {
  const direction = c.direction === 'horizontal' ? 'flex-row overflow-x-auto' : 'flex-col'
  return [direction, ALIGN[c.align] ?? ALIGN.stretch]
}

/** The style a component's weight gives it inside a Row or Column. */
export function weightStyle(c) {
  return typeof c?.weight === 'number' && c.weight > 0 ? { flex: `${c.weight} 1 0%`, minWidth: 0 } : undefined
}

/** The ARIA attributes of a component's @accessibility, resolved. */
export function accessibilityAttrs(c, ctx) {
  const a = c?.['@accessibility']
  if (!a) return {}
  const attrs = {}
  const label = resolveValue(a.label, ctx)
  const description = resolveValue(a.description, ctx)
  if (typeof label === 'string' && label) attrs['aria-label'] = label
  if (typeof description === 'string' && description) attrs['aria-description'] = description
  if (a.live === 'polite' || a.live === 'assertive') attrs['aria-live'] = a.live
  if (resolveValue(a.hidden, ctx) === true) attrs['aria-hidden'] = 'true'
  return attrs
}

/** A resolved value as display text: a string, a number or a boolean; anything else is empty. */
export function displayText(value) {
  return ['string', 'number', 'boolean'].includes(typeof value) ? String(value) : ''
}

/**
 * A Text's markdown as HTML. The spec allows no HTML, images or links in it,
 * and an image would make the page fetch whatever the agent (or a value bound
 * from the data model) named, so both go, keeping a link's words.
 */
export function textHtml(text) {
  return DOMPurify.sanitize(renderMarkdown(text), { FORBID_TAGS: ['img', 'a', 'picture', 'source'] })
}

/**
 * Whether url may be drawn as media: a relative link to our own stored files,
 * or exactly the link of one of the same message's attachments (a bucket's
 * public link). The server allows nothing else; this is the second check.
 */
export function isAllowedMedia(url, attachments = []) {
  if (typeof url !== 'string' || url === '') return false
  if (/^\/(storage|api\/v1\/workspaces)\//.test(url)) return !url.includes('/../')
  return (attachments ?? []).some((a) => a?.url === url)
}

const IMAGE_VARIANT = {
  icon: 'w-6 h-6',
  avatar: 'w-10 h-10 rounded-full',
  smallFeature: 'w-full max-w-32',
  mediumFeature: 'w-full max-w-80',
  largeFeature: 'w-full max-w-xl',
  header: 'w-full max-h-48',
}
const IMAGE_FIT = {
  contain: 'object-contain',
  cover: 'object-cover',
  fill: 'object-fill',
  none: 'object-none',
  scaleDown: 'object-scale-down',
}

/** An Image's size and fit classes. A header crops unless told how to fit. */
export function imageClasses(c) {
  const fit = IMAGE_FIT[c.fit] ?? (c.variant === 'header' || c.variant === 'avatar' ? IMAGE_FIT.cover : IMAGE_FIT.fill)
  return [IMAGE_VARIANT[c.variant] ?? IMAGE_VARIANT.mediumFeature, fit]
}
