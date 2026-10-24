// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Keeping the versions in this repository in step.
 *
 * The desktop app, the web frontend, the Go backend and the Tauri desktop app
 * (whose version is written in four files) each carry their own version, and
 * they are meant to be the same number. That matters more for the
 * desktop app than for the others: its version is what electron-updater
 * compares against a GitHub Release to decide whether an update exists. A
 * desktop build stamped lower than the release it came from would offer to
 * update to itself, forever.
 *
 * Pure functions taking file contents rather than paths, so the rules are
 * testable without a filesystem.
 */

/** Where each version lives, relative to the repository root. */
export const VERSION_SOURCES = {
  desktop: 'desktop/package.json',
  frontend: 'frontend/package.json',
  backend: 'backend/internal/service/config/config.go',
  tauri: 'desktop-tauri/package.json',
  tauriConf: 'desktop-tauri/src-tauri/tauri.conf.json',
  tauriCargo: 'desktop-tauri/src-tauri/Cargo.toml',
  tauriCore: 'desktop-tauri/core/Cargo.toml',
}

/** How each source is read: a package.json-like JSON file, Go, or a Cargo.toml. */
export const VERSION_FORMATS = {
  desktop: 'json',
  frontend: 'json',
  backend: 'go',
  tauri: 'json',
  tauriConf: 'json',
  tauriCargo: 'cargo',
  tauriCore: 'cargo',
}

/**
 * Pull the version out of a package.json's contents.
 *
 * @returns {string|null} null when the file is unreadable or has no version.
 */
export function parsePackageVersion(contents) {
  try {
    const version = JSON.parse(contents)?.version
    return typeof version === 'string' && version !== '' ? version : null
  } catch {
    return null
  }
}

/**
 * Pull the version out of the Go config source.
 *
 * It is a constant rather than anything machine-readable, so this reads the
 * declaration directly. The leading `v` is dropped: everything else in the
 * repository writes the bare number, and comparing `v0.4.8` with `0.4.8` would
 * fail for no reason.
 */
export function parseGoVersion(contents) {
  const match = /_appVersion\s*=\s*"v?([^"]+)"/.exec(String(contents ?? ''))
  return match ? match[1] : null
}

/**
 * Pull the package's own version out of a Cargo.toml.
 *
 * Only the `[package]` table counts: a dependency's `version = "…"` sits in
 * another table, and reading that would compare against somebody else's number.
 */
export function parseCargoVersion(contents) {
  let inPackage = false
  for (const raw of String(contents ?? '').split('\n')) {
    const line = raw.trim()
    if (line.startsWith('[')) inPackage = line === '[package]'
    else if (inPackage) {
      const match = /^version\s*=\s*"([^"]+)"/.exec(line)
      if (match) return match[1]
    }
  }
  return null
}

const PARSERS = { json: parsePackageVersion, go: parseGoVersion, cargo: parseCargoVersion }

/** Every version, read from each source's contents (keyed as `VERSION_SOURCES`). */
export function readVersions(contents) {
  return Object.fromEntries(
    Object.keys(VERSION_SOURCES).map((name) => [name, PARSERS[VERSION_FORMATS[name]](contents[name])])
  )
}

/**
 * Compare the versions.
 *
 * @param {Record<string, string|null>} versions  keyed as `VERSION_SOURCES`
 * @returns {{ok: boolean, version: string|null, problems: string[]}}
 */
export function compareVersions(versions) {
  const problems = []

  for (const [name, version] of Object.entries(versions)) {
    if (!version) problems.push(`could not read the version from ${VERSION_SOURCES[name] ?? name}`)
  }
  if (problems.length > 0) return { ok: false, version: null, problems }

  // The desktop version is the reference: it is the one electron-updater
  // compares against a release, so it is the one that must be right.
  const expected = versions.desktop
  for (const [name, version] of Object.entries(versions)) {
    if (version !== expected) {
      problems.push(`${VERSION_SOURCES[name] ?? name} is ${version}, but desktop is ${expected}`)
    }
  }

  return { ok: problems.length === 0, version: expected, problems }
}

/**
 * Check that a git tag matches the version being built.
 *
 * Publishing `v0.5.0` from a tree stamped 0.4.8 would put artifacts named for
 * one version inside a release named for another, and electron-updater would
 * then hand users an "update" to the build they already have.
 *
 * @param {string} ref  a git ref such as `refs/tags/v0.5.0`, or a bare tag
 */
export function tagMatchesVersion(ref, version) {
  const raw = String(ref ?? '')
  if (!raw) return { ok: true, reason: 'no tag to check' }

  // GITHUB_REF is set on every build, not only releases: a pull request gets
  // refs/pull/N/merge and a branch push gets refs/heads/<name>. Neither is a
  // tag, so there is nothing here to disagree with the version.
  if (raw.startsWith('refs/') && !raw.startsWith('refs/tags/')) {
    return { ok: true, reason: 'not a tag build' }
  }

  const tag = raw.replace(/^refs\/tags\//, '')
  const tagged = tag.replace(/^v/, '')
  if (tagged !== version) {
    return { ok: false, reason: `tag ${tag} does not match version ${version}` }
  }
  return { ok: true, reason: `tag ${tag} matches version ${version}` }
}
