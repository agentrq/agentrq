// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { defineConfig } from 'vite'
import { builtinModules } from 'node:module'
import { fileURLToPath, URL } from 'node:url'

const resolvePath = (p) => fileURLToPath(new URL(p, import.meta.url))

export default defineConfig({
  build: {
    outDir: resolvePath('./dist/preload'),
    emptyOutDir: true,
    target: 'node20',
    minify: false,
    lib: {
      // The app's own preload, and the one every side panel guest gets.
      entry: {
        index: resolvePath('./src/preload/index.js'),
        panel: resolvePath('./src/preload/panel.js'),
      },
      // Sandboxed preload scripts are loaded as CommonJS — an ES module preload
      // simply will not run with `sandbox: true`.
      formats: ['cjs'],
      fileName: (_format, entryName) => `${entryName}.cjs`,
    },
    rollupOptions: {
      external: ['electron', ...builtinModules, ...builtinModules.map((m) => `node:${m}`)],
    },
  },
})
