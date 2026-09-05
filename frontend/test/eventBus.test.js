import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { createApp, defineComponent, h, ref } from 'vue'

import { useEventBus } from '../src/useEventBus'

/**
 * A stand-in for the browser's EventSource that lets a test drive the stream:
 * open it, push a message, fail it.
 */
class FakeEventSource {
  static instances = []

  constructor(url) {
    this.url = url
    this.closed = false
    this.onopen = null
    this.onerror = null
    this.onmessage = null
    FakeEventSource.instances.push(this)
  }

  close() {
    this.closed = true
  }

  open() {
    this.onopen?.()
  }

  send(payload) {
    this.onmessage?.({ data: JSON.stringify(payload) })
  }

  sendRaw(data) {
    this.onmessage?.({ data })
  }

  fail() {
    this.onerror?.(new Error('stream died'))
  }

  static last() {
    return FakeEventSource.instances[FakeEventSource.instances.length - 1]
  }
}

const connectedEvent = (workspaceId, connected) => ({
  type: 'agent.connected',
  payload: { workspaceId, connected },
})

let fetchMock

beforeEach(() => {
  FakeEventSource.instances = []
  vi.stubGlobal('EventSource', FakeEventSource)
  fetchMock = vi.fn().mockResolvedValue({ status: 200 })
  vi.stubGlobal('fetch', fetchMock)
  delete window.__AGENTRQ_BASE_PATH__
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('useEventBus', () => {
  describe('the URL it opens', () => {
    it('uses the user-wide stream when no workspace is named', () => {
      useEventBus().connect()

      expect(FakeEventSource.last().url).toBe('/api/v1/events/stream')
    })

    it('scopes to one workspace when given an ID', () => {
      useEventBus('0ZzhYQG2qtl').connect()

      expect(FakeEventSource.last().url).toBe('/api/v1/workspaces/0ZzhYQG2qtl/events')
    })

    it('unwraps a ref, so a route param can be passed straight in', () => {
      const id = ref('0iAx25vra8v')
      useEventBus(id).connect()

      expect(FakeEventSource.last().url).toBe('/api/v1/workspaces/0iAx25vra8v/events')
    })

    it('honours the base path the backend injects for a sub-path deployment', () => {
      window.__AGENTRQ_BASE_PATH__ = '/agentrq/'

      useEventBus().connect()

      expect(FakeEventSource.last().url).toBe('/agentrq/api/v1/events/stream')
    })

    it('opens one stream however many times connect is called', () => {
      const bus = useEventBus()

      bus.connect()
      bus.connect()

      expect(FakeEventSource.instances).toHaveLength(1)
    })
  })

  describe('onEvent', () => {
    // The reason this exists. A Vue watcher on the buffer is scheduled, so two
    // events arriving in one flush collapse into a single callback and a
    // handler reading the last element loses the earlier one — which for
    // `agent.connected` means an indicator that never moves.
    it('delivers every event, not just the last of a burst', () => {
      const bus = useEventBus()
      const seen = []
      bus.onEvent((e) => seen.push(e))
      bus.connect()

      FakeEventSource.last().send(connectedEvent('a', true))
      FakeEventSource.last().send(connectedEvent('b', true))

      expect(seen.map(e => e.payload.workspaceId)).toEqual(['a', 'b'])
    })

    it('delivers to every handler', () => {
      const bus = useEventBus()
      const first = vi.fn()
      const second = vi.fn()
      bus.onEvent(first)
      bus.onEvent(second)
      bus.connect()

      FakeEventSource.last().send(connectedEvent('a', true))

      expect(first).toHaveBeenCalledOnce()
      expect(second).toHaveBeenCalledOnce()
    })

    it('stops delivering once unsubscribed', () => {
      const bus = useEventBus()
      const handler = vi.fn()
      const off = bus.onEvent(handler)
      bus.connect()

      off()
      FakeEventSource.last().send(connectedEvent('a', true))

      expect(handler).not.toHaveBeenCalled()
    })

    it('keeps the other handlers running when one throws', () => {
      const err = vi.spyOn(console, 'error').mockImplementation(() => {})
      const bus = useEventBus()
      const survivor = vi.fn()
      bus.onEvent(() => { throw new Error('handler blew up') })
      bus.onEvent(survivor)
      bus.connect()

      FakeEventSource.last().send(connectedEvent('a', true))

      expect(survivor).toHaveBeenCalledOnce()
      expect(err).toHaveBeenCalled()
    })

    it('drops a malformed frame without reaching the handlers', () => {
      const err = vi.spyOn(console, 'error').mockImplementation(() => {})
      const bus = useEventBus()
      const handler = vi.fn()
      bus.onEvent(handler)
      bus.connect()

      FakeEventSource.last().sendRaw('not json')

      expect(handler).not.toHaveBeenCalled()
      expect(bus.events.value).toEqual([])
      expect(err).toHaveBeenCalled()
    })
  })

  describe('the buffer', () => {
    it('collects events for views that render the whole list', () => {
      const bus = useEventBus()
      bus.connect()

      FakeEventSource.last().send(connectedEvent('a', true))

      expect(bus.events.value).toEqual([connectedEvent('a', true)])
    })

    // The shell holds its stream open for the life of the app. An array nobody
    // reads would grow for every event that had ever arrived.
    it('stays empty when the subscriber asked not to buffer', () => {
      const bus = useEventBus(undefined, { buffer: false })
      const handler = vi.fn()
      bus.onEvent(handler)
      bus.connect()

      FakeEventSource.last().send(connectedEvent('a', true))

      expect(bus.events.value).toEqual([])
      expect(handler).toHaveBeenCalledOnce()
    })

    it('starts clean on a reconnect, so nothing is replayed as new', () => {
      const bus = useEventBus()
      bus.connect()
      FakeEventSource.last().send(connectedEvent('a', true))

      bus.disconnect()
      bus.connect()

      expect(bus.events.value).toEqual([])
    })
  })

  describe('connection state', () => {
    it('reports connected once the stream opens', () => {
      const bus = useEventBus()
      bus.connect()
      expect(bus.isConnected.value).toBe(false)

      FakeEventSource.last().open()

      expect(bus.isConnected.value).toBe(true)
    })

    it('reports disconnected on failure', async () => {
      vi.spyOn(console, 'error').mockImplementation(() => {})
      const bus = useEventBus()
      bus.connect()
      FakeEventSource.last().open()

      FakeEventSource.last().fail()

      expect(bus.isConnected.value).toBe(false)
    })

    it('reports disconnected when closed deliberately', () => {
      const bus = useEventBus()
      bus.connect()
      FakeEventSource.last().open()

      bus.disconnect()

      expect(bus.isConnected.value).toBe(false)
      expect(FakeEventSource.instances[0].closed).toBe(true)
    })

    it('does nothing when disconnecting a stream that was never opened', () => {
      const bus = useEventBus()

      bus.disconnect()

      expect(FakeEventSource.instances).toHaveLength(0)
    })
  })

  describe('inside a component', () => {
    /** Mount a component that runs `setup`, and return a function to unmount it. */
    const mount = (setup) => {
      const app = createApp(defineComponent({ setup, render: () => h('div') }))
      app.mount(document.createElement('div'))
      return () => app.unmount()
    }

    it('closes the stream when the component goes away', () => {
      let bus
      const unmount = mount(() => {
        bus = useEventBus()
        bus.connect()
      })

      unmount()

      expect(FakeEventSource.instances[0].closed).toBe(true)
      expect(bus.isConnected.value).toBe(false)
    })

    it('drops a handler registered in a component that has unmounted', () => {
      const handler = vi.fn()
      let bus
      const unmount = mount(() => {
        bus = useEventBus()
        bus.onEvent(handler)
        bus.connect()
      })
      const source = FakeEventSource.instances[0]

      unmount()
      source.send(connectedEvent('a', true))

      expect(handler).not.toHaveBeenCalled()
    })
  })

  describe('reconnection', () => {
    it('retries after a second, then backs off', async () => {
      vi.spyOn(console, 'error').mockImplementation(() => {})
      const bus = useEventBus()
      bus.connect()

      FakeEventSource.last().fail()
      await vi.advanceTimersByTimeAsync(0)
      expect(FakeEventSource.instances).toHaveLength(1)

      await vi.advanceTimersByTimeAsync(1000)
      expect(FakeEventSource.instances).toHaveLength(2)

      FakeEventSource.last().fail()
      await vi.advanceTimersByTimeAsync(1000)
      expect(FakeEventSource.instances).toHaveLength(2)

      await vi.advanceTimersByTimeAsync(1000)
      expect(FakeEventSource.instances).toHaveLength(3)
    })

    it('caps the backoff at thirty seconds', async () => {
      vi.spyOn(console, 'error').mockImplementation(() => {})
      const bus = useEventBus()
      bus.connect()

      // 1s, 2s, 4s, 8s, 16s, 30s, 30s...
      for (const delay of [1000, 2000, 4000, 8000, 16000, 30000]) {
        FakeEventSource.last().fail()
        await vi.advanceTimersByTimeAsync(0)
        const before = FakeEventSource.instances.length
        await vi.advanceTimersByTimeAsync(delay)
        expect(FakeEventSource.instances.length).toBe(before + 1)
      }

      FakeEventSource.last().fail()
      await vi.advanceTimersByTimeAsync(0)
      const before = FakeEventSource.instances.length
      await vi.advanceTimersByTimeAsync(30000)
      expect(FakeEventSource.instances.length).toBe(before + 1)
    })

    it('resets the backoff once a stream opens again', async () => {
      vi.spyOn(console, 'error').mockImplementation(() => {})
      const bus = useEventBus()
      bus.connect()

      FakeEventSource.last().fail()
      await vi.advanceTimersByTimeAsync(1000)
      FakeEventSource.last().fail()
      await vi.advanceTimersByTimeAsync(2000)
      FakeEventSource.last().open()

      FakeEventSource.last().fail()
      await vi.advanceTimersByTimeAsync(0)
      const before = FakeEventSource.instances.length
      await vi.advanceTimersByTimeAsync(1000)
      expect(FakeEventSource.instances.length).toBe(before + 1)
    })

    it('stops retrying and goes to the login page when the session is gone', async () => {
      vi.spyOn(console, 'error').mockImplementation(() => {})
      const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
      fetchMock.mockResolvedValue({ status: 401 })
      const href = vi.fn()
      vi.stubGlobal('location', { pathname: '/workspaces/a', set href(v) { href(v) } })

      const bus = useEventBus()
      bus.connect()
      FakeEventSource.last().fail()
      await vi.advanceTimersByTimeAsync(60000)

      expect(href).toHaveBeenCalledWith('/login')
      expect(FakeEventSource.instances).toHaveLength(1)
      expect(warn).toHaveBeenCalled()
    })

    it('keeps the base path when redirecting to login', async () => {
      vi.spyOn(console, 'error').mockImplementation(() => {})
      vi.spyOn(console, 'warn').mockImplementation(() => {})
      window.__AGENTRQ_BASE_PATH__ = '/agentrq/'
      fetchMock.mockResolvedValue({ status: 401 })
      const href = vi.fn()
      vi.stubGlobal('location', { pathname: '/agentrq/workspaces/a', set href(v) { href(v) } })

      useEventBus().connect()
      FakeEventSource.last().fail()
      await vi.advanceTimersByTimeAsync(0)

      expect(href).toHaveBeenCalledWith('/agentrq/login')
    })

    it('does not navigate when already on the login page', async () => {
      vi.spyOn(console, 'error').mockImplementation(() => {})
      vi.spyOn(console, 'warn').mockImplementation(() => {})
      fetchMock.mockResolvedValue({ status: 401 })
      const href = vi.fn()
      vi.stubGlobal('location', { pathname: '/login', set href(v) { href(v) } })

      useEventBus().connect()
      FakeEventSource.last().fail()
      await vi.advanceTimersByTimeAsync(0)

      expect(href).not.toHaveBeenCalled()
    })

    it('keeps retrying when the auth check itself cannot be reached', async () => {
      vi.spyOn(console, 'error').mockImplementation(() => {})
      fetchMock.mockRejectedValue(new Error('offline'))

      useEventBus().connect()
      FakeEventSource.last().fail()
      await vi.advanceTimersByTimeAsync(1000)

      expect(FakeEventSource.instances).toHaveLength(2)
    })

    // Every stream errors at once when the backend restarts. One auth check
    // between them, not one per stream.
    it('shares a single auth check across simultaneous failures', async () => {
      vi.spyOn(console, 'error').mockImplementation(() => {})
      let resolveAuth
      fetchMock.mockReturnValue(new Promise((r) => { resolveAuth = r }))

      const first = useEventBus()
      const second = useEventBus('0ZzhYQG2qtl')
      first.connect()
      second.connect()
      FakeEventSource.instances[0].fail()
      FakeEventSource.instances[1].fail()
      await vi.advanceTimersByTimeAsync(0)

      expect(fetchMock).toHaveBeenCalledOnce()

      resolveAuth({ status: 200 })
      await vi.advanceTimersByTimeAsync(1000)
    })
  })
})
