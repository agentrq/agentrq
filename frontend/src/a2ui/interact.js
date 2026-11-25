// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// What the card's inputs and buttons do: a value written back into the
// surface's data model, the checks that hold a button back, and the action a
// click sends. Kept out of the .vue files so every branch is tested.

import { computed, ref, toRaw } from 'vue'

import { toBoolean } from './functions.js'
import { resolvePath, setPointer } from './pointer.js'
import { isDynamic, resolveValue } from './resolve.js'
import { displayText } from './render.js'

const isObject = (v) => v !== null && typeof v === 'object' && !Array.isArray(v)
const isBinding = (v) => isObject(v) && typeof v['@path'] === 'string'

/** Writes value at a binding's path in the surface's data model. */
export function writeBinding(binding, value, ctx) {
  const surface = ctx.surface
  surface.dataModel = setPointer(surface.dataModel, resolvePath(binding['@path'], ctx.scope), value)
}

/**
 * The value of property prop of the component getC() names, read and written
 * as a v-model: a bound one lives in the data model, so whatever else reads
 * that path sees it; a literal or a call keeps the person's edit to itself.
 */
export function boundModel(getC, getCtx, prop) {
  const local = ref()
  const edited = ref(false)
  return computed({
    get: () => {
      const v = getC()[prop]
      if (isBinding(v) || !edited.value) return resolveValue(v, getCtx())
      return local.value
    },
    set: (value) => {
      const v = getC()[prop]
      if (isBinding(v)) return writeBinding(v, value, getCtx())
      edited.value = true
      local.value = value
    },
  })
}

/**
 * What every input of a card shares: its value as a v-model, its label, and
 * its failing checks' messages, shown once the person has touched it, never
 * on a read-only card.
 */
export function useInput(props, prop = 'value') {
  const touched = ref(false)
  return {
    model: boundModel(() => props.c, () => props.ctx, prop),
    label: computed(() => displayText(resolveValue(props.c.label, props.ctx))),
    error: computed(() => (touched.value && !props.ctx.ui.readOnly ? failedChecks(props.c, props.ctx).join(' ') : '')),
    readOnly: computed(() => props.ctx.ui.readOnly),
    touch: () => { touched.value = true },
  }
}

/**
 * The messages of the component's failing @checks; none when all pass. A
 * condition gives a boolean or a { valid, message } result, whose message
 * wins over the rule's.
 */
export function failedChecks(c, ctx) {
  const out = []
  for (const rule of c?.['@checks'] ?? []) {
    const result = resolveValue(rule.condition, ctx)
    if (toBoolean(result)) continue
    const own = isObject(result) && typeof result.message === 'string' ? result.message : ''
    out.push(own || rule.message || 'This value is not valid')
  }
  return out
}

/** value with every binding and call in it resolved, through lists and objects. */
export function resolveDeep(value, ctx) {
  if (isDynamic(value)) return resolveValue(value, ctx)
  if (Array.isArray(value)) return value.map((v) => resolveDeep(v, ctx))
  if (isObject(value)) return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, resolveDeep(v, ctx)]))
  return value
}

/**
 * What a click on Button c sends: its event's name, its context and
 * userMessage resolved now, and a copy of the data model as it stands.
 */
export function buttonAction(c, ctx) {
  const event = c.action.event
  return {
    surfaceId: ctx.surface.surfaceId,
    name: event.name,
    sourceComponentId: c.id,
    context: resolveDeep(event.context ?? {}, ctx),
    userMessage: displayText(resolveValue(event.userMessage, ctx)).trim(),
    dataModel: structuredClone(toRaw(ctx.surface.dataModel)),
  }
}

/**
 * Runs a click on Button c. A functionCall runs here, as a click (openUrl
 * opens only then); an event goes to ui.send, unless the card is read-only
 * or a click is already on its way.
 */
export function clickButton(c, ctx) {
  const ui = ctx.ui
  if (c.action.functionCall) {
    resolveValue(c.action.functionCall, { ...ctx, userActivated: true })
    return
  }
  if (ui.readOnly || ui.busy) return
  ui.send(buttonAction(c, ctx))
}

/** Whether Button c is the one the card was answered with. */
export const isChosen = (c, ui) => !!ui.answered && !!c.action.event && c.action.event.name === ui.answered.name

/** Whether Button c can be clicked: not an answer to a closed card, and every check passes. */
export function buttonEnabled(c, ctx) {
  const ui = ctx.ui
  if (c.action.event && (ui.readOnly || ui.busy)) return false
  return failedChecks(c, ctx).length === 0
}

/** The <input> type and value a DateTimeInput draws, from its ISO 8601 value. */
export function dateInput(c, value) {
  const s = typeof value === 'string' ? value : ''
  if (c.enableTime && !c.enableDate) return { type: 'time', value: /^\d{2}:\d{2}/.exec(s.replace(/^.*T/, ''))?.[0] ?? '' }
  if (c.enableTime) return { type: 'datetime-local', value: /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}/.exec(s)?.[0] ?? '' }
  return { type: 'date', value: /^\d{4}-\d{2}-\d{2}/.exec(s)?.[0] ?? '' }
}

/** A Slider's range: min (0 unset), max, and a step snapping to `steps` divisions. */
export function sliderRange(c) {
  const min = typeof c.min === 'number' ? c.min : 0
  const step = c.steps ? (c.max - min) / c.steps : 'any'
  return { min, max: c.max, step }
}

/** The options a ChoicePicker shows, with their labels resolved and filtered by query. */
export function choiceOptions(c, ctx, query = '') {
  const q = query.trim().toLowerCase()
  return c.options
    .map((o) => ({ value: o.value, label: displayText(resolveValue(o.label, ctx)) || o.value }))
    .filter((o) => !q || o.label.toLowerCase().includes(q))
}

/** A ChoicePicker's selection, a list, after option value is picked. */
export function pickChoice(c, selected, value) {
  if (c.variant !== 'multipleSelection') return [value]
  return selected.includes(value) ? selected.filter((v) => v !== value) : [...selected, value]
}

/**
 * What an answered card says: for a question (elicit), its status once not
 * pending and the content the person sent; for a reply's card, `answered`.
 * Null while it waits for an answer.
 */
export function cardAnswer(metadata) {
  if (metadata?.type === 'elicitation_request') {
    if (!metadata.status || metadata.status === 'pending') return null
    return { name: metadata.content?.name ?? null }
  }
  return metadata?.answered ? { name: metadata.answered.name ?? null } : null
}
