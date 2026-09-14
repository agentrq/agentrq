// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * The window icon — the one thing Linux will not work out for itself.
 *
 * Windows takes a window's taskbar icon from the icon compiled into the `.exe`,
 * and macOS takes the Dock icon from the `.icns` inside the app bundle. Both of
 * those are produced by electron-builder from the single `icon:` in
 * electron-builder.yml, so neither platform has to be told anything at runtime —
 * which is exactly why the icon looks right on both and wrong on Linux.
 *
 * Linux has no equivalent. Nothing about an AppImage or a `.deb` tells a
 * *running window* what to display, so a BrowserWindow created without an
 * `icon` shows Electron's own default in the taskbar, the alt-tab switcher and
 * the window list. The icon has to be handed to each window explicitly.
 *
 * The second half of the same problem lives in the packaging config rather than
 * here: a desktop environment links a running window to its installed launcher
 * entry by app id, so `desktopName` and `linux.syncDesktopName` have to agree
 * with the entry electron-builder writes. See electron-builder.yml.
 */

import { join } from 'node:path'

/**
 * The icon file, named as the *frontend* publishes it.
 *
 * There is deliberately no copy of this asset under `desktop/src`: the
 * renderer's Vite config sets `publicDir` to `frontend/public`, so the build
 * already places this file at the root of the renderer output, and
 * electron-builder already packages the whole renderer directory. A second
 * copy would be a second thing to keep in step with the web app's icon.
 *
 * This is the rounded, transparent mark rather than `large-icon.png`, which is
 * the same artwork on an opaque square tile. Windows and macOS want the tile —
 * it is what their packaged icon is built from — but on Linux the window icon
 * sits beside the launcher icon in the switcher and the dock, and that one is
 * rendered from the SVG, which is rounded. Two shapes for one app, a few
 * pixels apart, is precisely the kind of wrongness this module exists to stop.
 */
export const ICON_FILENAME = 'agentrq.png'

/**
 * Whether this platform needs to be handed a window icon at all.
 *
 * Passing one on Windows or macOS would not be harmful, but it would override
 * the icon the packaged app already carries with a raw PNG that has not been
 * through electron-builder's `.ico`/`.icns` conversion — losing, among other
 * things, the small-size variants those formats exist to hold.
 */
export function needsWindowIcon(platform) {
  return platform === 'linux'
}

/**
 * Where the icon might be, most-preferred first.
 *
 * @param {object} roots
 * @param {string} roots.rendererRoot the built renderer directory (`dist/renderer`)
 * @param {string} roots.projectRoot  the `desktop/` package directory
 */
export function iconSearchPaths({ rendererRoot, projectRoot }) {
  return [
    // What a packaged app has. Electron reads through the asar transparently,
    // so this resolves inside `app.asar` just as it does from a plain build.
    join(rendererRoot, ICON_FILENAME),
    // `npm run dev` serves the renderer from Vite and never builds it, so the
    // path above does not exist there. Fall back to the source asset that the
    // build would have copied — the same file, one step earlier.
    join(projectRoot, '..', 'frontend', 'public', ICON_FILENAME),
  ]
}

/**
 * Resolve the window icon, or null when this platform does not need one and
 * when — on the platform that does — the asset cannot be found.
 *
 * A missing file returns null rather than throwing: an icon is cosmetic, and a
 * shell that refuses to open a window because it could not decorate it would
 * turn a cosmetic defect into an unusable app.
 *
 * @param {object} deps
 * @param {string} deps.platform     `process.platform`
 * @param {string} deps.rendererRoot the built renderer directory
 * @param {string} deps.projectRoot  the `desktop/` package directory
 * @param {(path: string) => boolean} deps.exists
 */
export function resolveWindowIcon({ platform, rendererRoot, projectRoot, exists }) {
  if (!needsWindowIcon(platform)) return null
  return iconSearchPaths({ rendererRoot, projectRoot }).find((path) => exists(path)) ?? null
}

/**
 * The BrowserWindow option fragment for an icon, spreadable at a call site.
 *
 * Spreading nothing is what keeps the other two platforms on their packaged
 * icon: `{ icon: undefined }` and no `icon` key are not the same thing to
 * Electron on every platform, and the empty object is unambiguous.
 */
export function windowIconOptions(iconPath) {
  return iconPath ? { icon: iconPath } : {}
}
