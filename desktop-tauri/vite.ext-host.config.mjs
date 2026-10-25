// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// The extension host, as one file for the bundled Node to run: its own entry
// and desktop/src/main/extensions, which import nothing but Node's built-ins.
import { defineConfig } from 'vite'
import { fileURLToPath, URL } from 'node:url'

const resolvePath = (p) => fileURLToPath(new URL(p, import.meta.url))

export default defineConfig({
  build: {
    ssr: resolvePath('./src/ext-host/main.js'),
    outDir: resolvePath('./dist/ext-host'),
    emptyOutDir: true,
    target: 'node24',
    minify: false,
    rollupOptions: {
      output: { entryFileNames: 'agentrq-ext-host.mjs', format: 'es' },
    },
  },
  ssr: { target: 'node', noExternal: true },
})
