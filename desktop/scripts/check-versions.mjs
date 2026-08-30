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

import {
  VERSION_SOURCES,
  compareVersions,
  parseGoVersion,
  parsePackageVersion,
  tagMatchesVersion,
} from '../src/version.js'

const repoRoot = (p) => fileURLToPath(new URL(`../../${p}`, import.meta.url))

const read = async (path) => {
  try {
    return await readFile(repoRoot(path), 'utf-8')
  } catch {
    return ''
  }
}

const [desktop, frontend, backend] = await Promise.all([
  read(VERSION_SOURCES.desktop),
  read(VERSION_SOURCES.frontend),
  read(VERSION_SOURCES.backend),
])

const result = compareVersions({
  desktop: parsePackageVersion(desktop),
  frontend: parsePackageVersion(frontend),
  backend: parseGoVersion(backend),
})

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

console.log(`✓ desktop, frontend and backend are all at ${result.version}`)
if (process.env.GITHUB_REF?.startsWith('refs/tags/')) console.log(`✓ ${tag.reason}`)
