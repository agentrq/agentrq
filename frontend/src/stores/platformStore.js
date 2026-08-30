import { defineStore } from 'pinia'

/** Platforms the app can run on. Anything else is treated as the browser. */
const KNOWN_PLATFORMS = ['web', 'desktop']

/**
 * Which shell the app is running inside.
 *
 * The desktop build renders the very same components as the browser build, so
 * this is how a component offers something only one of them can do — a native
 * file reveal, say — without either build growing its own copy of the view.
 *
 * Read it rather than sniffing the user agent or probing for `window.agentrq`:
 * those answer "what is this runtime", which is not the same question and drifts
 * as the desktop app gains capabilities.
 */
export const usePlatformStore = defineStore('platform', {
  state: () => ({
    platform: 'web',
  }),
  getters: {
    isDesktop: (state) => state.platform === 'desktop',
    isWeb: (state) => state.platform === 'web',
  },
  actions: {
    setPlatform(next) {
      // An unrecognised value falls back to 'web': the browser build is the
      // one with no extra capabilities, so it is the safe assumption.
      this.platform = KNOWN_PLATFORMS.includes(next) ? next : 'web'
    },
  },
})
