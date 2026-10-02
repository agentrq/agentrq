// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * A page in the side panel asking for a permission — the camera, a location,
 * notifications — and the person being asked, as a browser would ask them.
 *
 * The request is held here and the question goes to the window, which draws a
 * bar under the panel's toolbar. Whatever is answered is remembered for that
 * site (permission-store.js), so the next request is answered without asking.
 *
 * Never left waiting: a question nobody answers is a Block after a deadline,
 * and one replaced by another, or by the page going away, is a Block at once.
 * A held callback is a page hanging on a promise that will never settle.
 */

/** What a page may have without asking, as a browser gives it. */
export const ALWAYS_ALLOWED = new Set(['clipboard-sanitized-write', 'fullscreen', 'persistent-storage', 'screen-wake-lock'])

/**
 * What is worth asking about, by the name it is remembered under.
 *
 * Several Chromium names are one question to a person — a precise and an
 * approximate location are both "your location" — so they share a key, and a
 * single answer covers both. Anything not here, including every permission a
 * future Chromium adds, is refused without asking: an unknown permission is
 * not one to grant by default.
 */
const KEYS = {
  geolocation: 'geolocation',
  'geolocation-approximate': 'geolocation',
  notifications: 'notifications',
  midi: 'midi',
  midiSysex: 'midi',
  'display-capture': 'display-capture',
  'clipboard-read': 'clipboard-read',
  'idle-detection': 'idle-detection',
  'window-management': 'window-management',
  'local-fonts': 'local-fonts',
  'storage-access': 'storage-access',
  'top-level-storage-access': 'storage-access',
  pointerLock: 'pointer-lock',
  keyboardLock: 'keyboard-lock',
  'speaker-selection': 'speaker-selection',
}

/** How long a question waits before it is a Block. */
export const ANSWER_DEADLINE_MS = 60_000

function originOf(rawUrl) {
  try {
    const url = new URL(String(rawUrl ?? ''))
    return `${url.protocol}//${url.host}`
  } catch {
    return ''
  }
}

/**
 * The keys a request is about. `media` is the camera, the microphone or both,
 * and they are separate decisions — allowing a call's microphone is not
 * allowing its camera.
 *
 * @returns {string[]} empty when the permission is not one to ask about
 */
export function permissionKeys(permission, details = {}) {
  if (permission === 'media') {
    const types = details.mediaTypes ?? (details.mediaType ? [details.mediaType] : [])
    const keys = []
    if (types.includes('video')) keys.push('camera')
    if (types.includes('audio')) keys.push('microphone')
    return keys
  }
  return KEYS[permission] ? [KEYS[permission]] : []
}

/**
 * @param {object} options
 * @param {ReturnType<import('./permission-store.js').createPermissionStore>} options.store
 * @param {(question: { id: number, guestId: number, origin: string, permissions: string[] }) => void} options.ask
 * @param {(id: number) => void} [options.settled]  told whenever a question stops
 *   waiting, however it ended — so the window can take its bar down
 * @param {number} [options.deadlineMs]
 * @param {typeof setTimeout} [options.setTimer]
 * @param {typeof clearTimeout} [options.clearTimer]
 */
export function createPermissionBroker({
  store,
  ask,
  settled = () => {},
  deadlineMs = ANSWER_DEADLINE_MS,
  setTimer = setTimeout,
  clearTimer = clearTimeout,
}) {
  /** id → the request waiting on a person */
  const pending = new Map()
  let nextId = 1

  // Only ever called for a question still waiting: answering checks first,
  // and settling clears the deadline that would otherwise come back for it.
  function settle(id, granted) {
    const waiting = pending.get(id)
    pending.delete(id)
    clearTimer(waiting.timer)
    waiting.callback(granted)
    settled(id)
  }

  function pendingFor(guestId) {
    return [...pending.values()].filter((waiting) => waiting.guestId === guestId)
  }

  return {
    /** `session.setPermissionRequestHandler`'s handler. */
    async request(webContents, permission, callback, details = {}) {
      if (ALWAYS_ALLOWED.has(permission)) return callback(true)
      const keys = permissionKeys(permission, details)
      const origin = originOf(details.requestingUrl ?? webContents?.getURL?.())
      if (keys.length === 0 || !origin) return callback(false)

      await store.load()
      const decided = keys.map((key) => store.get(origin, key))
      if (decided.includes('block')) return callback(false)
      if (decided.every((decision) => decision === 'allow')) return callback(true)

      // One question per page at a time. A second replaces the first, which is
      // a Block: two bars stacked under one toolbar is not something anybody
      // can answer.
      const guestId = webContents?.id ?? 0
      for (const earlier of pendingFor(guestId)) settle(earlier.id, false)

      const id = nextId++
      const timer = setTimer(() => settle(id, false), deadlineMs)
      pending.set(id, { id, guestId, origin, keys, callback, timer })
      ask({ id, guestId, origin, permissions: keys })
    },

    /** `session.setPermissionCheckHandler`'s handler: the same record, without asking. */
    check(_webContents, permission, requestingOrigin, details = {}) {
      if (ALWAYS_ALLOWED.has(permission)) return true
      const keys = permissionKeys(permission, details)
      const origin = originOf(requestingOrigin)
      return keys.length > 0 && Boolean(origin) && keys.every((key) => store.get(origin, key) === 'allow')
    },

    /**
     * The person's answer. Remembered for the site, then given to the page.
     *
     * @param {number} id
     * @param {'allow'|'block'} decision
     * @returns {Promise<boolean>} false when the question was already settled
     */
    async answer(id, decision) {
      const waiting = pending.get(id)
      if (!waiting) return false
      const granted = decision === 'allow'
      // Recorded before the page hears, so a check it makes straight after
      // already sees the answer; the writes to disk finish behind it.
      const saves = waiting.keys.map((key) => store.set(waiting.origin, key, granted ? 'allow' : 'block'))
      settle(id, granted)
      await Promise.all(saves)
      return true
    },

    /** The page navigated or the panel closed: whatever it was asking is a Block. */
    cancelFor(guestId) {
      for (const waiting of pendingFor(guestId)) settle(waiting.id, false)
    },
  }
}
