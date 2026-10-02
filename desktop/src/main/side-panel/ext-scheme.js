// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { lstat, readFile, realpath } from 'node:fs/promises'
import { dirname, resolve, sep } from 'node:path'

import { EXT_SCHEME } from './guest.js'
import { mimeTypeFor } from '../protocol.js'

/**
 * `agentrq-ext://<extension>/<path>`: an Extension's own pages, in the side panel.
 *
 * Each Extension is a host of its own, and therefore an origin of its own, so
 * its storage is its own and one Extension's page cannot read another's.
 * Registered on the panel's partitions only — the app's window never loads it.
 *
 * A file is served only from the folder of a panel page the Extension
 * declared (`provides.panels[].entry`), never the rest of the package, which
 * holds its Node code. The path is checked twice: as a string here, and as the
 * file it really is once symlinks are followed.
 */

/** Big enough for a bundled single-page app; bounded, because it is read whole. */
export const MAX_PANEL_FILE_BYTES = 16 * 1024 * 1024

/** As a manifest entry's segments: no `.` or `..`, and nothing hidden. */
const SEGMENT_RE = /^[a-zA-Z0-9_-][a-zA-Z0-9._-]*$/

const fail = (status, reason) => ({ ok: false, status, reason })

/**
 * The relative path an `agentrq-ext:` URL names, or '' when it names nothing
 * that could be served.
 */
export function panelPathOf(url) {
  let decoded
  try {
    decoded = decodeURIComponent(url.pathname.replace(/^\/+/, ''))
  } catch {
    return ''
  }
  if (!decoded || decoded.includes('\\')) return ''
  const segments = decoded.split('/')
  return segments.every((segment) => SEGMENT_RE.test(segment)) ? decoded : ''
}

/** The declared panel folder `path` is inside, or '' when it is in none of them. */
export function panelFolderFor(installation, path) {
  for (const panel of installation?.manifest?.provides?.panels ?? []) {
    const folder = dirname(panel.entry)
    if (path.startsWith(`${folder}/`)) return folder
  }
  return ''
}

/**
 * Read one file of a panel folder, refusing anything that resolves outside it.
 *
 * @param {string} dir     the installation's directory
 * @param {string} folder  the declared panel folder, relative to it
 * @param {string} path    the file, relative to the installation
 */
export async function readPanelFile(dir, folder, path, fs = { lstat, readFile, realpath }) {
  let root
  let file
  try {
    root = await fs.realpath(resolve(dir, folder))
    file = await fs.realpath(resolve(dir, path))
  } catch (error) {
    return fail(404, error?.code === 'ENOENT' ? 'No such file in this extension.' : 'That file cannot be read.')
  }
  // `sep` on the end, so `panel-evil/` beside `panel/` is not inside it.
  if (!file.startsWith(root + sep)) return fail(404, 'That file is not in a panel folder of this extension.')

  try {
    const stat = await fs.lstat(file)
    if (!stat.isFile()) return fail(404, 'That is not a file.')
    if (stat.size > MAX_PANEL_FILE_BYTES) return fail(413, 'That file is too large to serve.')
    return { ok: true, body: await fs.readFile(file), mimeType: mimeTypeFor(path) }
  } catch {
    return fail(404, 'That file cannot be read.')
  }
}

/**
 * The protocol handler.
 *
 * @param {object} options
 * @param {() => Promise<Array<object>>} options.installations  what is installed
 * @param {typeof readPanelFile} [options.read]
 * @returns {(request: Request) => Promise<Response>}
 */
export function createExtSchemeHandler({ installations, read = readPanelFile }) {
  const refuse = (status, reason) =>
    new Response(reason, { status, headers: { 'content-type': 'text/plain; charset=utf-8' } })

  return async (request) => {
    const url = new URL(request.url)
    if (url.protocol !== `${EXT_SCHEME}:`) return refuse(404, 'Not an extension page.')

    const installation = (await installations()).find(
      (candidate) => candidate.name === url.host && candidate.enabled !== false,
    )
    if (!installation) return refuse(404, 'That extension is not installed, or is turned off.')

    const path = panelPathOf(url)
    const folder = path && panelFolderFor(installation, path)
    if (!folder) return refuse(404, 'That file is not in a panel folder of this extension.')

    const file = await read(installation.dir, folder, path)
    if (!file.ok) return refuse(file.status, file.reason)
    // Never cached across an update: the folder is replaced when the
    // extension is, and yesterday's script against today's code is a bug
    // report nobody can reproduce.
    return new Response(file.body, { status: 200, headers: { 'content-type': file.mimeType, 'cache-control': 'no-cache' } })
  }
}
