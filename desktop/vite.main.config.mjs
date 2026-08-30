import { defineConfig } from 'vite'
import { builtinModules } from 'node:module'
import { fileURLToPath, URL } from 'node:url'

const resolvePath = (p) => fileURLToPath(new URL(p, import.meta.url))

export default defineConfig({
  build: {
    outDir: resolvePath('./dist/main'),
    emptyOutDir: true,
    target: 'node20',
    minify: false,
    lib: {
      entry: resolvePath('./src/main/index.js'),
      formats: ['es'],
      fileName: () => 'index.js',
    },
    rollupOptions: {
      // Electron and Node builtins are provided by the runtime; electron-updater
      // is a real dependency resolved from node_modules at runtime.
      external: [
        'electron',
        'electron-updater',
        ...builtinModules,
        ...builtinModules.map((m) => `node:${m}`),
      ],
    },
  },
})
