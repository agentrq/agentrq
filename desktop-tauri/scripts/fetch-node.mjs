// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Fetch the pinned Node that runs the extension host, checked against its
 * SHA-256, into `src-tauri/binaries/agentrq-ext-host-<target triple>[.exe]`:
 * where Tauri's `externalBin` looks for it, and copies it beside the app.
 *
 *     node scripts/fetch-node.mjs [<target triple>]
 *
 * The triple is the argument, then TAURI_ENV_TARGET_TRIPLE (set by `tauri
 * build --target`), then rustc's host. A binary already fetched for this pin
 * is kept.
 */

import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { chmod, copyFile, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

/** The latest LTS when this was pinned. Bump all of it together. */
const VERSION = 'v24.22.1'

/** Each triple's artifact on nodejs.org, and its SHA-256 from SHASUMS256.txt. */
const ARTIFACTS = {
  'aarch64-apple-darwin': {
    file: `node-${VERSION}-darwin-arm64.tar.gz`,
    sha256: '89c682593d76cd44233fa53b1563264e7de9cbb189cad169135c5b4fadfbc0a7',
  },
  'x86_64-apple-darwin': {
    file: `node-${VERSION}-darwin-x64.tar.gz`,
    sha256: 'd132557e7ee2b85af08b0c8c34338e65c6430f2238d01f727728ef42dc83b035',
  },
  'aarch64-unknown-linux-gnu': {
    file: `node-${VERSION}-linux-arm64.tar.xz`,
    sha256: 'dbabf7a1cbf947e6059ffad2c27b80041db97dea1e553b62afdee7f13fca336e',
  },
  'x86_64-unknown-linux-gnu': {
    file: `node-${VERSION}-linux-x64.tar.xz`,
    sha256: '6c172a9ab6718f7b801fb5a7bd486af2ee5e32758086a04b84fcce2aa77095e6',
  },
  'x86_64-pc-windows-msvc': {
    file: 'win-x64/node.exe',
    sha256: 'd702daa815eabbff763c7d3808e235cf031cf2ca70211b56847bc72f51e240e5',
  },
  'aarch64-pc-windows-msvc': {
    file: 'win-arm64/node.exe',
    sha256: '8bea91dc437fe01e7d92ef0efc6c9eaf654369aaba9b2d3e9841552ea7b95a00',
  },
}

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..')

function hostTriple() {
  const info = execFileSync('rustc', ['-vV'], { encoding: 'utf8' })
  return info.match(/^host: (\S+)$/m)[1]
}

const triple = process.argv[2] || process.env.TAURI_ENV_TARGET_TRIPLE || hostTriple()
const artifact = ARTIFACTS[triple]
if (!artifact) {
  console.error(`No pinned Node for ${triple}. Known: ${Object.keys(ARTIFACTS).join(', ')}`)
  process.exit(1)
}

const windows = triple.includes('windows')
const target = join(ROOT, 'src-tauri', 'binaries', `agentrq-ext-host-${triple}${windows ? '.exe' : ''}`)
// What was fetched, so a pin that moved fetches again.
const stamp = `${target}.sha256`

const fetched = await readFile(stamp, 'utf8').catch(() => '')
if (fetched.trim() === artifact.sha256) {
  console.log(`Node ${VERSION} for ${triple} is already at ${target}`)
  process.exit(0)
}

const url = `https://nodejs.org/dist/${VERSION}/${artifact.file}`
console.log(`Fetching ${url}`)
const response = await fetch(url)
if (!response.ok) throw new Error(`${url}: HTTP ${response.status}`)
const bytes = Buffer.from(await response.arrayBuffer())
const sha256 = createHash('sha256').update(bytes).digest('hex')
if (sha256 !== artifact.sha256) {
  throw new Error(`${url}: SHA-256 ${sha256}, expected ${artifact.sha256}`)
}

await mkdir(dirname(target), { recursive: true })
if (windows) {
  await writeFile(target, bytes)
} else {
  // Unpacked in a directory of its own, and only the binary kept.
  const work = await mkdtemp(join(tmpdir(), 'agentrq-node-'))
  try {
    const archive = join(work, artifact.file)
    await writeFile(archive, bytes)
    execFileSync('tar', ['-xf', archive, '-C', work])
    const name = artifact.file.replace(/\.tar\.(gz|xz)$/, '')
    await copyFile(join(work, name, 'bin', 'node'), target)
    await chmod(target, 0o755)
  } finally {
    await rm(work, { recursive: true, force: true })
  }
}
await writeFile(stamp, `${artifact.sha256}\n`)
console.log(`Node ${VERSION} for ${triple} is at ${target}`)
