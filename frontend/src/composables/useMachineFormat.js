// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * Turning a machine's numbers into something a person can read at a glance.
 *
 * Pure functions, separate from the views, because the interesting decisions
 * here are all about what an absent value means — and those are worth testing
 * rather than burying in a template.
 */

const UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']

/**
 * Bytes, at the scale a person would say them out loud.
 *
 * Decimal rather than binary units: a disk sold as 500 GB should read as
 * roughly 500 GB, and explaining the difference is not this label's job.
 */
export function formatBytes(bytes) {
  if (!Number.isFinite(bytes) || bytes < 0) return '—'
  if (bytes === 0) return '0 B'
  let n = bytes
  let unit = 0
  while (n >= 1000 && unit < UNITS.length - 1) {
    n /= 1000
    unit += 1
  }
  // One decimal below 100, none above: "4.2 GB" and "512 GB" both read well,
  // "4 GB" loses information and "512.3 GB" is noise.
  return `${n < 100 ? n.toFixed(1) : Math.round(n)} ${UNITS[unit]}`
}

/** A percentage, to one decimal, or a dash when there is no reading. */
export function formatPercent(value) {
  if (!Number.isFinite(value)) return '—'
  return `${value.toFixed(1)}%`
}

/**
 * How long a machine has been up, in the two largest units that apply.
 *
 * "3d 4h" rather than "3 days, 4 hours, 12 minutes and 6 seconds": this is a
 * glance, and the seconds have never mattered to anyone reading it.
 */
export function formatUptime(seconds) {
  if (!Number.isFinite(seconds) || seconds < 0) return '—'
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return `${d}d ${h}h`
  if (h > 0) return `${h}h ${m}m`
  if (m > 0) return `${m}m`
  return `${Math.floor(seconds)}s`
}

/**
 * How full a filesystem is.
 *
 * Used space rather than free, because that is what a bar fills up with — and
 * a mount whose size is unknown gets no bar rather than an empty one, which
 * would read as a disk with nothing on it.
 */
export function diskUsedPercent(disk) {
  if (!disk || !Number.isFinite(disk.total) || disk.total <= 0) return null
  const free = Number.isFinite(disk.free) ? disk.free : 0
  return Math.min(100, Math.max(0, ((disk.total - free) / disk.total) * 100))
}

/**
 * Memory in use, as a percentage.
 *
 * Derived from `available` rather than from a "used" figure, for the reason
 * the daemon reports available in the first place: on Linux the page cache
 * makes "used" look alarming on a perfectly healthy machine.
 */
export function memoryUsedPercent(metrics) {
  if (!metrics || !Number.isFinite(metrics.memTotal) || metrics.memTotal <= 0) return null
  const available = Number.isFinite(metrics.memAvailable) ? metrics.memAvailable : 0
  return Math.min(100, Math.max(0, ((metrics.memTotal - available) / metrics.memTotal) * 100))
}

/**
 * The load average, or nothing at all.
 *
 * Windows has no load average and the daemon sends none. Rendering three
 * zeroes there would say "this machine is perfectly idle", which is the most
 * misleading thing this row could claim.
 */
export function formatLoadAvg(loadAvg) {
  if (!Array.isArray(loadAvg) || loadAvg.length === 0) return null
  return loadAvg.map((n) => (Number.isFinite(n) ? n.toFixed(2) : '—')).join('  ')
}

/**
 * How stale a snapshot is, in words.
 *
 * A machine that went offline keeps its last numbers, and they stop being true
 * the moment it does. Saying when they were taken is what stops somebody
 * deciding to launch an agent based on an hour-old reading.
 */
export function formatAge(reportedAt, now = Date.now()) {
  if (!reportedAt) return null
  const then = new Date(reportedAt).getTime()
  if (!Number.isFinite(then)) return null
  const seconds = Math.max(0, Math.round((now - then) / 1000))
  if (seconds < 60) return 'just now'
  return `${formatUptime(seconds)} ago`
}

/** Session statuses that mean the session can still change. */
const LIVE = new Set(['starting', 'running'])

/** Whether a session is still going. */
export function isSessionLive(status) {
  return LIVE.has(status)
}

/**
 * The colour a status should read as.
 *
 * Returned as a token rather than as classes so the mapping is testable and
 * the view owns how a token looks.
 */
export function sessionTone(status) {
  if (status === 'running') return 'good'
  if (status === 'starting') return 'pending'
  if (status === 'failed') return 'bad'
  return 'muted'
}

/**
 * What a session's row is called.
 *
 * The workspace, because that is the thing that tells one row from the next. A
 * machine runs agents for several workspaces at once and most of them are the
 * same kind, so a list headed "claude-code" three times answers none of the
 * questions somebody opened the page with: which of these is mine, which one
 * is the migration, which one can I stop.
 *
 * A session whose workspace has been deleted, or one recorded before the name
 * was carried, falls back to the kind — never to the id, which is not an
 * answer to anybody.
 */
export function sessionLabel(session) {
  const name = (session?.workspaceName ?? '').trim()
  return name || session?.kind || 'agent'
}

/**
 * The line underneath: what is running, and how it is doing.
 *
 * The kind is included only when it is not already the heading, so a session
 * with no workspace name does not say "claude-code · claude-code".
 */
export function sessionSummary(session) {
  const kind = session?.kind || 'agent'
  const parts = []
  if (sessionLabel(session) !== kind) parts.push(kind)
  parts.push(session?.status || 'unknown')
  if (Number.isInteger(session?.exitCode)) parts.push(`exit ${session.exitCode}`)
  if (session?.restored) parts.push('restored')
  return parts.join(' · ')
}
