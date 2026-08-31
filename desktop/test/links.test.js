import { describe, expect, it } from 'vitest'

import { LinkTarget, classifyLink, linkWindowBounds } from '../src/main/links.js'

const APP_ORIGIN = 'app://agentrq'
const classify = (url) => classifyLink(url, { appOrigin: APP_ORIGIN })

describe('classifyLink', () => {
  it('leaves the app to its own router', () => {
    expect(classify('app://agentrq/tasks/123')).toBe(LinkTarget.App)
    expect(classify('app://agentrq/')).toBe(LinkTarget.App)
  })

  it('does not mistake another app:// host for the app', () => {
    // URL.origin is the string "null" for a non-special scheme, so a comparison
    // written against it would treat every app:// URL as ours.
    expect(classify('app://elsewhere/tasks/123')).toBe(LinkTarget.Blocked)
  })

  it('opens web content in a window', () => {
    // The links the interface actually renders: docs, terms, privacy, and the
    // URL attached to a message.
    expect(classify('https://agentrq.com/docs')).toBe(LinkTarget.Window)
    expect(classify('https://agentrq.com/tos')).toBe(LinkTarget.Window)
    expect(classify('http://192.168.1.10:3000/report')).toBe(LinkTarget.Window)
  })

  it('hands schemes a browser cannot render to the system', () => {
    expect(classify('mailto:hi@agentrq.com')).toBe(LinkTarget.System)
    expect(classify('tel:+15551234')).toBe(LinkTarget.System)
    expect(classify('sms:+15551234')).toBe(LinkTarget.System)
  })

  it('refuses schemes that reach the machine rather than the web', () => {
    // A message body is attacker-influenced text. Passing any of these to
    // shell.openExternal is how that text starts executing things.
    expect(classify('javascript:alert(1)')).toBe(LinkTarget.Blocked)
    expect(classify('data:text/html,<script>alert(1)</script>')).toBe(LinkTarget.Blocked)
    expect(classify('file:///etc/passwd')).toBe(LinkTarget.Blocked)
    expect(classify('vbscript:msgbox(1)')).toBe(LinkTarget.Blocked)
  })

  it('is not fooled by an unusual spelling of the scheme', () => {
    expect(classify('JavaScript:alert(1)')).toBe(LinkTarget.Blocked)
    expect(classify('HTTPS://agentrq.com/docs')).toBe(LinkTarget.Window)
  })

  it('refuses anything that is not a URL', () => {
    expect(classify('/tasks/123')).toBe(LinkTarget.Blocked)
    expect(classify('')).toBe(LinkTarget.Blocked)
    expect(classify(null)).toBe(LinkTarget.Blocked)
    expect(classify(undefined)).toBe(LinkTarget.Blocked)
  })

  it('treats app:// as ordinary when no app origin is given', () => {
    // Without an origin to compare against, nothing may claim to be the app.
    expect(classifyLink('app://agentrq/tasks')).toBe(LinkTarget.Blocked)
  })
})

describe('linkWindowBounds', () => {
  it('follows the main window without copying it exactly', () => {
    const { width, height } = linkWindowBounds({ width: 1400, height: 1000 })
    expect(width).toBe(1120)
    expect(height).toBe(850)
  })

  it('stays usable beside a very small main window', () => {
    const { width, height } = linkWindowBounds({ width: 400, height: 300 })
    expect(width).toBe(640)
    expect(height).toBe(480)
  })

  it('stops growing beside a very large one', () => {
    const { width, height } = linkWindowBounds({ width: 5120, height: 2880 })
    expect(width).toBe(1200)
    expect(height).toBe(900)
  })

  it('has an answer when the parent has no bounds yet', () => {
    expect(linkWindowBounds(null)).toEqual({ width: 819, height: 653 })
    expect(linkWindowBounds(undefined)).toEqual({ width: 819, height: 653 })
  })
})
