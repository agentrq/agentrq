// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { defineConfig } from '@playwright/test'
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { apiPort, rootToken, webPort } from './lib/settings.js'

// The suite runs its own backend and frontend, on ports of their own, so it
// never talks to (or kills) the dev server somebody is already using on
// 3000/5173. Each run starts from an empty SQLite database in a temp folder.
const repo = resolve(import.meta.dirname, '..')
const dataDir = process.env.QA_DATA_DIR || mkdtempSync(join(tmpdir(), 'agentrq-qa-'))

// A host without Chromium's system libraries can point at a folder holding
// them. It is given to the browser alone: set for the whole run, it would
// shadow the libraries Go and Node load too.
// The servers' own logs, shown only when asked for: the backend logs every
// request, which buries the test report.
const serverLogs = process.env.QA_SERVER_LOGS ? 'pipe' : 'ignore'

const browserEnv = process.env.QA_BROWSER_LIBRARY_PATH
  ? { ...process.env, LD_LIBRARY_PATH: process.env.QA_BROWSER_LIBRARY_PATH }
  : undefined

export default defineConfig({
  testDir: './tests',
  // The server limits how fast one user may create workspaces and tasks, and
  // every test signs in as the same root user, so the tests take turns.
  workers: 1,
  fullyParallel: false,
  timeout: 120_000,
  expect: { timeout: 15_000 },
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: `http://localhost:${webPort}`,
    viewport: { width: 1400, height: 900 },
    // The app marks elements for tests with data-test, as its own unit tests do.
    testIdAttribute: 'data-test',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    launchOptions: browserEnv ? { env: browserEnv } : {},
  },
  webServer: [
    {
      name: 'backend',
      // The server reads its config relative to its working directory.
      command: 'go run .',
      cwd: join(repo, 'backend/cmd/server'),
      url: `http://localhost:${apiPort}/api/v1/auth/user`,
      // Unauthenticated, /auth/user answers 401, which Playwright counts as up.
      reuseExistingServer: !process.env.CI,
      timeout: 300_000,
      stdout: 'ignore',
      stderr: serverLogs,
      env: {
        ...process.env,
        PORT: String(apiPort),
        AGENTRQ_AUTH_ROOT_LOGIN_ENABLED: 'true',
        AGENTRQ_AUTH_ROOT_ACCESS_TOKEN: rootToken,
        // The server refuses to start without one; this one signs only the
        // run's own throwaway sessions.
        AGENTRQ_AUTH_JWT_SECRET: process.env.AGENTRQ_AUTH_JWT_SECRET || 'agentrq-qa-jwt-secret',
        AGENTRQ_SQLITE_DSN: join(dataDir, 'agentrq.db'),
        AGENTRQ_STORAGE_DIR: dataDir,
        AGENTRQ_RATELIMIT_ENABLED: 'false',
      },
    },
    {
      name: 'frontend',
      command: `npx vite --port ${webPort} --strictPort`,
      cwd: join(repo, 'frontend'),
      url: `http://localhost:${webPort}`,
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
      stdout: 'ignore',
      stderr: serverLogs,
      env: { ...process.env, AGENTRQ_DEV_API_TARGET: `http://localhost:${apiPort}` },
    },
  ],
})
