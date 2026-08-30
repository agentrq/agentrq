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
      entry: resolvePath('./src/preload/index.js'),
      // Sandboxed preload scripts are loaded as CommonJS — an ES module preload
      // simply will not run with `sandbox: true`.
      formats: ['cjs'],
      fileName: () => 'index.cjs',
    },
    rollupOptions: {
      external: ['electron', ...builtinModules, ...builtinModules.map((m) => `node:${m}`)],
    },
  },
})
