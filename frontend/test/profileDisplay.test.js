// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, expect, it } from 'vitest'

import { duplicateNotice, profileDisplay } from '../src/composables/useProfileDisplay'

describe('profileDisplay', () => {
  it('leads with the account, since that is what identifies a profile', () => {
    // The reason this exists: "Default" and "Work" do not tell you which
    // account you are about to switch to.
    expect(profileDisplay({
      label: 'Work',
      serverUrl: 'https://app.agentrq.com',
      identity: { name: 'Ada Lovelace', email: 'ada@example.com' },
    })).toMatchObject({ title: 'Ada Lovelace', subtitle: 'ada@example.com', initial: 'A' })
  })

  it('uses the email when there is no name', () => {
    expect(profileDisplay({ label: 'Work', serverUrl: 'https://a.test', identity: { email: 'ada@example.com' } }))
      .toMatchObject({ title: 'ada@example.com', subtitle: 'https://a.test' })
  })

  it('does not print the same thing twice', () => {
    // An email as both title and subtitle reads as a bug.
    const { title, subtitle } = profileDisplay({ serverUrl: 'https://a.test', identity: { email: 'a@b.com' } })

    expect(title).not.toBe(subtitle)
  })

  it('falls back to the profile name when nobody is signed in', () => {
    expect(profileDisplay({ label: 'Work', serverUrl: 'https://a.test', identity: null }))
      .toMatchObject({ title: 'Work', subtitle: 'https://a.test' })
  })

  it('says so plainly when a profile has never been set up', () => {
    expect(profileDisplay({ label: 'Work', serverUrl: '', identity: null }))
      .toMatchObject({ title: 'Work', subtitle: 'Not signed in' })
  })

  it('shows the server for a profile signed in with a name only', () => {
    expect(profileDisplay({ label: 'Work', serverUrl: 'https://a.test', identity: { name: 'Ada' } }))
      .toMatchObject({ title: 'Ada', subtitle: 'https://a.test' })
  })

  it('always has something to show, whatever it is handed', () => {
    for (const p of [undefined, null, {}, { label: '   ' }, { identity: {} }]) {
      const display = profileDisplay(p)
      expect(display.title).toBe('Profile')
      expect(display.initial).toBe('P')
      expect(display.subtitle).toBe('Not signed in')
    }
  })

  it('trims what it is given', () => {
    expect(profileDisplay({ label: '  Work  ', identity: { name: '  Ada  ' } }).title).toBe('Ada')
  })
})

describe('a profile whose server cannot be asked right now', () => {
  const sam = { name: 'Sam Rivera', email: 'sam@example.com', picture: '' }

  // The bug this exists for: the session lasts a day, the lookup gives up
  // after four seconds, and the app starts knowing nothing — so "cannot say"
  // is an ordinary answer, and reading it as "no account" turned every row in
  // the switcher back into the same word.
  it('is still named by the account it belongs to', () => {
    const d = profileDisplay({ label: 'Default', account: sam, identity: null })

    expect(d.title).toBe('Sam Rivera')
    expect(d.initial).toBe('S')
    expect(d.signedIn).toBe(false)
  })

  // Otherwise a profile that has simply been away for a day looks broken
  // rather than merely logged out.
  it('says it is signed out rather than pretending otherwise', () => {
    expect(profileDisplay({ label: 'Default', account: sam }).subtitle).toBe('Signed out')
    expect(profileDisplay({ account: { email: 'sam@example.com' } }).subtitle).toBe('Signed out')
  })

  it('prefers who it is signed in as now over who it was', () => {
    const d = profileDisplay({
      label: 'Default',
      account: { name: 'Old Name', email: 'old@example.com' },
      identity: sam,
    })

    expect(d.title).toBe('Sam Rivera')
    expect(d.subtitle).toBe('sam@example.com')
    expect(d.signedIn).toBe(true)
  })

  // A profile that has never signed in anywhere has nothing to remember, and
  // falls back to its label as it always did.
  it('falls back to the label when there is nothing to remember', () => {
    const d = profileDisplay({ label: 'Profile 2', account: null, serverUrl: '' })

    expect(d.title).toBe('Profile 2')
    expect(d.subtitle).toBe('Not signed in')
    expect(d.signedIn).toBe(false)
  })
})

describe('duplicateNotice', () => {
  const all = [
    { id: 'first', label: 'Default', identity: { name: 'Sam Rivera', email: 'sam@example.com' } },
    { id: 'second', label: 'Profile 2', duplicateOf: 'first' },
  ]

  it('names the profile that already holds the account', () => {
    expect(duplicateNotice(all[1], all)).toBe('Same account as Sam Rivera')
  })

  it('says nothing about a profile on its own account', () => {
    expect(duplicateNotice(all[0], all)).toBe('')
    expect(duplicateNotice({ id: 'x' }, all)).toBe('')
    expect(duplicateNotice(null, all)).toBe('')
  })

  // The notice is worth more than the name in it: a duplicate nobody is told
  // about is a duplicate nobody removes.
  it('still says so when the other profile cannot be named', () => {
    expect(duplicateNotice(all[1], [])).toBe('Same account as another profile')
    expect(duplicateNotice(all[1], null)).toBe('Same account as another profile')
  })
})
