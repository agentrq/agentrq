import { ref } from 'vue'

/**
 * Stand-in for `virtual:pwa-register/vue`, which only exists when
 * vite-plugin-pwa is part of the build.
 *
 * Two builds need it: the desktop renderer, which deliberately ships no service
 * worker (its assets are already local, and updates come through
 * electron-updater), and the test run, which has no PWA plugin either. App.vue
 * imports the virtual module directly, so without this stand-in neither build
 * would resolve.
 *
 * The shape must match what App.vue destructures: a writable `needRefresh` ref
 * and an awaitable `updateServiceWorker`. Both are inert, so App.vue's
 * "new version — reload" banner simply never appears.
 */
export function useRegisterSW() {
  return {
    needRefresh: ref(false),
    offlineReady: ref(false),
    updateServiceWorker: async () => {},
  }
}
