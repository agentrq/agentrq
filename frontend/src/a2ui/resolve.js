// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// A component property's value: a literal, {"@path": pointer} into the data
// model, or {"@call": name, "args": {…}} of a catalog function, its
// arguments resolved first.

import { callFunction } from './functions.js'
import { getPointer, resolvePath } from './pointer.js'

const isObject = (v) => v !== null && typeof v === 'object' && !Array.isArray(v)

/** Whether value is a binding or a call rather than a literal. */
export function isDynamic(value) {
  return isObject(value) && (typeof value['@path'] === 'string' || typeof value['@call'] === 'string')
}

/**
 * The value of value in ctx, { surface, scope, index, locale, … }; scope
 * and index are those of the List template item being drawn, if any.
 */
export function resolveValue(value, ctx) {
  if (!isObject(value)) return value
  if (typeof value['@path'] === 'string') {
    return getPointer(ctx.surface.dataModel, resolvePath(value['@path'], ctx.scope))
  }
  if (typeof value['@call'] === 'string') {
    return callFunction(value['@call'], resolveArgs(value.args, ctx), ctx)
  }
  return value
}

/** A call's arguments, each resolved; a list argument item by item. */
export function resolveArgs(args, ctx) {
  const out = {}
  if (!isObject(args)) return out
  for (const [k, v] of Object.entries(args)) {
    out[k] = Array.isArray(v) ? v.map((item) => resolveValue(item, ctx)) : resolveValue(v, ctx)
  }
  return out
}
