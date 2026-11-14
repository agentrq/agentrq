// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Read by the Playwright config, which starts the servers, and by the tests,
// which sign in to them, so both always agree.
export const apiPort = Number(process.env.QA_API_PORT || 3911)
export const webPort = Number(process.env.QA_WEB_PORT || 5912)
export const rootToken = process.env.QA_ROOT_TOKEN || 'agentrq-qa'
// The run's database and files. Set once, by the config, so the test workers
// it starts read the same folder.
export const dataDir = process.env.QA_DATA_DIR || ''
// What the run's server signs sessions with.
export const jwtSecret = process.env.AGENTRQ_AUTH_JWT_SECRET || 'agentrq-qa-jwt-secret'
