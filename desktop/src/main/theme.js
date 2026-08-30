/**
 * Keeping the native chrome in step with the app's own theme.
 *
 * The web app has one source of truth for appearance: the `theme` value in
 * `frontend/src/stores/themeStore.js`, which toggles a `.dark` class. The
 * desktop shell has to follow *that*, not the OS — a user who has explicitly
 * chosen light mode inside AgentRQ should not get a dark title bar because
 * their system is dark.
 *
 * The one case where the OS does decide is the store's own 'system' setting,
 * which is exactly what it means.
 */

/** Values the theme store can hold. */
export const THEMES = ['system', 'light', 'dark']

/**
 * Map the app's theme onto Electron's `nativeTheme.themeSource`.
 *
 * The names line up, but the mapping is explicit so an unrecognised stored
 * value degrades to following the system rather than to an arbitrary choice.
 */
export function themeSourceFor(theme) {
  return THEMES.includes(theme) ? theme : 'system'
}

/**
 * Window background colour for a theme.
 *
 * This is what shows during a reload, before the renderer has painted. Getting
 * it wrong produces a white flash in dark mode on every navigation — the values
 * match the app's own surfaces (`bg-zinc-50` and `zinc-950`).
 *
 * @param {string} theme       the app's setting
 * @param {boolean} systemDark whether the OS is currently dark, used only when
 *                             the setting is 'system'
 */
export function backgroundColorFor(theme, systemDark = false) {
  const dark = theme === 'dark' || (themeSourceFor(theme) === 'system' && systemDark)
  return dark ? '#09090b' : '#fafafa'
}

/**
 * Apply a theme to the shell.
 *
 * @param {object} deps
 * @param {{ themeSource: string, shouldUseDarkColors: boolean }} deps.nativeTheme
 * @param {() => Array<{ setBackgroundColor: (color: string) => void }>} deps.windows
 */
export function applyTheme(theme, { nativeTheme, windows }) {
  const source = themeSourceFor(theme)
  nativeTheme.themeSource = source

  // Read back rather than recomputing: with 'system' the answer is the OS's,
  // and nativeTheme is the thing that knows it.
  const color = backgroundColorFor(theme, nativeTheme.shouldUseDarkColors)
  for (const win of windows()) {
    win.setBackgroundColor(color)
  }

  return { source, color }
}
