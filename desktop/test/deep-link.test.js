import { describe, it, expect } from 'vitest'

import {
  DEEP_LINK_SCHEME,
  LINKABLE_SECTIONS,
  deepLinkFromArgv,
  parseDeepLink,
} from '../src/main/deep-link.js'

describe('parseDeepLink', () => {
  it('maps a workspace link onto its route', () => {
    expect(parseDeepLink('agentrq://workspaces/0ZzhYQG2qtl')).toBe('/workspaces/0ZzhYQG2qtl')
  })

  it('maps a task link onto its route', () => {
    expect(parseDeepLink('agentrq://workspaces/0ZzhYQG2qtl/tasks/0hua6QI7nXN')).toBe(
      '/workspaces/0ZzhYQG2qtl/tasks/0hua6QI7nXN'
    )
  })

  it('accepts every section the route table exposes', () => {
    for (const section of LINKABLE_SECTIONS) {
      expect(parseDeepLink(`agentrq://${section}/abc`)).toBe(`/${section}/abc`)
    }
  })

  it('accepts the triple-slash form a link can also take', () => {
    // agentrq://workspaces/x puts 'workspaces' in the host; agentrq:///workspaces/x
    // puts it in the path. Both are things that turn up in the wild.
    expect(parseDeepLink('agentrq:///workspaces/abc')).toBe('/workspaces/abc')
  })

  it('opens the default view for a bare link', () => {
    expect(parseDeepLink('agentrq://')).toBe('/')
    expect(parseDeepLink('agentrq:///')).toBe('/')
  })

  it('keeps a query string', () => {
    expect(parseDeepLink('agentrq://tasks/all?filter=mine')).toBe('/tasks/all?filter=mine')
  })

  it('decodes percent-encoded segments', () => {
    expect(parseDeepLink('agentrq://workspaces/abc%2Ddef')).toBe('/workspaces/abc-def')
  })

  it('refuses another scheme', () => {
    // Otherwise any page could hand the shell an https:// URL to "navigate" to.
    expect(parseDeepLink('https://evil.example.com/workspaces/abc')).toBeNull()
    expect(parseDeepLink('file:///etc/passwd')).toBeNull()
  })

  it('refuses a section that is not a route', () => {
    expect(parseDeepLink('agentrq://admin/abc')).toBeNull()
    expect(parseDeepLink('agentrq://api/v1/auth/user')).toBeNull()
  })

  it('refuses a segment that is not a plain identifier', () => {
    // Deep links are external input; a segment carrying a traversal, a slash or
    // a control character has no business reaching the router.
    expect(parseDeepLink('agentrq://workspaces/..%2F..%2Fetc')).toBeNull()
    expect(parseDeepLink('agentrq://workspaces/a%00b')).toBeNull()
    expect(parseDeepLink('agentrq://workspaces/a b')).toBeNull()
  })

  it('refuses anything unparseable', () => {
    expect(parseDeepLink('not a url')).toBeNull()
    expect(parseDeepLink('')).toBeNull()
    expect(parseDeepLink(null)).toBeNull()
    expect(parseDeepLink(undefined)).toBeNull()
  })

  it('tolerates a malformed escape rather than throwing', () => {
    // decodeURIComponent throws on '%zz'; the segment is then judged as-is,
    // and '%' is not a valid identifier character.
    expect(parseDeepLink('agentrq://workspaces/%zz')).toBeNull()
  })

  it('names its scheme', () => {
    expect(DEEP_LINK_SCHEME).toBe('agentrq')
  })
})

describe('deepLinkFromArgv', () => {
  it('finds the link Windows and Linux pass as an argument', () => {
    const argv = ['/path/to/AgentRQ', '--flag', 'agentrq://workspaces/abc']
    expect(deepLinkFromArgv(argv)).toBe('agentrq://workspaces/abc')
  })

  it('returns nothing when there is no link', () => {
    expect(deepLinkFromArgv(['/path/to/AgentRQ', '--flag'])).toBeNull()
    expect(deepLinkFromArgv([])).toBeNull()
    expect(deepLinkFromArgv()).toBeNull()
  })

  it('ignores non-string arguments', () => {
    expect(deepLinkFromArgv([null, 42, 'agentrq://events'])).toBe('agentrq://events')
  })
})
