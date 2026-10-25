// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Collects one build leg's Tauri bundles as `AgentRQ-Tauri-<version>-<arch>.<ext>`, and
 * builds `latest-tauri.json` from the signatures of every leg.
 *
 *   collect-artifacts.mjs --platform macos --arch arm64 [--bundle-dir D] [--out D] [--signed]
 *   collect-artifacts.mjs --manifest --dir D --tag vX.Y.Z [--repo owner/name]
 */

import { copyFileSync, existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const TAURI_ROOT = join(__dirname, '..')
const REPO_ROOT = join(TAURI_ROOT, '..')

// Only the top level of each target dir: the rest of `bundle/` is the bundler's
// working files (the AppDir, the deb staging dir, the dmg scripts).
// `updater` marks the files Tauri v2 signs for the updater. `updaterOnly` ones exist only
// when updater artifacts are built, so an unsigned build neither has nor needs them.
const BUNDLES = {
  dmg: { dir: 'dmg', suffix: '.dmg' },
  'app.tar.gz': { dir: 'macos', suffix: '.app.tar.gz', updater: true, updaterOnly: true },
  AppImage: { dir: 'appimage', suffix: '.AppImage', updater: true },
  deb: { dir: 'deb', suffix: '.deb' },
  exe: { dir: 'nsis', suffix: '-setup.exe', updater: true },
}

export const PLATFORM_BUNDLES = {
  macos: ['dmg', 'app.tar.gz'],
  linux: ['AppImage', 'deb'],
  windows: ['exe'],
}

// Never alias one arch's archive to another's key: an Intel Mac offered the arm64
// app installs it and then cannot launch it.
const UPDATER_PLATFORMS = {
  'arm64.app.tar.gz': 'darwin-aarch64',
  'x64.app.tar.gz': 'darwin-x86_64',
  'x64.AppImage': 'linux-x86_64',
  'x64.exe': 'windows-x86_64',
}

function tauriVersion() {
  return JSON.parse(readFileSync(join(TAURI_ROOT, 'src-tauri', 'tauri.conf.json'), 'utf8')).version
}

function topLevelFiles(dir) {
  if (!existsSync(dir)) return []
  return readdirSync(dir, { withFileTypes: true })
    .filter((e) => e.isFile())
    .map((e) => e.name)
}

export function collectArtifacts({
  bundleDir = join(TAURI_ROOT, 'src-tauri', 'target', 'release', 'bundle'),
  outDir = join(REPO_ROOT, 'tauri-release'),
  version = tauriVersion(),
  platform,
  arch,
  signed = false,
}) {
  const kinds = PLATFORM_BUNDLES[platform]
  if (!kinds) throw new Error(`unknown platform "${platform}"`)
  if (arch !== 'arm64' && arch !== 'x64') throw new Error(`unknown arch "${arch}"`)

  const problems = []
  const copies = []
  for (const ext of kinds) {
    const { dir, suffix, updater, updaterOnly } = BUNDLES[ext]
    if (updaterOnly && !signed) continue
    const name = `AgentRQ-Tauri-${version}-${arch}.${ext}`
    const found = topLevelFiles(join(bundleDir, dir)).filter((f) => f.endsWith(suffix))
    if (found.length === 0) {
      problems.push(`no *${suffix} in bundle/${dir}`)
      continue
    }
    if (found.length > 1) {
      problems.push(`${found.join(', ')} would all be ${name}`)
      continue
    }
    const src = join(bundleDir, dir, found[0])
    copies.push([src, name])
    if (signed && updater) {
      if (existsSync(`${src}.sig`)) copies.push([`${src}.sig`, `${name}.sig`])
      else problems.push(`no signature beside bundle/${dir}/${found[0]}`)
    }
  }
  if (problems.length > 0) throw new Error(problems.join('; '))

  mkdirSync(outDir, { recursive: true })
  for (const [src, name] of copies) copyFileSync(src, join(outDir, name))
  return copies.map(([, name]) => name)
}

export function generateManifest({
  dir,
  tag,
  version = tag.replace(/^v/, ''),
  repo = 'agentrq/agentrq',
  pubDate = new Date().toISOString(),
}) {
  const prefix = `AgentRQ-Tauri-${version}-`
  const sigs = readdirSync(dir).filter((f) => f.endsWith('.sig')).sort()
  if (sigs.length === 0) throw new Error(`no .sig files in ${dir}`)

  const platforms = {}
  const unmatched = []
  for (const sig of sigs) {
    const archive = sig.slice(0, -'.sig'.length)
    const key = archive.startsWith(prefix) && UPDATER_PLATFORMS[archive.slice(prefix.length)]
    if (!key) {
      unmatched.push(sig)
      continue
    }
    platforms[key] = {
      signature: readFileSync(join(dir, sig), 'utf8').trim(),
      url: `https://github.com/${repo}/releases/download/${tag}/${archive}`,
    }
  }
  if (unmatched.length > 0) {
    throw new Error(`signatures matching no updater platform for ${version}: ${unmatched.join(', ')}`)
  }

  const sorted = Object.fromEntries(Object.entries(platforms).sort(([a], [b]) => a.localeCompare(b)))
  return { version, notes: `Release ${tag}`, pub_date: pubDate, platforms: sorted }
}

function option(args, name) {
  const i = args.indexOf(name)
  return i === -1 ? undefined : args[i + 1]
}

export function run(args = process.argv.slice(2), { env = process.env, log = console.log } = {}) {
  try {
    if (args.includes('--manifest')) {
      const dir = resolve(option(args, '--dir') ?? '.')
      const tag = option(args, '--tag')
      if (!tag) throw new Error('--manifest needs --tag')
      const manifest = generateManifest({
        dir,
        tag,
        repo: option(args, '--repo') ?? env.GITHUB_REPOSITORY,
      })
      const path = join(dir, 'latest-tauri.json')
      writeFileSync(path, JSON.stringify(manifest, null, 2) + '\n')
      log(`Wrote ${path} for ${Object.keys(manifest.platforms).join(', ')}`)
      return 0
    }

    const bundleDir = option(args, '--bundle-dir')
    const outDir = option(args, '--out')
    const names = collectArtifacts({
      bundleDir: bundleDir && resolve(bundleDir),
      outDir: outDir && resolve(outDir),
      version: option(args, '--version'),
      platform: option(args, '--platform'),
      arch: option(args, '--arch'),
      signed: args.includes('--signed'),
    })
    log(`Collected ${names.length} files:`)
    for (const name of names) log(`  ${name}`)
    return 0
  } catch (err) {
    log(`::error::${err.message}`)
    return 1
  }
}

if (process.argv[1] && fileURLToPath(import.meta.url) === resolve(process.argv[1])) {
  process.exitCode = run()
}
