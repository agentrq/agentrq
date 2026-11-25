// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// The basic catalog's icon names, drawn the way the app draws every other
// icon: a 24×24 stroked outline in the current colour.

import { resolveValue } from './resolve.js'

const circle = (cx, cy, r) => `M${cx - r} ${cy}a${r} ${r} 0 1 0 ${2 * r} 0a${r} ${r} 0 1 0 ${-2 * r} 0`

const RING = circle(12, 12, 9)
const CALENDAR = 'M4 6a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v13a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2zM4 10h16M8 2v4M16 2v4'
const HEART = 'M12 20s-7-4.4-9-9a4.8 4.8 0 0 1 9-3 4.8 4.8 0 0 1 9 3c-2 4.6-9 9-9 9z'
const BELL = 'M6 16v-5a6 6 0 0 1 12 0v5l2 2H4zM10 20a2 2 0 0 0 4 0'
const STAR = 'M12 3l2.8 5.7 6.2.9-4.5 4.4 1.1 6.2L12 17.3l-5.6 2.9 1.1-6.2L3 9.6l6.2-.9z'
const EYE = `M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z${circle(12, 12, 3)}`
const SPEAKER = 'M4 9h4l5-4v14l-5-4H4z'
const LOCK_BODY = 'M5 11h14v10H5z'
const SLASH = 'M3 3l18 18'

export const ICON_PATHS = {
  accountCircle: `${RING}${circle(12, 10, 3)}M6.2 18.4a7 7 0 0 1 11.6 0`,
  add: 'M12 5v14M5 12h14',
  arrowBack: 'M19 12H5M12 19l-7-7 7-7',
  arrowForward: 'M5 12h14M12 5l7 7-7 7',
  attachFile: 'M15.172 7l-6.586 6.586a2 2 0 102.828 2.828l6.414-6.586a4 4 0 00-5.656-5.656l-6.415 6.585a6 6 0 108.486 8.486L20.5 13',
  calendarToday: CALENDAR,
  call: 'M5 4h4l2 5-2.5 1.5a11 11 0 0 0 5 5L15 13l5 2v4a2 2 0 0 1-2 2A16 16 0 0 1 3 6a2 2 0 0 1 2-2',
  camera: `M3 8a2 2 0 0 1 2-2h2l2-3h6l2 3h2a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z${circle(12, 13, 4)}`,
  check: 'M5 13l4 4L19 7',
  close: 'M6 6l12 12M18 6L6 18',
  delete: 'M4 7h16M10 11v6M14 11v6M6 7l1 12a2 2 0 0 0 2 2h6a2 2 0 0 0 2-2l1-12M9 7V4h6v3',
  download: 'M12 4v12M7 11l5 5 5-5M4 20h16',
  edit: 'M4 20h4L19 9l-4-4L4 16zM13.5 6.5l4 4',
  event: `${CALENDAR}M9 15l2 2 4-4`,
  error: `${RING}M12 7.5v5.5M12 16.5v.01`,
  fastForward: 'M3 6l8 6-8 6zM13 6l8 6-8 6z',
  favorite: HEART,
  favoriteOff: `${HEART}${SLASH}`,
  folder: 'M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z',
  help: `${RING}M9.5 9a2.5 2.5 0 1 1 3.5 2.3c-.6.3-1 .9-1 1.6v.6M12 17v.01`,
  home: 'M3 11l9-8 9 8M5 9.5V20h5v-6h4v6h5V9.5',
  info: `${RING}M12 11v6M12 7.5v.01`,
  locationOn: `M12 21s-7-6.2-7-11a7 7 0 0 1 14 0c0 4.8-7 11-7 11z${circle(12, 10, 2.5)}`,
  lock: `${LOCK_BODY}M8 11V7a4 4 0 0 1 8 0v4`,
  lockOpen: `${LOCK_BODY}M8 11V7a4 4 0 0 1 7.8-1.3`,
  mail: 'M3 6h18v12H3zM3 7l9 6 9-6',
  menu: 'M4 6h16M4 12h16M4 18h16',
  moreVert: 'M12 5v.01M12 12v.01M12 19v.01',
  moreHoriz: 'M5 12h.01M12 12h.01M19 12h.01',
  notificationsOff: `${BELL}${SLASH}`,
  notifications: BELL,
  pause: 'M8 5v14M16 5v14',
  payment: 'M3 6h18v12H3zM3 10h18M7 15h3',
  person: `${circle(12, 8, 4)}M4 21a8 8 0 0 1 16 0`,
  phone: 'M7 4a2 2 0 0 1 2-2h6a2 2 0 0 1 2 2v16a2 2 0 0 1-2 2H9a2 2 0 0 1-2-2zM11 18h2',
  photo: `M3 5h18v14H3zM3 16l5-5 4 4 3-3 6 6${circle(16, 9, 1.5)}`,
  play: 'M7 4l13 8-13 8z',
  print: 'M7 9V3h10v6M7 17H4V9h16v8h-3M7 14h10v7H7z',
  refresh: 'M20 11a8 8 0 1 0-2.3 5.7M20 4v7h-7',
  rewind: 'M21 6l-8 6 8 6zM11 6l-8 6 8 6z',
  search: `${circle(11, 11, 7)}M21 21l-5-5`,
  send: 'M3 11l18-8-8 18-2-8zM11 13L21 3',
  settings: `${circle(12, 12, 3)}${circle(12, 12, 7)}M12 2v3M12 19v3M2 12h3M19 12h3M4.9 4.9L7 7M17 17l2.1 2.1M4.9 19.1L7 17M17 7l2.1-2.1`,
  share: `${circle(18, 5, 2.5)}${circle(6, 12, 2.5)}${circle(18, 19, 2.5)}M8.2 10.8l7.6-4.6M8.2 13.2l7.6 4.6`,
  shoppingCart: `M3 4h2l2.4 11h10.2l2-8H6.2${circle(9, 19.5, 1.5)}${circle(17, 19.5, 1.5)}`,
  skipNext: 'M5 5l10 7-10 7zM19 5v14',
  skipPrevious: 'M19 5L9 12l10 7zM5 5v14',
  star: STAR,
  starHalf: `${STAR}M12 3v14.3`,
  starOff: `${STAR}${SLASH}`,
  stop: 'M6 6h12v12H6z',
  upload: 'M12 16V4M7 9l5-5 5 5M4 20h16',
  visibility: EYE,
  visibilityOff: `${EYE}${SLASH}`,
  volumeDown: `${SPEAKER}M16 9.5a3.5 3.5 0 0 1 0 5`,
  volumeMute: SPEAKER,
  volumeOff: `${SPEAKER}M16 9l5 6M21 9l-5 6`,
  volumeUp: `${SPEAKER}M16 9.5a3.5 3.5 0 0 1 0 5M18.5 7a7 7 0 0 1 0 10`,
  warning: 'M12 3l10 18H2zM12 10v5M12 18v.01',
}

/** What an Icon component draws in ctx, its name and any svgPath resolved. */
export function iconFor(c, ctx) {
  const name = resolveValue(c.name, ctx)
  return iconDrawing(name !== null && typeof name === 'object' ? { svgPath: resolveValue(name.svgPath, ctx) } : name)
}

/**
 * What an Icon's resolved name draws: { stroke } for a catalog name,
 * { fill } for a custom { svgPath }, or null for anything else.
 */
export function iconDrawing(name) {
  if (typeof name === 'string') return Object.hasOwn(ICON_PATHS, name) ? { stroke: ICON_PATHS[name] } : null
  if (typeof name?.svgPath === 'string' && name.svgPath !== '') return { fill: name.svgPath }
  return null
}
