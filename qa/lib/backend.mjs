// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// The run's backend, kept by a small supervisor that can restart it: a test
// sends this process SIGUSR2, and the server is stopped and started again on
// the same port, database and secrets, as a deploy would. Playwright starts
// this in the server's folder and stops it, and the server with it, at the
// end of the run.

import { execFileSync, spawn } from 'node:child_process'
import { writeFileSync } from 'node:fs'
import { join } from 'node:path'

const dataDir = process.env.QA_DATA_DIR

// The server itself is the child, not a `go run` in front of it, so a
// signal reaches it. QA_BACKEND_CMD (a prebuilt server) is exec'd for the
// same reason.
let command
let args
let shell = false
if (process.env.QA_BACKEND_CMD) {
  command = `exec ${process.env.QA_BACKEND_CMD}`
  args = []
  shell = true
} else {
  command = join(dataDir, 'agentrq-qa-server')
  args = []
  execFileSync('go', ['build', '-o', command, '.'], { stdio: 'inherit' })
}

let child
let restarting = false

function start() {
  child = spawn(command, args, { shell, stdio: 'inherit' })
  child.on('exit', (code) => {
    if (!restarting) process.exit(code ?? 1)
  })
  // Read by restartBackend (lib/mcp.js): the restart is over once this
  // changes and the server answers again.
  writeFileSync(join(dataDir, 'backend.started'), `${process.pid} ${Date.now()}`)
}

function stop() {
  return new Promise((resolve) => {
    if (child.exitCode !== null || child.signalCode !== null) return resolve()
    child.once('exit', resolve)
    child.kill('SIGTERM')
  })
}

process.on('SIGUSR2', async () => {
  restarting = true
  await stop()
  restarting = false
  start()
})
for (const signal of ['SIGTERM', 'SIGINT']) {
  process.on(signal, async () => {
    restarting = true
    await stop()
    process.exit(0)
  })
}

start()
