// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// An agent's update to a card already on the page, applied to the surface
// drawn from it rather than drawing it again: what the person entered stays
// wherever the agent left the value alone, and what the agent changed is
// named, for the page to mark.

import { toRaw } from 'vue'

import { resolveDeep } from './interact.js'
import { formatPointer } from './pointer.js'

const isObject = (v) => v !== null && typeof v === 'object' && !Array.isArray(v)
const clone = (v) => structuredClone(toRaw(v))

/** Whether two JSON values are the same. */
export function sameJSON(a, b) {
  return JSON.stringify(toRaw(a)) === JSON.stringify(toRaw(b))
}

function merge(base, local, next, path, changed) {
  if (sameJSON(base, next)) return clone(local)
  const bothObjects = isObject(base) && isObject(local) && isObject(next)
  const sameArrays = Array.isArray(base) && Array.isArray(local) && Array.isArray(next) &&
    base.length === next.length && local.length === next.length
  if (!bothObjects && !sameArrays) {
    changed.push(formatPointer(path))
    return clone(next)
  }
  if (sameArrays) return next.map((v, i) => merge(base[i], local[i], v, [...path, String(i)], changed))
  const out = {}
  for (const k of Object.keys(next)) out[k] = merge(base[k], local[k], next[k], [...path, k], changed)
  for (const k of Object.keys(local)) {
    if (Object.hasOwn(next, k)) continue
    if (Object.hasOwn(base, k)) changed.push(formatPointer([...path, k])) // the agent removed it
    else out[k] = clone(local[k]) // the person's alone
  }
  return out
}

/**
 * The data model after the agent moved its own from base to next, while
 * the person's copy had become local: a value the agent left alone keeps
 * the person's, one it changed takes the agent's. changed lists the
 * pointers the agent changed.
 */
export function mergeDataModel(base, local, next) {
  const changed = []
  const value = merge(base, local, next, [], changed)
  return { value: isObject(value) ? value : {}, changed }
}

/**
 * Applies next, the surface an agent's update draws, to state, the one on
 * the page, whose data model the agent last left as base. Null when it
 * cannot be patched (another surface, or none): the caller draws next.
 * Otherwise what the agent changed: { paths, ids }.
 */
export function patchSurface(state, base, next) {
  if (!state || !next || state.surfaceId !== next.surfaceId) return null
  const ids = []
  for (const id of [...state.components.keys()]) {
    if (!next.components.has(id)) state.components.delete(id)
  }
  for (const [id, c] of next.components) {
    if (sameJSON(state.components.get(id), c)) continue
    if (state.components.has(id)) ids.push(id)
    state.components.set(id, clone(c))
  }
  state.catalogId = next.catalogId
  const { value, changed } = mergeDataModel(base, state.dataModel, next.dataModel)
  state.dataModel = value
  return { paths: changed, ids }
}

// Components that only lay others out: a change to one is its children's.
const LAYOUT = new Set(['Row', 'Column', 'List', 'Card', 'Tabs', 'Modal'])

/**
 * What component c shows in ctx, as JSON: its definition with every binding
 * and call resolved, so a value read through formatString counts too. Its
 * action is left out (a call there runs only on a click), and a layout
 * component shows nothing of its own: what it holds is marked instead.
 */
export function shownValues(c, ctx) {
  if (!c || LAYOUT.has(c.component)) return ''
  const rest = { ...c }
  delete rest.action
  return JSON.stringify(resolveDeep(rest, ctx))
}
