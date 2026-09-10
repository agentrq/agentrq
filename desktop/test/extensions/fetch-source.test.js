import { describe, it, expect, vi } from 'vitest'
import { EventEmitter } from 'node:events'
import { createHash } from 'node:crypto'
import { access, mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import { createFetchSource } from '../../src/main/extensions/fetch-source.js'

/**
 * Thin on purpose — everything decidable lives in source.js and install.js,
 * where it is tested without a network or a disk. What is pinned here is the
 * part that has to touch the machine: the arguments given to git, and that a
 * download is hashed before anything is written where it could be loaded.
 */

/** A spawned process that succeeds, or fails with text on stderr. */
function fakeSpawn({ fail = null, stdout = '' } = {}) {
  return vi.fn((command, args) => {
    const child = new EventEmitter()
    child.stdout = new EventEmitter()
    child.stderr = new EventEmitter()
    queueMicrotask(() => {
      if (fail) {
        child.stderr.emit('data', fail)
        child.emit('close', 128)
        return
      }
      if (stdout) child.stdout.emit('data', stdout)
      child.emit('close', 0)
    })
    child.command = command
    child.args = args
    return child
  })
}

describe('createFetchSource · git', () => {
  it('clones shallowly, without submodules, and pins the commit', async () => {
    // Submodules would let a repository pull in code from somewhere the user
    // never named — exactly the surprise this feature is trying not to have.
    const spawn = fakeSpawn({ stdout: 'c0ffee1234\n' })
    const fetchSource = createFetchSource({ spawn })

    const result = await fetchSource({ kind: 'git', url: 'git@github.com:acme/x.git', ref: '' }, '/tmp/s')

    const [command, args] = spawn.mock.calls[0]
    expect(command).toBe('git')
    expect(args).toContain('--depth')
    expect(args).toContain('--no-recurse-submodules')
    expect(args).toContain('git@github.com:acme/x.git')
    expect(result.commit).toBe('c0ffee1234')
  })

  it('clones the ref when one was given', async () => {
    const spawn = fakeSpawn({ stdout: 'abc' })
    await createFetchSource({ spawn })({ kind: 'git', url: 'u', ref: 'next' }, '/tmp/s')

    const [, args] = spawn.mock.calls[0]
    expect(args).toContain('--branch')
    expect(args[args.indexOf('--branch') + 1]).toBe('next')
  })

  it('reports what git said when a clone fails', async () => {
    // A private repository the user cannot reach is the common case, and the
    // message git gives is far more useful than anything invented here.
    const spawn = fakeSpawn({ fail: 'ERROR: Repository not found.' })

    await expect(
      createFetchSource({ spawn })({ kind: 'git', url: 'git@github.com:acme/secret.git' }, '/tmp/s'),
    ).rejects.toThrow('Repository not found')
  })
})

describe('createFetchSource · release', () => {
  it('does not unpack an asset whose digest is not the one the manifest named', async () => {
    // `tar` is what turns a stranger's bytes into files on disk. Refusing after
    // it has run is refusing after the interesting part already happened, so
    // the digest is compared before anything is written and the installer is
    // handed the mismatch to report.
    const spawn = fakeSpawn()
    const bytes = new TextEncoder().encode('not the release you asked for')
    const fetchImpl = vi.fn(async () => ({ ok: true, arrayBuffer: async () => bytes }))
    const into = await mkdtemp(join(tmpdir(), 'agentrq-fetch-test-'))

    const result = await createFetchSource({ spawn, fetchImpl })(
      { kind: 'release', repo: 'a/b', release: 'v1', asset: 'x.tgz', sha256: 'a'.repeat(64) },
      into,
    )

    expect(result.sha256).not.toBe('a'.repeat(64))
    expect(spawn).not.toHaveBeenCalled()
    await expect(access(join(into, 'x.tgz'))).rejects.toThrow()
    await rm(into, { recursive: true, force: true })
  })

  it('unpacks an asset that matches the digest', async () => {
    const spawn = fakeSpawn()
    const bytes = new TextEncoder().encode('the real thing')
    const sha256 = createHash('sha256').update(Buffer.from(bytes)).digest('hex')
    const fetchImpl = vi.fn(async () => ({ ok: true, arrayBuffer: async () => bytes }))
    const into = await mkdtemp(join(tmpdir(), 'agentrq-fetch-test-'))

    const result = await createFetchSource({ spawn, fetchImpl })(
      { kind: 'release', repo: 'a/b', release: 'v1', asset: 'x.tgz', sha256 },
      into,
    )

    expect(result.sha256).toBe(sha256)
    expect(spawn.mock.calls[0][0]).toBe('tar')
    await rm(into, { recursive: true, force: true })
  })

  it('refuses a download the server would not give', async () => {
    const fetchImpl = vi.fn(async () => ({ ok: false, status: 404 }))

    await expect(
      createFetchSource({ spawn: fakeSpawn(), fetchImpl })(
        { kind: 'release', repo: 'a/b', release: 'v1', asset: 'x.tgz' },
        '/tmp/s',
      ),
    ).rejects.toThrow('Could not download x.tgz (404)')
  })
})
