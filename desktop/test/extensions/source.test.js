// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, it, expect } from 'vitest'

import {
  describeSource,
  pinFor,
  releaseSource,
  resolveSource,
  verifyDigest,
} from '../../src/main/extensions/source.js'

/**
 * Three sources, and the difference between them is how much can be verified: a
 * published asset is a stranger's code pinned by a digest, a clone is the user's
 * own repository pinned by a commit, and a linked folder is pinned by nothing at
 * all and says so.
 */

describe('resolveSource', () => {
  it('takes owner/repo, which is what people paste', () => {
    expect(resolveSource('acme/linear').source).toEqual({
      kind: 'git',
      url: 'https://github.com/acme/linear.git',
      ref: '',
    })
  })

  it('takes a browser URL, and keeps the branch if one is in it', () => {
    // A URL copied from the address bar while looking at a branch means that
    // branch; throwing the ref away would quietly install something else.
    expect(resolveSource('https://github.com/acme/linear').source.url).toBe(
      'https://github.com/acme/linear.git',
    )
    expect(resolveSource('https://github.com/acme/linear/tree/next').source).toEqual({
      kind: 'git',
      url: 'https://github.com/acme/linear.git',
      ref: 'next',
    })
  })

  it('keeps a ref that has slashes in it', () => {
    expect(resolveSource('https://github.com/acme/linear/tree/feat/thing').source.ref).toBe('feat/thing')
  })

  it('takes an SSH remote, which is how a private repository is usually cloned', () => {
    // The whole point of the git source: `git` authenticates with the key the
    // user already has, and nothing here ever sees a credential.
    expect(resolveSource('git@github.com:acme/private.git').source).toEqual({
      kind: 'git',
      url: 'git@github.com:acme/private.git',
      ref: '',
    })
  })

  it('takes a self-hosted URL, not only github.com', () => {
    expect(resolveSource('https://git.acme.internal/team/ext').source.url).toBe(
      'https://git.acme.internal/team/ext.git',
    )
  })

  it('does not double the .git suffix', () => {
    expect(resolveSource('https://github.com/acme/linear.git').source.url).toBe(
      'https://github.com/acme/linear.git',
    )
  })

  it('takes a path, absolute or relative or file:', () => {
    expect(resolveSource('/Users/me/ext').source).toEqual({ kind: 'local', path: '/Users/me/ext' })
    expect(resolveSource('./ext').source.kind).toBe('local')
    expect(resolveSource('file:///Users/me/ext').source.path).toBe('/Users/me/ext')
  })

  it('uses the platform’s own idea of an absolute path', () => {
    const isAbsolute = (p) => /^[A-Z]:\\/.test(p)
    expect(resolveSource('C:\\Users\\me\\ext', { isAbsolute }).source).toEqual({
      kind: 'local',
      path: 'C:\\Users\\me\\ext',
    })
  })

  it('asks for something rather than failing silently on nothing', () => {
    expect(resolveSource('').reason).toBe('Enter a repository or a folder to install from.')
    expect(resolveSource('   ').ok).toBe(false)
    expect(resolveSource(undefined).ok).toBe(false)
  })

  it('refuses what it cannot make sense of', () => {
    expect(resolveSource('just some words').reason).toContain('not a repository or a folder')
    expect(resolveSource('https://github.com/').reason).toContain('names no repository')
    expect(resolveSource('http://[').reason).toContain('not a URL this understands')
  })
})

describe('releaseSource', () => {
  const entry = {
    fullName: 'acme/linear',
    manifest: {
      name: 'linear',
      artifact: { release: 'v1.2.0', asset: 'linear.tgz', sha256: 'a'.repeat(64) },
    },
  }

  it('carries the digest, which is the only reason this source can be trusted', () => {
    expect(releaseSource(entry).source).toEqual({
      kind: 'release',
      name: 'linear',
      repo: 'acme/linear',
      release: 'v1.2.0',
      asset: 'linear.tgz',
      sha256: 'a'.repeat(64),
    })
  })

  it('refuses an entry with nothing to install', () => {
    expect(releaseSource({ manifest: {} }).reason).toContain('no release to install')
    expect(releaseSource(undefined).ok).toBe(false)
  })
})

describe('pinFor', () => {
  it('pins a release by its digest, because the tag naming it can move', () => {
    const source = { kind: 'release', sha256: 'a'.repeat(64) }
    expect(pinFor(source)).toEqual({ kind: 'sha256', value: 'a'.repeat(64) })
  })

  it('pins a clone by the commit it landed on', () => {
    // A branch moves; the commit it pointed at does not.
    expect(pinFor({ kind: 'git' }, { commit: 'deadbee' })).toEqual({ kind: 'commit', value: 'deadbee' })
    expect(pinFor({ kind: 'git' })).toEqual({ kind: 'commit', value: '' })
  })

  it('admits that a linked folder is pinned by nothing', () => {
    // It is a directory the user edits. Claiming to have verified it would be a
    // lie, and the honest answer is what lets the UI mark it as live.
    expect(pinFor({ kind: 'local' })).toEqual({ kind: 'none', value: '' })
  })
})

describe('verifyDigest', () => {
  const good = 'a'.repeat(64)

  it('accepts a match, whatever the casing', () => {
    expect(verifyDigest(good, good.toUpperCase()).ok).toBe(true)
  })

  it('refuses a mismatch outright, rather than warning about it', () => {
    // A tag can be moved after an install, so a mismatch means the bytes are not
    // what the manifest described — tampered or corrupt, neither clickable-past.
    const { ok, reason } = verifyDigest(good, 'b'.repeat(64))

    expect(ok).toBe(false)
    expect(reason).toContain('was not installed')
    expect(reason).toContain('changed after it was published')
  })

  it('refuses when there is nothing usable to check against', () => {
    // A manifest with no digest cannot pin anything, and neither can one whose
    // digest is the wrong shape.
    expect(verifyDigest('', good).reason).toContain('no usable checksum')
    expect(verifyDigest('short', good).ok).toBe(false)
    expect(verifyDigest(undefined, good).ok).toBe(false)
    expect(verifyDigest(null, good).ok).toBe(false)
  })

  it('refuses when the download could not be checksummed', () => {
    expect(verifyDigest(good, '').reason).toContain('could not be checksummed')
    expect(verifyDigest(good, undefined).ok).toBe(false)
  })
})

describe('describeSource', () => {
  it('says where each kind came from', () => {
    expect(describeSource({ kind: 'release', repo: 'acme/x', release: 'v1' })).toBe('acme/x · v1')
    expect(describeSource({ kind: 'git', url: 'git@x:y.git' })).toBe('git@x:y.git')
    expect(describeSource({ kind: 'git', url: 'git@x:y.git', ref: 'next' })).toBe('git@x:y.git · next')
    // Marked as linked, because it can change under the app.
    expect(describeSource({ kind: 'local', path: '/tmp/x' })).toBe('/tmp/x · linked')
    expect(describeSource(undefined)).toBe('Unknown source')
  })
})
