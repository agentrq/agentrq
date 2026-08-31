import { describe, it, expect, vi, beforeEach } from 'vitest'

import { usePendingSend } from '../src/composables/usePendingSend'

/** A controllable stand-in for setInterval, so nothing has to wait. */
function fakeClock() {
  let nextId = 1
  const timers = new Map()
  return {
    setTimer: (fn) => {
      const id = nextId++
      timers.set(id, fn)
      return id
    },
    clearTimer: (id) => timers.delete(id),
    /** Advance by `n` ticks, stopping early if the timer was cleared. */
    tick: (n = 1) => {
      for (let i = 0; i < n; i++) {
        for (const fn of [...timers.values()]) fn()
      }
    },
    /** The callback a queued tick would run, captured before clearing. */
    pendingCallback: () => [...timers.values()][0],
    get running() {
      return timers.size
    },
  }
}

const TASK_A = { workspaceId: 'ws1', taskId: 'task-a' }
const TASK_B = { workspaceId: 'ws2', taskId: 'task-b' }

let deliver
let clock
let send

beforeEach(() => {
  deliver = vi.fn()
  clock = fakeClock()
  send = usePendingSend({ deliver, setTimer: clock.setTimer, clearTimer: clock.clearTimer })
})

describe('usePendingSend', () => {
  it('holds the message rather than sending it straight away', () => {
    send.start({ text: 'hello', atts: [], seconds: 3, target: TASK_A })

    expect(deliver).not.toHaveBeenCalled()
    expect(send.pending.value.secondsLeft).toBe(3)
  })

  it('counts down and delivers when it reaches zero', () => {
    send.start({ text: 'hello', atts: [], seconds: 3, target: TASK_A })

    clock.tick(2)
    expect(deliver).not.toHaveBeenCalled()
    expect(send.pending.value.secondsLeft).toBe(1)

    clock.tick(1)
    expect(deliver).toHaveBeenCalledOnce()
    expect(send.pending.value).toBeNull()
  })

  it('delivers to the task it was written in, not the one on screen now', () => {
    // The bug. The view is reused across tasks, so a countdown started in one
    // task was still running when another appeared, and posted the message
    // against whichever task happened to be showing.
    send.start({ text: 'for A', atts: [], seconds: 2, target: TASK_A })
    clock.tick(2)

    expect(deliver).toHaveBeenCalledWith(expect.objectContaining({ text: 'for A', target: TASK_A }))
  })

  it('still addresses the original task when flushed on navigation', () => {
    send.start({ text: 'for A', atts: [{ name: 'a.png' }], seconds: 30, target: TASK_A })

    // The user clicks task B while A's message is still counting down.
    send.flush()

    expect(deliver).toHaveBeenCalledWith(
      expect.objectContaining({ text: 'for A', target: TASK_A, atts: [{ name: 'a.png' }] })
    )
    expect(send.pending.value).toBeNull()
    expect(clock.running).toBe(0)
  })

  it('stops the countdown once it has been delivered', () => {
    send.start({ text: 'hello', atts: [], seconds: 1, target: TASK_A })
    clock.tick(1)
    expect(deliver).toHaveBeenCalledOnce()

    // A timer left running would deliver again, against whatever came next.
    clock.tick(5)
    expect(deliver).toHaveBeenCalledOnce()
    expect(clock.running).toBe(0)
  })

  it('gives the message back on cancel without sending it', () => {
    const atts = [{ name: 'a.png' }]
    send.start({ text: 'wait no', atts, seconds: 10, target: TASK_A })

    const held = send.cancel()

    expect(deliver).not.toHaveBeenCalled()
    expect(held).toMatchObject({ text: 'wait no', atts })
    expect(send.pending.value).toBeNull()
    expect(clock.running).toBe(0)
  })

  it('delivers the first message before holding a second', () => {
    // Otherwise starting a second send would drop the first without a trace.
    send.start({ text: 'first', atts: [], seconds: 10, target: TASK_A })
    send.start({ text: 'second', atts: [], seconds: 10, target: TASK_B })

    expect(deliver).toHaveBeenCalledOnce()
    expect(deliver).toHaveBeenCalledWith(expect.objectContaining({ text: 'first', target: TASK_A }))
    expect(send.pending.value).toMatchObject({ text: 'second', target: TASK_B })
    expect(clock.running).toBe(1)
  })

  it('drops the message on stop, for teardown', () => {
    send.start({ text: 'hello', atts: [], seconds: 10, target: TASK_A })

    send.stop()

    expect(deliver).not.toHaveBeenCalled()
    expect(send.pending.value).toBeNull()
    expect(clock.running).toBe(0)
  })

  it('does nothing when there is nothing held', () => {
    expect(send.flush()).toBeNull()
    expect(send.cancel()).toBeNull()
    expect(() => send.stop()).not.toThrow()
    expect(deliver).not.toHaveBeenCalled()
  })

  it('survives a tick that arrives after it was already resolved', () => {
    // A queued timer callback can still run after the timer was cleared, and
    // must not resurrect a message that was taken back.
    send.start({ text: 'hello', atts: [], seconds: 5, target: TASK_A })
    const queuedTick = clock.pendingCallback()
    send.cancel()

    expect(() => queuedTick()).not.toThrow()
    expect(deliver).not.toHaveBeenCalled()
    expect(send.pending.value).toBeNull()
  })

  it('uses real timers when none are injected', () => {
    vi.useFakeTimers()
    const realDeliver = vi.fn()
    const real = usePendingSend({ deliver: realDeliver })

    real.start({ text: 'hello', atts: [], seconds: 2, target: TASK_A })
    vi.advanceTimersByTime(2000)

    expect(realDeliver).toHaveBeenCalledOnce()
    vi.useRealTimers()
  })
})
