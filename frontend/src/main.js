/**
 * Browser bootstrap.
 *
 * The application itself is assembled in `app.js`, which the desktop renderer
 * calls too. Only what is genuinely browser-specific belongs here: the base
 * path the Go backend injects into index.html, and the service worker, which
 * the desktop build replaces with electron-updater.
 */
import { createWebHistory } from 'vue-router'

import { createAgentRQApp } from './app'

const { app } = createAgentRQApp({
  history: createWebHistory(window.__AGENTRQ_BASE_PATH__ || '/'),
  platform: 'web',
})

app.mount('#app')
