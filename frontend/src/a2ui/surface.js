// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// A surface: the stored A2UI messages of one message's card, applied in
// order into reactive state a renderer draws from its "root" component.

import { reactive, toRaw } from 'vue'

import { getPointer, parsePointer, resolvePath, setPointer } from './pointer.js'
import { validateMessages } from './validate.js'

export const ROOT_ID = 'root'

// A message from the view is a reactive proxy, which structuredClone refuses.
const clone = (v) => (v === undefined ? v : structuredClone(toRaw(v)))

/** An empty surface. */
export function createSurfaceState() {
  return reactive({
    surfaceId: null,
    catalogId: null,
    rootId: ROOT_ID,
    components: new Map(),
    dataModel: {},
    deleted: false,
  })
}

function putComponents(state, components) {
  for (const c of components ?? []) state.components.set(c.id, clone(c))
}

/** Applies one message to state. A message of no known kind changes nothing. */
export function applyMessage(state, message) {
  if (message?.createSurface) {
    const m = message.createSurface
    state.surfaceId = m.surfaceId
    state.catalogId = m.catalogId ?? null
    state.deleted = false
    state.components.clear()
    state.dataModel = clone(m.dataModel) ?? {}
    putComponents(state, m.components)
  } else if (message?.updateComponents) {
    putComponents(state, message.updateComponents.components)
  } else if (message?.updateDataModel) {
    const m = message.updateDataModel
    state.dataModel = setPointer(state.dataModel, parsePointer(m.path ?? '/'), clone(m.value))
  } else if (message?.deleteSurface) {
    state.deleted = true
    state.components.clear()
    state.dataModel = {}
  }
  return state
}

/** A surface from messages, applied in order. */
export function applyMessages(messages, state = createSurfaceState()) {
  for (const m of messages) applyMessage(state, m)
  return state
}

/** Whether metadata is a message's card: a reply's, or a question asked as one. */
export function isCardMetadata(metadata) {
  return metadata?.type === 'a2ui' || (metadata?.type === 'elicitation_request' && metadata.mode === 'a2ui')
}

/**
 * The surface a message's metadata carries, or null when it carries none or
 * one that fails validation: the caller then shows the message's text. An
 * answered question's card holds the data model its answer sent, so it
 * shows what the person entered.
 */
export function loadSurface(metadata, options = {}) {
  if (!isCardMetadata(metadata) || !Array.isArray(metadata.messages)) return null
  if (!validateMessages(metadata.messages, options).ok) return null
  const state = applyMessages(metadata.messages)
  const sent = metadata.content?.dataModel
  if (metadata.type === 'elicitation_request' && sent !== null && typeof sent === 'object' && !Array.isArray(sent)) {
    state.dataModel = clone(sent)
  }
  return state.deleted ? null : state
}

/** The context values are resolved in at the top of the surface. */
export function surfaceContext(state, extra = {}) {
  return { surface: state, scope: [], ...extra }
}

/**
 * The children a ChildList draws, each { key, id, ctx }: a static list of ids
 * in ctx, or a template's component once per item of the list at its path,
 * in a context whose relative paths start at that item and whose @index is
 * the item's position.
 */
export function childEntries(state, childList, ctx) {
  if (Array.isArray(childList)) return childList.map((id) => ({ key: id, id, ctx }))
  if (childList === null || typeof childList !== 'object' || typeof childList.path !== 'string') return []
  const tokens = resolvePath(childList.path, ctx.scope)
  const items = getPointer(state.dataModel, tokens)
  if (items === null || typeof items !== 'object') return []
  const keys = Array.isArray(items) ? items.map((_, i) => String(i)) : Object.keys(items)
  return keys.map((k, i) => ({
    key: `${childList.componentId}/${k}`,
    id: childList.componentId,
    ctx: { ...ctx, scope: [...tokens, k], index: i },
  }))
}
