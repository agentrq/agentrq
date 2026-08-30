import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath, URL } from 'node:url'

const resolvePath = (p) => fileURLToPath(new URL(p, import.meta.url))

const FRONTEND_SRC = resolvePath('../frontend/src')
const FRONTEND_ROOT = resolvePath('../frontend')
const FRONTEND_NODE_MODULES = resolvePath('../frontend/node_modules')

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
    alias: [
      // The whole point of the desktop package: the renderer is built from the
      // frontend's own sources, not a copy of them.
      { find: '@app', replacement: FRONTEND_SRC },
      // The desktop ships no service worker, so vite-plugin-pwa's virtual
      // module needs a stand-in. Rather than an inert one, it resolves to the
      // Electron updater bridge: App.vue's existing "new version available"
      // banner is exactly the prompt an app update needs, so the desktop drives
      // that instead of growing a second banner beside it.
      { find: 'virtual:pwa-register/vue', replacement: resolvePath('../frontend/src/desktop/useDesktopUpdates.js') },
      // The shared runtime packages come from frontend/node_modules. This
      // package deliberately installs no copy of its own: a file under
      // desktop/src could not otherwise resolve `vue-router`, and two copies of
      // Vue in one bundle break provide/inject and every store.
      { find: /^(vue|vue-router|pinia)$/, replacement: `${FRONTEND_NODE_MODULES}/$1` },
    ],
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
