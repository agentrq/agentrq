// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'

import { SEALED_PREFIX, createVault } from '../src/ext-host/vault.js'
import { createConfigStore } from '../../desktop/src/main/extensions/config.js'
import { createStorageStore } from '../../desktop/src/main/extensions/storage.js'

/** The shell's keychain, as the wire answers for it: reversible, and countable. */
function shell({ available = true, refuse = false, openFails = false, vaultFails = false } = {}) {
  const asked = []
  return {
    asked,
    async request(method, params) {
      asked.push(method)
      if (method === 'vault') {
        if (vaultFails) throw new Error('no answer')
        return { available }
      }
      if (method === 'seal') {
        if (refuse) return { ok: false, reason: refuse === true ? '' : refuse }
        return { ok: true, sealed: params.texts.map((text) => `${SEALED_PREFIX}${Buffer.from(text).toString('base64')}`) }
      }
      if (method === 'open') {
        if (openFails) throw new Error('no answer')
        return {
          opened: params.blobs.map((blob) =>
            blob.startsWith(`${SEALED_PREFIX}bad`) ? null : Buffer.from(blob.slice(SEALED_PREFIX.length), 'base64').toString(),
          ),
        }
      }
      throw new Error(`unexpected ${method}`)
    },
  }
}

function file(initial) {
  let held = initial
  return {
    read: async () => {
      if (held === undefined) throw new Error('ENOENT')
      return JSON.parse(JSON.stringify(held))
    },
    write: async (value) => {
      held = JSON.parse(JSON.stringify(value))
    },
    get: () => held,
  }
}

const FIELDS = [
  { key: 'url', type: 'string', label: 'URL' },
  { key: 'token', type: 'secret', label: 'Token' },
]

describe('the vault the extension stores are handed', () => {
  it('matches the shell s prefix', () => {
    expect(SEALED_PREFIX).toBe('arqv1:')
  })

  it('is unavailable until the shell says otherwise, and after it fails to', async () => {
    const sealing = createVault(shell())
    expect(sealing.vault.available()).toBe(false)
    expect(await sealing.refresh()).toBe(true)
    expect(sealing.vault.available()).toBe(true)

    const silent = createVault(shell({ vaultFails: true }))
    expect(await silent.refresh()).toBe(false)
    expect(await createVault({ request: async () => null }).refresh()).toBe(false)
  })

  it('never writes a secret in the clear, and reads it back', async () => {
    const rpc = shell()
    const sealing = createVault(rpc)
    await sealing.refresh()
    const disk = file()
    const config = createConfigStore({ store: sealing.wrap(disk), vault: sealing.vault })

    expect(await config.save('demo', FIELDS, { url: 'https://x', token: 'hunter2' })).toEqual({ ok: true })
    const written = JSON.stringify(disk.get())
    expect(written).not.toContain('hunter2')
    expect(written).not.toContain('agentrq-pending')
    expect(disk.get().secrets.demo.token.startsWith(SEALED_PREFIX)).toBe(true)
    expect(await config.resolve('demo')).toEqual({ url: 'https://x', token: 'hunter2' })

    // Saved again: the secret already sealed is not sealed a second time.
    const seals = rpc.asked.filter((m) => m === 'seal').length
    expect(await config.save('demo', FIELDS, { url: 'https://y' })).toEqual({ ok: true })
    expect(rpc.asked.filter((m) => m === 'seal').length).toBe(seals)
    expect(disk.get().secrets.demo.token.startsWith(SEALED_PREFIX)).toBe(true)

    // Another run reads it back through the shell.
    const next = createVault(shell())
    const reread = createConfigStore({ store: next.wrap(disk), vault: next.vault })
    expect(await reread.resolve('demo')).toEqual({ url: 'https://y', token: 'hunter2' })
  })

  it('refuses a secret where the shell has no keychain', async () => {
    const sealing = createVault(shell({ available: false }))
    await sealing.refresh()
    const config = createConfigStore({ store: sealing.wrap(file()), vault: sealing.vault })
    const saved = await config.save('demo', FIELDS, { token: 'x' })
    expect(saved.ok).toBe(false)
    expect(saved.reason).toMatch(/no secure storage/)
  })

  it('fails the write when the shell will not seal', async () => {
    const sealing0 = createVault(shell({ refuse: true }))
    await sealing0.refresh()
    const disk = file()
    const warned = []
    const storage = createStorageStore({ store: sealing0.wrap(disk), vault: sealing0.vault, logger: { warn: (...a) => warned.push(a) } })
    const result = await storage.for('demo').secret.set('key', 'value')
    expect(result.ok).toBe(false)
    expect(disk.get()).toBeUndefined()
    const sealing = createVault(shell({ refuse: 'The keychain refused.' }))
    await expect(sealing.wrap(file()).write({ secrets: { a: sealing.vault.encrypt('x') } })).rejects.toThrow(
      'The keychain refused.',
    )
    const silent = createVault({ request: async () => null })
    await expect(silent.wrap(file()).write({ a: silent.vault.encrypt('x') })).rejects.toThrow(
      'This system has no secure storage available.',
    )
  })

  it('answers a secret it cannot open as absent, never as ciphertext', async () => {
    const disk = file({ data: {}, secrets: { demo: { good: `${SEALED_PREFIX}${Buffer.from('ok').toString('base64')}`, bad: `${SEALED_PREFIX}bad` } } })
    const sealing = createVault(shell())
    const storage = createStorageStore({ store: sealing.wrap(disk), vault: sealing.vault })
    const at = storage.for('demo')
    expect(await at.secret.get('good')).toBe('ok')
    expect(await at.secret.get('bad')).toBe('')

    const unopened = createVault(shell({ openFails: true }))
    const lost = createStorageStore({ store: unopened.wrap(disk), vault: unopened.vault })
    expect(await lost.for('demo').secret.get('good')).toBe('')
  })

  it('leaves everything that is not a secret as it was', async () => {
    const sealing = createVault(shell())
    const disk = file()
    const token = sealing.vault.encrypt('s')
    await sealing.wrap(disk).write({ list: [1, 'two', null, { deep: token }], flag: true })
    expect(disk.get()).toEqual({
      list: [1, 'two', null, { deep: `${SEALED_PREFIX}${Buffer.from('s').toString('base64')}` }],
      flag: true,
    })
    expect(await sealing.wrap(disk).read()).toEqual(disk.get())
    expect(sealing.vault.decrypt(disk.get().list[3].deep)).toBe('s')
    expect(() => sealing.vault.decrypt('anything else')).toThrow('cannot be read')
  })
})
