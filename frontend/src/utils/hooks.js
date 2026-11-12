// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * What the hook pages share: turning a hook's inputs into form fields and
 * back, and the URL with its token, which the server gives only once.
 */

// The URL with its token, from the create or rotate that made it, kept for
// the registration page that follows. Taking it forgets it: a reload shows
// the URL without the token, as the server does.
const tokenUrls = new Map()

export function rememberTokenUrl(id, url) {
  if (id && url) tokenUrls.set(id, url)
}

export function takeTokenUrl(id) {
  const url = tokenUrls.get(id) || ''
  tokenUrls.delete(id)
  return url
}

// A form field's text for an input's stored value: a list as comma-separated.
export function inputText(input, value) {
  if (value === undefined || value === null) return input.type === 'boolean' ? false : ''
  if (input.type === 'array') return value.join(', ')
  if (input.type === 'boolean') return !!value
  return String(value)
}

// The value to send for a field's text, or undefined to leave it out.
export function inputValue(input, text) {
  if (input.type === 'boolean') return !!text
  const s = String(text ?? '').trim()
  if (!s) return undefined
  if (input.type === 'array') {
    const items = s.split(',').map((t) => t.trim()).filter(Boolean)
    return input.items === 'number' ? items.map(Number) : items
  }
  if (input.type === 'number') return Number(s)
  return s
}

// Whether a value from inputValue holds text that was not a number: JSON
// would send it as null, which the server takes for a field left out.
export function notANumber(value) {
  return [].concat(value).some((n) => typeof n === 'number' && !Number.isFinite(n))
}

// A random secret, 32 hex characters, for a provider's webhook secret.
export function generateSecret() {
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  return [...bytes].map((b) => b.toString(16).padStart(2, '0')).join('')
}

// The colour a delivery's status reads in.
export const statusStyles = {
  published: 'text-emerald-700 bg-emerald-50 dark:text-emerald-300 dark:bg-emerald-500/10',
  failed: 'text-red-700 bg-red-50 dark:text-red-300 dark:bg-red-500/10',
  rejected: 'text-amber-700 bg-amber-50 dark:text-amber-300 dark:bg-amber-500/10',
}

export function statusStyle(status) {
  return statusStyles[status] || 'text-gray-600 bg-gray-100 dark:text-zinc-300 dark:bg-zinc-800'
}

export function plural(n, noun) {
  return `${n} ${noun}${n === 1 ? '' : 's'}`
}
