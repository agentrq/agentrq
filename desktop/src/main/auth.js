/**
 * OAuth sign-in for the desktop app.
 *
 * `LoginView.vue` renders its Google and GitHub buttons as ordinary links to
 * `/api/v1/auth/<provider>/login`. In the browser that just works. Here it
 * cannot: the link resolves against the app:// origin, and the provider's
 * redirect chain has to run against the *real* https origin for the callback to
 * come back to a server that can set a cookie.
 *
 * So the shell intercepts the navigation and runs the flow in a dedicated
 * window pointed at the real server. That window shares the default session, so
 * the `at` cookie the callback sets lands in the same jar `net.fetch` reads —
 * which is what makes the main window authenticated the moment it reloads.
 *
 * The login view itself is untouched, and the browser build is unaffected.
 *
 * Electron is injected rather than imported, so the rules below are testable in
 * plain Node.
 */

/** Providers whose sign-in the shell has to take over. */
export const OAUTH_PROVIDERS = ['google', 'github']

const OAUTH_LOGIN_PATTERN = new RegExp(`^/api/v1/auth/(${OAUTH_PROVIDERS.join('|')})/login/?$`)

/** Name of the session cookie the backend sets on a successful login. */
export const AUTH_COOKIE = 'at'

/**
 * Is this the start of an OAuth sign-in?
 *
 * @returns {string|null} the provider name, or null.
 */
export function matchOAuthLogin(pathname) {
  const match = OAUTH_LOGIN_PATTERN.exec(pathname)
  return match ? match[1] : null
}

/**
 * Has the OAuth window come back to the application?
 *
 * The provider redirects to the backend's callback, which sets the cookie and
 * then redirects on to the app. Landing anywhere on the server that is not
 * still inside the auth endpoints means the round trip finished — success or
 * failure is then decided by whether a cookie actually exists.
 */
export function isOAuthReturn(url, serverUrl) {
  let target
  let server
  try {
    target = new URL(url)
    server = new URL(serverUrl)
  } catch {
    return false
  }

  if (target.origin !== server.origin) return false
  return !target.pathname.startsWith('/api/v1/auth/')
}

/**
 * Where the OAuth window should start: the same path the link pointed at, but
 * on the real server rather than the app:// origin.
 */
export function oauthStartUrl(serverUrl, pathname, search = '') {
  return new URL(pathname + search, `${serverUrl}/`).toString()
}

/**
 * Run an OAuth sign-in.
 *
 * @param {object} deps
 * @param {string} deps.serverUrl
 * @param {string} deps.startUrl          where the flow begins, from `oauthStartUrl`
 * @param {() => object} deps.createWindow  builds the OAuth BrowserWindow
 * @param {() => Promise<boolean>} deps.hasAuthCookie
 * @returns {Promise<{ ok: boolean, reason?: string }>} resolves once the window
 *          closes, however it closed.
 */
export function runOAuthFlow({ serverUrl, startUrl, createWindow, hasAuthCookie }) {
  return new Promise((resolve) => {
    const win = createWindow()
    let settled = false

    // Whatever happens, the window closing is what ends the flow — including
    // the user giving up and closing it by hand.
    const finish = (result) => {
      if (settled) return
      settled = true
      resolve(result)
      if (!win.isDestroyed()) win.close()
    }

    const checkNavigation = async (url) => {
      if (settled || !isOAuthReturn(url, serverUrl)) return
      finish(
        (await hasAuthCookie())
          ? { ok: true }
          : { ok: false, reason: 'Sign-in did not complete' }
      )
    }

    win.webContents.on('did-navigate', (_event, url) => checkNavigation(url))
    // The callback usually arrives as a redirect rather than a fresh
    // navigation, so both have to be watched.
    win.webContents.on('did-redirect-navigation', (_event, url) => checkNavigation(url))

    win.on('closed', () => {
      if (settled) return
      settled = true
      resolve({ ok: false, reason: 'Sign-in window was closed' })
    })

    win.loadURL(startUrl)
  })
}
