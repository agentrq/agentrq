// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect } from 'vitest'
import { trimAutolink } from '../src/utils/markedAutolinks'
import { linksInText } from '../src/composables/useTaskLinks'
import { renderMarkdown } from '../src/utils/markdown'

const CATALOG = 'https://app.agentrq.com/a2ui/v1_0/catalogs/agentrq/catalog.json'

describe('a bare URL ends at a backtick', () => {
  it('leaves the backtick out of the link the panel lists', () => {
    expect(linksInText(`catalog: ${CATALOG}\` and more`)).toEqual([{ href: CATALOG, text: CATALOG }])
  })

  it('trims the punctuation left before the backtick', () => {
    expect(linksInText('see https://x.com/a.json.` now')).toEqual([{ href: 'https://x.com/a.json', text: 'https://x.com/a.json' }])
  })

  it('keeps the scheme of a www link', () => {
    expect(linksInText('at www.x.com/a` ok')).toEqual([{ href: 'http://www.x.com/a', text: 'www.x.com/a' }])
  })

  it('cuts at the first backtick of several', () => {
    expect(linksInText('https://x.com/a`b`c')).toEqual([{ href: 'https://x.com/a', text: 'https://x.com/a' }])
  })

  it('links nothing when the backtick follows the scheme', () => {
    expect(linksInText('go https://`x` there')).toEqual([])
    expect(linksInText('go www.`x` there')).toEqual([])
  })

  it('leaves a URL without a backtick alone', () => {
    expect(linksInText(`${CATALOG}.`)).toEqual([{ href: CATALOG, text: CATALOG }])
    expect(linksInText(`\`${CATALOG}\``)).toEqual([])
  })

  it('renders the message link without %60, and the backtick as text', () => {
    const html = renderMarkdown(`${CATALOG}\` x`)
    expect(html).toContain(`href="${CATALOG}"`)
    expect(html).not.toContain('%60')
    expect(html).toContain('</a>')
  })
})

describe('trimAutolink', () => {
  it('returns the token itself when it has no backtick', () => {
    const token = { type: 'link', raw: 'https://x.com', text: 'https://x.com', href: 'https://x.com' }
    expect(trimAutolink(token)).toBe(token)
  })
})
