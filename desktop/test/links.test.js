// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, expect, it } from 'vitest'

import { LinkTarget, classifyLink } from '../src/main/links.js'

const APP_ORIGIN = 'app://agentrq'
const SERVER_URL = 'https://app.agentrq.com'
const classify = (url) => classifyLink(url, { appOrigin: APP_ORIGIN, serverUrl: SERVER_URL })

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

  it('sends web content to the browser', () => {
    // The links the interface actually renders: docs, terms, privacy, and the
    // URL attached to a message.
    expect(classify('https://agentrq.com/docs')).toBe(LinkTarget.System)
    expect(classify('https://agentrq.com/tos')).toBe(LinkTarget.System)
    expect(classify('http://192.168.1.10:3000/report')).toBe(LinkTarget.System)
  })

  it('keeps signing in to AgentRQ inside the app', () => {
    // The browser would earn the cookie in its own jar and leave the app
    // signed out, so this one flow may not leave.
    expect(classify(`${SERVER_URL}/api/v1/auth/google/login`)).toBe(LinkTarget.SignIn)
    expect(classify(`${SERVER_URL}/api/v1/auth/github/callback?code=abc`)).toBe(LinkTarget.SignIn)
  })

  it('does not treat an ordinary page on the server as signing in', () => {
    expect(classify(`${SERVER_URL}/tasks/123`)).toBe(LinkTarget.System)
    expect(classify(`${SERVER_URL}/api/v1/workspaces`)).toBe(LinkTarget.System)
  })

  it('will not keep somebody else’s auth page in the app', () => {
    // The path alone is not the test: an attacker controls their own paths.
    expect(classify('https://evil.example/api/v1/auth/google/login')).toBe(LinkTarget.System)
    expect(classify('http://app.agentrq.com/api/v1/auth/google/login')).toBe(LinkTarget.System)
    expect(classify('https://app.agentrq.com.evil.example/api/v1/auth/google/login')).toBe(
      LinkTarget.System
    )
  })

  it('has no sign-in exception before a server is chosen', () => {
    // The connection screen runs with no server configured, and nothing may
    // claim to be its sign-in.
    expect(classifyLink(`${SERVER_URL}/api/v1/auth/google/login`, { appOrigin: APP_ORIGIN })).toBe(
      LinkTarget.System
    )
    expect(
      classifyLink(`${SERVER_URL}/api/v1/auth/google/login`, {
        appOrigin: APP_ORIGIN,
        serverUrl: 'not a url',
      })
    ).toBe(LinkTarget.System)
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
    expect(classify('HTTPS://agentrq.com/docs')).toBe(LinkTarget.System)
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
