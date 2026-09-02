/**
 * Desktop renderer bootstrap.
 *
 * Two things can be mounted here. Until a server has been chosen there is
 * nothing for the application to talk to, so the connection screen goes up
 * instead; once one is stored, the frontend's own `createAgentRQApp` builds the
 * real app — the identical component tree and route table the browser gets.
 *
 * The connection screen is deliberately a desktop-only view rather than a route
 * in the shared table: the browser build is served *by* a server and can never
 * need it, and adding it to the shared routes would put a dead route in the web
 * app.
 *
 * `createWebHistory` with no base is correct here: the renderer is served from
 * the root of the app:// origin, and the protocol handler does the same SPA
 * fallback the Go backend does, so ordinary history routing works.
 *
 * No service worker is registered. The desktop app's assets are already local,
 * and updates arrive through electron-updater (phase 6, task 0huaAvwzIuH)
 * rather than a waiting worker.
 */
import { createApp, watch } from 'vue'
import { createWebHistory } from 'vue-router'

import { createAgentRQApp } from '@app/app'
import { useThemeStore } from '@app/stores/themeStore'
import { useToasts } from '@app/composables/useToasts'
import ConnectionView from '@app/desktop/ConnectionView.vue'

// A failure here means the shell could not answer, which is not something the
// user can act on from a blank window — fall back to the connection screen so
// there is always something on screen to do.
const connection = await window.agentrq.connection
  .get()
  .catch(() => ({ configured: false, serverUrl: '' }))

if (connection.configured) {
  const { app, router } = createAgentRQApp({
    history: createWebHistory('/'),
    platform: 'desktop',
    // macOS hides the title bar, which leaves the page to draw the window's
    // drag handle and to keep its own chrome clear of the traffic lights.
    os: window.agentrq.platform,
  })

  // Every "go here" from the shell arrives on one channel: notification
  // clicks, agentrq:// deep links, the tray, the global shortcut and the menu.
  // The shell names a destination; the router the shared factory built does
  // the navigating.
  window.agentrq.navigation?.onNavigate((route) => {
    router.push(route).catch(() => {
      // An unknown or unchanged route is not worth surfacing — the window is
      // focused either way, which is most of what the request asked for.
    })
  })

  app.mount('#app')

  // Transient update states are reported through the toast system. The one
  // state that is not transient — an update downloaded and waiting — raises
  // App.vue's existing "new version available" banner instead, which is both
  // the right affordance and identical to what the browser build shows.
  const { addToast, notifyInfo, notifySuccess, notifyError } = useToasts()
  let lastUpdateStatus = null
  window.agentrq.updates?.onStatus((state) => {
    if (state.status === lastUpdateStatus) return
    lastUpdateStatus = state.status
    // A six-hourly background check that finds nothing must stay silent;
    // the same answer to a question the user asked should be reported.
    if (!state.announce) return

    if (state.status === 'checking') notifyInfo('Checking for updates…', 'Updates')
    else if (state.status === 'up-to-date') notifySuccess('AgentRQ is up to date', 'Updates')
    else if (state.status === 'available') notifyInfo(`Downloading version ${state.version}…`, 'Updates')
    else if (state.status === 'error') {
      // An unsigned macOS build cannot replace itself, but the user is not
      // stuck — there is a command that does it. A four-second toast is no use
      // for something you have to retype, so this one waits to be dismissed.
      if (state.remedy) {
        addToast(`${state.detail}. Update with:  ${state.remedy}`, 'error', 'Update failed', 0)
      } else {
        notifyError(state.detail, 'Update failed')
      }
    }
    else if (state.status === 'disabled') notifyInfo(state.detail, 'Updates')
  })

  // The theme store is the app's single source of truth for appearance, so the
  // native chrome follows it rather than the OS. Mounting first means pinia is
  // active and the stored preference has already been applied.
  const themeStore = useThemeStore()
  window.agentrq.theme?.set(themeStore.theme)
  watch(
    () => themeStore.theme,
    (theme) => window.agentrq.theme?.set(theme)
  )
} else {
  createApp(ConnectionView, {
    initialUrl: connection.serverUrl,
    // A profile added and not yet connected can be abandoned; a first run has
    // nothing to go back to. The shell knows which this is.
    canCancel: Boolean(connection.canCancel),
    // Passed rather than read from the platform store: this view is mounted on
    // its own, before the application and its pinia exist.
    isMac: window.agentrq.platform === 'darwin',
  }).mount('#app')
}
