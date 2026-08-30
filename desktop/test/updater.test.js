import { describe, it, expect, vi } from 'vitest'
import { EventEmitter } from 'node:events'

import {
  UPDATE_CHECK_INTERVAL_MS,
  UpdateStatus,
  createUpdater,
  describeUpdateError,
  remedyForUpdateError,
  INSTALL_COMMAND,
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
    expect(describeUpdateError(new Error('something odd'))).toBe('something odd')
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
    expect(remedyForUpdateError(new Error('something odd'))).toBe('')
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
    expect(UPDATE_CHECK_INTERVAL_MS).toBe(6 * 60 * 60 * 1000)
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
    autoUpdater.emit('error', new Error('boom'))

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
