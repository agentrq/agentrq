// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * Browser bootstrap.
 *
 * The application itself is assembled in `app.js`, which the desktop renderer
 * calls too. Only what is genuinely browser-specific belongs here: the base
 * path the Go backend injects into index.html, the service worker, which
 * the desktop build replaces with electron-updater, reloading a tab whose
 * build a deploy replaced, and telling the Chrome
 * extension's popup, when it frames the app, which page is open.
 */
import { createWebHistory } from 'vue-router'

import { createAgentRQApp } from './app'
import { reportRouteToExtensionPopup } from './utils/extensionPopup'
import { followNotificationClicks } from './composables/useNotificationClick'
import { captureInstallPrompt } from './utils/installPrompt'
import { reloadOnStaleChunk } from './utils/staleChunk'

const { app, router } = createAgentRQApp({
  history: createWebHistory(window.__AGENTRQ_BASE_PATH__ || '/'),
  platform: 'web',
})

reloadOnStaleChunk(router)
reportRouteToExtensionPopup(router)
followNotificationClicks(router)
captureInstallPrompt()

app.mount('#app')
