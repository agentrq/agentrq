// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { afterEach, describe, expect, it, vi } from 'vitest'

import { FUNCTIONS, callFunction, interpolate, patternProblem, toBoolean } from '../src/a2ui/functions.js'

const data = {
  user: { name: 'Ada', count: 2847, price: 1234.5 },
  items: [{ name: 'first' }, { name: 'second' }],
  when: new Date(2026, 0, 16, 14, 30, 5).toISOString(),
  zero: 0,
  tag: '${x}',
}
const ctx = (extra = {}) => ({ surface: { dataModel: data }, scope: [], locale: 'en-US', ...extra })
const call = (name, args, extra) => callFunction(name, args, ctx(extra))

describe('a2ui functions', () => {
  afterEach(() => vi.restoreAllMocks())

  it('has the 14 basic functions and @index', () => {
    expect(Object.keys(FUNCTIONS).sort()).toEqual([
      '@index', 'and', 'email', 'formatCurrency', 'formatDate', 'formatNumber', 'formatString', 'length',
      'not', 'numeric', 'openUrl', 'or', 'pluralize', 'regex', 'required',
    ])
    expect(call('nope', {})).toBeUndefined()
    expect(call('toString', {})).toBeUndefined()
    expect(call('not')).toBe(true) // no args at all
  })

  it('coerces a validation result to its valid', () => {
    expect(toBoolean({ valid: false })).toBe(false)
    expect(toBoolean({ valid: 1 })).toBe(true)
    expect(toBoolean({})).toBe(true)
    expect(toBoolean(null)).toBe(false)
    expect(toBoolean(0)).toBe(false)
  })

  it('required', () => {
    for (const v of [null, undefined, '', [], {}]) expect(call('required', { value: v })).toBe(false)
    for (const v of ['x', 0, false, [1], { a: 1 }]) expect(call('required', { value: v })).toBe(true)
  })

  it('length counts code points', () => {
    expect(call('length', { value: '😀😀', max: 2 })).toBe(true)
    expect(call('length', { value: 'abc', min: 4 })).toBe(false)
    expect(call('length', { value: 'abc', max: 2 })).toBe(false)
    expect(call('length', { value: undefined, min: 0 })).toBe(true)
  })

  it('numeric', () => {
    expect(call('numeric', { value: 5, min: 1, max: 10 })).toBe(true)
    expect(call('numeric', { value: '5', max: 4 })).toBe(false)
    expect(call('numeric', { value: 0, min: 1 })).toBe(false)
    expect(call('numeric', { value: 'x', min: 1 })).toBe(false)
    expect(call('numeric', { value: null, min: 0 })).toBe(false)
    expect(call('numeric', { value: '', min: 0 })).toBe(false)
    expect(call('numeric', { value: 3 })).toBe(true)
  })

  it('email', () => {
    expect(call('email', { value: 'a@b.co' })).toBe(true)
    expect(call('email', { value: 'a@b' })).toBe(false)
    expect(call('email', { value: 3 })).toBe(false)
  })

  it('regex runs within the spec dialect only', () => {
    expect(call('regex', { value: 'abc123', pattern: '^[a-z]+\\d+$' })).toBe(true)
    expect(call('regex', { value: 'ABC', pattern: '^[a-z]+$' })).toBe(false)
    expect(call('regex', { value: undefined, pattern: '^$' })).toBe(true)
    expect(call('regex', { value: 'a', pattern: 3 })).toBe(false)
    expect(call('regex', { value: 'a', pattern: '(' })).toBe(false) // does not compile
    expect(call('regex', { value: 'a'.repeat(4097), pattern: 'a' })).toBe(false)
    expect(call('regex', { value: 'aaaa', pattern: '(a+)+$' })).toBe(false)
  })

  it('refuses what the spec forbids in a pattern', () => {
    expect(patternProblem('x'.repeat(256))).toBe('')
    expect(patternProblem('😀'.repeat(256))).toBe('') // code points, not UTF-16 units
    expect(patternProblem('x'.repeat(257))).toMatch(/longer/)
    expect(patternProblem('(a)\\1')).toMatch(/backreferences/)
    expect(patternProblem('(?<n>a)\\k<n>')).toMatch(/named groups/)
    expect(patternProblem('(?<=a)b')).toMatch(/lookbehind/)
    expect(patternProblem('(?<!a)b')).toMatch(/lookbehind/)
    expect(patternProblem('\\p{L}')).toMatch(/\\p/)
    expect(patternProblem('[\\P{L}]')).toMatch(/\\p/)
    expect(patternProblem('[\\]a(]\\\\1')).toBe('') // escapes inside and after a class
    expect(patternProblem('[abc')).toBe('') // unclosed class: RegExp refuses it later
    expect(patternProblem('a\\')).toBe('')
    expect(patternProblem('[a\\')).toBe('')
    expect(patternProblem('(a+)')).toBe('')
    expect(patternProblem('(a*)*')).toMatch(/repeat/)
    expect(patternProblem('((a+)b)+')).toMatch(/repeat/)
    expect(patternProblem('(a{2})?')).toMatch(/repeat/)
    expect(patternProblem('(a+)b')).toBe('')
    expect(patternProblem('(ab)+')).toBe('')
    expect(patternProblem('(?:x|y)*z+')).toBe('')
    expect(patternProblem('a)+')).toBe('') // unbalanced: RegExp refuses it
  })

  it('formatNumber and formatCurrency', () => {
    expect(call('formatNumber', { value: 1234.567 })).toBe('1,234.567')
    expect(call('formatNumber', { value: 1234.567, decimals: 1, grouping: false })).toBe('1234.6')
    expect(call('formatNumber', { value: 2, decimals: 99 })).toBe('2.' + '0'.repeat(20))
    expect(call('formatNumber', { value: 2, decimals: -3 })).toBe('2')
    expect(call('formatNumber', { value: 2, decimals: 'x' })).toBe('2')
    for (const v of [null, undefined, '', 'x']) expect(call('formatNumber', { value: v })).toBe('')
    expect(call('formatCurrency', { value: 199.99, currency: 'USD' })).toBe('$199.99')
    expect(call('formatCurrency', { value: 1000, currency: 'EUR', decimals: 0 })).toBe('€1,000')
    expect(call('formatCurrency', { value: 5, currency: 'not a code' })).toBe('5')
    for (const v of [null, undefined, '', 'x']) expect(call('formatCurrency', { value: v, currency: 'USD' })).toBe('')
  })

  it('formatDate takes TR35 patterns', () => {
    const d = new Date(2026, 0, 16, 14, 30, 5)
    const f = (format, value = d) => call('formatDate', { value, format })
    expect(f('MMM dd, yyyy')).toBe('Jan 16, 2026')
    expect(f('HH:mm:ss')).toBe('14:30:05')
    expect(f('h:mm a')).toBe('2:30 PM')
    expect(f('hh a', new Date(2026, 0, 16, 0, 5))).toBe('12 AM')
    expect(f('EEEE, d MMMM')).toBe('Friday, 16 January')
    expect(f('E yy M/d m s H')).toBe('Fri 26 1/16 30 5 14')
    expect(f("'at' h 'o''clock' ''")).toBe("at 2 o'clock '")
    expect(f('yyyy-MM-dd', d.toISOString())).toBe('2026-01-16')
    expect(f('yyyy', d.getTime())).toBe('2026')
    expect(f('yyyy', 'not a date')).toBe('')
    expect(f('yyyy', null)).toBe('')
    expect(f('yyyy', '')).toBe('')
    expect(call('formatDate', { value: d, format: 3 })).toBe('')
  })

  it('formatDate falls back to AM/PM when the locale names no period', () => {
    vi.spyOn(Intl.DateTimeFormat.prototype, 'formatToParts').mockReturnValue([])
    expect(call('formatDate', { value: new Date(2026, 0, 1, 9), format: 'a' })).toBe('AM')
    expect(call('formatDate', { value: new Date(2026, 0, 1, 21), format: 'a' })).toBe('PM')
  })

  it('pluralize', () => {
    const forms = { one: 'review', other: 'reviews' }
    expect(call('pluralize', { value: 1, ...forms })).toBe('review')
    expect(call('pluralize', { value: 5, ...forms })).toBe('reviews')
    expect(call('pluralize', { value: 0, ...forms })).toBe('reviews')
    expect(call('pluralize', { value: 0, zero: 'none', ...forms })).toBe('none')
    expect(call('pluralize', { value: 'x', ...forms })).toBe('reviews')
    expect(call('pluralize', { value: 'x' })).toBe('')
    expect(call('pluralize', { value: 1, other: 'n' })).toBe('n')
    expect(call('pluralize', { value: 1 })).toBe('')
    expect(callFunction('pluralize', { value: 2, two: 'two', few: 'few', other: 'o' }, ctx({ locale: 'ar' }))).toBe('two')
  })

  it('openUrl opens only an http(s) link, and only from a click', () => {
    const open = vi.fn()
    expect(call('openUrl', { url: 'https://agentrq.com/x' }, { open })).toBeUndefined()
    expect(open).not.toHaveBeenCalled()
    for (const url of ['javascript:alert(1)', 'data:text/html,x', 'mailto:hi@example.com', 'not a url', 3]) {
      call('openUrl', { url }, { open, userActivated: true })
    }
    expect(open).not.toHaveBeenCalled()
    call('openUrl', { url: 'https://agentrq.com/x' }, { open, userActivated: true })
    call('openUrl', { url: 'http://example.com' }, { open, userActivated: true })
    expect(open.mock.calls).toEqual([['https://agentrq.com/x'], ['http://example.com/']])
  })

  it('openUrl uses a new window, without an opener, by default', () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    call('openUrl', { url: 'http://example.com' }, { userActivated: true })
    expect(open).toHaveBeenCalledWith('http://example.com/', '_blank', 'noopener,noreferrer')
  })

  it('and, or, not', () => {
    expect(call('and', { values: [true, { valid: true }] })).toBe(true)
    expect(call('and', { values: [true, false] })).toBe(false)
    expect(call('and', { values: [] })).toBe(false)
    expect(call('and', {})).toBe(false)
    expect(call('or', { values: [false, { valid: true }] })).toBe(true)
    expect(call('or', { values: [false] })).toBe(false)
    expect(call('or', {})).toBe(false)
    expect(call('not', { value: { valid: false } })).toBe(true)
  })

  it('@index only inside a List template', () => {
    expect(call('@index', {})).toBeUndefined()
    expect(call('@index', {}, { index: 2 })).toBe(2)
    expect(call('@index', { offset: 1 }, { index: 2 })).toBe(3)
    expect(call('@index', { offset: 'x' }, { index: 2 })).toBe(2)
  })

  describe('formatString', () => {
    const fmt = (value, extra) => call('formatString', { value }, extra)

    it('interpolates paths, absolute and relative to the item', () => {
      expect(fmt('Hi ${/user/name}!')).toBe('Hi Ada!')
      expect(fmt('${ name }', { scope: ['items', '1'] })).toBe('second')
      expect(fmt('[${/missing}]')).toBe('[]')
      expect(fmt('${/user}')).toBe(JSON.stringify(data.user))
      expect(fmt('${/zero}')).toBe('0')
      expect(fmt('${/tag}')).toBe('${x}') // a value is never interpolated again
      expect(fmt(3)).toBe('')
    })

    it('calls functions with named arguments', () => {
      expect(fmt("(${formatNumber(value: ${/user/count})} ${pluralize(value: ${/user/count}, one: 'review', other: 'reviews')})")).toBe('(2,847 reviews)')
      expect(fmt("${formatDate(value: ${/when}, format: 'EEEE, MMMM d')}")).toBe('Friday, January 16')
      expect(fmt('${formatCurrency(value: ${/user/price}, currency: "EUR", decimals: 0)}')).toBe('€1,235')
      expect(fmt('${formatNumber(value: -1.5e2, grouping: false)}')).toBe('-150')
      expect(fmt("${formatString(value: 'a\\'b')}")).toBe("a'b")
      expect(fmt('${not(value: true)} ${not(value: false)} ${not(value: null)}')).toBe('false true true')
      expect(fmt('${and(values: true)}')).toBe('false') // not a list
      expect(fmt('${not(value: required(value: ${/user/name}))}')).toBe('false')
      expect(fmt('${@index(offset: 1)}', { index: 0 })).toBe('1')
      expect(fmt('${ @index ( ) }', { index: 4 })).toBe('4')
      expect(fmt('${not(value:true,)}')).toBe('false')
    })

    it('keeps \\${ and what does not parse as written', () => {
      expect(fmt('cost: \\${/user/name}')).toBe('cost: ${/user/name}')
      expect(fmt('${now()} x')).toBe('${now()} x')
      expect(fmt('a ${/user/name')).toBe('a ${/user/name')
      expect(fmt('${}')).toBe('${}')
      expect(fmt('${   }')).toBe('${   }')
      expect(fmt('${not(value: true) x}')).toBe('${not(value: true) x}')
      expect(fmt('${not(1: true)}')).toBe('${not(1: true)}')
      expect(fmt('${not(value true)}')).toBe('${not(value true)}')
      expect(fmt('${not(value: true x)}')).toBe('${not(value: true x)}')
      expect(fmt("${not(value: 'open)}")).toBe("${not(value: 'open)}")
      expect(fmt('${not(value: ?)}')).toBe('${not(value: ?)}')
      expect(fmt('${not(value: maybe)}')).toBe('${not(value: maybe)}')
      expect(fmt('${not(value: nope())}')).toBe('${not(value: nope())}')
      expect(fmt("${not(value: '\\")).toBe("${not(value: '\\")
    })

    it('stops at the nesting limit', () => {
      const nested = (n) => (n === 0 ? '${/user/name}' : `\${not(value: ${nested(n - 1)})}`)
      expect(fmt(nested(15))).toBe('false')
      expect(fmt(nested(17)).startsWith('${')).toBe(true)
    })
  })

  it('interpolate is the parser formatString uses', () => {
    expect(interpolate('${/user/name}', ctx())).toBe('Ada')
  })
})
