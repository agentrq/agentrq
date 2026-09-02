import { defineStore } from 'pinia'

/** Platforms the app can run on. Anything else is treated as the browser. */
const KNOWN_PLATFORMS = ['web', 'desktop']

/**
 * What `process.platform` calls macOS.
 *
 * The desktop shell hides the title bar there, which is the one case where the
 * page has to draw its own window chrome — so the app needs to know the OS, not
 * only that it is the desktop build.
 */
const MACOS = 'darwin'

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
    /** `process.platform` from the shell; '' in the browser, where it has no meaning. */
    os: '',
  }),
  getters: {
    isDesktop: (state) => state.platform === 'desktop',
    isWeb: (state) => state.platform === 'web',
    /**
     * The one combination that changes how the app is laid out: on macOS the
     * shell hides the title bar, so the page owns the window's drag handle and
     * has to keep its own chrome clear of the traffic lights.
     */
    isMacDesktop: (state) => state.platform === 'desktop' && state.os === MACOS,
  },
  actions: {
    setPlatform(next, os = '') {
      // An unrecognised value falls back to 'web': the browser build is the
      // one with no extra capabilities, so it is the safe assumption.
      this.platform = KNOWN_PLATFORMS.includes(next) ? next : 'web'
      // Never remembered for the browser, where a page drawing window chrome
      // would be drawing chrome for a window it does not own.
      this.os = this.platform === 'desktop' && typeof os === 'string' ? os : ''
    },
  },
})
