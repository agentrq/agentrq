// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// A Chart's points, colour and heatmap grid, as the analytics pages draw
// theirs. The server checks a literal list; a bound one may hold anything, so
// what cannot be drawn is left out here.

import { statsPalette } from '../composables/useStatsRange.js'
import { MAX_CHART_POINTS } from './validate.js'

const HEX = /^#[0-9a-fA-F]{6}$/
const DAY = /^\d{4}-\d{2}-\d{2}$/
const HOUR = /^\d{4}-\d{2}-\d{2} \d{2}:00$/
const DAY_SECONDS = 86400
// The longest spans a heatmap's grid draws, as on the server: a year of
// days, a month of hours.
const HEATMAP_DAYS = { day: 366, hour: 31 }

/** The points that can be drawn: a label, and a value zero or more. */
export function chartPoints(data) {
  if (!Array.isArray(data)) return []
  return data
    .filter((p) => typeof p?.label === 'string' && p.label !== '' && Number.isFinite(p.value) && p.value >= 0)
    .slice(0, MAX_CHART_POINTS)
}

/**
 * The colour: a #rrggbb as given, else the analytics tone for messages or,
 * by default, for tasks, in this theme; a heatmap's is its tone's name.
 */
export function chartColor(color, variant, isDark) {
  if (HEX.test(color ?? '')) return color
  const p = statsPalette(isDark)
  const messages = color === 'messages'
  if (variant === 'heatmap') return messages ? p.messageHeatmap : p.taskHeatmap
  return messages ? p.messageChart : p.taskChart
}

// A label's local time in unix seconds, as the heatmap reads its buckets.
function seconds(label) {
  return Math.floor(new Date(label.length === 10 ? `${label}T00:00:00` : `${label.replace(' ', 'T')}:00`).getTime() / 1000)
}

/**
 * A heatmap's props: days or hours by the first point's label, the points
 * that are the same, and the range they span, its latest part when longer
 * than the grid draws.
 */
export function heatmapProps(points) {
  const granularity = HOUR.test(points[0]?.label ?? '') ? 'hour' : 'day'
  const pattern = granularity === 'hour' ? HOUR : DAY
  const kept = points.map((p) => ({ bucket: p.label, count: p.value, at: seconds(p.label) }))
    .filter((p) => pattern.test(p.bucket) && !Number.isNaN(p.at))
  if (kept.length === 0) return { granularity, data: [], rangeStart: 0, rangeEnd: 0 }
  const rangeEnd = Math.max(...kept.map((p) => p.at))
  const earliest = rangeEnd - (HEATMAP_DAYS[granularity] * DAY_SECONDS - (granularity === 'hour' ? 3600 : DAY_SECONDS))
  const rangeStart = Math.max(earliest, Math.min(...kept.map((p) => p.at)))
  return {
    granularity,
    data: kept.filter((p) => p.at >= rangeStart).map(({ bucket, count }) => ({ bucket, count })),
    rangeStart,
    rangeEnd,
  }
}

/** What a screen reader hears: the label, then each point's label and value. */
export function chartSummary(label, points, max = 24) {
  const shown = points.slice(0, max).map((p) => `${p.label}: ${p.value}`)
  if (points.length > max) shown.push(`and ${points.length - max} more`)
  return [label, shown.join(', ')].filter(Boolean).join('. ')
}
