// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Collect, rename, and generate updater manifests for Tauri release artifacts.
 *
 * Renames bundles to `AgentRQ-Tauri-<version>-<arch>.<ext>` (arch `arm64` or `x64`, no spaces;
 * keeps `.sig` beside its file).
 * Generates `latest-tauri.json` (Tauri static updater format: version, notes, pub_date,
 * and platforms keyed `darwin-aarch64`, `darwin-x86_64`, `linux-x86_64`, `windows-x86_64`,
 * each with signature and url) from the `.sig` files.
 * Skips updater manifest with a warning when there is no signing key.
 */

import { copyFileSync, existsSync, mkdirSync, readFileSync, readdirSync, statSync, writeFileSync } from 'node:fs'
import { basename, dirname, extname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const TAURI_ROOT = join(__dirname, '..')
const REPO_ROOT = join(TAURI_ROOT, '..')

export function normalizeArch(filename, fallbackArch = process.arch) {
  const lower = filename.toLowerCase()
  if (lower.includes('amd64') || lower.includes('x86_64') || lower.includes('x64')) {
    return 'x64'
  }
  if (lower.includes('aarch64') || lower.includes('arm64')) {
    return 'arm64'
  }
  return fallbackArch === 'arm64' || fallbackArch === 'aarch64' ? 'arm64' : 'x64'
}

export function detectExtension(filename) {
  const lower = filename.toLowerCase()
  const isSig = lower.endsWith('.sig')
  const base = isSig ? filename.slice(0, -4) : filename
  const lowerBase = base.toLowerCase()

  let ext = ''
  if (lowerBase.endsWith('.app.tar.gz')) {
    ext = 'app.tar.gz'
  } else if (lowerBase.endsWith('.appimage.tar.gz')) {
    ext = 'AppImage.tar.gz'
  } else if (lowerBase.endsWith('.nsis.zip')) {
    ext = 'nsis.zip'
  } else if (lowerBase.endsWith('.tar.gz')) {
    ext = 'tar.gz'
  } else if (lowerBase.endsWith('.appimage')) {
    ext = 'AppImage'
  } else if (lowerBase.endsWith('.dmg')) {
    ext = 'dmg'
  } else if (lowerBase.endsWith('.deb')) {
    ext = 'deb'
  } else if (lowerBase.endsWith('.exe')) {
    ext = 'exe'
  } else if (lowerBase.endsWith('.zip')) {
    ext = 'zip'
  } else {
    ext = extname(base).replace(/^\./, '')
  }

  return { ext, isSig }
}

export function renameArtifact(filename, version, fallbackArch = process.arch) {
  const { ext, isSig } = detectExtension(filename)
  if (!ext) return null
  const arch = normalizeArch(filename, fallbackArch)
  const target = `AgentRQ-Tauri-${version}-${arch}.${ext}`
  return isSig ? `${target}.sig` : target
}

function walkDir(dir) {
  const results = []
  if (!existsSync(dir)) return results
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const fullPath = join(dir, entry.name)
    if (entry.isDirectory()) {
      results.push(...walkDir(fullPath))
    } else if (entry.isFile()) {
      results.push(fullPath)
    }
  }
  return results
}

export function collectArtifacts({
  bundleDir = join(TAURI_ROOT, 'src-tauri', 'target', 'release', 'bundle'),
  outDir = join(REPO_ROOT, 'tauri-release'),
  version,
  fallbackArch = process.arch,
} = {}) {
  if (!version) {
    const tauriConf = JSON.parse(readFileSync(join(TAURI_ROOT, 'src-tauri', 'tauri.conf.json'), 'utf8'))
    version = tauriConf.version
  }

  mkdirSync(outDir, { recursive: true })
  const allFiles = walkDir(bundleDir)
  const collected = []

  // Collect non-sig files first
  const nonSigFiles = allFiles.filter((f) => !f.endsWith('.sig'))
  for (const file of nonSigFiles) {
    const base = basename(file)
    const renamed = renameArtifact(base, version, fallbackArch)
    if (!renamed) continue
    const dest = join(outDir, renamed)
    copyFileSync(file, dest)
    collected.push({ src: file, dest, name: renamed })

    // Check if a .sig file exists beside the original file
    const sigPath = `${file}.sig`
    if (existsSync(sigPath)) {
      const sigDest = `${dest}.sig`
      copyFileSync(sigPath, sigDest)
      collected.push({ src: sigPath, dest: sigDest, name: `${renamed}.sig` })
    }
  }

  // Also check for any standalone .sig files in bundleDir
  const sigFiles = allFiles.filter((f) => f.endsWith('.sig'))
  for (const file of sigFiles) {
    const base = basename(file)
    const renamed = renameArtifact(base, version, fallbackArch)
    if (!renamed) continue
    const dest = join(outDir, renamed)
    if (!existsSync(dest)) {
      copyFileSync(file, dest)
      collected.push({ src: file, dest, name: renamed })
    }
  }

  return collected
}

export function generateManifest({
  dir = join(REPO_ROOT, 'tauri-release'),
  version,
  tag,
  repo = process.env.GITHUB_REPOSITORY || 'agentrq/agentrq',
  notes = '',
  pubDate = new Date().toISOString(),
  existingManifest = null,
} = {}) {
  if (!version) {
    const tauriConf = JSON.parse(readFileSync(join(TAURI_ROOT, 'src-tauri', 'tauri.conf.json'), 'utf8'))
    version = tauriConf.version
  }
  if (!tag) {
    tag = `v${version}`
  }

  const platforms = existingManifest?.platforms ? { ...existingManifest.platforms } : {}
  const files = existsSync(dir) ? readdirSync(dir) : []
  const sigFiles = files.filter((f) => f.endsWith('.sig'))

  if (sigFiles.length === 0) {
    return null
  }

  const sigs = {}

  for (const sigFile of sigFiles) {
    const content = readFileSync(join(dir, sigFile), 'utf8').trim()
    const archiveName = sigFile.slice(0, -4)
    const lower = archiveName.toLowerCase()
    const url = `https://github.com/${repo}/releases/download/${tag}/${archiveName}`

    if (lower.includes('arm64') && (lower.endsWith('.app.tar.gz') || lower.endsWith('.tar.gz'))) {
      sigs['darwin-aarch64'] = { signature: content, url }
    } else if (lower.includes('x64') && (lower.endsWith('.app.tar.gz') || lower.endsWith('.tar.gz')) && lower.includes('darwin')) {
      sigs['darwin-x86_64'] = { signature: content, url }
    } else if (lower.endsWith('.appimage.tar.gz') || (lower.includes('x64') && lower.endsWith('.tar.gz') && !lower.includes('app.tar.gz'))) {
      sigs['linux-x86_64'] = { signature: content, url }
    } else if (lower.endsWith('.nsis.zip') || (lower.includes('x64') && lower.endsWith('.zip'))) {
      sigs['windows-x86_64'] = { signature: content, url }
    }
  }

  // Populate platforms
  if (sigs['darwin-aarch64']) {
    platforms['darwin-aarch64'] = sigs['darwin-aarch64']
  }
  if (sigs['darwin-x86_64']) {
    platforms['darwin-x86_64'] = sigs['darwin-x86_64']
  } else if (sigs['darwin-aarch64']) {
    // If only one macOS slice was built (e.g. runner arch), keep darwin-x86_64 keyed
    platforms['darwin-x86_64'] = sigs['darwin-aarch64']
  }
  if (sigs['linux-x86_64']) {
    platforms['linux-x86_64'] = sigs['linux-x86_64']
  }
  if (sigs['windows-x86_64']) {
    platforms['windows-x86_64'] = sigs['windows-x86_64']
  }

  return {
    version,
    notes: existingManifest?.notes || notes || `Release ${tag}`,
    pub_date: pubDate,
    platforms,
  }
}

export function run() {
  const args = process.argv.slice(2)
  const isManifestOnly = args.includes('--manifest-only')

  const versionArgIdx = args.indexOf('--version')
  const version = versionArgIdx !== -1 ? args[versionArgIdx + 1] : undefined

  const dirArgIdx = args.indexOf('--dir')
  const dir = dirArgIdx !== -1 ? resolve(args[dirArgIdx + 1]) : join(REPO_ROOT, 'tauri-release')

  const tagArgIdx = args.indexOf('--tag')
  const tag = tagArgIdx !== -1 ? args[tagArgIdx + 1] : process.env.TAG || (version ? `v${version}` : undefined)

  const repoArgIdx = args.indexOf('--repo')
  const repo = repoArgIdx !== -1 ? args[repoArgIdx + 1] : process.env.GITHUB_REPOSITORY || 'agentrq/agentrq'

  const hasSigningKey = Boolean(
    process.env.TAURI_SIGNING_PRIVATE_KEY ||
    process.env.TAURI_KEY ||
    process.env.TAURI_SIGNING_KEY
  )

  if (!isManifestOnly) {
    console.log(`Collecting Tauri release artifacts into ${dir}...`)
    const collected = collectArtifacts({ outDir: dir, version })
    console.log(`Collected ${collected.length} files:`)
    for (const c of collected) {
      console.log(`  ${c.name}`)
    }
  }

  if (!hasSigningKey) {
    console.log('::warning::No TAURI_SIGNING_PRIVATE_KEY configured; updater artifacts will not be signed. Skipping latest-tauri.json generation.')
    return
  }

  let existing = null
  const manifestPath = join(dir, 'latest-tauri.json')
  if (existsSync(manifestPath)) {
    try {
      existing = JSON.parse(readFileSync(manifestPath, 'utf8'))
    } catch {
      // ignore
    }
  }

  const manifest = generateManifest({ dir, version, tag, repo, existingManifest: existing })
  if (manifest && Object.keys(manifest.platforms).length > 0) {
    writeFileSync(manifestPath, JSON.stringify(manifest, null, 2) + '\n')
    console.log(`Generated ${manifestPath} with platforms: ${Object.keys(manifest.platforms).join(', ')}`)
  } else {
    console.log('::warning::No .sig files found; skipping latest-tauri.json generation.')
  }
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  run()
}
