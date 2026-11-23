// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { ref, watch } from 'vue'

/**
 * Desktop stand-in for `virtual:pwa-register/vue`.
 *
 * App.vue already has the right piece of UI for "a new version is available":
 * the banner it shows when a service worker is waiting, with an "Update now"
 * button and a dismiss. The desktop app has no service worker, but it has the
 * same thing to say — so rather than build a second banner that looks almost
 * the same, this presents the Electron updater through the interface App.vue
 * already consumes.
 *
 * The result is that the desktop update prompt is, by construction, pixel
 * identical to the web one, and App.vue needs no knowledge of either.
 *
 * The shape must match what App.vue destructures: a writable `needRefresh` ref
 * and an awaitable `updateServiceWorker`. `progress` is the one addition, and
 * App.vue treats it as optional since the web build has none.
 *
 * With no bridge present — the frontend's own test run, say — everything stays
 * inert, exactly as the plain stub does.
 */
export function useRegisterSW() {
  const needRefresh = ref(false)
  const offlineReady = ref(false)
  /** Whether this build has to shell out to the installer to update itself. */
  const useInstaller = ref(false)
  /**
   * How far an update the user asked for has got, or null before they ask:
   * `{ phase, percent, version }`, where percent is null when unknown.
   */
  const progress = ref(null)

  /** The version currently being offered, and the one the user waved away. */
  let offeredVersion = ''
  let dismissedVersion = null

  // App.vue dismisses by writing the ref directly — that is the web PWA
  // contract and not worth changing for one platform. So the dismissal is
  // observed here rather than announced.
  // Synchronous: the dismissal has to be recorded the instant it happens, or a
  // status arriving in the same tick would be compared against a stale answer
  // and put the banner straight back up.
  watch(
    needRefresh,
    (now, before) => {
      if (before && !now) dismissedVersion = offeredVersion
    },
    { flush: 'sync' },
  )

  const updates = globalThis.window?.agentrq?.updates
  if (updates) {
    updates.onStatus((state) => {
      if (progress.value) followProgress(state)
      if (!isOfferable(state)) return

      // Raised here, never lowered.
      //
      // This used to be `needRefresh.value = state.status === 'ready'`, which
      // reads correctly and is wrong: every later status recomputed it, so the
      // next background check — 'checking', then 'up-to-date' — took the banner
      // away with nobody having dismissed it. At a fifteen-minute interval that
      // is four disappearances an hour.
      //
      // An update does not stop being available because we looked again, so
      // only the user takes the banner down.
      offeredVersion = state.version ?? ''
      if (offeredVersion === dismissedVersion) return

      needRefresh.value = true
      // Anything but a downloaded-and-installable update has to go through the
      // installer — including a 'ready' that turned out not to be.
      useInstaller.value = state.status !== 'ready'
    })
  }

  /** Move the bar on, or take it down and offer again if the install failed. */
  function followProgress(state) {
    const version = progress.value.version
    if (state.status === 'installing' && state.progress) {
      progress.value = { ...state.progress, version }
    } else if (state.status === 'installed') {
      progress.value = { phase: 'installed', percent: 100, version }
    } else if (state.status === 'error') {
      progress.value = null
    }
  }

  /** Put the offer back after an install that did not start. */
  function restoreOffer() {
    progress.value = null
    needRefresh.value = true
  }

  return {
    needRefresh,
    offlineReady,
    useInstaller,
    progress,
    /** Take down the "installed, restart to finish" notice. */
    dismissProgress: () => {
      progress.value = null
    },
    updateServiceWorker: async () => {
      // App.vue clears needRefresh before calling this, which the watch above
      // takes for a dismissal. It was not one: if the install fails, the offer
      // has to come back.
      dismissedVersion = null
      const version = offeredVersion

      // Two routes, because a build that cannot replace itself still has one:
      // an unsigned macOS app is refused by Squirrel.Mac every time, and the
      // installer that swaps the whole bundle is the only way it ever updates.
      if (useInstaller.value) {
        progress.value = { phase: 'preparing', percent: null, version }
        const result = await updates?.installViaScript()
        if (updates && !result?.ok) restoreOffer()
        return
      }

      // Already downloaded, so all that is left is the restart.
      progress.value = { phase: 'restarting', percent: null, version }
      const installed = await updates?.installNow()
      if (updates && !installed) restoreOffer()
    },
  }
}

/**
 * Whether a status is worth putting a banner up for.
 *
 * `ready` is the ordinary case: downloaded and waiting, where restarting does
 * it. `available` matters only for the builds that cannot install what they
 * downloaded — the download will never complete into a `ready` they can act
 * on, so the offer has to rest on availability alone, with the installer
 * behind it.
 *
 * Exported because it is also the readable way to state the rule, and the
 * tests assert it directly.
 */
export function isOfferable(state) {
  if (state?.status === 'ready') return true
  if (!state?.canInstallViaScript) return false

  // 'available' matters only for a build that cannot install what it downloads:
  // there is no 'ready' coming that it could act on.
  if (state.status === 'available') return true

  // And an install that just failed for want of a signature is the same
  // situation arriving the other way round — the app reached 'ready', tried,
  // and Squirrel refused. The offer stands; only the route changes. Recognised
  // by the remedy rather than by parsing the message again, since the main
  // process has already made that judgement.
  return state.status === 'error' && Boolean(state.remedy)
}

/**
 * What the banner says while an update is under way.
 *
 * Exported so the wording is tested here, where coverage reaches, rather than
 * only in App.vue's template.
 */
export function progressLabel(progress) {
  const name = progress?.version ? `AgentRQ ${progress.version}` : 'the update'
  switch (progress?.phase) {
    case 'downloading':
      return `Downloading ${name}…`
    case 'installing':
      return `Installing ${name}…`
    case 'restarting':
      return 'Restarting to update…'
    case 'installed':
      return `${progress.version ? `AgentRQ ${progress.version}` : 'The update'} is installed. Restart AgentRQ to finish.`
    default:
      return `Preparing ${name}…`
  }
}
