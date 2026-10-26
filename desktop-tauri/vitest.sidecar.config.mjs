// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { resolve } from 'node:path'
import { defineConfig } from 'vitest/config'

// The examples against what ships: desktop's own example tests, and the five
// examples through the bundled extension host, both run by the Node.js the app
// downloads. `scripts/test-sidecar.mjs` names that Node and the bundle.
export default defineConfig({
  root: resolve(import.meta.dirname, '..'),
  test: {
    environment: 'node',
    include: ['desktop/test/examples/*.test.js', 'desktop-tauri/test/ext-host-examples.test.js'],
  },
})
