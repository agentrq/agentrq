/**
 * Auto-update.
 *
 * The browser build gets new code by reloading; the desktop app has to fetch
 * and install it, which is the one capability a packaged app genuinely cannot
 * do without. Updates come from this repository's GitHub Releases, published by
 * the workflow in phase 7.
 *
 * The design rule the prompt has to respect: no native modals. The web app
 * already has exactly the right piece of UI for this — the banner App.vue shows
 * when a new service worker is waiting — so the desktop build drives that same
 * banner rather than inventing a second one. Transient states (checking, up to
 * date, failed) go through the toast system.
 *
 * electron-updater is injected rather than imported, so the whole state machine
 * is testable in plain Node with no Electron and no network.
 */

/** Six hours: frequent enough to matter, rare enough to be invisible. */
export const UPDATE_CHECK_INTERVAL_MS = 6 * 60 * 60 * 1000

export const UpdateStatus = {
  Idle: 'idle',
  Checking: 'checking',
  Available: 'available',
  Downloading: 'downloading',
  Ready: 'ready',
  UpToDate: 'up-to-date',
  Error: 'error',
  Disabled: 'disabled',
}

/**
 * Why updating is unavailable, or null when it is available.
 *
 * Running unpackaged is the important one: `make dev` must never reach the
 * update path, where electron-updater would try to replace a development
 * checkout with a release build.
 */
export function updaterDisabledReason({ isPackaged }) {
  if (!isPackaged) return 'Updates are disabled in a development build'
  return null
}

/**
 * The failure an unsigned macOS build hits every single time: Squirrel.Mac
 * validates the signature before replacing the app, and refuses.
 */
const UNSIGNED = /code signature|not signed|SQRLUpdater|codesign/i

/**
 * The one-command installer, which replaces the bundle wholesale instead of
 * asking Squirrel.Mac to patch it — so the signature check never applies.
 * Published from the agentrq-static repository (`src/install.sh`).
 */
export const INSTALL_COMMAND = 'curl -fsSL https://agentrq.com/install.sh | sh'

/**
 * Turn an updater failure into something worth showing a person.
 *
 * The signature case is called out by name because it is the predictable one:
 * macOS refuses to install an update onto an unsigned app, and the raw error
 * says nothing a user could act on.
 */
export function describeUpdateError(error) {
  const message = String(error?.message ?? error ?? 'Unknown error')

  if (UNSIGNED.test(message)) {
    return 'This build is not signed, so it cannot update itself'
  }
  if (/net::|ENOTFOUND|ETIMEDOUT|ECONNREFUSED|EAI_AGAIN/i.test(message)) {
    return 'Could not reach the update server'
  }
  if (/404|no published versions|latest.*\.yml/i.test(message)) {
    return 'No published release to update to'
  }
  // Only a build packaged with a publish configuration carries this file, so
  // its absence means the build was never set up to update itself.
  if (/app-update\.yml/i.test(message)) {
    return 'This build has no update configuration'
  }
  return message
}

/**
 * A command that gets the user past this failure, or '' when there is none.
 *
 * Only the signature case has one. Every other failure here is a network or
 * packaging problem that reinstalling would not touch, and offering a command
 * that cannot help is worse than offering nothing.
 */
export function remedyForUpdateError(error) {
  const message = String(error?.message ?? error ?? '')
  return UNSIGNED.test(message) ? INSTALL_COMMAND : ''
}

/**
 * Whether a status change should interrupt the user.
 *
 * A background check that finds nothing must stay silent — six-hourly "you are
 * up to date" toasts would be pure noise. The same status *asked for* by the
 * menu should answer, because the user is waiting for one.
 */
export function shouldAnnounce(status, { manual }) {
  if (status === UpdateStatus.Ready || status === UpdateStatus.Available) return true
  return Boolean(manual)
}

/**
 * @param {object} deps
 * @param {object} deps.autoUpdater      electron-updater's autoUpdater
 * @param {boolean} deps.isPackaged
 * @param {(state: object) => void} deps.onStatus
 * @param {typeof setInterval} [deps.setTimer]
 * @param {typeof clearInterval} [deps.clearTimer]
 * @param {{ warn: Function }} [deps.logger]
 */
export function createUpdater({
  autoUpdater,
  isPackaged,
  onStatus,
  setTimer = setInterval,
  clearTimer = clearInterval,
  logger = console,
}) {
  const disabledReason = updaterDisabledReason({ isPackaged })

  let status = disabledReason ? UpdateStatus.Disabled : UpdateStatus.Idle
  let detail = disabledReason ?? ''
  let remedy = ''
  let manual = false
  let timer = null
  let version = ''

  function publish(next, nextDetail = '', nextRemedy = '') {
    status = next
    detail = nextDetail
    remedy = nextRemedy
    onStatus({
      status,
      detail,
      remedy,
      version,
      manual,
      announce: shouldAnnounce(next, { manual }),
    })
  }

  function bindEvents() {
    autoUpdater.on('checking-for-update', () => publish(UpdateStatus.Checking))
    autoUpdater.on('update-available', (info) => {
      version = info?.version ?? ''
      publish(UpdateStatus.Available)
    })
    autoUpdater.on('download-progress', (progress) => {
      publish(UpdateStatus.Downloading, `${Math.round(progress?.percent ?? 0)}%`)
    })
    autoUpdater.on('update-downloaded', (info) => {
      version = info?.version ?? version
      publish(UpdateStatus.Ready)
    })
    autoUpdater.on('update-not-available', () => publish(UpdateStatus.UpToDate))
    autoUpdater.on('error', (error) => {
      logger.warn?.('update failed:', error)
      publish(UpdateStatus.Error, describeUpdateError(error), remedyForUpdateError(error))
    })
  }

  async function check({ manual: isManual = false } = {}) {
    if (disabledReason) {
      // Answer the question when it was actually asked, rather than silently
      // doing nothing and leaving the menu looking broken.
      manual = isManual
      publish(UpdateStatus.Disabled, disabledReason)
      return { ok: false, reason: disabledReason }
    }

    manual = isManual
    try {
      await autoUpdater.checkForUpdates()
      return { ok: true }
    } catch (error) {
      // electron-updater also emits 'error'; catching here keeps a rejected
      // promise from surfacing as an unhandled rejection.
      const reason = describeUpdateError(error)
      publish(UpdateStatus.Error, reason, remedyForUpdateError(error))
      return { ok: false, reason }
    }
  }

  return {
    /** Wire up events, check once, then keep checking on a timer. */
    start() {
      if (disabledReason) {
        publish(UpdateStatus.Disabled, disabledReason)
        return
      }

      // Download in the background so an update is ready the moment the user
      // agrees to it, and install on quit if they never do.
      autoUpdater.autoDownload = true
      autoUpdater.autoInstallOnAppQuit = true

      bindEvents()
      check({ manual: false })
      timer = setTimer(() => check({ manual: false }), UPDATE_CHECK_INTERVAL_MS)
    },

    /** The menu's "Check for Updates…". */
    checkNow: () => check({ manual: true }),

    /** Restart into the new version now. */
    installNow() {
      if (status !== UpdateStatus.Ready) return false
      autoUpdater.quitAndInstall()
      return true
    },

    stop() {
      if (timer !== null) clearTimer(timer)
      timer = null
    },

    get state() {
      return { status, detail, remedy, version, enabled: !disabledReason }
    },
  }
}
