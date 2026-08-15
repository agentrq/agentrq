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
  external: [/^@deepseek-ai\//, /^@modelcontextprotocol\//],
})
