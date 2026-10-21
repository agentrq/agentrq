// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { ref } from 'vue'
import { fetchAdminStats } from '../api'

/**
 * The admin page's per-user figures, re-read every `intervalMs` once started.
 *
 * The admin session is a cookie the page never sees; a read that comes back
 * signed out stops the timer and calls `onSignedOut`, so the page can send the
 * operator to sign in again. A failed read keeps the last figures and says so.
 */
export function useAdminStats({ fetchFn = fetchAdminStats, intervalMs = 10000, onSignedOut = () => {} } = {}) {
  const stats = ref(null)
  const error = ref('')
  const loading = ref(false)
  const updatedAt = ref(null)
  let timer = null

  function stop() {
    clearInterval(timer)
    timer = null
  }

  async function refresh() {
    loading.value = true
    try {
      const res = await fetchFn()
      if (!res) {
        stop()
        onSignedOut()
        return
      }
      stats.value = res
      updatedAt.value = new Date()
      error.value = ''
    } catch (err) {
      error.value = err.message
    } finally {
      loading.value = false
    }
  }

  async function start() {
    stop()
    timer = setInterval(refresh, intervalMs)
    await refresh()
  }

  return { stats, error, loading, updatedAt, start, stop, refresh }
}
