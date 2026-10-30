// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The encryption for extension secrets and the supervisor token.
 *
 * The key is random and lives in a file only this user can read, beside what
 * it protects — what the Tauri app does. The OS keychain is never asked: on
 * macOS `safeStorage` made it prompt for the login password, at every launch.
 */

import { createCipheriv, createDecipheriv, randomBytes } from 'node:crypto'
import * as nodeFs from 'node:fs'
import { dirname } from 'node:path'

const KEY_LEN = 32
const IV_LEN = 12
const TAG_LEN = 16
/** What a sealed blob starts with, so another format is never fed to the cipher. */
const MAGIC = Buffer.from('ARQV1')

/**
 * @param {object} deps
 * @param {string} deps.keyFile  where the key is kept
 * @param {object} [deps.fs]     node:fs, for tests
 * @returns {{ available(): boolean, encrypt(text: string): string, decrypt(blob: string): string }}
 */
export function createVault({ keyFile, fs = nodeFs }) {
  let key = null

  /** The key in `keyFile`, or a new one written there. Throws when it cannot be written. */
  function loadKey() {
    if (key) return key
    try {
      const held = fs.readFileSync(keyFile)
      if (held.length === KEY_LEN) return (key = held)
    } catch {
      // Missing: made below. A file that is not a key is replaced, since what
      // it sealed could not be opened anyway.
    }
    const made = randomBytes(KEY_LEN)
    fs.mkdirSync(dirname(keyFile), { recursive: true })
    // Whole or not at all: a half-written key would lose every secret.
    const part = `${keyFile}.part`
    fs.writeFileSync(part, made, { mode: 0o600 })
    fs.renameSync(part, keyFile)
    return (key = made)
  }

  return {
    /** False where the key cannot be kept; a secret is then refused, never written in the clear. */
    available() {
      try {
        loadKey()
        return true
      } catch {
        return false
      }
    },

    /** `MAGIC`, a fresh IV, the tag, then the ciphertext, as base64. */
    encrypt(text) {
      const iv = randomBytes(IV_LEN)
      const cipher = createCipheriv('aes-256-gcm', loadKey(), iv)
      const body = Buffer.concat([cipher.update(String(text), 'utf8'), cipher.final()])
      return Buffer.concat([MAGIC, iv, cipher.getAuthTag(), body]).toString('base64')
    },

    /** Throws for anything this vault did not seal, or that was changed since. */
    decrypt(blob) {
      const sealed = Buffer.from(String(blob), 'base64')
      if (sealed.length < MAGIC.length + IV_LEN + TAG_LEN || !sealed.subarray(0, MAGIC.length).equals(MAGIC)) {
        throw new Error('not sealed by this vault')
      }
      const iv = sealed.subarray(MAGIC.length, MAGIC.length + IV_LEN)
      const tag = sealed.subarray(MAGIC.length + IV_LEN, MAGIC.length + IV_LEN + TAG_LEN)
      const decipher = createDecipheriv('aes-256-gcm', loadKey(), iv)
      decipher.setAuthTag(tag)
      return Buffer.concat([decipher.update(sealed.subarray(MAGIC.length + IV_LEN + TAG_LEN)), decipher.final()]).toString('utf8')
    },
  }
}
