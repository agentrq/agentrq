// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * `agentrq-ext-host`: the Node process the Tauri shell runs extensions in.
 *
 * Wiring only, and excluded from coverage like the renderer's entry; the
 * shell's tests and `test/ext-host-examples.test.js` run it as a process.
 *
 *     node agentrq-ext-host.mjs <data dir> <app version> [<to disable, as JSON>]
 *
 * stdout is the wire to the shell, so nothing else may write to it: an
 * extension's `console.log` goes to stderr, which the shell logs.
 */

import { spawn } from 'node:child_process'
import * as fs from 'node:fs/promises'
import { join } from 'node:path'
import { pathToFileURL } from 'node:url'

import { createPeer } from './rpc.js'
import { createExtensionHost } from './runtime.js'

const wire = process.stdout.write.bind(process.stdout)
process.stdout.write = process.stderr.write.bind(process.stderr)
console.log = console.error
console.info = console.error

// Electron's main process logs an uncaught throw and carries on; so does this.
process.on('uncaughtException', (error) => console.error('[ext-host] uncaught:', error))
process.on('unhandledRejection', (error) => console.error('[ext-host] unhandled rejection:', error))

const [dataDir, appVersion, disabled = '[]'] = process.argv.slice(2)

const jsonFile = (path) => ({
  read: async () => JSON.parse(await fs.readFile(path, 'utf8')),
  write: async (value) => {
    await fs.mkdir(join(path, '..'), { recursive: true })
    await fs.writeFile(path, JSON.stringify(value, null, 2), 'utf8')
  },
})

// Filled in once the host exists: it needs the peer to be built.
const handlers = {}
const notifications = {}
const peer = createPeer({
  write: (line) => wire(line),
  handlers,
  notifications,
  onStray: (line) => console.error('[ext-host] not a message:', line.slice(0, 200)),
})

const host = createExtensionHost({
  rpc: peer,
  dataDir,
  appVersion,
  // `file://` because an absolute Windows path is not an import specifier.
  load: (dir) => import(/* @vite-ignore */ pathToFileURL(join(dir, 'index.js')).href),
  jsonFile,
  fs,
  spawn,
  join,
})
Object.assign(handlers, host.handlers)
Object.assign(notifications, host.notifications)

process.stdin.setEncoding('utf8')
process.stdin.on('data', (chunk) => peer.receive(chunk))
// The shell is gone, or asked this to stop: unload, and leave.
process.stdin.on('end', () => {
  peer.close()
  host.stop()
  process.exit(0)
})

peer.notify('ready', { pid: process.pid })
host
  .start(JSON.parse(disabled))
  .then(() => peer.notify('started', {}))
  .catch((error) => console.error('[ext-host] did not start:', error))
