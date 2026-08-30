import { describe, it, expect, vi, afterEach } from 'vitest'

import { useRegisterSW } from '../src/desktop/useDesktopUpdates'

/** Install a fake desktop bridge and return the status callback it captured. */
function withBridge({ installNow = vi.fn() } = {}) {
  let emit = () => {}
  window.agentrq = {
    updates: {
      onStatus: (callback) => {
        emit = callback
      },
      installNow,
    },
  }
  return { emit: (state) => emit(state), installNow }
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

  it('lowers the banner if the state moves on', () => {
    const bridge = withBridge()
    const { needRefresh } = useRegisterSW()

    bridge.emit({ status: 'ready' })
    bridge.emit({ status: 'error' })

    expect(needRefresh.value).toBe(false)
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
