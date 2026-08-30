import { ref } from 'vue'

/**
 * Inert stand-in for `virtual:pwa-register/vue`, which only exists when
 * vite-plugin-pwa is part of the build.
 *
 * Used by the test run, which has no PWA plugin. (The desktop build resolves
 * the same virtual module to `src/desktop/useDesktopUpdates.js`, which drives
 * App.vue's banner from the Electron updater instead.)
 *
 * The shape must match what App.vue destructures: a writable `needRefresh` ref
 * and an awaitable `updateServiceWorker`. Both are inert, so App.vue's
 * "new version" banner simply never appears in a test.
 */
export function useRegisterSW() {
  return {
    needRefresh: ref(false),
    offlineReady: ref(false),
    updateServiceWorker: async () => {},
  }
}
