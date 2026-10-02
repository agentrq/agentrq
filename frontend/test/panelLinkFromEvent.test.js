// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, describe, expect, it } from 'vitest'

import { panelLinkFromEvent } from '../src/composables/useMarkdownLinks'

/** Rendered message content, the way MarkdownBody draws it. */
function message(html) {
  const holder = document.createElement('div')
  holder.className = 'md-body'
  holder.innerHTML = html
  document.body.append(holder)
  return holder
}

/** A click on `target`, with whatever modifiers the test names. */
function click(target, init = {}) {
  const event = new MouseEvent('click', { bubbles: true, cancelable: true, button: 0, ...init })
  Object.defineProperty(event, 'target', { value: target })
  return event
}

afterEach(() => { document.body.innerHTML = '' })

describe('panelLinkFromEvent', () => {
  it('opens a web link in a message beside the app', () => {
    const body = message('<p>See <a href="https://example.com/notes">the notes</a></p>')
    expect(panelLinkFromEvent(click(body.querySelector('a')))).toBe('https://example.com/notes')
  })

  it('finds the link from something inside it', () => {
    const body = message('<a href="http://localhost:8080/report"><strong>report</strong></a>')
    expect(panelLinkFromEvent(click(body.querySelector('strong')))).toBe('http://localhost:8080/report')
  })

  it('leaves a Cmd, Ctrl, Shift or Alt click to the system browser', () => {
    const link = message('<a href="https://example.com/">x</a>').querySelector('a')
    for (const modifier of ['metaKey', 'ctrlKey', 'shiftKey', 'altKey']) {
      expect(panelLinkFromEvent(click(link, { [modifier]: true })), modifier).toBe('')
    }
  })

  it('leaves anything but the primary button alone', () => {
    const link = message('<a href="https://example.com/">x</a>').querySelector('a')
    expect(panelLinkFromEvent(click(link, { button: 1 }))).toBe('')
  })

  it('ignores a click something else already handled', () => {
    const link = message('<a href="https://example.com/">x</a>').querySelector('a')
    const event = click(link)
    event.preventDefault()
    expect(panelLinkFromEvent(event)).toBe('')
  })

  it('answers only clicks, never key presses', () => {
    const link = message('<a href="https://example.com/">x</a>').querySelector('a')
    expect(panelLinkFromEvent({ type: 'keydown', key: 'Enter', target: link })).toBe('')
    expect(panelLinkFromEvent(undefined)).toBe('')
  })

  it('keeps links the app draws itself out of it', () => {
    const outside = document.createElement('a')
    outside.href = 'https://agentrq.com/docs'
    document.body.append(outside)
    expect(panelLinkFromEvent(click(outside))).toBe('')
  })

  it('does nothing for a click that is not on a link', () => {
    const body = message('<p>plain text</p>')
    expect(panelLinkFromEvent(click(body.querySelector('p')))).toBe('')
    expect(panelLinkFromEvent(click(null))).toBe('')
  })

  it('leaves mail, file, script and in-app links to their own handling', () => {
    const body = message([
      '<a id="mail" href="mailto:hi@example.com">m</a>',
      '<a id="file" href="file:///etc/passwd">f</a>',
      '<a id="script" href="javascript:alert(1)">s</a>',
      '<a id="app" href="/workspaces/w1">a</a>',
    ].join(''))
    for (const id of ['mail', 'file', 'script', 'app']) {
      expect(panelLinkFromEvent(click(body.querySelector(`#${id}`))), id).toBe('')
    }
  })

  it('never opens AgentRQ sign-in there, whose cookie has to reach the app', () => {
    const link = message('<a href="https://app.agentrq.com/api/v1/auth/google/login">sign in</a>').querySelector('a')
    expect(panelLinkFromEvent(click(link))).toBe('')
  })

  it('leaves a download link to download', () => {
    const link = message('<a href="https://example.com/report.pdf" download>report</a>').querySelector('a')
    expect(panelLinkFromEvent(click(link))).toBe('')
  })

  it('ignores an address it cannot read', () => {
    const link = message('<a href="https://[bad">broken</a>').querySelector('a')
    expect(panelLinkFromEvent(click(link))).toBe('')
  })
})
