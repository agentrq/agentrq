// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi, afterEach } from 'vitest'

import { isOfferable, progressLabel, useRegisterSW } from '../src/desktop/useDesktopUpdates'

/** Install a fake desktop bridge and return the status callback it captured. */
function withBridge({ installNow = vi.fn(), installViaScript = vi.fn() } = {}) {
  let emit = () => {}
  window.agentrq = {
    updates: {
      onStatus: (callback) => {
        emit = callback
      },
      installNow,
      installViaScript,
    },
  }
  return { emit: (state) => emit(state), installNow, installViaScript }
}

afterEach(() => {
  delete window.agentrq
})

describe('useRegisterSW on the desktop', () => {
  it('matches the shape App.vue destructures', () => {
    // App.vue does `const { needRefresh, updateServiceWorker } = useRegisterSW()`
    // and writes to needRefresh, so it has to be a writable ref.
    const { needRefresh, offlineReady, updateServiceWorker } = useRegisterSW()

    expect(needRefresh.value).toBe(false)
    expect(offlineReady.value).toBe(false)
    expect(typeof updateServiceWorker).toBe('function')

    needRefresh.value = true
    expect(needRefresh.value).toBe(true)
  })

  it('raises the banner only once an update is downloaded and waiting', () => {
    // 'ready' is the one state where restarting achieves anything; prompting
    // during a download would offer a restart that does nothing.
    const bridge = withBridge()
    const { needRefresh } = useRegisterSW()

    for (const status of ['checking', 'available', 'downloading', 'up-to-date', 'error', 'disabled']) {
      bridge.emit({ status })
      expect(needRefresh.value, status).toBe(false)
    }

    bridge.emit({ status: 'ready' })
    expect(needRefresh.value).toBe(true)
  })

  it('keeps the banner up when a later check moves the state on', () => {
    // The banner used to be recomputed on every status, so the next background
    // check took it away with nobody having dismissed it — four times an hour
    // at the current interval. An update does not stop being available because
    // we looked again.
    const bridge = withBridge()
    const { needRefresh } = useRegisterSW()

    bridge.emit({ status: 'ready', version: '1.2.0' })
    for (const status of ['checking', 'up-to-date', 'error', 'downloading']) {
      bridge.emit({ status })
      expect(needRefresh.value, status).toBe(true)
    }
  })

  it('stays down once dismissed, for that version', () => {
    // The other half of the same rule: only the user takes it down, and it
    // must not spring back on the next check fifteen minutes later.
    const bridge = withBridge()
    const { needRefresh } = useRegisterSW()

    bridge.emit({ status: 'ready', version: '1.2.0' })
    needRefresh.value = false

    bridge.emit({ status: 'ready', version: '1.2.0' })
    expect(needRefresh.value).toBe(false)
  })

  it('offers again when a newer version arrives', () => {
    // Dismissing 1.2.0 says nothing about 1.3.0.
    const bridge = withBridge()
    const { needRefresh } = useRegisterSW()

    bridge.emit({ status: 'ready', version: '1.2.0' })
    needRefresh.value = false

    bridge.emit({ status: 'ready', version: '1.3.0' })
    expect(needRefresh.value).toBe(true)
  })

  it('falls back to the installer when a restart-install is refused', async () => {
    // The banner is already up from 'ready'; the failure switches the route
    // rather than taking the offer away.
    const bridge = withBridge()
    const { needRefresh, updateServiceWorker } = useRegisterSW()

    bridge.emit({ status: 'ready', version: '1.2.0', canInstallViaScript: true })
    bridge.emit({
      status: 'error',
      version: '1.2.0',
      remedy: 'curl -fsSL https://agentrq.com/install.sh | sh',
      canInstallViaScript: true,
    })

    expect(needRefresh.value).toBe(true)
    await updateServiceWorker(true)

    expect(bridge.installViaScript).toHaveBeenCalledOnce()
    expect(bridge.installNow).not.toHaveBeenCalled()
  })

  it('offers the installer to a build that cannot install what it downloads', async () => {
    // An unsigned macOS build never reaches 'ready' — Squirrel.Mac refuses the
    // swap — so the offer has to rest on 'available', with the installer behind
    // it. Restarting would achieve nothing here.
    const bridge = withBridge()
    const { needRefresh, updateServiceWorker } = useRegisterSW()

    bridge.emit({ status: 'available', version: '1.2.0', canInstallViaScript: true })
    expect(needRefresh.value).toBe(true)

    await updateServiceWorker(true)

    expect(bridge.installViaScript).toHaveBeenCalledOnce()
    expect(bridge.installNow).not.toHaveBeenCalled()
  })

  it('restarts rather than reinstalling when the build can install itself', async () => {
    const bridge = withBridge()
    const { updateServiceWorker } = useRegisterSW()

    bridge.emit({ status: 'ready', version: '1.2.0' })
    await updateServiceWorker(true)

    expect(bridge.installNow).toHaveBeenCalledOnce()
    expect(bridge.installViaScript).not.toHaveBeenCalled()
  })

  it('installs when App.vue calls updateServiceWorker', async () => {
    const bridge = withBridge()
    const { updateServiceWorker } = useRegisterSW()

    await updateServiceWorker(true)

    expect(bridge.installNow).toHaveBeenCalledOnce()
  })

  it('stays inert with no bridge, exactly as the plain stub does', async () => {
    // This is what the frontend's own test run and any non-Electron context see.
    const { needRefresh, updateServiceWorker } = useRegisterSW()

    expect(needRefresh.value).toBe(false)
    await expect(updateServiceWorker(true)).resolves.toBeUndefined()
  })

  it('stays inert when the bridge exists without an updates surface', async () => {
    window.agentrq = { isDesktop: true }
    const { needRefresh, updateServiceWorker } = useRegisterSW()

    expect(needRefresh.value).toBe(false)
    await expect(updateServiceWorker(true)).resolves.toBeUndefined()
  })
})

describe('progress once the user clicks Update now', () => {
  const unsigned = { status: 'available', version: '1.2.0', canInstallViaScript: true }

  /** What App.vue does on click: clear the banner, then update. */
  async function clickUpdate(sw) {
    sw.needRefresh.value = false
    await sw.updateServiceWorker(true)
  }

  it('shows nothing before anyone asks, even while a download runs', () => {
    // A background download is not something the user asked to watch.
    const bridge = withBridge()
    const { progress } = useRegisterSW()

    bridge.emit({ status: 'downloading', progress: { phase: 'downloading', percent: 40 } })
    expect(progress.value).toBeNull()
  })

  it('follows the installer from click to finish', async () => {
    const bridge = withBridge({ installViaScript: vi.fn(async () => ({ ok: true })) })
    const sw = useRegisterSW()
    bridge.emit(unsigned)

    await clickUpdate(sw)
    expect(sw.progress.value).toEqual({ phase: 'preparing', percent: null, version: '1.2.0' })

    bridge.emit({ status: 'installing', progress: { phase: 'downloading', percent: 42.5 } })
    expect(sw.progress.value).toEqual({ phase: 'downloading', percent: 42.5, version: '1.2.0' })

    // A status with nothing to say about progress leaves the bar where it is.
    bridge.emit({ status: 'installing' })
    bridge.emit({ status: 'checking' })
    expect(sw.progress.value.percent).toBe(42.5)

    bridge.emit({ status: 'installing', progress: { phase: 'installing', percent: null } })
    expect(sw.progress.value.phase).toBe('installing')

    bridge.emit({ status: 'installed', detail: 'Restart AgentRQ to finish updating' })
    expect(sw.progress.value).toEqual({ phase: 'installed', percent: 100, version: '1.2.0' })

    sw.dismissProgress()
    expect(sw.progress.value).toBeNull()
  })

  it('offers the update again when the installer fails', async () => {
    // Clicking cleared needRefresh, which reads as a dismissal; it must not
    // keep the offer down after a failure the user never saw coming.
    const bridge = withBridge({ installViaScript: vi.fn(async () => ({ ok: true })) })
    const sw = useRegisterSW()
    bridge.emit(unsigned)
    await clickUpdate(sw)

    bridge.emit({ ...unsigned, status: 'error', detail: 'download failed', remedy: 'curl …' })

    expect(sw.progress.value).toBeNull()
    expect(sw.needRefresh.value).toBe(true)
  })

  it('offers it again when the installer would not start', async () => {
    const bridge = withBridge({ installViaScript: vi.fn(async () => ({ ok: false, reason: 'EPERM' })) })
    const sw = useRegisterSW()
    bridge.emit(unsigned)

    await clickUpdate(sw)

    expect(sw.progress.value).toBeNull()
    expect(sw.needRefresh.value).toBe(true)
  })

  it('shows a restart for an update that is already downloaded', async () => {
    const bridge = withBridge({ installNow: vi.fn(async () => true) })
    const sw = useRegisterSW()
    bridge.emit({ status: 'ready', version: '1.2.0' })

    await clickUpdate(sw)

    expect(sw.progress.value).toEqual({ phase: 'restarting', percent: null, version: '1.2.0' })
    expect(sw.needRefresh.value).toBe(false)
  })

  it('puts the offer back when the restart is refused', async () => {
    const bridge = withBridge({ installNow: vi.fn(async () => false) })
    const sw = useRegisterSW()
    bridge.emit({ status: 'ready', version: '1.2.0' })

    await clickUpdate(sw)

    expect(sw.progress.value).toBeNull()
    expect(sw.needRefresh.value).toBe(true)
  })

  it('leaves everything alone with no bridge', async () => {
    const sw = useRegisterSW()
    await clickUpdate(sw)

    expect(sw.needRefresh.value).toBe(false)
  })
})

describe('progressLabel', () => {
  it('names the version when it is known', () => {
    expect(progressLabel({ phase: 'preparing', version: '1.2.0' })).toBe('Preparing AgentRQ 1.2.0…')
    expect(progressLabel({ phase: 'downloading', version: '1.2.0' })).toBe('Downloading AgentRQ 1.2.0…')
    expect(progressLabel({ phase: 'installing', version: '1.2.0' })).toBe('Installing AgentRQ 1.2.0…')
    expect(progressLabel({ phase: 'restarting', version: '1.2.0' })).toBe('Restarting to update…')
    expect(progressLabel({ phase: 'installed', version: '1.2.0' })).toBe(
      'AgentRQ 1.2.0 is installed. Restart AgentRQ to finish.',
    )
  })

  it('still reads when it is not', () => {
    expect(progressLabel({ phase: 'downloading' })).toBe('Downloading the update…')
    expect(progressLabel({ phase: 'installed' })).toBe('The update is installed. Restart AgentRQ to finish.')
    expect(progressLabel(null)).toBe('Preparing the update…')
  })
})

describe('isOfferable', () => {
  it('offers a downloaded update', () => {
    expect(isOfferable({ status: 'ready' })).toBe(true)
  })

  it('offers an available one only where the installer can act on it', () => {
    // Without the installer, 'available' is mid-flight: the download has not
    // finished and there is nothing to restart into yet.
    expect(isOfferable({ status: 'available', canInstallViaScript: true })).toBe(true)
    expect(isOfferable({ status: 'available' })).toBe(false)
    expect(isOfferable({ status: 'available', canInstallViaScript: false })).toBe(false)
  })

  it('keeps offering after an install refused for want of a signature', () => {
    // The other way into the same situation: the app reached 'ready', tried to
    // install, and Squirrel refused. The update is still there and the
    // installer can still apply it.
    expect(isOfferable({ status: 'error', remedy: 'curl …', canInstallViaScript: true })).toBe(true)
    // An error with no remedy is a network or packaging problem the installer
    // would not fix, so it offers nothing.
    expect(isOfferable({ status: 'error', canInstallViaScript: true })).toBe(false)
  })

  it('offers nothing for the states in between', () => {
    for (const status of ['checking', 'downloading', 'up-to-date', 'error', 'disabled']) {
      expect(isOfferable({ status }), status).toBe(false)
      expect(isOfferable({ status, canInstallViaScript: true }), status).toBe(false)
    }
    expect(isOfferable(undefined)).toBe(false)
  })
})
