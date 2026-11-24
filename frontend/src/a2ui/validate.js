// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// The server's rules for a card (backend/internal/service/a2ui), checked
// again on a stored one: a row from before them, or edited by hand, is
// refused here and the message's text is shown instead.

export const BASIC_CATALOG_ID = 'https://a2ui.org/specification/v1_0/catalogs/basic/catalog.json'
// The server's 64 KiB is of the ui as the agent sent it; stored, each
// attachment:<filename> has become a longer link, so a stored card gets twice.
export const MAX_BYTES = 2 * (64 << 10)
export const MAX_COMPONENTS = 200
export const MAX_DEPTH = 16

const SURFACE_ID = /^[A-Za-z0-9_-]{1,64}$/
const KINDS = ['createSurface', 'updateComponents', 'updateDataModel', 'deleteSurface']
const FUNCTION_NAMES = new Set([
  'required', 'regex', 'length', 'numeric', 'email', 'formatString', 'formatNumber', 'formatCurrency',
  'formatDate', 'pluralize', 'openUrl', 'and', 'or', 'not', '@index',
])

export const DISPLAY_COMPONENTS = ['Text', 'Image', 'Icon', 'Video', 'AudioPlayer', 'Row', 'Column', 'List', 'Card', 'Tabs', 'Divider']
export const INTERACTIVE_COMPONENTS = ['Button', 'TextField', 'CheckBox', 'ChoicePicker', 'Slider', 'DateTimeInput', 'Modal']

// --- value shapes -----------------------------------------------------------

const isObject = (v) => v !== null && typeof v === 'object' && !Array.isArray(v)
const onlyKeys = (o, keys) => Object.keys(o).every((k) => keys.includes(k))
const isString = (v) => typeof v === 'string'
const isNumber = (v) => typeof v === 'number' && Number.isFinite(v)
const isBoolean = (v) => typeof v === 'boolean'

const isBinding = (v) => isObject(v) && isString(v['@path']) && onlyKeys(v, ['@path'])

function isCall(v) {
  if (!isObject(v) || !FUNCTION_NAMES.has(v['@call']) || !onlyKeys(v, ['@call', 'args', 'catalogId'])) return false
  if (v.catalogId !== undefined && v.catalogId !== BASIC_CATALOG_ID) return false
  if (v.args === undefined) return true
  return isObject(v.args) && Object.values(v.args).every((a) => (Array.isArray(a) ? a.every(isValue) : isValue(a)))
}

const dynamic = (literal) => (v) => literal(v) || isBinding(v) || isCall(v)

// A literal of any type, a binding or a call; an object literal may not use
// a key the spec reserves ("@" then anything but "@").
function isValue(v) {
  if (isBinding(v) || isCall(v)) return true
  if (Array.isArray(v)) return v.every(isValue)
  if (isObject(v)) return Object.keys(v).every((k) => !/^@([^@]|$)/.test(k)) && Object.values(v).every(isValue)
  return v !== undefined
}

const DS = dynamic(isString)
const DN = dynamic(isNumber)
const DB = dynamic(isBoolean)
const DSL = dynamic((v) => Array.isArray(v) && v.every(isString))
const oneOf = (...values) => (v) => values.includes(v)
const shape = (fields, required = []) => (v) =>
  isObject(v) && onlyKeys(v, Object.keys(fields)) && required.every((k) => k in v) && Object.entries(v).every(([k, x]) => fields[k](x))
const listOf = (item, min = 0) => (v) => Array.isArray(v) && v.length >= min && v.every(item)

const CHILD = isString
const CHILD_LIST = (v) => listOf(isString)(v) || shape({ componentId: isString, path: isString }, ['componentId', 'path'])(v)
export const ICON_NAMES = [
  'accountCircle', 'add', 'arrowBack', 'arrowForward', 'attachFile', 'calendarToday', 'call', 'camera', 'check', 'close',
  'delete', 'download', 'edit', 'event', 'error', 'fastForward', 'favorite', 'favoriteOff', 'folder', 'help', 'home',
  'info', 'locationOn', 'lock', 'lockOpen', 'mail', 'menu', 'moreVert', 'moreHoriz', 'notificationsOff', 'notifications',
  'pause', 'payment', 'person', 'phone', 'photo', 'play', 'print', 'refresh', 'rewind', 'search', 'send', 'settings',
  'share', 'shoppingCart', 'skipNext', 'skipPrevious', 'star', 'starHalf', 'starOff', 'stop', 'upload', 'visibility',
  'visibilityOff', 'volumeDown', 'volumeMute', 'volumeOff', 'volumeUp', 'warning',
]
const ICON = (v) => oneOf(...ICON_NAMES)(v) || shape({ svgPath: DS }, ['svgPath'])(v) || isBinding(v)
const ACTION = (v) =>
  shape({ event: shape({ name: isString, userMessage: DS, context: (c) => isObject(c) && Object.values(c).every(isValue) }, ['name']) }, ['event'])(v) ||
  shape({ functionCall: isCall }, ['functionCall'])(v)
const CHECKS = listOf(shape({ condition: (c) => isBinding(c) || isCall(c), message: isString }, ['condition']))

// Every component's own properties, the catalog's, beside the common ones.
const COMMON = {
  id: isString,
  component: isString,
  '@catalogId': isString,
  '@accessibility': shape({ label: DS, description: DS, live: oneOf('off', 'polite', 'assertive'), hidden: DB }),
  '@metadata': shape({ extensions: isObject }),
  weight: isNumber,
}
const CHECKABLE = { '@checks': CHECKS }

const COMPONENTS = {
  Text: [{ text: DS, variant: oneOf('caption', 'body') }, ['text']],
  Image: [{
    url: DS, description: DS, fit: oneOf('contain', 'cover', 'fill', 'none', 'scaleDown'),
    variant: oneOf('icon', 'avatar', 'smallFeature', 'mediumFeature', 'largeFeature', 'header'),
  }, ['url']],
  Icon: [{ name: ICON }, ['name']],
  Video: [{ url: DS, posterUrl: DS }, ['url']],
  AudioPlayer: [{ url: DS, description: DS }, ['url']],
  Row: [{
    children: CHILD_LIST, justify: oneOf('center', 'end', 'spaceAround', 'spaceBetween', 'spaceEvenly', 'start', 'stretch'),
    align: oneOf('start', 'center', 'end', 'stretch'),
  }, ['children']],
  Column: [{
    children: CHILD_LIST, justify: oneOf('start', 'center', 'end', 'spaceBetween', 'spaceAround', 'spaceEvenly', 'stretch'),
    align: oneOf('center', 'end', 'start', 'stretch'),
  }, ['children']],
  List: [{ children: CHILD_LIST, direction: oneOf('vertical', 'horizontal'), align: oneOf('start', 'center', 'end', 'stretch') }, ['children']],
  Card: [{ child: CHILD }, ['child']],
  Tabs: [{ tabs: listOf(shape({ title: DS, child: CHILD }, ['title', 'child']), 1) }, ['tabs']],
  Divider: [{ axis: oneOf('horizontal', 'vertical') }, []],
  Modal: [{ trigger: CHILD, content: CHILD }, ['trigger', 'content']],
  Button: [{ ...CHECKABLE, child: CHILD, variant: oneOf('default', 'primary', 'borderless'), action: ACTION }, ['child', 'action']],
  TextField: [{ ...CHECKABLE, label: DS, value: DS, placeholder: DS, variant: oneOf('longText', 'number', 'shortText', 'obscured') }, ['label', 'value']],
  CheckBox: [{ ...CHECKABLE, label: DS, value: DB }, ['label', 'value']],
  ChoicePicker: [{
    ...CHECKABLE, label: DS, variant: oneOf('multipleSelection', 'mutuallyExclusive'),
    options: listOf(shape({ label: DS, value: isString }, ['label', 'value'])), value: DSL,
    displayStyle: oneOf('checkbox', 'chips'), filterable: isBoolean,
  }, ['options', 'value']],
  Slider: [{ ...CHECKABLE, label: DS, min: isNumber, max: isNumber, value: DN, steps: (v) => Number.isInteger(v) && v >= 1 }, ['value', 'max']],
  DateTimeInput: [{ ...CHECKABLE, value: DS, enableDate: isBoolean, enableTime: isBoolean, min: DS, max: DS, label: DS }, ['value']],
}

const MEDIA = { Image: ['url'], Video: ['url', 'posterUrl'], AudioPlayer: ['url'] }

/**
 * Whether url is a link the server resolved an attachment to: the public
 * /storage/ route, the task's attachment route, or an absolute public link,
 * a bucket's or the server's own, which is http on a server without TLS.
 */
export function isStoredMediaUrl(url) {
  return typeof url === 'string' && (/^\/(storage|api\/v1\/workspaces)\//.test(url) || /^https?:\/\/[^/]/.test(url))
}

const MESSAGES = {
  createSurface: shape({
    surfaceId: isString, catalogId: isString, sendDataModel: isBoolean, components: listOf(() => true, 1),
    dataModel: isObject, metadata: shape({ extensions: isObject }),
  }, ['surfaceId']),
  updateComponents: shape({ surfaceId: isString, components: listOf(() => true, 1) }, ['surfaceId', 'components']),
  updateDataModel: shape({ surfaceId: isString, path: isString, value: () => true }, ['surfaceId', 'value']),
  deleteSurface: shape({ surfaceId: isString }, ['surfaceId']),
}

// --- the checks -------------------------------------------------------------

function checkComponent(c, at, allowed) {
  if (!isObject(c)) return `${at}: not an object`
  const [own, required] = (allowed.includes(c.component) && COMPONENTS[c.component]) || []
  if (!own) return `${at}: component ${JSON.stringify(c.component)} is not allowed here`
  if (!isString(c.id) || c.id === '') return `${at}: id must be a string`
  const fields = { ...COMMON, ...own }
  for (const [k, v] of Object.entries(c)) {
    if (!Object.hasOwn(fields, k)) return `${at}: ${k} is not a property of ${c.component}`
    if (!fields[k](v)) return `${at}: ${k} is not valid`
  }
  const missing = required.find((k) => !(k in c))
  if (missing) return `${at}: ${missing} is required`
  if (c['@catalogId'] !== undefined && c['@catalogId'] !== BASIC_CATALOG_ID) return `${at}: @catalogId must be the basic catalog`
  const media = (MEDIA[c.component] ?? []).find((p) => p in c && !isStoredMediaUrl(c[p]))
  if (media) return `${at}: ${media} must be a stored file`
  return ''
}

// The ids a component names.
function refs(c) {
  const out = []
  for (const p of ['child', 'trigger', 'content']) if (isString(c[p])) out.push(c[p])
  if (Array.isArray(c.children)) out.push(...c.children)
  else if (isObject(c.children)) out.push(c.children.componentId)
  for (const t of c.tabs ?? []) out.push(t.child)
  return out
}

function checkGraph(components) {
  if (components.size === 0) return ''
  if (!components.has('root')) return 'no component has id "root"'
  for (const [id, c] of components) {
    const missing = refs(c).find((r) => !components.has(r))
    if (missing !== undefined) return `${id}: names ${JSON.stringify(missing)}, which is not a component`
  }
  const heights = new Map()
  const walking = new Set()
  const height = (id) => {
    if (heights.has(id)) return heights.get(id)
    if (walking.has(id)) return Infinity // a cycle
    walking.add(id)
    const h = 1 + Math.max(0, ...refs(components.get(id)).map(height))
    walking.delete(id)
    heights.set(id, h)
    return h
  }
  for (const id of components.keys()) {
    if (height(id) === Infinity) return `components form a cycle through ${JSON.stringify(id)}`
  }
  if (heights.get('root') > MAX_DEPTH) return `the component tree is deeper than ${MAX_DEPTH}`
  return ''
}

function byteLength(v) {
  return new TextEncoder().encode(JSON.stringify(v)).length
}

/**
 * Whether messages, a stored card's, may be drawn: { ok, problems }.
 * options.interactive allows the input components, as on the server.
 */
export function validateMessages(messages, options = {}) {
  const fail = (problem) => ({ ok: false, problems: [problem] })
  if (!Array.isArray(messages) || messages.length === 0) return fail('ui must be a non-empty array')
  if (byteLength(messages) > MAX_BYTES) return fail(`ui is larger than ${MAX_BYTES} bytes`)
  const allowed = options.interactive ? [...DISPLAY_COMPONENTS, ...INTERACTIVE_COMPONENTS] : DISPLAY_COMPONENTS
  const components = new Map()
  let surfaceId = null
  let count = 0
  for (const [i, m] of messages.entries()) {
    const at = `ui[${i}]`
    if (!isObject(m) || m.version !== '1.0') return fail(`${at}: version must be "1.0"`)
    const kinds = Object.keys(m).filter((k) => k !== 'version')
    if (kinds.length !== 1 || !KINDS.includes(kinds[0])) return fail(`${at}: must hold exactly one of ${KINDS.join(', ')}`)
    const kind = kinds[0]
    const body = m[kind]
    if (!MESSAGES[kind](body)) return fail(`${at}.${kind}: not valid`)
    if (!SURFACE_ID.test(body.surfaceId)) return fail(`${at}.${kind}: surfaceId is not valid`)
    if (surfaceId !== null && body.surfaceId !== surfaceId) return fail(`${at}.${kind}: one message carries one surface`)
    surfaceId = body.surfaceId
    if (kind === 'createSurface' && body.catalogId !== BASIC_CATALOG_ID) return fail(`${at}.${kind}: catalogId must be the basic catalog`)
    const seen = new Set()
    for (const [j, c] of (body.components ?? []).entries()) {
      const cat = `${at}.${kind}.components[${j}]`
      if (++count > MAX_COMPONENTS) return fail(`${cat}: more than ${MAX_COMPONENTS} components`)
      const problem = checkComponent(c, cat, allowed)
      if (problem) return fail(problem)
      if (seen.has(c.id)) return fail(`${cat}: id ${JSON.stringify(c.id)} is used twice`)
      seen.add(c.id)
      components.set(c.id, c)
    }
  }
  const problem = checkGraph(components)
  return problem ? fail(problem) : { ok: true, problems: [] }
}
