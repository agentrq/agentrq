// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The server's public, all-account totals the login page shows: the same
 * figures, from the same `/pub/stats` stream, as the landing page's stats bar.
 */

/** The figures in the order they are shown, with their labels. */
export const PUBLIC_STATS = [
  { key: 'tasks', label: 'Tasks Created' },
  { key: 'users', label: 'Users Signed Up' },
  { key: 'messages', label: 'Messages Sent' },
  { key: 'mcpCalls', label: 'MCP Calls' },
  { key: 'approvals', label: 'Approvals' },
]

/**
 * Folds one `/pub/stats` event, `[{action, total}]`, into the figures shown.
 *
 * @param {Array<{action: string, total: number}>} entries
 * @returns {{tasks: number, users: number, messages: number, mcpCalls: number, approvals: number}}
 */
export function publicStats(entries) {
  const t = {}
  for (const { action, total } of entries) t[action] = total
  const n = (action) => t[action] || 0
  return {
    tasks: n('task_create'),
    users: n('user_create'),
    messages: n('message_create'),
    mcpCalls: n('mcp_tool_call'),
    approvals: n('mcp_permission_manual') + n('mcp_permission_auto') + n('task_approve_manual'),
  }
}

/** 1234 → "1.2K", 2000000 → "2M". */
export function formatCount(n) {
  if (n >= 1000000) return (n / 1000000).toFixed(1).replace(/\.0$/, '') + 'M'
  if (n >= 1000) return (n / 1000).toFixed(1).replace(/\.0$/, '') + 'K'
  return String(n)
}

/**
 * Follows `/pub/stats`, calling `onStats` with each event's figures. A server
 * that cannot stream them (the desktop app's proxy carries only `/api`) just
 * never calls it, so the page shows no figures rather than zeros.
 *
 * @param {(stats: ReturnType<typeof publicStats>) => void} onStats
 * @returns {() => void} stops following
 */
export function followPublicStats(onStats) {
  const es = new EventSource('/pub/stats')
  es.onmessage = (e) => {
    try {
      onStats(publicStats(JSON.parse(e.data)))
    } catch {
      // A malformed event changes nothing on screen.
    }
  }
  return () => es.close()
}
