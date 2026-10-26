// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Run the examples' tests on the Node.js the app runs extensions on, through
 * the extension host's bundle as it ships (vitest.sidecar.config.mjs).
 *
 *   node scripts/test-sidecar.mjs <node>
 *
 * `<node>` is the pinned binary: the one the app downloaded into its data
 * folder, or one unpacked from the same nodejs.org archive.
 */
import { spawnSync } from 'node:child_process'
import { existsSync } from 'node:fs'
import { resolve } from 'node:path'

const root = resolve(import.meta.dirname, '..')
const node = process.argv[2]
const bundle = resolve(root, 'dist/ext-host/agentrq-ext-host.mjs')
if (!node || !existsSync(node)) throw new Error('Name the Node.js binary the app downloaded.')
if (!existsSync(bundle)) throw new Error(`${bundle} is missing: run \`npm run build:ext-host\` first`)

const run = spawnSync(
  node,
  [resolve(root, 'node_modules/vitest/vitest.mjs'), 'run', '--config', resolve(root, 'vitest.sidecar.config.mjs')],
  { stdio: 'inherit', env: { ...process.env, AGENTRQ_EXT_HOST_ENTRY: bundle } },
)
process.exit(run.status ?? 1)
