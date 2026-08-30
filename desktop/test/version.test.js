import { describe, it, expect } from 'vitest'

import {
  VERSION_SOURCES,
  compareVersions,
  parseGoVersion,
  parsePackageVersion,
  tagMatchesVersion,
} from '../src/version.js'

describe('parsePackageVersion', () => {
  it('reads the version', () => {
    expect(parsePackageVersion('{"name":"x","version":"0.4.8"}')).toBe('0.4.8')
  })

  it('returns nothing when there is no usable version', () => {
    expect(parsePackageVersion('{"name":"x"}')).toBeNull()
    expect(parsePackageVersion('{"version":""}')).toBeNull()
    expect(parsePackageVersion('{"version":42}')).toBeNull()
  })

  it('returns nothing rather than throwing on unreadable JSON', () => {
    expect(parsePackageVersion('{ not json')).toBeNull()
    expect(parsePackageVersion('')).toBeNull()
    expect(parsePackageVersion(undefined)).toBeNull()
  })
})

describe('parseGoVersion', () => {
  it('reads the constant out of the Go source', () => {
    const source = `const (\n\t_appName = "AgentRQ"\n\t_appVersion   = "v0.4.8"\n)`
    expect(parseGoVersion(source)).toBe('0.4.8')
  })

  it('drops the leading v, which nothing else in the repo uses', () => {
    // Comparing 'v0.4.8' with '0.4.8' would fail for no reason at all.
    expect(parseGoVersion('_appVersion = "v1.2.3"')).toBe('1.2.3')
    expect(parseGoVersion('_appVersion = "1.2.3"')).toBe('1.2.3')
  })

  it('tolerates whatever spacing the file happens to use', () => {
    expect(parseGoVersion('_appVersion="v0.1.0"')).toBe('0.1.0')
    expect(parseGoVersion('_appVersion    =    "v0.1.0"')).toBe('0.1.0')
  })

  it('returns nothing when the constant is absent', () => {
    // Better to fail the release than to compare against a guess.
    expect(parseGoVersion('const _other = "v1.0.0"')).toBeNull()
    expect(parseGoVersion('')).toBeNull()
    expect(parseGoVersion(null)).toBeNull()
  })
})

describe('compareVersions', () => {
  it('passes when all three agree', () => {
    expect(compareVersions({ desktop: '0.4.8', frontend: '0.4.8', backend: '0.4.8' })).toEqual({
      ok: true,
      version: '0.4.8',
      problems: [],
    })
  })

  it('names what disagrees, and with what', () => {
    const result = compareVersions({ desktop: '0.5.0', frontend: '0.4.8', backend: '0.4.8' })

    expect(result.ok).toBe(false)
    expect(result.problems).toEqual([
      `${VERSION_SOURCES.frontend} is 0.4.8, but desktop is 0.5.0`,
      `${VERSION_SOURCES.backend} is 0.4.8, but desktop is 0.5.0`,
    ])
  })

  it('treats the desktop version as the reference', () => {
    // It is the number electron-updater compares against a release, so it is
    // the one that has to be right.
    const result = compareVersions({ desktop: '0.4.8', frontend: '0.5.0', backend: '0.4.8' })
    expect(result.problems).toEqual([`${VERSION_SOURCES.frontend} is 0.5.0, but desktop is 0.4.8`])
  })

  it('fails when a version could not be read at all', () => {
    const result = compareVersions({ desktop: '0.4.8', frontend: null, backend: '0.4.8' })

    expect(result.ok).toBe(false)
    expect(result.problems).toEqual([`could not read the version from ${VERSION_SOURCES.frontend}`])
  })

  it('names an unknown source by its key rather than printing undefined', () => {
    // A fourth version could be added to the repo; the message should still be
    // readable if someone forgets to describe where it lives.
    const missing = compareVersions({ desktop: '0.4.8', mobile: null })
    expect(missing.problems).toEqual(['could not read the version from mobile'])

    const mismatched = compareVersions({ desktop: '0.4.8', mobile: '0.1.0' })
    expect(mismatched.problems).toEqual(['mobile is 0.1.0, but desktop is 0.4.8'])
  })

  it('reports every unreadable source rather than only the first', () => {
    const result = compareVersions({ desktop: null, frontend: null, backend: null })
    expect(result.problems).toHaveLength(3)
    expect(result.version).toBeNull()
  })
})

describe('tagMatchesVersion', () => {
  it('accepts a tag that matches', () => {
    expect(tagMatchesVersion('refs/tags/v0.4.8', '0.4.8')).toEqual({
      ok: true,
      reason: 'tag v0.4.8 matches version 0.4.8',
    })
  })

  it('accepts a bare tag as well as a full ref', () => {
    expect(tagMatchesVersion('v0.4.8', '0.4.8').ok).toBe(true)
    expect(tagMatchesVersion('0.4.8', '0.4.8').ok).toBe(true)
  })

  it('rejects a tag that does not match the tree being built', () => {
    // Otherwise the release is named for one version and its artifacts for
    // another, and electron-updater offers users the build they already have.
    expect(tagMatchesVersion('refs/tags/v0.5.0', '0.4.8')).toEqual({
      ok: false,
      reason: 'tag v0.5.0 does not match version 0.4.8',
    })
  })

  it('passes when there is no tag, which is every non-release build', () => {
    expect(tagMatchesVersion('', '0.4.8').ok).toBe(true)
    expect(tagMatchesVersion(undefined, '0.4.8').ok).toBe(true)
    expect(tagMatchesVersion(null, '0.4.8').ok).toBe(true)
  })

  it('is not fooled by a branch ref', () => {
    // refs/heads/main is not a tag; stripping only the tag prefix leaves it
    // unequal to the version, which is the correct answer for a release build.
    expect(tagMatchesVersion('refs/heads/main', '0.4.8').ok).toBe(false)
  })
})
