import { defineConfig } from 'vitest/config'

export default defineConfig({
  test: {
    environment: 'node',
    include: ['test/**/*.test.js'],
    coverage: {
      provider: 'v8',
      include: ['src/main/**/*.js', 'src/version.js'],
      // The Electron entry point is app lifecycle wiring: it imports `electron`
      // at module scope and its behaviour is only observable with a real
      // Electron binary. Every rule it depends on lives in the modules beside
      // it, which are covered.
      //
      // fetch-source.js is the same category for the same reason: it is the
      // shim that copies a directory, shells out to git and unpacks an archive,
      // and none of that is observable without a real disk and a real git. Its
      // decisions — what a source is, whether a digest matches, what a failure
      // does — all live in source.js and install.js, which are covered, and the
      // two things in it that are judgement rather than plumbing (the clone
      // arguments, and hashing before anything is written) have tests of their
      // own in fetch-source.test.js.
      exclude: ['src/main/index.js', 'src/main/extensions/fetch-source.js'],
      // The example extensions in `examples/extensions/` are tested from this
      // suite (`test/examples/`) but are **not** in the numbers above: a
      // coverage include cannot reach outside the project root, and rooting
      // this config at the repository breaks the provider's own resolution.
      // Their tests are written to the same standard regardless; what is
      // missing is the gate, not the coverage.
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
