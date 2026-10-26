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

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..')
const pins = JSON.parse(await readFile(join(ROOT, 'src', 'node-pins.json'), 'utf8'))
const VERSION = pins.version
const ARTIFACTS = pins.artifacts

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
