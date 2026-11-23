// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * Why `npm run dev` prints this on macOS, and what to do about it:
 *
 *   sandbox_extension_issue_file failed for .../Electron Helper.app/.../Resources: 1
 *
 * Chromium's helper processes run in the OS sandbox, so the browser process
 * asks macOS for a read extension covering the helper's own Resources
 * directory. That line is macOS refusing — it is printed by libsandbox, not by
 * Chromium, which logs nothing here and simply carries on with a null token
 * (sandbox/mac/seatbelt_extension.cc). So it is a message about the machine,
 * not about this app, and it is dev-only: a packaged build ships a signed
 * bundle in a normal location and never asks for the extension this way.
 *
 * It is still worth catching, because the two causes that are actually ours to
 * fix look identical from the log: a quarantined or a damaged copy of the
 * unpackaged Electron under node_modules. macOS attaches
 * `com.apple.quarantine` to downloaded archives, and Electron's is downloaded
 * on install — recent macOS releases have been increasingly willing to refuse
 * things inside such a bundle, up to deleting it outright.
 *
 * This runs before Electron starts and says which it is. It never blocks the
 * launch: the message is usually harmless, and a preflight that stopped dev on
 * a harmless warning would be worse than the warning.
 */

/** Where the helper keeps the resources the failing extension covers. */
export const HELPER_RESOURCES = 'Contents/Frameworks/Electron Helper.app/Contents/Resources'

/** What macOS marks anything it considers downloaded. */
export const QUARANTINE_ATTRIBUTE = 'com.apple.quarantine'

/**
 * The advice for a set of observations, or null when there is nothing to say.
 *
 * A pure function, so the wording and the rules are testable without a Mac.
 *
 * @param {object} found
 * @param {string} found.platform            `process.platform`
 * @param {string} found.bundlePath          the unpackaged Electron.app
 * @param {boolean} found.resourcesMissing   the helper's Resources are not there
 * @param {boolean} found.quarantined        the bundle carries [QUARANTINE_ATTRIBUTE]
 */
export function describeSandboxProblem({ platform, bundlePath, resourcesMissing, quarantined }) {
  // Every other platform reaches none of this: the sandbox extension is a
  // macOS mechanism and there is nothing to warn about.
  if (platform !== 'darwin') return null

  if (resourcesMissing) {
    return [
      `The Electron helper's resources are missing from ${bundlePath}.`,
      'macOS deletes or truncates this bundle when it decides the download was untrustworthy.',
      'Reinstall it with:  rm -rf node_modules/electron && npm install',
    ].join('\n  ')
  }

  if (quarantined) {
    return [
      `${bundlePath} is quarantined (${QUARANTINE_ATTRIBUTE}).`,
      'macOS will refuse the sandbox extension its helper processes ask for, which is the',
      '"sandbox_extension_issue_file failed ... Operation not permitted" line in the log.',
      `Clear it with:  xattr -dr ${QUARANTINE_ATTRIBUTE} node_modules/electron/dist`,
    ].join('\n  ')
  }

  return null
}

/**
 * Run the checks above against a real disk.
 *
 * Both the filesystem and the attribute lookup are injected, so the wiring is
 * exercised in the test suite on whatever platform it happens to run on.
 *
 * @param {object} deps
 * @param {string} deps.platform
 * @param {string} deps.electronPath   what `import electronPath from 'electron'` gives —
 *                                     the executable inside the bundle
 * @param {(p: string) => boolean} deps.exists
 * @param {(p: string) => boolean} deps.hasQuarantine
 */
export function checkMacSandbox({ platform, electronPath, exists, hasQuarantine }) {
  if (platform !== 'darwin') return null

  // .../Electron.app/Contents/MacOS/Electron -> .../Electron.app
  const bundlePath = electronPath.replace(/\/Contents\/MacOS\/[^/]+$/, '')
  // Nothing to diagnose if the path is not the bundle layout we expect; saying
  // something confident about a tree we do not recognise would be worse than
  // saying nothing.
  if (bundlePath === electronPath) return null

  return describeSandboxProblem({
    platform,
    bundlePath,
    resourcesMissing: !exists(`${bundlePath}/${HELPER_RESOURCES}`),
    quarantined: hasQuarantine(bundlePath),
  })
}
