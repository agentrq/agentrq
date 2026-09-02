import { describe, expect, it } from 'vitest'

import {
  DEFAULT_PROFILE_LABEL,
  activateProfile,
  activeProfile,
  addProfile,
  canDiscardActiveProfile,
  isValidProfileId,
  makeProfile,
  migrateProfiles,
  partitionFor,
  removeProfile,
  renameProfile,
  updateProfile,
} from '../src/main/profiles.js'

const base = () => migrateProfiles(null)

describe('migrateProfiles', () => {
  it('gives a first run one profile to start from', () => {
    const state = migrateProfiles(null)

    expect(state.profiles).toHaveLength(1)
    expect(state.profiles[0].label).toBe(DEFAULT_PROFILE_LABEL)
    expect(state.activeProfileId).toBe(state.profiles[0].id)
  })

  it('carries a pre-profiles config into the first profile', () => {
    // Upgrading must not look like being reconfigured: the server and the mute
    // list were already chosen and belong to the account already signed in.
    const state = migrateProfiles({
      version: 2,
      serverUrl: 'https://app.agentrq.com',
      mutedWorkspaces: ['ws1', 'ws2'],
    })

    expect(state.profiles[0]).toMatchObject({
      serverUrl: 'https://app.agentrq.com',
      mutedWorkspaces: ['ws1', 'ws2'],
    })
  })

  it('gives the migrated profile a partition like any other', () => {
    // Its `at` cookie is in the default session, so this signs the user out
    // once on upgrade. Agreed trade: the session lasts 24 hours anyway, and the
    // alternative is one profile that is special-cased wherever a session is
    // resolved.
    const state = migrateProfiles({ version: 2, serverUrl: 'https://x.test' })

    expect(state.profiles[0].partition).toBe(partitionFor(state.profiles[0].id))
    expect(state.profiles[0].partition).not.toBe('')
  })

  it('repairs a stored profile that has no partition of its own', () => {
    // An empty partition would mean the shared default session, which is the
    // one thing a profile must never have.
    const state = migrateProfiles({ profiles: [{ id: 'one', label: 'X', partition: '' }] })

    expect(state.profiles[0].partition).toBe(partitionFor('one'))
  })

  it('keeps profiles that were already stored', () => {
    const state = migrateProfiles({
      profiles: [
        { id: 'one', label: 'Personal', partition: 'persist:profile-one', serverUrl: 'https://a.test' },
        { id: 'two', label: 'Work', partition: 'persist:profile-two', serverUrl: 'https://b.test' },
      ],
      activeProfileId: 'two',
    })

    expect(state.profiles.map((p) => p.label)).toEqual(['Personal', 'Work'])
    expect(state.activeProfileId).toBe('two')
  })

  it('refuses two profiles sharing an id', () => {
    // They would share a partition, so they would be one account wearing two
    // names — the single thing this list must never contain.
    const state = migrateProfiles({
      profiles: [
        { id: 'one', label: 'First' },
        { id: 'one', label: 'Second' },
      ],
    })

    expect(state.profiles).toHaveLength(1)
    expect(state.profiles[0].label).toBe('First')
  })

  it('drops entries whose id could not be a directory name', () => {
    // The id becomes a folder under the app's Partitions directory.
    const state = migrateProfiles({
      profiles: [
        { id: '../escape', label: 'Bad' },
        { id: 'Uppercase', label: 'Also bad' },
        { id: 'fine', label: 'Good' },
      ],
    })

    expect(state.profiles.map((p) => p.id)).toEqual(['fine'])
  })

  it('falls back to the first profile when the active id names nothing', () => {
    const state = migrateProfiles({
      profiles: [{ id: 'one', label: 'Personal' }],
      activeProfileId: 'deleted',
    })

    expect(state.activeProfileId).toBe('one')
  })

  it('never returns an empty list, whatever it is handed', () => {
    for (const raw of [null, undefined, [], 'nonsense', 42, { profiles: 'no' }, { profiles: [] }]) {
      expect(migrateProfiles(raw).profiles.length).toBeGreaterThanOrEqual(1)
    }
  })
})

describe('addProfile', () => {
  it('gives a new profile its own session', () => {
    const state = addProfile(base(), { id: 'work1', label: 'Work' })

    expect(state.profiles).toHaveLength(2)
    expect(state.profiles[1].partition).toBe(partitionFor('work1'))
    expect(state.profiles[1].partition).not.toBe(state.profiles[0].partition)
  })

  it('switches to what it just added', () => {
    // Adding a profile you then have to go and select is a step nobody wants.
    expect(addProfile(base(), { id: 'work1', label: 'Work' }).activeProfileId).toBe('work1')
  })

  it('refuses an id already in use', () => {
    const state = addProfile(base(), { id: 'work1', label: 'Work' })

    expect(addProfile(state, { id: 'work1', label: 'Other' })).toBe(state)
  })

  it('refuses an unusable id', () => {
    const state = base()
    for (const id of ['', '../x', 'has space', 'CAPS', undefined]) {
      expect(addProfile(state, { id, label: 'x' })).toBe(state)
    }
  })

  it('names an unnamed profile rather than leaving it blank', () => {
    expect(addProfile(base(), { id: 'x1', label: '   ' }).profiles[1].label).toBe(DEFAULT_PROFILE_LABEL)
  })
})

describe('removeProfile', () => {
  it('refuses to remove the last one', () => {
    // An app with no profile has no session to run in.
    const state = base()
    expect(removeProfile(state, state.profiles[0].id)).toBe(state)
  })

  it('moves off a profile it just removed', () => {
    const state = addProfile(base(), { id: 'work1', label: 'Work' })
    expect(state.activeProfileId).toBe('work1')

    const after = removeProfile(state, 'work1')

    expect(after.profiles).toHaveLength(1)
    expect(after.activeProfileId).toBe(after.profiles[0].id)
  })

  it('leaves the active profile alone when another is removed', () => {
    let state = addProfile(base(), { id: 'work1', label: 'Work' })
    state = addProfile(state, { id: 'work2', label: 'Second' })

    expect(removeProfile(state, 'work1').activeProfileId).toBe('work2')
  })

  it('ignores an id it does not have', () => {
    const state = addProfile(base(), { id: 'work1', label: 'Work' })
    expect(removeProfile(state, 'nope')).toBe(state)
  })

  it('goes back to the profile the caller names', () => {
    // Abandoning a profile has to land on the one it was added from. With
    // three profiles that is not the first in the list, so falling back to
    // profiles[0] would drop the user into a third account they never chose.
    let state = addProfile(base(), { id: 'work1', label: 'Work' })
    state = addProfile(state, { id: 'work2', label: 'Second' })
    state = addProfile(state, { id: 'work3', label: 'Third' })

    expect(removeProfile(state, 'work3', 'work1').activeProfileId).toBe('work1')
  })

  it('falls back to the first remaining when the named one is gone too', () => {
    const state = addProfile(base(), { id: 'work1', label: 'Work' })

    const after = removeProfile(state, 'work1', 'vanished')

    expect(after.activeProfileId).toBe(after.profiles[0].id)
  })

  it('never lands on the profile it just removed', () => {
    const state = addProfile(base(), { id: 'work1', label: 'Work' })

    expect(removeProfile(state, 'work1', 'work1').activeProfileId).not.toBe('work1')
  })

  it('leaves an untouched active profile alone even when a fallback is named', () => {
    let state = addProfile(base(), { id: 'work1', label: 'Work' })
    state = addProfile(state, { id: 'work2', label: 'Second' })

    expect(removeProfile(state, 'work1', 'nonsense').activeProfileId).toBe('work2')
  })
})

describe('canDiscardActiveProfile', () => {
  it('says no during a first run', () => {
    // The only profile there is, and no server yet: this is the screen the app
    // cannot start without, not a step the user chose to take.
    expect(canDiscardActiveProfile(base())).toBe(false)
  })

  it('says no when the only profile is configured', () => {
    const state = updateProfile(base(), base().activeProfileId, { serverUrl: 'https://app.agentrq.com' })
    expect(canDiscardActiveProfile(state)).toBe(false)
  })

  it('says yes for a profile added and never connected', () => {
    let state = updateProfile(base(), base().activeProfileId, { serverUrl: 'https://app.agentrq.com' })
    state = addProfile(state, { id: 'work1', label: 'Work' })

    expect(canDiscardActiveProfile(state)).toBe(true)
  })

  it('says no once that profile has a server', () => {
    // It is a working account now; the way out of it is the switcher.
    let state = addProfile(base(), { id: 'work1', label: 'Work' })
    state = updateProfile(state, 'work1', { serverUrl: 'https://work.example.com' })

    expect(canDiscardActiveProfile(state)).toBe(false)
  })

  it('says no rather than throwing when there is no state at all', () => {
    // Called from the shell, which can ask before the profiles are loaded.
    expect(canDiscardActiveProfile(null)).toBe(false)
    expect(canDiscardActiveProfile({})).toBe(false)
  })
})

describe('activateProfile', () => {
  it('switches to a profile that exists', () => {
    const state = addProfile(base(), { id: 'work1', label: 'Work' })
    const first = state.profiles[0].id

    expect(activateProfile(state, first).activeProfileId).toBe(first)
  })

  it('ignores one that does not, rather than leaving nothing active', () => {
    const state = base()
    expect(activateProfile(state, 'ghost')).toBe(state)
  })
})

describe('renameProfile and updateProfile', () => {
  it('renames, tidying the label', () => {
    const state = renameProfile(base(), base().profiles[0].id, '  Personal   account ')
    expect(state.profiles[0].label).toBe('Personal account')
  })

  it('keeps the old name rather than accepting a blank one', () => {
    const id = base().profiles[0].id
    expect(renameProfile(base(), id, '   ').profiles[0].label).toBe(DEFAULT_PROFILE_LABEL)
  })

  it('trims a label that is really a paste accident', () => {
    const id = base().profiles[0].id
    expect(renameProfile(base(), id, 'x'.repeat(200)).profiles[0].label).toHaveLength(40)
  })

  it('updates the server and mutes of one profile only', () => {
    let state = addProfile(base(), { id: 'work1', label: 'Work' })
    const other = state.profiles[0].id

    state = updateProfile(state, 'work1', {
      serverUrl: 'https://work.test',
      mutedWorkspaces: ['a', 'a', 'b', ''],
    })

    expect(state.profiles[1]).toMatchObject({
      serverUrl: 'https://work.test',
      mutedWorkspaces: ['a', 'b'],
    })
    // The other profile is untouched: settings belong to one account, not all.
    expect(state.profiles.find((p) => p.id === other).serverUrl).toBe('')
  })

  it('ignores an unknown profile', () => {
    const state = base()
    expect(renameProfile(state, 'ghost', 'x')).toBe(state)
    expect(updateProfile(state, 'ghost', { serverUrl: 'https://x.test' })).toBe(state)
  })
})

describe('activeProfile', () => {
  it('returns the one in use', () => {
    const state = addProfile(base(), { id: 'work1', label: 'Work' })
    expect(activeProfile(state).id).toBe('work1')
  })

  it('falls back to the first rather than returning nothing', () => {
    const state = { ...base(), activeProfileId: 'ghost' }
    expect(activeProfile(state)).toBe(state.profiles[0])
  })
})

describe('ids and partitions', () => {
  it('accepts ids that are safe in a path and rejects the rest', () => {
    for (const id of ['default', 'a1b2c3', 'work-2']) expect(isValidProfileId(id)).toBe(true)
    for (const id of ['', '../x', 'a/b', 'CAPS', '-lead', 'x'.repeat(65), null, 7]) {
      expect(isValidProfileId(id)).toBe(false)
    }
  })

  it('builds a persistent partition, so a sign-in outlives the window', () => {
    expect(partitionFor('a1b2c3')).toBe('persist:profile-a1b2c3')
  })

  it('rejects a profile with no usable id', () => {
    expect(makeProfile({ id: '../escape' })).toBeNull()
    expect(makeProfile(null)).toBeNull()
  })
})

describe('edges the caller can reach', () => {
  it('names a profile whose label is not text at all', () => {
    // Stored state is read off disk, so a label can be any JSON value.
    for (const label of [42, null, {}, ['x'], true]) {
      expect(makeProfile({ id: 'one', label }).label).toBe(DEFAULT_PROFILE_LABEL)
    }
  })

  it('still produces a usable profile when handed an unusable first id', () => {
    // The caller supplies this id; it must not be able to produce a profile
    // with no partition, which would mean the shared default session.
    for (const bad of ['../escape', 'CAPS', '', undefined, 7]) {
      const state = migrateProfiles({ serverUrl: 'https://x.test' }, bad)

      expect(state.profiles).toHaveLength(1)
      expect(isValidProfileId(state.profiles[0].id)).toBe(true)
      expect(state.profiles[0].partition).toBe(partitionFor(state.profiles[0].id))
    }
  })

  it('renames through updateProfile as well as renameProfile', () => {
    const state = updateProfile(base(), base().profiles[0].id, { label: '  Personal  ' })

    expect(state.profiles[0].label).toBe('Personal')
  })

  it('leaves the name alone when updateProfile is given none', () => {
    const state = updateProfile(base(), base().profiles[0].id, { serverUrl: 'https://x.test' })

    expect(state.profiles[0].label).toBe(DEFAULT_PROFILE_LABEL)
  })
})
