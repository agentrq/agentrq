import { describe, expect, it } from 'vitest'

import { profileDisplay } from '../src/composables/useProfileDisplay'

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
