import { ref } from 'vue'

/**
 * Desktop stand-in for `virtual:pwa-register/vue`.
 *
 * App.vue already has the right piece of UI for "a new version is available":
 * the banner it shows when a service worker is waiting, with an "Update now"
 * button and a dismiss. The desktop app has no service worker, but it has the
 * same thing to say — so rather than build a second banner that looks almost
 * the same, this presents the Electron updater through the interface App.vue
 * already consumes.
 *
 * The result is that the desktop update prompt is, by construction, pixel
 * identical to the web one, and App.vue needs no knowledge of either.
 *
 * The shape must match what App.vue destructures: a writable `needRefresh` ref
 * and an awaitable `updateServiceWorker`.
 *
 * With no bridge present — the frontend's own test run, say — everything stays
 * inert, exactly as the plain stub does.
 */
export function useRegisterSW() {
  const needRefresh = ref(false)
  const offlineReady = ref(false)

  const updates = globalThis.window?.agentrq?.updates
  if (updates) {
    updates.onStatus((state) => {
      // 'ready' means downloaded and waiting: the only state where restarting
      // achieves anything, and so the only one that raises the banner.
      needRefresh.value = state.status === 'ready'
    })
  }

  return {
    needRefresh,
    offlineReady,
    updateServiceWorker: async () => {
      // App.vue clears needRefresh and awaits this; on success the app is
      // replaced by the new version, so nothing after it runs.
      await updates?.installNow()
    },
  }
}
