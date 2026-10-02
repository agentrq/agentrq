// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { mkdir, readFile, rename, rm, writeFile } from 'node:fs/promises'
import { join } from 'node:path'

/**
 * What somebody answered when a page in the side panel asked for a permission.
 *
 * Kept on this machine only — a JSON file per profile in the app's own data
 * folder — and never sent anywhere: which sites somebody lets use their camera
 * is theirs. One file per profile, so forgetting a profile can delete it.
 *
 * Every failure is survivable. A file that cannot be read is an empty record,
 * so every page asks again rather than the panel refusing to start; a write
 * that fails keeps the answer for this run and says so in the log.
 */

export const PERMISSIONS_DIR = 'side-panel-permissions'

/** Only the two answers a person can give. */
const DECISIONS = new Set(['allow', 'block'])

/**
 * @param {object} options
 * @param {string} options.dir         the app's user-data folder
 * @param {string} options.profileId
 * @param {{ warn: Function }} [options.logger]
 * @param {{ mkdir, readFile, rename, rm, writeFile }} [options.fs]  test seam
 */
export function createPermissionStore({ dir, profileId, logger = console, fs = { mkdir, readFile, rename, rm, writeFile } }) {
  const folder = join(dir, PERMISSIONS_DIR)
  const file = join(folder, `${encodeURIComponent(profileId)}.json`)
  /** origin → permission → decision */
  let record = new Map()
  let loaded = null

  function parse(text) {
    const parsed = new Map()
    const raw = JSON.parse(text)
    for (const [origin, permissions] of Object.entries(raw?.origins ?? {})) {
      const kept = new Map()
      for (const [permission, decision] of Object.entries(permissions ?? {})) {
        if (DECISIONS.has(decision)) kept.set(permission, decision)
      }
      if (kept.size) parsed.set(origin, kept)
    }
    return parsed
  }

  function serialise() {
    const origins = {}
    for (const [origin, permissions] of record) origins[origin] = Object.fromEntries(permissions)
    return JSON.stringify({ origins }, null, 2)
  }

  async function persist() {
    // Written beside the file and renamed over it, so a crash mid-write leaves
    // the previous answers rather than half of a JSON document.
    const temporary = `${file}.tmp`
    try {
      await fs.mkdir(folder, { recursive: true })
      await fs.writeFile(temporary, serialise())
      await fs.rename(temporary, file)
    } catch (error) {
      logger.warn?.(`[side-panel] site permissions could not be saved, and last only until the app quits: ${error?.message ?? error}`)
    }
  }

  return {
    /** Read the file once; later calls wait on the same read. */
    load() {
      loaded ??= fs.readFile(file, 'utf8').then(
        (text) => { record = parse(text) },
        (error) => {
          if (error?.code !== 'ENOENT') logger.warn?.(`[side-panel] site permissions could not be read, so every site will ask again: ${error?.message ?? error}`)
        },
      ).catch((error) => {
        // Only parsing gets here, and a parse error always has a message.
        logger.warn?.(`[side-panel] site permissions could not be read, so every site will ask again: ${error.message}`)
      })
      return loaded
    },

    /** @returns {'allow'|'block'|undefined} */
    get(origin, permission) {
      return record.get(origin)?.get(permission)
    },

    async set(origin, permission, decision) {
      if (!DECISIONS.has(decision)) return
      if (!record.has(origin)) record.set(origin, new Map())
      record.get(origin).set(permission, decision)
      await persist()
    },

    /** One permission of a site, or every one of them when none is named. */
    async remove(origin, permission) {
      if (!record.has(origin)) return
      if (permission) record.get(origin).delete(permission)
      if (!permission || record.get(origin).size === 0) record.delete(origin)
      await persist()
    },

    /** Every decision, by site, for the Site permissions list. Sorted, so it does not shuffle. */
    list() {
      return [...record.keys()].sort().map((origin) => ({
        origin,
        permissions: [...record.get(origin)]
          .sort(([a], [b]) => a.localeCompare(b))
          .map(([permission, decision]) => ({ permission, decision })),
      }))
    },

    /** The profile is gone: so is everything it answered. */
    async forget() {
      record = new Map()
      try {
        await fs.rm(file, { force: true })
      } catch (error) {
        logger.warn?.(`[side-panel] site permissions of a removed profile could not be deleted: ${error?.message ?? error}`)
      }
    },
  }
}
