import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath, URL } from 'node:url'

const resolvePath = (p) => fileURLToPath(new URL(p, import.meta.url))

const FRONTEND_SRC = resolvePath('../frontend/src')
const FRONTEND_ROOT = resolvePath('../frontend')

const { version } = JSON.parse(
  await import('node:fs/promises').then((fs) => fs.readFile(resolvePath('./package.json'), 'utf-8'))
)

export default defineConfig({
  root: resolvePath('./src/renderer'),
  // Assets are served from the root of the app:// origin.
  base: '/',
  publicDir: resolvePath('../frontend/public'),
  define: {
    __APP_VERSION__: JSON.stringify(version),
  },
  resolve: {
    alias: {
      // The whole point of the desktop package: the renderer is built from the
      // frontend's own sources, not a copy of them.
      '@app': FRONTEND_SRC,
      // vite-plugin-pwa is not in this build, so its virtual module needs a
      // stand-in — see the stub for why the desktop ships no service worker.
      'virtual:pwa-register/vue': resolvePath('./src/renderer/stubs/pwa-register.js'),
    },
    // Frontend sources resolve their imports from frontend/node_modules while
    // this config lives in desktop/. Without dedupe a second copy of Vue can be
    // pulled in, and two Vue runtimes break provide/inject and every store.
    dedupe: ['vue', 'vue-router', 'pinia'],
  },
  plugins: [vue(), tailwindcss()],
  optimizeDeps: {
    exclude: ['@huggingface/transformers'],
  },
  build: {
    outDir: resolvePath('./dist/renderer'),
    emptyOutDir: true,
    // Chromium is the only target, so there is no reason to down-level.
    target: 'chrome124',
  },
  server: {
    port: 5174,
    strictPort: true,
    // The renderer is fetched through the app:// protocol handler in dev, so
    // the dev server is talking to the Electron main process rather than to a
    // browser tab on the same origin.
    cors: true,
    fs: {
      // Vite's default allowlist is rooted at `root`; the frontend sources and
      // its node_modules sit outside it.
      allow: [resolvePath('./'), FRONTEND_ROOT],
    },
  },
})
