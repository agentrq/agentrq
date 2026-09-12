// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { createHash } from 'node:crypto'
import { cp, lstat, mkdir, mkdtemp, readFile, realpath, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, resolve, sep } from 'node:path'

/**
 * Getting the bytes, for each of the three sources.
 *
 * Deliberately thin. Everything that can be decided — what a source is, whether
 * a digest matches, what happens when any of it fails — lives in `source.js` and
 * `install.js`, where it is tested without a network or a disk. What is left
 * here is the part that genuinely has to touch the machine, kept small enough to
 * read in one go.
 *
 * ## `git` does the authenticating
 *
 * A private repository works because the user's own SSH key or credential helper
 * is what clones it. Nothing here handles a token, which is both less to secure
 * and less to get wrong — and it is the only reason a private extension can work
 * at all without a registry standing in the middle.
 *
 * Submodules are deliberately not fetched: they would let a repository pull in
 * code from somewhere the user never named, which is exactly the surprise this
 * whole feature is trying not to have.
 */

/** Where staging happens. Removed by the installer whether or not it succeeds. */
export async function makeTempDir() {
  return mkdtemp(join(tmpdir(), 'agentrq-ext-'))
}

/** A filesystem error in words, without the path it happened to be holding. */
function whyNot(error) {
  const code = String(error?.code ?? '')
  if (code === 'ENOENT') return 'that file is not there'
  if (code === 'EACCES' || code === 'EPERM') return 'that file cannot be read'
  return String(error?.message ?? error ?? 'unknown error')
}

/** How much drawer a frame is worth handing. Bounded, because it is parsed there. */
// Mermaid bundled and minified is five megabytes, which sets the floor here:
// a cap below that refuses the first real drawer anybody writes. Bounded all
// the same, because it is parsed in a window somebody is reading in.
export const MAX_DRAWER_BYTES = 16 * 1024 * 1024

/**
 * Reads a drawer's file out of the directory an extension was installed into.
 *
 * The entry was already checked when the manifest was parsed, and it is checked
 * again here — against the *resolved* path this time. Two guards that fail
 * differently: the first is a rule about a string, and this one is a fact about
 * the filesystem. A symlink pointing out of the directory satisfies the first
 * and is caught by the second, because `realpath` is what it actually is.
 */
export async function readDrawer(dir, entry) {
  if (!dir || !entry) return { ok: false, reason: 'no drawer was named' }

  let root
  let file
  try {
    root = await realpath(resolve(dir))
    file = await realpath(resolve(root, entry))
  } catch (error) {
    return { ok: false, reason: whyNot(error) }
  }

  // `sep` on the end, so a sibling directory whose name merely starts the same
  // way — `/ext/mermaid-evil` beside `/ext/mermaid` — is not inside it.
  if (file !== root && !file.startsWith(root + sep)) {
    return { ok: false, reason: 'that file is not inside the extension' }
  }

  try {
    const stat = await lstat(file)
    if (!stat.isFile()) return { ok: false, reason: 'that is not a file' }
    if (stat.size > MAX_DRAWER_BYTES) return { ok: false, reason: 'that drawer is too large' }
    return { ok: true, code: await readFile(file, 'utf8') }
  } catch (error) {
    return { ok: false, reason: whyNot(error) }
  }
}

/** The manifest inside an unpacked extension, or null when there is none. */
export async function readManifest(dir) {
  try {
    return await readFile(join(dir, 'agentrq-extension.json'), 'utf8')
  } catch {
    return null
  }
}

/**
 * Runs a command, resolving with its output or rejecting with its error text.
 *
 * `spawn` is injected so this file can be exercised without running anything.
 */
function run(spawn, command, args, options = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, { ...options, stdio: ['ignore', 'pipe', 'pipe'] })
    let out = ''
    let err = ''
    child.stdout?.on('data', (chunk) => {
      out += chunk
    })
    child.stderr?.on('data', (chunk) => {
      err += chunk
    })
    child.on('error', reject)
    child.on('close', (code) => {
      // git writes progress to stderr, so its text is only an error when the
      // exit code says so.
      if (code === 0) resolve(out.trim())
      else reject(new Error(err.trim() || `${command} exited with ${code}`))
    })
  })
}

/**
 * Fetches a source into `into`, reporting what identifies what arrived.
 *
 * @param {object} deps
 * @param {typeof import('node:child_process').spawn} deps.spawn
 * @param {typeof fetch} [deps.fetchImpl]
 */
export function createFetchSource({ spawn, fetchImpl = fetch }) {
  return async function fetchSource(source, into) {
    const dir = join(into, 'unpacked')
    await mkdir(dir, { recursive: true })

    if (source.kind === 'local') {
      // Copied rather than linked: this is the non-linked path, where the point
      // is a snapshot that cannot change underneath the app.
      await cp(source.path, dir, { recursive: true })
      return { dir }
    }

    if (source.kind === 'git') {
      const args = ['clone', '--depth', '1', '--no-recurse-submodules']
      if (source.ref) args.push('--branch', source.ref)
      args.push(source.url, dir)
      await run(spawn, 'git', args)

      // The commit is what pins a clone: a branch moves, the commit it pointed
      // at does not.
      const commit = await run(spawn, 'git', ['-C', dir, 'rev-parse', 'HEAD'])
      // Nothing should carry the clone's own history into an install.
      await rm(join(dir, '.git'), { recursive: true, force: true })
      return { dir, commit }
    }

    const url = `https://github.com/${source.repo}/releases/download/${source.release}/${source.asset}`
    const response = await fetchImpl(url)
    if (!response.ok) throw new Error(`Could not download ${source.asset} (${response.status})`)

    const bytes = Buffer.from(await response.arrayBuffer())
    // Hashed before anything is unpacked. The installer refuses on a mismatch,
    // and it can only do that if nothing has been written yet.
    const sha256 = createHash('sha256').update(bytes).digest('hex')

    // Compared here as well as there, because `tar` is what turns a stranger's
    // bytes into files on disk: an archive that is not the one the manifest
    // named must never reach it, however carefully the installer refuses
    // afterwards. The digest travels back with nothing written, so the
    // installer still owns the refusal and its wording.
    if (sha256 !== String(source.sha256 ?? '').toLowerCase()) return { dir, sha256 }

    const archive = join(into, source.asset)
    await writeFile(archive, bytes)
    // `tar` rather than a dependency: it is present on macOS and Linux, and
    // Windows has shipped bsdtar as tar.exe since Windows 10 1803.
    await run(spawn, 'tar', ['-xzf', archive, '-C', dir, '--strip-components', '1'])

    return { dir, sha256 }
  }
}
