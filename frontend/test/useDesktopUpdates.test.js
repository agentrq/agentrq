// Copyright 2026 Contextual, Inc. https://agentrq.com

import { describe, it, expect, vi, afterEach } from 'vitest'

import { isOfferable, useRegisterSW } from '../src/desktop/useDesktopUpdates'

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
