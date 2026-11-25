// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// What the card's components draw with: their classes, the media check and
// the text's markdown. Kept out of the .vue files so every branch is tested.

import DOMPurify from 'dompurify'

import { renderMarkdown } from '../utils/markdown.js'
import {
  CANCEL_COLORS, CHAT_BUTTON, CHAT_SECONDARY_COLORS, CONTROL_SIZE, DISABLED_BUTTON, PRIMARY_COLORS, chatSegmentClass,
} from '../utils/formStyles.js'
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

const isButton = (components, id) => components?.get(id)?.component === 'Button'

/**
 * Whether Row c is the card's decision: Buttons only, which the agent did
 * not place. It sits at the bottom right, as the chat's Cancel and Send Now.
 */
export function isDecisionRow(c, components) {
  return c?.component === 'Row' && !c.justify && Array.isArray(c.children) && c.children.length > 0 &&
    c.children.every((id) => isButton(components, id))
}

/** A Row's justify, align and gap classes; a decision row's on the right, its Buttons as close as the chat's. */
export function rowClasses(c, components) {
  if (!isDecisionRow(c, components)) return [...flexClasses(c), 'gap-x-3']
  return [...flexClasses({ ...c, justify: 'end' }), 'gap-x-2']
}

/**
 * A Row's children in the order drawn: a decision row's primary Buttons last,
 * on the right as the chat's Send Now, and so last for the keyboard too.
 */
export function rowOrder(c, components, children) {
  if (!isDecisionRow(c, components)) return children
  const primary = (child) => components.get(child.id).variant === 'primary'
  return [...children.filter((child) => !primary(child)), ...children.filter(primary)]
}

/**
 * The class a Column gives its child id: a Button alone on its line sits on
 * the right, unless the Column aligns it; a decision row closing the Column
 * is set off by a dashed line, as the chat's Send Now.
 */
export function columnChildClass(c, components, id, last) {
  if (isButton(components, id)) return c.align ? undefined : 'self-end'
  if (last && isDecisionRow(components?.get(id), components)) {
    return 'pt-3 border-t border-dashed border-gray-300 dark:border-zinc-600'
  }
  return undefined
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

const BUTTON_VARIANT = { primary: PRIMARY_COLORS, borderless: CANCEL_COLORS, default: CHAT_SECONDARY_COLORS }

/**
 * A Button's classes: the chat's own button (Cancel, Send Now), primary,
 * secondary or borderless. The one that answered its card stays at full strength
 * though disabled.
 */
export function buttonClass(variant, chosen = false) {
  return [CHAT_BUTTON, BUTTON_VARIANT[variant] ?? BUTTON_VARIANT.default, chosen ? 'disabled:cursor-default' : DISABLED_BUTTON]
}

const FOCUS = 'outline-none focus-visible:ring-2 focus-visible:ring-gray-900/20 dark:focus-visible:ring-white/25'

/** A ChoicePicker chip: a segment of the chat's toggle, raised while picked; one not picked fades on a closed card. */
export function chipClass(picked) {
  return [chatSegmentClass(picked), picked ? 'disabled:cursor-default' : 'disabled:cursor-default disabled:opacity-50']
}

/** A ChoicePicker row: a radio or check and its label, the height of a chat field, as the app's choice cards. */
export function choiceRowClass(picked, readOnly) {
  return [
    `flex items-center gap-2.5 rounded-lg border transition-colors ${CONTROL_SIZE.chat}`,
    'has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-gray-900/20 dark:has-[:focus-visible]:ring-white/20',
    picked
      ? 'border-gray-900 dark:border-zinc-200 bg-gray-50 dark:bg-zinc-800/60 text-gray-900 dark:text-white font-medium'
      : 'border-gray-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-gray-700 dark:text-zinc-300',
    readOnly ? (picked ? 'cursor-default' : 'cursor-default opacity-50') : picked ? 'cursor-pointer' : 'cursor-pointer hover:border-gray-400 dark:hover:border-zinc-500',
  ]
}
