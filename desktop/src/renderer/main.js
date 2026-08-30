/**
 * Desktop renderer bootstrap.
 *
 * The application is assembled by the frontend's own `createAgentRQApp`, so
 * this build renders the identical component tree and route table the browser
 * does — there is deliberately no second list of routes to keep in step.
 *
 * `createWebHistory` with no base is correct here: the renderer is served from
 * the root of the app:// origin, and the protocol handler does the same SPA
 * fallback the Go backend does, so ordinary history routing works.
 *
 * No service worker is registered. The desktop app's assets are already local,
 * and updates arrive through electron-updater (phase 6, task 0huaAvwzIuH)
 * rather than a waiting worker.
 */
import { createWebHistory } from 'vue-router'

import { createAgentRQApp } from '@app/app'

const { app } = createAgentRQApp({
  history: createWebHistory('/'),
  platform: 'desktop',
})

app.mount('#app')
