import { describe, it, expect, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

import { usePlatformStore } from '../src/stores/platformStore'

describe('platformStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('starts as the browser, the build with no extra capabilities', () => {
    const store = usePlatformStore()

    expect(store.platform).toBe('web')
    expect(store.isWeb).toBe(true)
    expect(store.isDesktop).toBe(false)
  })

  it('records the desktop platform', () => {
    const store = usePlatformStore()
    store.setPlatform('desktop')

    expect(store.platform).toBe('desktop')
    expect(store.isDesktop).toBe(true)
    expect(store.isWeb).toBe(false)
  })

  it('records the web platform', () => {
    const store = usePlatformStore()
    store.setPlatform('desktop')
    store.setPlatform('web')

    expect(store.isWeb).toBe(true)
  })

  it('knows the desktop build on macOS, which draws its own window chrome', () => {
    const store = usePlatformStore()
    store.setPlatform('desktop', 'darwin')

    expect(store.os).toBe('darwin')
    expect(store.isMacDesktop).toBe(true)
  })

  it('is not macOS chrome on another desktop OS', () => {
    // Windows and Linux keep a real title bar, so the page must not start
    // reserving space for buttons that are not there.
    const store = usePlatformStore()
    store.setPlatform('desktop', 'win32')

    expect(store.isMacDesktop).toBe(false)
  })

  it('never remembers an OS for the browser', () => {
    // A page drawing window chrome in a browser tab would be drawing chrome
    // for a window it does not own.
    const store = usePlatformStore()
    store.setPlatform('web', 'darwin')

    expect(store.os).toBe('')
    expect(store.isMacDesktop).toBe(false)
  })

  it('records no OS when the shell does not name one', () => {
    const store = usePlatformStore()

    for (const value of [undefined, null, 42]) {
      store.setPlatform('desktop', value)
      expect(store.os).toBe('')
      expect(store.isMacDesktop).toBe(false)
    }
  })

  it('falls back to web for anything unrecognised', () => {
    // Guessing 'desktop' from an unknown value would hand a browser build
    // affordances it cannot deliver, so the safe direction is the other one.
    const store = usePlatformStore()

    for (const value of ['mobile', '', null, undefined, 42]) {
      store.setPlatform('desktop')
      store.setPlatform(value)
      expect(store.isWeb).toBe(true)
    }
  })
})
