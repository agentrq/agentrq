// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi } from 'vitest'
import { EventEmitter } from 'node:events'

import {
  UPDATE_CHECK_INTERVAL_MS,
  UpdateStatus,
  createUpdater,
  describeUpdateError,
  remedyForUpdateError,
  INSTALL_COMMAND,
  INSTALL_SCRIPT_COMMAND,
  canInstallViaScript,
  parseInstallerLog,
  INSTALL_POLL_INTERVAL_MS,
  shouldAnnounce,
  updaterDisabledReason,
} from '../src/main/updater.js'

/** Stand-in for electron-updater's autoUpdater. */
function fakeAutoUpdater({ checkForUpdates } = {}) {
  const updater = new EventEmitter()
  updater.autoDownload = false
  updater.autoInstallOnAppQuit = false
  updater.checkForUpdates = checkForUpdates ?? vi.fn(async () => ({}))
  updater.quitAndInstall = vi.fn()
  return updater
}

function setup({ isPackaged = true, checkForUpdates } = {}) {
  const autoUpdater = fakeAutoUpdater({ checkForUpdates })
  const onStatus = vi.fn()
  const setTimer = vi.fn(() => 'timer-id')
  const clearTimer = vi.fn()
  const logger = { warn: vi.fn() }

  const updater = createUpdater({ autoUpdater, isPackaged, onStatus, setTimer, clearTimer, logger })
  return { updater, autoUpdater, onStatus, setTimer, clearTimer, logger }
}

/** The status values reported, in order. */
const statuses = (onStatus) => onStatus.mock.calls.map(([state]) => state.status)

describe('updaterDisabledReason', () => {
  it('allows updating in a packaged app', () => {
    expect(updaterDisabledReason({ isPackaged: true })).toBeNull()
  })

  it('refuses in a development build', () => {
    // The hard guard: `make dev` must never reach the update path, where
    // electron-updater would try to replace a checkout with a release build.
    expect(updaterDisabledReason({ isPackaged: false })).toBe('Updates are disabled in a development build')
  })
})

describe('describeUpdateError', () => {
  it('explains the macOS signing requirement in words a person can act on', () => {
    // The predictable failure: Squirrel.Mac refuses to install onto an unsigned
    // app, and the raw error says nothing useful.
    expect(describeUpdateError(new Error('Could not get code signature for running application')))
      .toBe('This build is not signed, so it cannot update itself')
    expect(describeUpdateError(new Error('SQRLUpdater failed'))).toBe(
      'This build is not signed, so it cannot update itself'
    )
  })

  it('reports a network failure as one', () => {
    for (const message of ['net::ERR_INTERNET_DISCONNECTED', 'getaddrinfo ENOTFOUND github.com', 'ETIMEDOUT']) {
      expect(describeUpdateError(new Error(message))).toBe('Could not reach the update server')
    }
  })

  it('recognises there being nothing to update to', () => {
    expect(describeUpdateError(new Error('HttpError: 404 Not Found'))).toBe('No published release to update to')
    expect(describeUpdateError(new Error('Cannot find latest.yml'))).toBe('No published release to update to')
  })

  it('recognises a build that was never set up to update itself', () => {
    // Only a build packaged with a publish configuration carries app-update.yml;
    // an unpackaged or --dir build hits this and the raw ENOENT explains nothing.
    expect(describeUpdateError(new Error("ENOENT: no such file or directory, open '/x/app-update.yml'")))
      .toBe('This build has no update configuration')
  })

  it('passes an unrecognised message through rather than hiding it', () => {
    expect(describeUpdateError(new Error('unrecognised updater failure'))).toBe('unrecognised updater failure')
  })

  it('copes with a thrown value that is not an Error', () => {
    expect(describeUpdateError('plain string')).toBe('plain string')
    expect(describeUpdateError(null)).toBe('Unknown error')
    expect(describeUpdateError(undefined)).toBe('Unknown error')
  })
})

describe('remedyForUpdateError', () => {
  it('offers the install command for the signature failure', () => {
    // The whole point: this failure is not a dead end, and the user should not
    // have to go and find the command in the docs.
    expect(remedyForUpdateError(new Error('Could not get code signature for running application')))
      .toBe(INSTALL_COMMAND)
    expect(remedyForUpdateError(new Error('SQRLUpdater failed'))).toBe(INSTALL_COMMAND)
  })

  it('offers nothing for failures reinstalling would not fix', () => {
    // A command that cannot help is worse than no command at all.
    expect(remedyForUpdateError(new Error('net::ERR_CONNECTION_REFUSED'))).toBe('')
    expect(remedyForUpdateError(new Error('HttpError: 404 Not Found'))).toBe('')
    expect(remedyForUpdateError(new Error('unrecognised updater failure'))).toBe('')
    expect(remedyForUpdateError(undefined)).toBe('')
  })
})

describe('shouldAnnounce', () => {
  it('always announces an update that exists', () => {
    expect(shouldAnnounce(UpdateStatus.Available, { manual: false })).toBe(true)
    expect(shouldAnnounce(UpdateStatus.Ready, { manual: false })).toBe(true)
  })

  it('stays silent about a background check that found nothing', () => {
    // Six-hourly "you are up to date" toasts would be pure noise.
    expect(shouldAnnounce(UpdateStatus.UpToDate, { manual: false })).toBe(false)
    expect(shouldAnnounce(UpdateStatus.Checking, { manual: false })).toBe(false)
    expect(shouldAnnounce(UpdateStatus.Error, { manual: false })).toBe(false)
  })

  it('answers a question the user actually asked', () => {
    for (const status of [UpdateStatus.Checking, UpdateStatus.UpToDate, UpdateStatus.Error, UpdateStatus.Disabled]) {
      expect(shouldAnnounce(status, { manual: true })).toBe(true)
    }
  })
})

describe('createUpdater', () => {
  it('downloads in the background and installs on quit', () => {
    // So an update is ready the moment the user agrees, and still lands if
    // they never do.
    const { updater, autoUpdater } = setup()
    updater.start()

    expect(autoUpdater.autoDownload).toBe(true)
    expect(autoUpdater.autoInstallOnAppQuit).toBe(true)
  })

  it('checks at launch and then on a six-hourly timer', () => {
    const { updater, autoUpdater, setTimer } = setup()
    updater.start()

    expect(autoUpdater.checkForUpdates).toHaveBeenCalledOnce()
    expect(setTimer).toHaveBeenCalledWith(expect.any(Function), UPDATE_CHECK_INTERVAL_MS)
    // Fifteen minutes: a release should reach a running app the same session
    // it ships, not six hours later.
    expect(UPDATE_CHECK_INTERVAL_MS).toBe(15 * 60 * 1000)
  })

  it('keeps checking when the timer fires', () => {
    const { updater, autoUpdater, setTimer } = setup()
    updater.start()

    setTimer.mock.calls[0][0]()

    expect(autoUpdater.checkForUpdates).toHaveBeenCalledTimes(2)
  })

  it('reports the whole lifecycle of a successful update', () => {
    const { updater, autoUpdater, onStatus } = setup()
    updater.start()

    autoUpdater.emit('checking-for-update')
    autoUpdater.emit('update-available', { version: '0.5.0' })
    autoUpdater.emit('download-progress', { percent: 42.4 })
    autoUpdater.emit('update-downloaded', { version: '0.5.0' })

    expect(statuses(onStatus)).toEqual([
      UpdateStatus.Checking,
      UpdateStatus.Available,
      UpdateStatus.Downloading,
      UpdateStatus.Ready,
    ])
    expect(updater.state).toMatchObject({ status: UpdateStatus.Ready, version: '0.5.0' })
  })

  it('reports download progress as a rounded percentage', () => {
    const { updater, autoUpdater, onStatus } = setup()
    updater.start()

    autoUpdater.emit('download-progress', { percent: 42.4 })
    expect(onStatus.mock.calls.at(-1)[0].detail).toBe('42%')

    autoUpdater.emit('download-progress', {})
    expect(onStatus.mock.calls.at(-1)[0].detail).toBe('0%')
  })

  it('carries the download percentage as a number the banner can draw', () => {
    const { updater, autoUpdater, onStatus } = setup()
    updater.start()

    autoUpdater.emit('download-progress', { percent: 42.4 })
    expect(onStatus.mock.calls.at(-1)[0].progress).toEqual({ phase: 'downloading', percent: 42 })

    // And it is gone again once the download is done.
    autoUpdater.emit('update-downloaded', { version: '0.5.0' })
    expect(updater.state.progress).toBeNull()
  })

  it('reports being up to date', () => {
    const { updater, autoUpdater } = setup()
    updater.start()

    autoUpdater.emit('update-not-available')

    expect(updater.state.status).toBe(UpdateStatus.UpToDate)
  })

  it('reports a failure without throwing', () => {
    const { updater, autoUpdater, logger } = setup()
    updater.start()

    autoUpdater.emit('error', new Error('net::ERR_INTERNET_DISCONNECTED'))

    expect(updater.state).toMatchObject({
      status: UpdateStatus.Error,
      detail: 'Could not reach the update server',
    })
    expect(logger.warn).toHaveBeenCalled()
  })

  it('copes with an update-available carrying no version', () => {
    const { updater, autoUpdater } = setup()
    updater.start()

    autoUpdater.emit('update-available', undefined)
    expect(updater.state.version).toBe('')
  })

  it('keeps the version from update-available if the download omits it', () => {
    const { updater, autoUpdater } = setup()
    updater.start()

    autoUpdater.emit('update-available', { version: '0.5.0' })
    autoUpdater.emit('update-downloaded', {})

    expect(updater.state.version).toBe('0.5.0')
  })

  it('marks a manual check as announceable and a background one as not', () => {
    const { updater, autoUpdater, onStatus } = setup()
    updater.start()

    autoUpdater.emit('update-not-available')
    expect(onStatus.mock.calls.at(-1)[0].announce).toBe(false)

    updater.checkNow()
    autoUpdater.emit('update-not-available')
    expect(onStatus.mock.calls.at(-1)[0].announce).toBe(true)
  })

  it('surfaces a rejected check rather than leaving an unhandled rejection', () => {
    const checkForUpdates = vi.fn(async () => {
      throw new Error('HttpError: 404')
    })
    const { updater } = setup({ checkForUpdates })

    return updater.checkNow().then((result) => {
      expect(result).toEqual({ ok: false, reason: 'No published release to update to' })
      expect(updater.state.status).toBe(UpdateStatus.Error)
    })
  })

  it('reports a successful check', async () => {
    const { updater } = setup()
    expect(await updater.checkNow()).toEqual({ ok: true })
  })

  describe('in a development build', () => {
    it('never touches the updater', () => {
      const { updater, autoUpdater, setTimer } = setup({ isPackaged: false })
      updater.start()

      expect(autoUpdater.checkForUpdates).not.toHaveBeenCalled()
      expect(setTimer).not.toHaveBeenCalled()
      expect(autoUpdater.autoDownload).toBe(false)
    })

    it('says so, rather than appearing broken', async () => {
      // The menu item is reachable in dev; silently doing nothing would look
      // like a bug.
      const { updater, onStatus } = setup({ isPackaged: false })

      const result = await updater.checkNow()

      expect(result).toEqual({ ok: false, reason: 'Updates are disabled in a development build' })
      expect(onStatus.mock.calls.at(-1)[0]).toMatchObject({
        status: UpdateStatus.Disabled,
        announce: true,
      })
    })

    it('starts in the disabled state', () => {
      const { updater } = setup({ isPackaged: false })
      expect(updater.state).toMatchObject({ status: UpdateStatus.Disabled, enabled: false })

      updater.start()
      expect(updater.state.status).toBe(UpdateStatus.Disabled)
    })
  })

  describe('installNow', () => {
    it('restarts into the new version once one is downloaded', () => {
      const { updater, autoUpdater } = setup()
      updater.start()
      autoUpdater.emit('update-downloaded', { version: '0.5.0' })

      expect(updater.installNow()).toBe(true)
      expect(autoUpdater.quitAndInstall).toHaveBeenCalledOnce()
    })

    it('does nothing when there is nothing downloaded', () => {
      // Quitting the app to install an update that does not exist would be a
      // spectacular way to lose someone's work.
      const { updater, autoUpdater } = setup()
      updater.start()

      expect(updater.installNow()).toBe(false)
      autoUpdater.emit('update-available', { version: '0.5.0' })
      expect(updater.installNow()).toBe(false)

      expect(autoUpdater.quitAndInstall).not.toHaveBeenCalled()
    })
  })

  describe('stop', () => {
    it('cancels the timer', () => {
      const { updater, clearTimer } = setup()
      updater.start()
      updater.stop()

      expect(clearTimer).toHaveBeenCalledWith('timer-id')
    })

    it('is safe when nothing was started', () => {
      const { updater, clearTimer } = setup()
      updater.stop()

      expect(clearTimer).not.toHaveBeenCalled()
    })

    it('is safe to call twice', () => {
      const { updater, clearTimer } = setup()
      updater.start()
      updater.stop()
      updater.stop()

      expect(clearTimer).toHaveBeenCalledOnce()
    })
  })

  it('falls back to console when no logger is supplied', () => {
    const autoUpdater = fakeAutoUpdater()
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const updater = createUpdater({ autoUpdater, isPackaged: true, onStatus: () => {}, setTimer: () => 1 })

    updater.start()
    autoUpdater.emit('error', new Error('update server unreachable'))

    expect(warn).toHaveBeenCalled()
    warn.mockRestore()
  })

  it('uses real timers when none are injected', async () => {
    const autoUpdater = fakeAutoUpdater()
    const updater = createUpdater({ autoUpdater, isPackaged: true, onStatus: () => {} })

    updater.start()
    expect(autoUpdater.checkForUpdates).toHaveBeenCalledOnce()
    // Leaving a six-hour interval armed would keep the test process alive.
    updater.stop()
  })
})

describe('canInstallViaScript', () => {
  it('is available where install.sh runs', () => {
    expect(canInstallViaScript('darwin')).toBe(true)
    expect(canInstallViaScript('linux')).toBe(true)
  })

  it('is not available on Windows', () => {
    // install.sh refuses Windows by design — the NSIS installer and
    // electron-updater already handle it — so a button here would only fail.
    expect(canInstallViaScript('win32')).toBe(false)
  })
})

describe('installViaScript', () => {
  /** An updater with a spawn we can inspect. */
  function withSpawn({ platform = 'darwin', spawn = vi.fn(() => ({ unref: vi.fn() })) } = {}) {
    const updater = createUpdater({
      autoUpdater: fakeAutoUpdater(),
      isPackaged: true,
      onStatus: () => {},
      spawn,
      platform,
      setTimer: vi.fn(),
      clearTimer: vi.fn(),
    })
    return { updater, spawn }
  }

  it('runs the installer detached, so it survives the app it closes', () => {
    // The installer's first act is to quit this app and wait for the process
    // to go. A child sharing our lifetime would be killed by the very thing it
    // just did, halfway through replacing the application bundle.
    const unref = vi.fn()
    const spawn = vi.fn(() => ({ unref }))
    const { updater } = withSpawn({ spawn })

    expect(updater.installViaScript()).toEqual({ ok: true })

    const [command, args, options] = spawn.mock.calls[0]
    expect(command).toBe('/bin/sh')
    expect(args).toEqual(['-c', INSTALL_SCRIPT_COMMAND])
    expect(options.detached).toBe(true)
    expect(options.stdio).toBe('ignore')
    expect(unref).toHaveBeenCalledOnce()
  })

  it('quits the running app and reopens it', () => {
    // install.sh refuses to replace a bundle that is running, and does not
    // relaunch on its own — it prints "Launch it with: open -a AgentRQ".
    expect(INSTALL_SCRIPT_COMMAND).toContain('--quit')
    expect(INSTALL_SCRIPT_COMMAND).toContain('open -a AgentRQ')
    // Chained with && so a failed install does not reopen a broken bundle.
    expect(INSTALL_SCRIPT_COMMAND).toContain('&& open -a AgentRQ')
  })

  it('refuses on a platform the installer does not support', () => {
    const { updater, spawn } = withSpawn({ platform: 'win32' })

    expect(updater.installViaScript().ok).toBe(false)
    expect(spawn).not.toHaveBeenCalled()
  })

  it('refuses when no spawn was provided', () => {
    const updater = createUpdater({
      autoUpdater: fakeAutoUpdater(),
      isPackaged: true,
      onStatus: () => {},
      platform: 'darwin',
      setTimer: vi.fn(),
      clearTimer: vi.fn(),
    })

    expect(updater.installViaScript().ok).toBe(false)
  })

  it('reports a spawn that fails rather than throwing', () => {
    const spawn = vi.fn(() => {
      throw new Error('EPERM')
    })
    const { updater } = withSpawn({ spawn })

    const result = updater.installViaScript()

    expect(result.ok).toBe(false)
    expect(updater.state.status).toBe(UpdateStatus.Error)
    // The command is still offered, so the user has a way through by hand.
    expect(updater.state.remedy).toBe(INSTALL_COMMAND)
  })

  it('tells the banner whether this route exists', () => {
    expect(withSpawn({ platform: 'darwin' }).updater.state.canInstallViaScript).toBe(true)
    expect(withSpawn({ platform: 'win32' }).updater.state.canInstallViaScript).toBe(false)
  })
})

describe('what the renderer is told', () => {
  it('carries canInstallViaScript on every published status', () => {
    // The banner decides on this, and it only ever sees what publish sends —
    // reading it from `state` instead would leave the renderer blind.
    const onStatus = vi.fn()
    const auto = fakeAutoUpdater()
    const updater = createUpdater({
      autoUpdater: auto,
      isPackaged: true,
      onStatus,
      spawn: vi.fn(() => ({ unref: vi.fn() })),
      platform: 'darwin',
      setTimer: vi.fn(),
      clearTimer: vi.fn(),
    })
    updater.start()

    auto.emit('update-available', { version: '1.2.0' })

    const published = onStatus.mock.calls.at(-1)[0]
    expect(published.canInstallViaScript).toBe(true)
    expect(published.status).toBe(UpdateStatus.Available)
    expect(published.version).toBe('1.2.0')
  })
})

describe('parseInstallerLog', () => {
  // What install.sh really writes: `say` lines, and curl's --progress-bar,
  // which redraws with a carriage return rather than a newline.
  const lookingUp = 'Looking up the latest AgentRQ release...\n'
  const downloading = `${lookingUp}Downloading AgentRQ-1.2.0-arm64.dmg...\n`

  it('starts out preparing', () => {
    expect(parseInstallerLog('')).toEqual({ phase: 'preparing', percent: null, error: '' })
    expect(parseInstallerLog(undefined).phase).toBe('preparing')
    expect(parseInstallerLog(lookingUp).phase).toBe('preparing')
  })

  it('reads the latest percentage off the progress bar', () => {
    expect(parseInstallerLog(downloading)).toEqual({ phase: 'downloading', percent: 0, error: '' })
    expect(parseInstallerLog(`${downloading}\r#=#=#   \r##      3.1%\r#####     41.7%`).percent).toBe(41.7)
    expect(parseInstallerLog(`${downloading}\r######## 100.0%\n`).percent).toBe(100)
  })

  it('ignores a percentage printed before the download began', () => {
    expect(parseInstallerLog(`50%\n${downloading}`).percent).toBe(0)
  })

  it('moves on to installing once the download is verified', () => {
    for (const line of [
      '  Checksum verified.',
      '  No published checksum for x.dmg; skipping verification.',
      '  openssl not found; skipping checksum verification.',
      'Quitting AgentRQ...',
      'Installing to /Applications/AgentRQ.app...',
    ]) {
      expect(parseInstallerLog(`${downloading}\r### 100.0%\n${line}\n`), line).toEqual({
        phase: 'installing',
        percent: null,
        error: '',
      })
    }
  })

  it('recognises a finished install, and one there was nothing to do for', () => {
    expect(parseInstallerLog('AgentRQ 1.2.0 installed to /home/me/.local/bin/agentrq').phase).toBe('installed')
    expect(parseInstallerLog('AgentRQ 1.2.0 is already installed. Nothing to do.').phase).toBe('installed')
  })

  it('picks out the reason the installer died', () => {
    const log = `${downloading}curl: (6) Could not resolve host\nerror: download failed: https://x \n`
    expect(parseInstallerLog(log).error).toBe('download failed: https://x')
  })
})

describe('following the installer', () => {
  /** A child process we can end by hand, and a log we can write to. */
  function running({ platform = 'darwin' } = {}) {
    const child = new EventEmitter()
    child.unref = vi.fn()
    let text = ''
    const log = { stdio: ['ignore', 7, 7], read: vi.fn(() => text), close: vi.fn() }
    const onStatus = vi.fn()
    const setTimer = vi.fn(() => 'poll-id')
    const clearTimer = vi.fn()
    const logger = { warn: vi.fn() }
    const spawn = vi.fn(() => child)
    const updater = createUpdater({
      autoUpdater: fakeAutoUpdater(),
      isPackaged: true,
      onStatus,
      spawn,
      platform,
      setTimer,
      clearTimer,
      logger,
      createInstallLog: () => log,
    })
    const poll = () => setTimer.mock.calls.find(([, ms]) => ms === INSTALL_POLL_INTERVAL_MS)[0]()
    return { updater, child, log, onStatus, setTimer, clearTimer, spawn, logger, poll, write: (s) => (text += s) }
  }

  it('hands the installer the log file, not a pipe', () => {
    const { updater, spawn, log } = running()

    expect(updater.installViaScript()).toEqual({ ok: true })
    expect(spawn.mock.calls[0][2]).toEqual({ detached: true, stdio: log.stdio })
  })

  it('reports each step of the install as it happens', () => {
    const { updater, onStatus, poll, write } = running()
    updater.installViaScript()

    expect(updater.state).toMatchObject({ status: UpdateStatus.Installing, progress: { phase: 'preparing', percent: null } })

    write('Downloading AgentRQ-1.2.0-arm64.dmg...\n\r##   12.5%')
    poll()
    expect(updater.state.progress).toEqual({ phase: 'downloading', percent: 12.5 })

    // Nothing new written: nothing new published.
    const published = onStatus.mock.calls.length
    poll()
    expect(onStatus.mock.calls.length).toBe(published)

    write('\r######## 100.0%\n  Checksum verified.\n')
    poll()
    expect(updater.state.progress).toEqual({ phase: 'installing', percent: null })
  })

  it('offers the update again, with the reason, when the installer fails', () => {
    const { updater, child, log, clearTimer, write } = running()
    updater.installViaScript()

    write('error: could not fetch https://api.github.com -- no such release\n')
    child.emit('exit', 1)

    expect(updater.state).toMatchObject({
      status: UpdateStatus.Error,
      detail: 'could not fetch https://api.github.com -- no such release',
      remedy: INSTALL_COMMAND,
      progress: null,
    })
    expect(clearTimer).toHaveBeenCalledWith('poll-id')
    expect(log.close).toHaveBeenCalledOnce()
  })

  it('says why when the installer stops without explaining itself', () => {
    const { updater, child } = running()
    updater.installViaScript()

    child.emit('exit', 137)

    expect(updater.state.detail).toBe('The installer stopped (exit code 137)')
  })

  it('reports a shell that never started', () => {
    const { updater, child, log } = running()
    updater.installViaScript()

    child.emit('error', new Error('spawn /bin/sh ENOENT'))
    // Node follows a failed spawn with 'exit' as well; it must not report twice.
    child.emit('exit', null)

    expect(updater.state).toMatchObject({ status: UpdateStatus.Error, detail: 'spawn /bin/sh ENOENT' })
    expect(log.close).toHaveBeenCalledOnce()
  })

  it('asks for a restart when the install finished and this app is still running', () => {
    // Linux: install.sh swaps the AppImage without quitting us, and the macOS
    // relaunch that follows it fails there — which is not the install failing.
    const { updater, child, write } = running({ platform: 'linux' })
    updater.installViaScript()

    write('AgentRQ 1.2.0 installed to /home/me/.local/bin/agentrq\n')
    child.emit('exit', 127)

    expect(updater.state).toMatchObject({ status: UpdateStatus.Installed, detail: 'Restart AgentRQ to finish updating' })
  })

  it('does not let a background check interrupt an install', async () => {
    const { updater, setTimer } = running()
    updater.start()
    updater.installViaScript()

    const scheduledCheck = setTimer.mock.calls.find(([, ms]) => ms === UPDATE_CHECK_INTERVAL_MS)[0]
    scheduledCheck()

    expect(await updater.checkNow()).toEqual({ ok: false, reason: 'An update is already being installed' })
    expect(updater.state.status).toBe(UpdateStatus.Installing)
  })

  it('closes the log when the installer cannot be started', () => {
    const log = { stdio: ['ignore', 7, 7], read: () => '', close: vi.fn() }
    const updater = createUpdater({
      autoUpdater: fakeAutoUpdater(),
      isPackaged: true,
      onStatus: () => {},
      spawn: () => {
        throw new Error('EPERM')
      },
      platform: 'darwin',
      setTimer: vi.fn(),
      clearTimer: vi.fn(),
      logger: { warn: vi.fn() },
      createInstallLog: () => log,
    })

    expect(updater.installViaScript().ok).toBe(false)
    expect(log.close).toHaveBeenCalledOnce()
  })
})
