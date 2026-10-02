// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'

import {
  EXT_SCHEME,
  PanelTarget,
  hardenGuest,
  navigationTarget,
  openTarget,
  panelPartitionFor,
} from '../../src/main/side-panel/guest.js'

const SERVER_URL = 'https://app.agentrq.com'
const options = { partition: 'persist:panel-p1', preload: '/app/dist/preload/panel.cjs' }

describe('panelPartitionFor', () => {
  it('gives each profile a jar of its own that is never the profile session', () => {
    expect(panelPartitionFor('p1')).toBe('persist:panel-p1')
    expect(panelPartitionFor('p1')).not.toBe('persist:profile-p1')
  })
})

describe('hardenGuest', () => {
  it('overwrites whatever the element asked for', () => {
    const webPreferences = {
      nodeIntegration: true,
      nodeIntegrationInSubFrames: true,
      nodeIntegrationInWorker: true,
      contextIsolation: false,
      sandbox: false,
      webSecurity: false,
      disablePopups: true,
      enableBlinkFeatures: 'CSSPseudoHas',
      experimentalFeatures: true,
      allowRunningInsecureContent: true,
      partition: 'persist:profile-p1',
      preload: '/somewhere/else.js',
    }
    const params = {
      src: 'https://example.com/',
      partition: 'persist:profile-p1',
      allowpopups: true,
      nodeintegration: 'on',
      preload: 'file:///evil.js',
    }

    expect(hardenGuest(webPreferences, params, options)).toEqual({ ok: true })
    expect(webPreferences).toEqual({
      contextIsolation: true,
      sandbox: true,
      webSecurity: true,
      partition: 'persist:panel-p1',
      preload: '/app/dist/preload/panel.cjs',
      // Popups on, not off: only then does one reach the window-open handler,
      // which refuses every one (wire.js).
      disablePopups: false,
      enableBlinkFeatures: '',
      experimentalFeatures: false,
      allowRunningInsecureContent: false,
    })
    expect(params).toEqual({ src: 'https://example.com/', partition: 'persist:panel-p1' })
  })

  it('accepts the web, an Extension page, the empty panel and no address at all', () => {
    for (const src of ['http://localhost:3000/', 'https://example.com', `${EXT_SCHEME}://notes/panel/index.html`, 'about:blank', undefined]) {
      expect(hardenGuest({}, { src }, options), `src ${src}`).toEqual({ ok: true })
    }
  })

  it('refuses a guest created on any other scheme', () => {
    for (const src of ['file:///etc/passwd', 'javascript:alert(1)', 'data:text/html,hi', 'app://agentrq/', 'about:config', 'not a url']) {
      const result = hardenGuest({}, { src }, options)
      expect(result.ok, `src ${src}`).toBe(false)
      expect(result.reason).toMatch(/^The side panel does not open/)
    }
  })
})

describe('navigationTarget', () => {
  const target = (url) => navigationTarget(url, { serverUrl: SERVER_URL })

  it('keeps the web and Extension pages in the panel', () => {
    expect(target('https://example.com/docs')).toBe(PanelTarget.Panel)
    expect(target('http://192.168.1.10:3000/')).toBe(PanelTarget.Panel)
    expect(target(`${EXT_SCHEME}://notes/panel/index.html`)).toBe(PanelTarget.Panel)
    expect(target('about:blank')).toBe(PanelTarget.Panel)
  })

  it('sends AgentRQ sign-in to the main window, whose session it has to land in', () => {
    expect(target(`${SERVER_URL}/api/v1/auth/google/login`)).toBe(PanelTarget.SignIn)
  })

  it('hands mail and phone links to the operating system', () => {
    expect(target('mailto:hi@example.com')).toBe(PanelTarget.System)
  })

  it('refuses file, script, data and app addresses', () => {
    for (const url of ['file:///etc/passwd', 'javascript:alert(1)', 'data:text/html,hi', 'app://agentrq/tasks']) {
      expect(target(url), url).toBe(PanelTarget.Blocked)
    }
  })

  it('works without options', () => {
    expect(navigationTarget('https://example.com')).toBe(PanelTarget.Panel)
    expect(navigationTarget(undefined)).toBe(PanelTarget.Blocked)
  })
})

describe('openTarget', () => {
  const open = (url, disposition) => openTarget({ url, disposition }, { serverUrl: SERVER_URL })

  it('opens a new-window request in the panel', () => {
    expect(open('https://example.com', 'foreground-tab')).toBe(PanelTarget.Panel)
    expect(open('https://example.com', 'new-window')).toBe(PanelTarget.Panel)
    expect(open('https://example.com', 'default')).toBe(PanelTarget.Panel)
    expect(openTarget({ url: 'https://example.com' })).toBe(PanelTarget.Panel)
  })

  it('sends a Cmd/Ctrl- or middle-click on a web link to the browser', () => {
    expect(open('https://example.com', 'background-tab')).toBe(PanelTarget.System)
  })

  it('keeps a Cmd/Ctrl-clicked Extension page in the panel, which is the only place it exists', () => {
    expect(open(`${EXT_SCHEME}://notes/panel/other.html`, 'background-tab')).toBe(PanelTarget.Panel)
  })

  it('applies the navigation rule to everything else', () => {
    expect(open('file:///etc/passwd', 'background-tab')).toBe(PanelTarget.Blocked)
    expect(open('mailto:hi@example.com', 'foreground-tab')).toBe(PanelTarget.System)
  })
})
