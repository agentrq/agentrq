// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Run every verify script, one after the other, and exit non-zero if any of
 * them failed. A failure in the first does not hide the results of the rest.
 *
 * The scratch backend is built once here and handed to each script, so a cold
 * Go cache is paid for once.
 *
 *   npm run verify
 */
import { spawn } from 'node:child_process'
import { mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import { buildBackend } from './lib/backend.mjs'

const SCRIPTS = [
  'verify-e2e.mjs',
  'verify-connection.mjs',
  'verify-markdown-links.mjs',
  'verify-side-panel.mjs',
  'verify-webmcp.mjs',
  'verify-ui-telemetry.mjs',
]

const run = (script, env) =>
  new Promise((resolveRun) => {
    const child = spawn(process.execPath, [join(import.meta.dirname, script)], { env, stdio: 'inherit' })
    child.once('error', () => resolveRun(1))
    child.once('exit', (code) => resolveRun(code ?? 1))
  })

const env = { ...process.env }
let dir
if (!env.AGENTRQ_SERVER_URL && !env.VERIFY_SERVER_BIN) {
  dir = await mkdtemp(join(tmpdir(), 'agentrq-verify-build-'))
  try {
    env.VERIFY_SERVER_BIN = await buildBackend(dir)
  } catch (err) {
    console.error('✗', err.message)
    await rm(dir, { recursive: true, force: true })
    process.exit(1)
  }
}

const failed = []
for (const script of SCRIPTS) {
  if ((await run(script, env)) !== 0) failed.push(script)
}
if (dir) await rm(dir, { recursive: true, force: true })

console.log(failed.length ? `\n✗ failed: ${failed.join(', ')}` : '\n✓ every verify script passed')
process.exit(failed.length ? 1 : 0)
