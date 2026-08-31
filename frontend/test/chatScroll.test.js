import { describe, it, expect, vi } from 'vitest'

import { scrollToBottom, shouldScrollOnViewChange } from '../src/composables/useChatScroll'

/** A scroll container with the geometry the real one has when full. */
const makeContainer = () => ({ scrollTop: 0, scrollHeight: 4200 })

/** Runs the callback immediately, standing in for Vue's nextTick. */
const runNow = (fn) => {
  fn()
}

describe('scrollToBottom', () => {
  it('moves the container to its newest content', () => {
    const el = makeContainer()

    scrollToBottom(() => el, runNow)

    expect(el.scrollTop).toBe(4200)
  })

  it('looks up the container after the wait, not before it', () => {
    // This is the whole bug. The chat pane is behind a v-if, so at the moment
    // the view switches back from History the ref is still null and the element
    // only exists once Vue has rendered. A guard evaluated before the wait
    // returns early, and the pane opens at the oldest message.
    let el = null
    const schedule = (fn) => {
      el = makeContainer() // the render happens here
      fn()
    }

    scrollToBottom(() => el, schedule)

    expect(el.scrollTop).toBe(4200)
  })

  it('does nothing when the container never appears', () => {
    // Switching away again before the render lands must not throw.
    expect(() => scrollToBottom(() => null, runNow)).not.toThrow()
    expect(() => scrollToBottom(() => undefined, runNow)).not.toThrow()
  })

  it('defers rather than scrolling inline', () => {
    // If it scrolled synchronously it would read a stale scrollHeight, taken
    // before the newest message had been laid out.
    const el = makeContainer()
    const schedule = vi.fn()

    scrollToBottom(() => el, schedule)

    expect(schedule).toHaveBeenCalledOnce()
    expect(el.scrollTop).toBe(0)
  })

  it('hands back what the scheduler returns, so a caller can await it', () => {
    const promise = Promise.resolve()

    expect(scrollToBottom(() => makeContainer(), () => promise)).toBe(promise)
  })
})

describe('shouldScrollOnViewChange', () => {
  it('scrolls when the chat pane comes back', () => {
    expect(shouldScrollOnViewChange('chat')).toBe(true)
  })

  it('leaves the history timeline where the reader left it', () => {
    // Being thrown to the end of a timeline you had scrolled through to find a
    // particular tool call would lose your place.
    expect(shouldScrollOnViewChange('history')).toBe(false)
  })
})
