// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { defineConfig } from 'tsdown'

// `prepare` runs this after a git install, where the consumer has no project
// references and no type-check context. Keep the build self-contained: bundle
// `src/` to `lib/`, emit declarations, and leave every peer/runtime dependency
// external so the harness supplies its own copies.
export default defineConfig({
  entry: ['src/index.ts'],
  outDir: 'lib',
  format: ['esm'],
  platform: 'node',
  target: 'node20',
  dts: true,
  clean: true,
  // tsdown 0.23 turns this on by default for `platform: 'node'`, which renames
  // the output to index.mjs/index.d.mts. This package is `"type": "module"`, so
  // .js is already ESM — and `main`, `types` and `exports` all name .js. Letting
  // the extension change would publish a package whose entry points point at
  // files that are not there, which no test here would catch.
  fixedExtension: false,
  external: [/^@deepseek-ai\//, /^@modelcontextprotocol\//],
})
