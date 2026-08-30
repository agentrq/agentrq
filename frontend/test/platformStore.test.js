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
