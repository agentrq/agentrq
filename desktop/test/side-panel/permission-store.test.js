// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { PERMISSIONS_DIR, createPermissionStore } from '../../src/main/side-panel/permission-store.js'

let dir
const logger = { warn: vi.fn() }
const store = (profileId = 'p1', extra = {}) => createPermissionStore({ dir, profileId, logger, ...extra })
const fileOf = (profileId = 'p1') => join(dir, PERMISSIONS_DIR, `${profileId}.json`)

beforeEach(async () => {
  dir = await mkdtemp(join(tmpdir(), 'agentrq-permissions-'))
  logger.warn.mockClear()
})

afterEach(async () => {
  await rm(dir, { recursive: true, force: true })
})

describe('the site permission store', () => {
  it('starts empty, and says nothing when there is no file yet', async () => {
    const permissions = store()
    await permissions.load()
    expect(permissions.list()).toEqual([])
    expect(permissions.get('https://meet.example', 'camera')).toBeUndefined()
    expect(logger.warn).not.toHaveBeenCalled()
  })

  it('remembers an answer across launches, on disk only', async () => {
    const first = store()
    await first.load()
    await first.set('https://meet.example', 'camera', 'allow')
    await first.set('https://meet.example', 'microphone', 'block')

    const second = store()
    await second.load()
    expect(second.get('https://meet.example', 'camera')).toBe('allow')
    expect(second.get('https://meet.example', 'microphone')).toBe('block')
    expect(JSON.parse(await readFile(fileOf(), 'utf8'))).toEqual({
      origins: { 'https://meet.example': { camera: 'allow', microphone: 'block' } },
    })
  })

  it('reads the file once however often it is asked to', async () => {
    const readFileSpy = vi.fn(async () => '{"origins":{}}')
    const permissions = store('p1', { fs: { readFile: readFileSpy } })
    await Promise.all([permissions.load(), permissions.load()])
    expect(readFileSpy).toHaveBeenCalledOnce()
  })

  it('keeps each profile to its own file', async () => {
    const work = store('work')
    await work.set('https://meet.example', 'camera', 'allow')
    const home = store('home')
    await home.load()
    expect(home.get('https://meet.example', 'camera')).toBeUndefined()
  })

  it('names the file safely whatever the profile id is', async () => {
    const odd = store('../../escape')
    await odd.set('https://meet.example', 'camera', 'allow')
    expect(JSON.parse(await readFile(join(dir, PERMISSIONS_DIR, '..%2F..%2Fescape.json'), 'utf8')).origins).toBeTruthy()
  })

  it('ignores anything but allow and block', async () => {
    const permissions = store()
    await permissions.set('https://meet.example', 'camera', 'maybe')
    expect(permissions.list()).toEqual([])
  })

  it('keeps only valid answers from the file', async () => {
    await mkdir(join(dir, PERMISSIONS_DIR), { recursive: true })
    await writeFile(fileOf(), JSON.stringify({ origins: {
      'https://a.example': { camera: 'allow', microphone: 'perhaps' },
      'https://b.example': { camera: 'sometimes' },
      'https://c.example': null,
    } }))
    const permissions = store()
    await permissions.load()
    expect(permissions.list()).toEqual([{ origin: 'https://a.example', permissions: [{ permission: 'camera', decision: 'allow' }] }])
  })

  it('treats a file with no origins as empty', async () => {
    await mkdir(join(dir, PERMISSIONS_DIR), { recursive: true })
    await writeFile(fileOf(), 'null')
    const permissions = store()
    await permissions.load()
    expect(permissions.list()).toEqual([])
  })

  it('treats an unreadable file as empty, and says every site will ask again', async () => {
    await mkdir(join(dir, PERMISSIONS_DIR), { recursive: true })
    await writeFile(fileOf(), '{not json')
    const permissions = store()
    await permissions.load()
    expect(permissions.list()).toEqual([])
    expect(logger.warn.mock.calls[0][0]).toMatch(/could not be read, so every site will ask again/)
  })

  it('says so when the file exists but cannot be opened', async () => {
    const denied = Object.assign(new Error('EACCES: permission denied'), { code: 'EACCES' })
    const permissions = store('p1', { fs: { readFile: async () => { throw denied } } })
    await permissions.load()
    expect(permissions.list()).toEqual([])
    expect(logger.warn.mock.calls[0][0]).toMatch(/EACCES: permission denied/)
  })

  it('keeps an answer for the run when it cannot be saved, and says so', async () => {
    // A file where the folder should be: nothing can be written under it.
    await writeFile(join(dir, PERMISSIONS_DIR), 'in the way')
    const permissions = store()
    await permissions.set('https://meet.example', 'camera', 'allow')
    expect(permissions.get('https://meet.example', 'camera')).toBe('allow')
    expect(logger.warn.mock.calls[0][0]).toMatch(/could not be saved, and last only until the app quits/)
  })

  it('removes one permission, or a whole site', async () => {
    const permissions = store()
    await permissions.set('https://meet.example', 'camera', 'allow')
    await permissions.set('https://meet.example', 'microphone', 'allow')
    await permissions.set('https://maps.example', 'geolocation', 'block')

    await permissions.remove('https://meet.example', 'camera')
    expect(permissions.get('https://meet.example', 'camera')).toBeUndefined()
    expect(permissions.get('https://meet.example', 'microphone')).toBe('allow')

    await permissions.remove('https://meet.example', 'microphone')
    expect(permissions.list().map((site) => site.origin)).toEqual(['https://maps.example'])

    await permissions.remove('https://maps.example')
    await permissions.remove('https://never.example')
    expect(permissions.list()).toEqual([])

    const reread = store()
    await reread.load()
    expect(reread.list()).toEqual([])
  })

  it('lists sites and permissions in a stable order', async () => {
    const permissions = store()
    await permissions.set('https://z.example', 'notifications', 'block')
    await permissions.set('https://a.example', 'microphone', 'allow')
    await permissions.set('https://a.example', 'camera', 'block')
    expect(permissions.list()).toEqual([
      { origin: 'https://a.example', permissions: [{ permission: 'camera', decision: 'block' }, { permission: 'microphone', decision: 'allow' }] },
      { origin: 'https://z.example', permissions: [{ permission: 'notifications', decision: 'block' }] },
    ])
  })

  it('forgets everything when its profile is removed', async () => {
    const permissions = store()
    await permissions.set('https://meet.example', 'camera', 'allow')
    await permissions.forget()
    expect(permissions.list()).toEqual([])
    await expect(readFile(fileOf(), 'utf8')).rejects.toThrow(/ENOENT/)
    // Forgetting twice, or with no file at all, is fine.
    await permissions.forget()
  })

  it('says so when a removed profile’s file cannot be deleted', async () => {
    const permissions = store('p1', { fs: { rm: async () => { throw new Error('EBUSY: resource busy') } } })
    await permissions.forget()
    expect(logger.warn.mock.calls[0][0]).toMatch(/could not be deleted: EBUSY: resource busy/)
  })

  it('logs to the console when no logger is given, and tolerates a bare error', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const permissions = createPermissionStore({ dir, profileId: 'p1', fs: {
      readFile: async () => { throw 'disk unplugged' },
      mkdir: async () => { throw 'disk unplugged' },
      rm: async () => { throw 'disk unplugged' },
    } })
    await permissions.load()
    await permissions.set('https://meet.example', 'camera', 'allow')
    await permissions.forget()
    expect(warn).toHaveBeenCalledTimes(3)
    expect(warn.mock.calls.every(([line]) => line.includes('disk unplugged'))).toBe(true)
    warn.mockRestore()
  })
})
