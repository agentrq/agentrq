import { describe, it, expect, vi } from 'vitest'
import { EventEmitter } from 'node:events'

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
