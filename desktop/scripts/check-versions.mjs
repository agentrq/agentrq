// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Fail the build when the repository's versions have drifted apart.
 *
 * Run in CI before packaging. Optionally also checks the git tag being released,
 * via GITHUB_REF.
 *
 *   node scripts/check-versions.mjs
 */
import { readFile } from 'node:fs/promises'
import { fileURLToPath, URL } from 'node:url'

import { VERSION_SOURCES, compareVersions, readVersions, tagMatchesVersion } from '../src/version.js'

const repoRoot = (p) => fileURLToPath(new URL(`../../${p}`, import.meta.url))

const read = async (path) => {
  try {
    return await readFile(repoRoot(path), 'utf-8')
  } catch {
    return ''
  }
}

const names = Object.keys(VERSION_SOURCES)
const files = await Promise.all(names.map((name) => read(VERSION_SOURCES[name])))
const result = compareVersions(readVersions(Object.fromEntries(names.map((name, i) => [name, files[i]]))))

if (!result.ok) {
  console.error('✗ versions are out of step:')
  for (const problem of result.problems) console.error(`  - ${problem}`)
  process.exit(1)
}

const tag = tagMatchesVersion(process.env.GITHUB_REF, result.version)
if (!tag.ok) {
  console.error(`✗ ${tag.reason}`)
  process.exit(1)
}

console.log(`✓ ${Object.values(VERSION_SOURCES).join(', ')} are all at ${result.version}`)
if (process.env.GITHUB_REF?.startsWith('refs/tags/')) console.log(`✓ ${tag.reason}`)
