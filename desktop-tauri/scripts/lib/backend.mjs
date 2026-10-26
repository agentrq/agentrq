// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The scratch backend the verify scripts run against, started the way `qa/`
 * starts it: built from `backend/` (read only), on an empty SQLite database in
 * a temp folder, with root login enabled and a throwaway JWT secret.
 *
 * `npm run verify` builds the server once and hands it to each script as
 * VERIFY_SERVER_BIN; a script run on its own builds it itself.
 *
 * Set AGENTRQ_SERVER_URL (and AGENTRQ_ROOT_TOKEN) to run against a server you
 * started yourself, as the Electron scripts do. It is then left alone.
 */
import { spawn } from 'node:child_process'
import { mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'

import { freePort, waitFor } from './webdriver.mjs'

const REPO = resolve(import.meta.dirname, '../../..')
const SERVER_DIR = join(REPO, 'backend/cmd/server')
const EXE = process.platform === 'win32' ? 'agentrq-server.exe' : 'agentrq-server'

/** The root login token of a scratch backend. */
export const ROOT_TOKEN = 'agentrq-verify'

/**
 * Build the server into `dir`: `backend/` stays as it was found. Asynchronous,
 * so a run's deadline can still fire during a cold build.
 */
export function buildBackend(dir) {
  const exe = join(dir, EXE)
  return new Promise((resolveBuild, reject) => {
    const build = spawn('go', ['build', '-o', exe, '.'], { cwd: SERVER_DIR, stdio: 'inherit' })
    build.once('error', reject)
    build.once('exit', (code) =>
      code === 0 ? resolveBuild(exe) : reject(new Error('go build of the backend failed'))
    )
  })
}

/**
 * @returns {Promise<{url: string, rootToken: string, stop: () => Promise<void>}>}
 */
export async function startBackend({ env = process.env } = {}) {
  if (env.AGENTRQ_SERVER_URL) {
    return {
      url: env.AGENTRQ_SERVER_URL,
      rootToken: env.AGENTRQ_ROOT_TOKEN ?? ROOT_TOKEN,
      stop: async () => {},
    }
  }

  const dir = await mkdtemp(join(tmpdir(), 'agentrq-verify-backend-'))
  const exe =
    env.VERIFY_SERVER_BIN ??
    (await buildBackend(dir).catch(async (err) => {
      await rm(dir, { recursive: true, force: true })
      throw err
    }))

  const port = await freePort()
  const url = `http://localhost:${port}`
  const child = spawn(exe, [], {
    // The server reads its config relative to its working directory.
    cwd: SERVER_DIR,
    stdio: ['ignore', 'ignore', env.VERIFY_SERVER_LOGS ? 'inherit' : 'ignore'],
    env: {
      ...env,
      PORT: String(port),
      AGENTRQ_AUTH_ROOT_LOGIN_ENABLED: 'true',
      AGENTRQ_AUTH_ROOT_ACCESS_TOKEN: ROOT_TOKEN,
      AGENTRQ_AUTH_JWT_SECRET: 'agentrq-verify-jwt-secret',
      AGENTRQ_SQLITE_DSN: join(dir, 'agentrq.db'),
      AGENTRQ_STORAGE_DIR: dir,
      AGENTRQ_RATELIMIT_ENABLED: 'false',
      AGENTRQ_BASE_URL: url,
    },
  })
  let exited = null
  child.once('error', (err) => {
    exited = err
  })
  child.once('exit', (code) => {
    exited = new Error(`the backend exited with ${code}`)
  })
  const stop = async () => {
    child.kill()
    await rm(dir, { recursive: true, force: true }).catch(() => {})
  }

  // Unauthenticated, /auth/user answers 401: the server is up.
  await waitFor(
    'the backend to answer',
    async () => {
      if (exited) throw exited
      return (await fetch(`${url}/api/v1/auth/user`, { signal: AbortSignal.timeout(5000) })).status === 401
    },
    { timeout: 60000 }
  ).catch(async (err) => {
    await stop()
    throw err
  })
  return { url, rootToken: ROOT_TOKEN, stop }
}
