// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// Chrome and Edge offer to install the app through `beforeinstallprompt`. The
// event is kept so the account menu can offer "Install app"; the browser never
// fires it inside the installed app.

import { ref } from 'vue'

import { isHomeScreenApp } from './iosHomeScreen'

let deferred = null

export const canInstall = ref(false)

function hold(event) {
  deferred = event
  canInstall.value = event !== null
}

// Called before mount: Chrome can fire the event before the app renders.
export function captureInstallPrompt(win = window) {
  if (isHomeScreenApp(win)) return
  win.addEventListener('beforeinstallprompt', (event) => {
    event.preventDefault()
    hold(event)
  })
  win.addEventListener('appinstalled', () => hold(null))
}

// A prompt can be shown once, so the event is spent whatever the answer.
export async function promptInstall() {
  const event = deferred
  if (!event) return
  hold(null)
  await event.prompt()
}
