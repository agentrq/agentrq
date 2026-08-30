import { defineConfig } from 'vitest/config'

export default defineConfig({
  test: {
    environment: 'node',
    include: ['test/**/*.test.js'],
    coverage: {
      provider: 'v8',
      include: ['src/main/**/*.js'],
      // The Electron entry point is app lifecycle wiring: it imports `electron`
      // at module scope and its behaviour is only observable with a real
      // Electron binary. Every rule it depends on lives in the modules beside
      // it, which are covered.
      exclude: ['src/main/index.js'],
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
