// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// iOS gives a web page Web Push only once it has been added to the Home Screen,
// and it has no install prompt of its own, so the app has to say how.

const HINT_SHOWN_KEY = 'iosHomeScreenHintShown'

export const HOME_SCREEN_HINT = 'Install AgentRQ: tap Share, then Add to Home Screen.'

// An iPad asks for the desktop site and reports itself as a Mac, so a Mac with
// a touch screen is an iPad.
export function isAppleMobile(nav) {
  return /iPad|iPhone|iPod/.test(nav.userAgent) || (nav.platform === 'MacIntel' && nav.maxTouchPoints > 1)
}

// `navigator.standalone` is Safari's own answer; the media query covers the rest.
export function isHomeScreenApp(win) {
  return win.navigator.standalone === true || win.matchMedia?.('(display-mode: standalone)').matches === true
}

export function needsHomeScreen(win = window) {
  return isAppleMobile(win.navigator) && !isHomeScreenApp(win)
}

// Shown once per browser, and stays until dismissed: an install takes longer
// than a toast's few seconds.
export function offerHomeScreen(notify, win = window) {
  if (!needsHomeScreen(win)) return false
  try {
    if (win.localStorage.getItem(HINT_SHOWN_KEY)) return false
    win.localStorage.setItem(HINT_SHOWN_KEY, '1')
  } catch {
    return false
  }
  notify(HOME_SCREEN_HINT)
  return true
}
