// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// CI's check that the app's own window never shows WebView2's notification
// prompt, run in the main webview through AGENTRQ_E2E_INIT_SCRIPT. It asks to
// notify, as the Workspace settings' Enable Push does, and sends the answer to
// extensions-server.mjs through the app's proxy. A prompt would leave the
// answer pending, and the step would run out of time.
if (window.top === window) {
  const report = (step, value) =>
    fetch('/api/v1/__acceptance', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ step, value }),
    })
  document.addEventListener('DOMContentLoaded', async () => {
    try {
      const before = typeof Notification === 'function' ? Notification.permission : typeof Notification
      await report('asking to notify', { before })
      const answer = typeof Notification === 'function' ? await Notification.requestPermission() : 'no Notification'
      await report('the window was answered', { answer, after: Notification.permission })
      await report(answer === 'denied' ? 'done' : 'failed', answer)
    } catch (error) {
      await report('failed', String(error))
    }
  })
}
