// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * Fail the build when a source file has lost its copyright notice.
 *
 * Run in CI on every pull request. The rules live in `src/copyright.js`, where
 * they are tested; this is the part that talks to git and the disk.
 *
 *   node scripts/copyright.mjs          # check, and name what is missing
 *   node scripts/copyright.mjs --fix    # add it where it is absent
 */
import { execFileSync } from 'node:child_process'
import { readFileSync, writeFileSync } from 'node:fs'
import { fileURLToPath, URL } from 'node:url'

import { NOTICE_LINES, carriesNotice, isOurs, withNotice } from '../src/copyright.js'

/** The repository, not this package: the notice belongs on every file in it. */
const REPO_ROOT = fileURLToPath(new URL('../../', import.meta.url))

const tracked = () =>
  execFileSync('git', ['ls-files'], { cwd: REPO_ROOT, encoding: 'utf8', maxBuffer: 32 * 1024 * 1024 })
    .split('\n')
    .filter(Boolean)
    .filter(isOurs)

const fix = process.argv.includes('--fix')
const files = tracked()
const missing = []

for (const path of files) {
  const full = fileURLToPath(new URL(path, `file://${REPO_ROOT}`))
  const contents = readFileSync(full, 'utf8')
  if (carriesNotice(contents, path)) continue

  if (fix) writeFileSync(full, withNotice(contents, path))
  else missing.push(path)
}

if (fix) {
  console.log(`✓ ${files.length} source files carry the notice`)
} else if (missing.length > 0) {
  console.error(`✗ ${missing.length} file${missing.length === 1 ? '' : 's'} without the copyright notice:\n`)
  for (const path of missing.slice(0, 40)) console.error(`    ${path}`)
  if (missing.length > 40) console.error(`    … and ${missing.length - 40} more`)
  // The whole block, because a file may be failing on the second line alone.
  console.error(`\nExpected, exactly:\n\n${NOTICE_LINES.map((line) => `    ${line}`).join('\n')}`)
  console.error(`\nRun: node desktop/scripts/copyright.mjs --fix`)
  process.exit(1)
} else {
  console.log(`✓ ${files.length} source files carry the copyright notice`)
}
