// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// The basic catalog's functions, and the @index system function. Each takes
// its arguments already resolved and the context it is called in:
// { surface, scope, index, locale, userActivated, open }. None of them
// throws: a value it cannot work with gives an empty result.

import { getPointer, resolvePath } from './pointer.js'

// The spec's limits on a regex: pattern length in code points, and the
// length of the input it is tried on.
export const MAX_PATTERN = 256
export const MAX_REGEX_INPUT = 4096
// How deeply formatString's expressions may nest.
const MAX_EXPRESSION_DEPTH = 16

const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

/** A boolean, or a validation result's `valid`, as a boolean. */
export function toBoolean(v) {
  return v !== null && typeof v === 'object' && 'valid' in v ? Boolean(v.valid) : Boolean(v)
}

const codePoints = (s) => [...s].length

/**
 * Why the spec refuses pattern, or '' when it may run: too long, a
 * backreference, lookbehind, a named group, \p{...}, or a quantified group
 * that itself holds a quantifier, which can backtrack catastrophically.
 */
export function patternProblem(pattern) {
  if (codePoints(pattern) > MAX_PATTERN) return `longer than ${MAX_PATTERN} code points`
  const groups = [] // per open group: whether it holds a repeating quantifier
  for (let i = 0; i < pattern.length; i++) {
    const c = pattern[i]
    if (c === '\\') {
      if (/[1-9kpP]/.test(pattern[i + 1] ?? '')) return 'backreferences and \\p{} are not allowed'
      i++
    } else if (c === '[') {
      for (i++; i < pattern.length && pattern[i] !== ']'; i++) {
        if (pattern[i] !== '\\') continue
        if (/[pP]/.test(pattern[i + 1] ?? '')) return 'backreferences and \\p{} are not allowed'
        i++
      }
    } else if (c === '(') {
      if (pattern.startsWith('?<', i + 1)) return 'lookbehind and named groups are not allowed'
      groups.push(false)
    } else if (c === ')') {
      const repeats = groups.pop()
      if (repeats && /[*+?{]/.test(pattern[i + 1] ?? '')) return 'a repeated group may not repeat inside'
      if (repeats && groups.length) groups[groups.length - 1] = true
    } else if ((c === '*' || c === '+' || c === '{') && groups.length) {
      groups[groups.length - 1] = true
    }
  }
  return ''
}

function regex({ value, pattern }) {
  const s = String(value ?? '')
  if (typeof pattern !== 'string' || patternProblem(pattern) || codePoints(s) > MAX_REGEX_INPUT) return false
  try {
    return new RegExp(pattern, 'u').test(s)
  } catch {
    return false
  }
}

function required({ value }) {
  if (value === null || value === undefined || value === '') return false
  if (Array.isArray(value)) return value.length > 0
  return typeof value !== 'object' || Object.keys(value).length > 0
}

function length({ value, min, max }) {
  const n = codePoints(String(value ?? ''))
  return (min === undefined || n >= min) && (max === undefined || n <= max)
}

function numeric({ value, min, max }) {
  const n = Number(value)
  if (value === null || value === '' || !Number.isFinite(n)) return false
  return (min === undefined || n >= min) && (max === undefined || n <= max)
}

const email = ({ value }) => typeof value === 'string' && EMAIL.test(value)

function numberOptions(decimals, grouping) {
  const opts = { useGrouping: grouping !== false }
  if (decimals !== undefined && Number.isFinite(Number(decimals))) {
    const d = Math.min(20, Math.max(0, Math.trunc(Number(decimals))))
    opts.minimumFractionDigits = d
    opts.maximumFractionDigits = d
  }
  return opts
}

function formatNumber({ value, decimals, grouping }, ctx) {
  const n = Number(value)
  if (value === null || value === undefined || value === '' || !Number.isFinite(n)) return ''
  return new Intl.NumberFormat(ctx.locale, numberOptions(decimals, grouping)).format(n)
}

function formatCurrency({ value, currency, decimals, grouping }, ctx) {
  const n = Number(value)
  if (value === null || value === undefined || value === '' || !Number.isFinite(n)) return ''
  try {
    return new Intl.NumberFormat(ctx.locale, { ...numberOptions(decimals, grouping), style: 'currency', currency }).format(n)
  } catch {
    // Not an ISO 4217 code.
    return formatNumber({ value, decimals, grouping }, ctx)
  }
}

const pad = (n, width) => String(n).padStart(width, '0')

function dateName(date, locale, options) {
  return new Intl.DateTimeFormat(locale, options).format(date)
}

function dayPeriod(date, locale) {
  const parts = new Intl.DateTimeFormat(locale, { hour: 'numeric', hour12: true }).formatToParts(date)
  return parts.find((p) => p.type === 'dayPeriod')?.value ?? (date.getHours() < 12 ? 'AM' : 'PM')
}

// A Unicode TR35 pattern's tokens: quoted literals, then runs of one letter.
const DATE_TOKENS = /'(?:[^']|'')*'|y+|M+|d+|E+|H+|h+|m+|s+|a+/g

function formatDate({ value, format }, ctx) {
  if (value === null || value === undefined || value === '' || typeof format !== 'string') return ''
  const date = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const loc = ctx.locale
  return format.replace(DATE_TOKENS, (tok) => {
    const n = tok.length
    switch (tok[0]) {
      case "'":
        return n === 2 ? "'" : tok.slice(1, -1).replace(/''/g, "'")
      case 'y':
        return n === 2 ? pad(date.getFullYear() % 100, 2) : pad(date.getFullYear(), n)
      case 'M':
        if (n >= 4) return dateName(date, loc, { month: 'long' })
        if (n === 3) return dateName(date, loc, { month: 'short' })
        return pad(date.getMonth() + 1, n)
      case 'd':
        return pad(date.getDate(), n)
      case 'E':
        return dateName(date, loc, { weekday: n >= 4 ? 'long' : 'short' })
      case 'H':
        return pad(date.getHours(), n)
      case 'h':
        return pad(date.getHours() % 12 || 12, n)
      case 'm':
        return pad(date.getMinutes(), n)
      case 's':
        return pad(date.getSeconds(), n)
      default: // 'a'
        return dayPeriod(date, loc)
    }
  })
}

function pluralize(args, ctx) {
  const n = Number(args.value)
  if (!Number.isFinite(n)) return args.other ?? ''
  if (n === 0 && args.zero !== undefined) return args.zero
  const category = new Intl.PluralRules(ctx.locale).select(n)
  return args[category] ?? args.other ?? ''
}

// Opens a web link (http or https only), and only from a click: a value
// drawn on the page never opens anything.
function openUrl({ url }, ctx) {
  if (!ctx.userActivated || typeof url !== 'string') return undefined
  let parsed
  try {
    parsed = new URL(url)
  } catch {
    return undefined
  }
  if (!['http:', 'https:'].includes(parsed.protocol)) return undefined
  const open = ctx.open ?? ((href) => window.open(href, '_blank', 'noopener,noreferrer'))
  open(parsed.href)
  return undefined
}

const values = (args) => (Array.isArray(args.values) ? args.values : [])

function index({ offset }, ctx) {
  if (ctx.index === undefined) return undefined
  return ctx.index + (Number.isFinite(Number(offset)) ? Number(offset) : 0)
}

function formatString({ value }, ctx) {
  return typeof value === 'string' ? interpolate(value, ctx) : ''
}

export const FUNCTIONS = Object.freeze({
  required,
  regex,
  length,
  numeric,
  email,
  formatString,
  formatNumber,
  formatCurrency,
  formatDate,
  pluralize,
  openUrl,
  and: (args) => values(args).length > 0 && values(args).every(toBoolean),
  or: (args) => values(args).some(toBoolean),
  not: ({ value }) => !toBoolean(value),
  '@index': index,
})

/** Calls the function name with resolved args; an unknown name gives undefined. */
export function callFunction(name, args, ctx) {
  if (!Object.hasOwn(FUNCTIONS, name)) return undefined
  return FUNCTIONS[name](args ?? {}, ctx)
}

// formatString: `${/abs/path}`, `${rel/path}` and `${fn(name: value, …)}`,
// where a value is a nested `${…}`, a quoted string, a number, true, false,
// null or another call. `\${` is a literal `${`. An expression that does not
// parse is kept as it was written.

class ExpressionError extends Error {}

function display(v) {
  if (v === null || v === undefined) return ''
  return typeof v === 'object' ? JSON.stringify(v) : String(v)
}

export function interpolate(template, ctx) {
  let out = ''
  let i = 0
  while (i < template.length) {
    if (template.startsWith('\\${', i)) {
      out += '${'
      i += 3
    } else if (template.startsWith('${', i)) {
      try {
        const [v, end] = expression(template, i + 2, ctx, 0)
        out += display(v)
        i = end + 1
      } catch {
        out += '${'
        i += 2
      }
    } else {
      out += template[i++]
    }
  }
  return out
}

const skipSpace = (s, i) => {
  while (s[i] === ' ' || s[i] === '\t' || s[i] === '\n') i++
  return i
}

const IDENT = /^[@A-Za-z_][A-Za-z0-9_]*/

// The expression starting at i, up to its closing '}': [value, index of '}'].
function expression(s, i, ctx, depth) {
  if (depth > MAX_EXPRESSION_DEPTH) throw new ExpressionError('too deep')
  i = skipSpace(s, i)
  const name = IDENT.exec(s.slice(i))?.[0]
  if (name && s[skipSpace(s, i + name.length)] === '(') {
    const [v, end] = call(s, i, ctx, depth)
    return closing(s, end, v)
  }
  const close = s.indexOf('}', i)
  if (close < 0) throw new ExpressionError('unclosed')
  const path = s.slice(i, close).trim()
  if (path === '') throw new ExpressionError('empty')
  return [getPointer(ctx.surface.dataModel, resolvePath(path, ctx.scope)), close]
}

function closing(s, i, v) {
  i = skipSpace(s, i)
  if (s[i] !== '}') throw new ExpressionError('expected }')
  return [v, i]
}

// A call at i: [value, index just past its ')'].
function call(s, i, ctx, depth) {
  const name = IDENT.exec(s.slice(i))[0]
  i = skipSpace(s, i + name.length) + 1 // past '('
  const args = {}
  i = skipSpace(s, i)
  while (s[i] !== ')') {
    const key = /^[A-Za-z_][A-Za-z0-9_]*/.exec(s.slice(i))?.[0]
    if (!key) throw new ExpressionError('expected an argument name')
    i = skipSpace(s, i + key.length)
    if (s[i] !== ':') throw new ExpressionError('expected :')
    const [v, next] = argument(s, skipSpace(s, i + 1), ctx, depth)
    args[key] = v
    i = skipSpace(s, next)
    if (s[i] === ',') i = skipSpace(s, i + 1)
    else if (s[i] !== ')') throw new ExpressionError('expected , or )')
  }
  if (!Object.hasOwn(FUNCTIONS, name)) throw new ExpressionError('unknown function')
  return [callFunction(name, args, ctx), i + 1]
}

const LITERALS = { true: true, false: false, null: null }

// An argument's value at i: [value, index just past it].
function argument(s, i, ctx, depth) {
  if (s.startsWith('${', i)) {
    const [v, end] = expression(s, i + 2, ctx, depth + 1)
    return [v, end + 1]
  }
  const quote = s[i]
  if (quote === "'" || quote === '"') {
    let out = ''
    for (let j = i + 1; j < s.length; j++) {
      if (s[j] === quote) return [out, j + 1]
      if (s[j] === '\\' && j + 1 < s.length) j++
      out += s[j]
    }
    throw new ExpressionError('unclosed string')
  }
  const number = /^-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?/.exec(s.slice(i))?.[0]
  if (number) return [Number(number), i + number.length]
  const word = IDENT.exec(s.slice(i))?.[0]
  if (word && Object.hasOwn(LITERALS, word)) return [LITERALS[word], i + word.length]
  if (word && s[skipSpace(s, i + word.length)] === '(') return call(s, i, ctx, depth + 1)
  throw new ExpressionError('expected a value')
}
