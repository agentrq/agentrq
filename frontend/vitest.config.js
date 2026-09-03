import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

const resolvePath = (p) => fileURLToPath(new URL(p, import.meta.url))

const { version } = JSON.parse(
  await import('node:fs/promises').then((fs) => fs.readFile(resolvePath('./package.json'), 'utf-8'))
)

export default defineConfig({
  // Deliberately not the app's vite.config.js: that one carries the PWA plugin,
  // whose service-worker generation has no place in a test run.
  plugins: [vue()],
  define: {
    __APP_VERSION__: JSON.stringify(version),
  },
  resolve: {
    alias: {
      // App.vue imports this virtual module, which vite-plugin-pwa would
      // normally provide.
      'virtual:pwa-register/vue': resolvePath('./stubs/pwa-register.js'),
    },
  },
  test: {
    // api.js reads `window` at module scope, and the click-outside directive
    // works on `document` — both need a DOM even for non-component tests.
    environment: 'jsdom',
    include: ['test/**/*.test.js'],
    // Styles are irrelevant to behaviour here, and processing them would drag
    // the whole Tailwind pipeline into every run.
    css: false,
    coverage: {
      provider: 'v8',
      include: [
        'src/app.js',
        'src/stores/platformStore.js',
        'src/desktop/*.js',
        'src/composables/useChatScroll.js',
        'src/composables/useTaskGroups.js',
        'src/composables/useAgentTelemetry.js',
        'src/composables/usePendingSend.js',
        'src/composables/useDirectoryPicker.js',
        'src/composables/useProfileDisplay.js',
        'src/composables/useAuthedFetch.js',
      ],
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
