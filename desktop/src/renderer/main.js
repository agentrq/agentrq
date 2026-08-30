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
import { createApp } from 'vue'
import { createWebHistory } from 'vue-router'

import { createAgentRQApp } from '@app/app'
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
  })

  // Clicking a native notification lands here: the shell has already focused
  // the window, and the router the shared factory built does the navigating.
  window.agentrq.notifications?.onActivate((route) => {
    router.push(route).catch(() => {
      // An unknown or unchanged route is not worth surfacing — the window is
      // focused either way, which is most of what the click asked for.
    })
  })

  app.mount('#app')
} else {
  createApp(ConnectionView, { initialUrl: connection.serverUrl }).mount('#app')
}
