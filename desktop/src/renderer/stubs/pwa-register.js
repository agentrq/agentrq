import { ref } from 'vue'

/**
 * Stand-in for `virtual:pwa-register/vue`, which only exists when
 * vite-plugin-pwa is in the build.
 *
 * The desktop app deliberately ships no service worker: its assets are already
 * local, and updates arrive through electron-updater rather than a waiting
 * worker. App.vue's "new version — reload" banner is therefore permanently
 * dormant here, and phase 6 (task 0huaAvwzIuH) puts the real update prompt in
 * its place.
 *
 * The shape must match what App.vue destructures: a writable `needRefresh` ref
 * and an awaitable `updateServiceWorker`.
 */
export function useRegisterSW() {
  return {
    needRefresh: ref(false),
    offlineReady: ref(false),
    updateServiceWorker: async () => {},
  }
}
