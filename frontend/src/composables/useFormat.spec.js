import { describe, it, expect } from 'vitest'
import { useFormat } from './useFormat'

const { toKebabCase, liveKebabCase } = useFormat()

describe('toKebabCase', () => {
  it('lowercases and hyphenates spaces', () => {
    expect(toKebabCase('Hello World')).toBe('hello-world')
  })

  it('collapses non-alphanumeric runs into a single hyphen', () => {
    expect(toKebabCase('foo__bar!!baz')).toBe('foo-bar-baz')
  })

  it('trims leading/trailing hyphens produced by punctuation', () => {
    expect(toKebabCase('  --Foo Bar--  ')).toBe('foo-bar')
  })

  it('returns empty string for falsy input', () => {
    expect(toKebabCase('')).toBe('')
    expect(toKebabCase(null)).toBe('')
    expect(toKebabCase(undefined)).toBe('')
  })
})

describe('liveKebabCase', () => {
  it('lowercases and replaces spaces/underscores with hyphens', () => {
    expect(liveKebabCase('Hello_World Foo')).toBe('hello-world-foo')
  })

  it('strips characters outside [a-z0-9-] without collapsing runs', () => {
    expect(liveKebabCase('foo!!bar')).toBe('foobar')
  })

  it('does not trim a trailing hyphen (used while typing)', () => {
    expect(liveKebabCase('Foo Bar ')).toBe('foo-bar-')
  })

  it('returns empty string for falsy input', () => {
    expect(liveKebabCase('')).toBe('')
    expect(liveKebabCase(null)).toBe('')
    expect(liveKebabCase(undefined)).toBe('')
  })
})
