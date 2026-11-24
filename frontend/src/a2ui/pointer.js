// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// RFC 6901 JSON Pointers into a surface's data model. A path that does not
// start with "/" is relative to a scope: the item a List template is drawn
// for, itself a list of tokens.

// Keys that would reach an object's prototype instead of its own data.
const FORBIDDEN = new Set(['__proto__', 'constructor', 'prototype'])

/** The tokens of a pointer; "" and "/" are the whole document. */
export function parsePointer(pointer) {
  if (pointer === '' || pointer === '/') return []
  const body = pointer.startsWith('/') ? pointer.slice(1) : pointer
  return body.split('/').map((t) => t.replace(/~1/g, '/').replace(/~0/g, '~'))
}

/** The pointer naming tokens, the inverse of parsePointer. */
export function formatPointer(tokens) {
  return tokens.map((t) => '/' + String(t).replace(/~/g, '~0').replace(/\//g, '~1')).join('')
}

/** The absolute tokens of path, relative ones taken from scope. */
export function resolvePath(path, scope = []) {
  const tokens = parsePointer(path)
  return path.startsWith('/') ? tokens : [...scope, ...tokens]
}

const isContainer = (v) => v !== null && typeof v === 'object'

/** The value at tokens in doc, or undefined when nothing is there. */
export function getPointer(doc, tokens) {
  let cur = doc
  for (const t of tokens) {
    if (!isContainer(cur) || FORBIDDEN.has(t) || !Object.hasOwn(cur, t)) return undefined
    cur = cur[t]
  }
  return cur
}

/**
 * Sets the value at tokens in doc and returns the document, which is the
 * value itself when tokens is empty. Missing parents are created as objects.
 * A null value deletes the key, as updateDataModel says; deleting from an
 * array removes the element. A path through a forbidden key changes nothing.
 */
export function setPointer(doc, tokens, value) {
  if (tokens.length === 0) return value === null ? {} : value
  if (tokens.some((t) => FORBIDDEN.has(t))) return doc
  const root = isContainer(doc) ? doc : {}
  let cur = root
  for (const t of tokens.slice(0, -1)) {
    if (!isContainer(cur[t])) cur[t] = {}
    cur = cur[t]
  }
  const last = tokens[tokens.length - 1]
  if (value !== null) {
    cur[last] = value
  } else if (Array.isArray(cur)) {
    if (/^\d+$/.test(last) && Number(last) < cur.length) cur.splice(Number(last), 1)
  } else {
    delete cur[last]
  }
  return root
}
