// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Launch the debug build of the Tauri app under `tauri-driver`, on a data
 * folder of its own, and hand back a WebDriver session on its main window.
 *
 * The folder is what makes a run repeatable: the app keeps its profiles and
 * its cookie jar under the user's data directory, so a run on a developer's
 * machine would otherwise start signed in, or touch their real profiles.
 */
import { mkdtemp, rm } from 'node:fs/promises'
import { existsSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'

import { Session, startDriver } from './webdriver.mjs'

const BINARY = resolve(
  import.meta.dirname,
  '../../src-tauri/target/debug',
  process.platform === 'win32' ? 'agentrq-tauri.exe' : 'agentrq-tauri'
)

/** Where each OS keeps what the app writes, pointed at `home`. */
function dataEnv(home) {
  if (process.platform === 'win32') return { APPDATA: home, LOCALAPPDATA: home }
  if (process.platform === 'darwin') return { HOME: home }
  return { XDG_DATA_HOME: home, XDG_CONFIG_HOME: home, XDG_CACHE_HOME: home }
}

/**
 * @param {object} options
 * @param {string} [options.serverUrl] Pin the app to this server, as
 *   AGENTRQ_SERVER_URL does for a user. Left out, the app starts unconfigured.
 * @param {object} [options.env] More environment for the app.
 * @param {string} [options.home] A data folder the caller keeps, and removes:
 *   a relaunch on it starts where the last run left off, signed in.
 * @returns {Promise<{session: Session, close: () => Promise<void>}>}
 */
export async function launchApp({ serverUrl, env: extra, home: kept } = {}) {
  if (!existsSync(BINARY)) {
    throw new Error(`${BINARY} is missing: run \`npm run build:debug\` first`)
  }
  const home = kept ?? (await mkdtemp(join(tmpdir(), 'agentrq-verify-app-')))
  const env = {
    ...dataEnv(home),
    // The app is run under Xvfb in CI, where the DMA-BUF renderer draws nothing.
    WEBKIT_DISABLE_DMABUF_RENDERER: '1',
    ...extra,
  }
  if (serverUrl) env.AGENTRQ_SERVER_URL = serverUrl
  else delete process.env.AGENTRQ_SERVER_URL

  const driver = await startDriver({
    port: process.env.VERIFY_DRIVER_PORT ? Number(process.env.VERIFY_DRIVER_PORT) : undefined,
    env,
    nativeDriver: process.env.VERIFY_NATIVE_DRIVER,
  })
  let session
  try {
    session = await Session.create(driver.base, BINARY)
  } catch (err) {
    await driver.stop()
    if (!kept) await rm(home, { recursive: true, force: true }).catch(() => {})
    throw err
  }
  return {
    session,
    close: async () => {
      await session.quit()
      await driver.stop()
      if (!kept) await rm(home, { recursive: true, force: true }).catch(() => {})
    },
  }
}
