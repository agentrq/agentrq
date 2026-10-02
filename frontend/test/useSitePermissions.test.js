// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'

import { PERMISSIONS, describeQuestion, hasDecisions, originOf, permissionName, siteLabel } from '../src/composables/useSitePermissions'

describe('siteLabel', () => {
  it('names a site by its host', () => {
    expect(siteLabel('https://meet.example')).toBe('meet.example')
    expect(siteLabel('http://localhost:8080')).toBe('localhost:8080')
  })

  it('names an Extension page by its Extension', () => {
    expect(siteLabel('agentrq-ext://notes')).toBe('The notes extension')
  })

  it('falls back to what it was given', () => {
    expect(siteLabel('not a url')).toBe('not a url')
    expect(siteLabel('about:blank')).toBe('about:blank')
    expect(siteLabel(undefined)).toBe('')
  })
})

describe('describeQuestion', () => {
  it('asks for one thing', () => {
    expect(describeQuestion({ origin: 'https://maps.example', permissions: ['geolocation'] })).toBe('maps.example wants to know your location')
  })

  it('asks for a call’s camera and microphone in one breath', () => {
    expect(describeQuestion({ origin: 'https://meet.example', permissions: ['camera', 'microphone'] })).toBe('meet.example wants to use your camera and microphone')
  })

  it('lists several things', () => {
    expect(describeQuestion({ origin: 'https://a.example', permissions: ['notifications', 'clipboard-read'] })).toBe('a.example wants to show notifications and read your clipboard')
    expect(describeQuestion({ origin: 'https://a.example', permissions: ['notifications', 'clipboard-read', 'midi'] })).toBe('a.example wants to show notifications, read your clipboard and use your MIDI devices')
  })

  it('still says something for a permission it has no words for', () => {
    expect(describeQuestion({ origin: 'https://a.example', permissions: ['nfc'] })).toBe('a.example wants to use nfc')
    expect(describeQuestion({ origin: 'https://a.example' })).toBe('a.example wants to ')
    expect(describeQuestion()).toBe(' wants to ')
  })

  it('has a name and a question for every permission the shell asks about', () => {
    for (const [key, words] of Object.entries(PERMISSIONS)) {
      expect(words.name, key).toBeTruthy()
      expect(words.ask, key).toBeTruthy()
    }
  })
})

describe('permissionName', () => {
  it('names a known permission, and shows an unknown one as it is', () => {
    expect(permissionName('camera')).toBe('Camera')
    expect(permissionName('nfc')).toBe('nfc')
  })
})

describe('originOf and hasDecisions', () => {
  const sites = [{ origin: 'https://meet.example', permissions: [{ permission: 'camera', decision: 'allow' }] }]

  it('finds a page’s site in the list', () => {
    expect(originOf('https://meet.example/room?x=1')).toBe('https://meet.example')
    expect(hasDecisions(sites, 'https://meet.example/room')).toBe(true)
    expect(hasDecisions(sites, 'https://other.example/')).toBe(false)
  })

  it('has nothing for a page with no site', () => {
    expect(originOf('about:blank')).toBe('')
    expect(originOf('not a url')).toBe('')
    expect(originOf(undefined)).toBe('')
    expect(hasDecisions(sites, '')).toBe(false)
    expect(hasDecisions(undefined, 'https://meet.example/')).toBe(false)
  })
})
