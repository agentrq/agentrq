// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { mkdtempSync, readFileSync, rmSync, statSync, writeFileSync, existsSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import { createVault } from '../src/main/vault.js'

describe('createVault', () => {
  let dir
  let keyFile

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), 'vault-'))
    keyFile = join(dir, 'nested', 'secrets.key')
  })

  afterEach(() => rmSync(dir, { recursive: true, force: true }))

  it('opens what it sealed, and never holds it in the clear', () => {
    const vault = createVault({ keyFile })
    const sealed = vault.encrypt('sk-123')
    expect(Buffer.from(sealed, 'base64').includes('sk-123')).toBe(false)
    expect(vault.decrypt(sealed)).toBe('sk-123')
    expect(vault.encrypt('sk-123')).not.toBe(sealed)
  })

  it('keeps one key, readable by this user only, across restarts', () => {
    expect(createVault({ keyFile }).available()).toBe(true)
    expect(readFileSync(keyFile)).toHaveLength(32)
    expect(existsSync(`${keyFile}.part`)).toBe(false)
    if (process.platform !== 'win32') expect(statSync(keyFile).mode & 0o777).toBe(0o600)

    const sealed = createVault({ keyFile }).encrypt('token')
    expect(createVault({ keyFile }).decrypt(sealed)).toBe('token')
  })

  it('refuses what it did not seal, or what was changed since', () => {
    const vault = createVault({ keyFile })
    const sealed = Buffer.from(vault.encrypt('token'), 'base64')

    const changed = Buffer.from(sealed)
    changed[changed.length - 1] ^= 1
    expect(() => vault.decrypt(changed.toString('base64'))).toThrow()

    // What safeStorage sealed before: no magic.
    expect(() => vault.decrypt(Buffer.from('v10' + 'x'.repeat(40)).toString('base64'))).toThrow('not sealed')
    expect(() => vault.decrypt('')).toThrow('not sealed')

    const otherKey = createVault({ keyFile: join(dir, 'other.key') })
    expect(() => otherKey.decrypt(sealed.toString('base64'))).toThrow()
  })

  it('replaces a file that is not a key', () => {
    const vault = createVault({ keyFile: join(dir, 'secrets.key') })
    writeFileSync(join(dir, 'secrets.key'), 'not a key')
    expect(vault.available()).toBe(true)
    expect(readFileSync(join(dir, 'secrets.key'))).toHaveLength(32)
  })

  it('is unavailable where the key cannot be written', () => {
    writeFileSync(join(dir, 'blocker'), '')
    const vault = createVault({ keyFile: join(dir, 'blocker', 'secrets.key') })
    expect(vault.available()).toBe(false)
    expect(() => vault.encrypt('token')).toThrow()
  })
})
