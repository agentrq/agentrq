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
      include: ['src/**/*.js'],
      // The renderer entry is imports, and one registration; what they load is
      // covered where it lives. The guest's init script is no module: its text
      // is run, placeholder filled, by test/panel-init.test.js.
      // The extension host's entry is wiring, run as a process by
      // test/ext-host-examples.test.js and by the shell's tests.
      exclude: ['src/renderer/main.js', 'src/renderer/panel-guest.js', 'src/guest/panel.js', 'src/ext-host/main.js'],
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
