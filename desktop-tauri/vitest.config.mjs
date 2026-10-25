// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { defineConfig } from 'vitest/config'

export default defineConfig({
  test: {
    environment: 'node',
    include: ['test/**/*.test.js'],
    coverage: {
      provider: 'v8',
      // The verify scripts drive a built app (WP10) and are run, not unit-tested.
      include: ['src/**/*.js', 'scripts/collect-artifacts.mjs', 'scripts/generate-icons.mjs'],
      // The renderer entry is imports, and one registration; what they load is
      // covered where it lives. The guest's init script is no module: its text
      // is run, placeholder filled, by test/panel-init.test.js.
      exclude: ['src/renderer/main.js', 'src/renderer/panel-guest.js', 'src/guest/panel.js'],
      reporter: ['text', 'lcov'],
      thresholds: {
        lines: 100,
        functions: 100,
        branches: 100,
        statements: 100,
      },
    },
  },
})
