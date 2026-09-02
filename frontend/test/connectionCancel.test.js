import { describe, it, expect, vi, afterEach } from 'vitest'

import { NO_PROFILE_TO_RETURN_TO, useConnectionCancel } from '../src/desktop/useConnectionCancel'

/** Install a fake desktop bridge whose cancel answers however the test needs. */
function withBridge(cancel) {
  window.agentrq = { connection: { cancel } }
  return cancel
}

afterEach(() => {
  delete window.agentrq
})

describe('useConnectionCancel', () => {
  it('reports nothing to show when the shell takes the window away', async () => {
    withBridge(vi.fn(async () => true))
    const { run } = useConnectionCancel()

    expect(await run()).toBe('')
  })

  it('stays disabled after a successful cancel', async () => {
    // The window is being replaced. Re-enabling the buttons for those few
    // frames would only offer an action that no longer means anything.
    withBridge(vi.fn(async () => true))
    const { cancelling, run } = useConnectionCancel()

    await run()

    expect(cancelling.value).toBe(true)
  })

  it('explains itself when the shell declines', async () => {
    // A button that silently does nothing is worse than one that says why.
    withBridge(vi.fn(async () => false))
    const { cancelling, run } = useConnectionCancel()

    expect(await run()).toBe(NO_PROFILE_TO_RETURN_TO)
    expect(cancelling.value).toBe(false)
  })

  it('surfaces a failed call and lets the user try again', async () => {
    withBridge(vi.fn(async () => {
      throw new Error('IPC went away')
    }))
    const { cancelling, run } = useConnectionCancel()

    expect(await run()).toBe('IPC went away')
    expect(cancelling.value).toBe(false)
  })

  it('describes a rejection that is not an Error', async () => {
    withBridge(vi.fn(async () => Promise.reject('nope')))
    const { run } = useConnectionCancel()

    expect(await run()).toBe('nope')
  })

  it('asks the shell once however many times the button is pressed', async () => {
    // Two presses would ask the shell to discard a profile it is already
    // discarding.
    let release
    const cancel = withBridge(vi.fn(() => new Promise((resolve) => { release = resolve })))
    const { run } = useConnectionCancel()

    const first = run()
    expect(await run()).toBe('')
    expect(cancel).toHaveBeenCalledTimes(1)

    release(true)
    await first
  })
})
