// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import { setActivePinia, createPinia } from 'pinia'

import { routes, clickOutside, createAuthGuard, createAgentRQApp } from '../src/app'
import { usePlatformStore } from '../src/stores/platformStore'

/** Flatten the route tree so nested paths can be asserted on directly. */
function flatten(list, parent = '') {
  return list.flatMap((route) => {
    const full = route.path.startsWith('/')
      ? route.path
      : `${parent.replace(/\/$/, '')}/${route.path}`
    return [full, ...flatten(route.children ?? [], full)]
  })
}

describe('routes', () => {
  // This is the parity contract: both builds mount this one table, so a route
  // that disappears here disappears from the desktop app too.
  it('covers every view the app can reach', () => {
    expect(flatten(routes)).toEqual([
      '/',
      '/workspaces/new',
      '/tasks/:filter',
      '/tasks/:filter/:workspaceId/:taskId',
      '/tasks/:filter/:workspaceId/:taskId/instances',
      '/workspaces/:id',
      '/workspaces/:id/board',
      '/workspaces/:id/analytics',
      '/workspaces/:id/settings',
      '/workspaces/:id/settings/skills/:name',
      '/workspaces/:id/tasks/:taskId',
      '/workspaces/:id/tasks/:taskId/instances',
      '/workspaces/:id/tasks/new',
      '/workspaces/:id/tasks/:taskId/edit',
      '/kanban',
      '/events',
      '/events/:id',
      // Desktop-only in practice, but it lives in the one route table like
      // everything else — the desktop build mounts exactly this list.
      '/extensions',
      '/extensions/:name/:pageId',
      '/machines',
      '/machines/:id',
      '/skills',
      '/skills/:name',
      '/sessions/:id',
      '/workflows',
      '/workflows/:id',
      '/login',
    ])
  })

  it('opens the create form at /workspaces/new, not a workspace with the id "new"', () => {
    const router = createRouter({ history: createMemoryHistory(), routes })
    const resolved = router.resolve('/workspaces/new')
    expect(resolved.matched).toHaveLength(1)
    expect(resolved.matched[0].path).toBe('/workspaces/new')
    expect(router.resolve('/workspaces/abc').matched[0].path).toBe('/workspaces/:id')
  })

  it('marks only the login route public', () => {
    const publicRoutes = routes.filter((r) => r.meta?.public).map((r) => r.path)
    expect(publicRoutes).toEqual(['/login'])
  })

  it('lazy-loads every view, so no route pulls its component into the entry chunk', () => {
    for (const route of routes) {
      expect(typeof route.component).toBe('function')
    }
  })

  // Slower than everything else here by three orders of magnitude, and the
  // timeout is deliberate rather than generous: this resolves every view in
  // the application for real, and the work is Vite transforming each one's
  // whole dependency tree. At the default 5s it sat ~30ms under the limit on
  // an idle machine, so it did not fail because something broke — it failed
  // whenever the rest of the suite was competing for the same CPU, which is
  // most runs. A long-running test asking for the time it takes is honest;
  // one that passes only on an unloaded machine teaches people to ignore red.
  it('resolves every route component', async () => {
    // Actually invoking each loader is the point: a mistyped path in a dynamic
    // import is invisible until someone navigates there, and the desktop build
    // inherits the same table, so a break here breaks both apps.
    const loaders = []
    const collect = (list) => {
      for (const route of list) {
        if (route.component) loaders.push([route.path, route.component])
        collect(route.children ?? [])
      }
    }
    collect(routes)

    // Concurrently, because these are independent imports and awaiting them
    // one at a time serialised every transform in the application. The path is
    // carried alongside so a failure still names the route that broke, which
    // is the whole value of the assertion.
    const resolved = await Promise.all(
      loaders.map(async ([path, loader]) => [path, await loader()])
    )

    // Every route in the table got a loader invoked, rather than most of them:
    // a route that somehow carries no component would otherwise be skipped
    // silently, and skipping is the one failure this test cannot report.
    expect(resolved.length).toBe(flatten(routes).length)
    for (const [path, module] of resolved) {
      expect(module.default, `${path} resolved to a module with no default export`).toBeTruthy()
    }
  }, 30_000)
})

describe('createAuthGuard', () => {
  it('lets a public route through without asking who is signed in', async () => {
    const fetchUser = vi.fn()
    const next = vi.fn()

    await createAuthGuard(fetchUser)({ meta: { public: true } }, {}, next)

    expect(fetchUser).not.toHaveBeenCalled()
    expect(next).toHaveBeenCalledWith()
  })

  it('allows a signed-in user through', async () => {
    const next = vi.fn()

    await createAuthGuard(async () => ({ id: 'user-1' }))({ meta: {} }, {}, next)

    expect(next).toHaveBeenCalledWith()
  })

  it('redirects to login when there is no user', async () => {
    const next = vi.fn()

    await createAuthGuard(async () => null)({ meta: {} }, {}, next)

    expect(next).toHaveBeenCalledWith('/login')
  })

  it('redirects to login when the lookup fails', async () => {
    // A network error means we cannot confirm the session, and an unconfirmed
    // session must not be treated as a valid one.
    const next = vi.fn()

    await createAuthGuard(async () => {
      throw new Error('offline')
    })({ meta: {} }, {}, next)

    expect(next).toHaveBeenCalledWith('/login')
  })

  it('treats a route with no meta as protected', async () => {
    const next = vi.fn()

    await createAuthGuard(async () => null)({ meta: {} }, {}, next)

    expect(next).toHaveBeenCalledWith('/login')
  })
})

describe('clickOutside directive', () => {
  let el

  beforeEach(() => {
    el = document.createElement('div')
    document.body.appendChild(el)
  })

  afterEach(() => {
    el.remove()
  })

  it('fires when the click lands outside the element', () => {
    const handler = vi.fn()
    clickOutside.mounted(el, { value: handler })

    const outside = document.createElement('button')
    document.body.appendChild(outside)
    outside.click()

    expect(handler).toHaveBeenCalledOnce()
    outside.remove()
  })

  it('stays quiet for a click on the element itself', () => {
    const handler = vi.fn()
    clickOutside.mounted(el, { value: handler })

    el.click()

    expect(handler).not.toHaveBeenCalled()
  })

  it('stays quiet for a click on a descendant', () => {
    // Without this, clicking anything inside an open dropdown would close it.
    const handler = vi.fn()
    const child = document.createElement('span')
    el.appendChild(child)
    clickOutside.mounted(el, { value: handler })

    child.click()

    expect(handler).not.toHaveBeenCalled()
  })

  it('stops listening once unmounted', () => {
    const handler = vi.fn()
    clickOutside.mounted(el, { value: handler })
    clickOutside.unmounted(el)

    const outside = document.createElement('button')
    document.body.appendChild(outside)
    outside.click()

    expect(handler).not.toHaveBeenCalled()
    outside.remove()
  })
})

describe('createAgentRQApp', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('builds an app wired to the shared route table', () => {
    const { app, router, pinia } = createAgentRQApp({ history: createMemoryHistory() })

    expect(app).toBeTruthy()
    expect(pinia).toBeTruthy()
    expect(router.getRoutes().length).toBeGreaterThanOrEqual(routes.length)
    expect(router.resolve('/login').matched).toHaveLength(1)
    expect(router.resolve('/workspaces/abc/board').matched).toHaveLength(2)
  })

  it('records the platform it was created for', () => {
    const { pinia } = createAgentRQApp({ history: createMemoryHistory(), platform: 'desktop' })

    expect(usePlatformStore(pinia).isDesktop).toBe(true)
  })

  it('defaults to the web platform', () => {
    const { pinia } = createAgentRQApp({ history: createMemoryHistory() })

    expect(usePlatformStore(pinia).isWeb).toBe(true)
  })

  it('carries the shell OS through, so the app knows it owns the window chrome', () => {
    const { pinia } = createAgentRQApp({
      history: createMemoryHistory(),
      platform: 'desktop',
      os: 'darwin',
    })

    expect(usePlatformStore(pinia).isMacDesktop).toBe(true)
  })

  it('records no OS when none is given', () => {
    const { pinia } = createAgentRQApp({ history: createMemoryHistory(), platform: 'desktop' })

    expect(usePlatformStore(pinia).os).toBe('')
  })

  it('registers the click-outside directive globally', () => {
    const { app } = createAgentRQApp({ history: createMemoryHistory() })

    expect(app.directive('click-outside')).toBe(clickOutside)
  })

  it('returns the app unmounted, so a caller can set up before it renders', () => {
    const { app } = createAgentRQApp({ history: createMemoryHistory() })

    expect(app._container).toBeFalsy()
  })

  it('guards navigation to a protected route', async () => {
    // Exercises the real guard the factory installs, rather than a stand-in:
    // with no session, a protected route must land on /login.
    const { router } = createAgentRQApp({ history: createMemoryHistory() })
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 401 }))

    await router.push('/events')

    expect(router.currentRoute.value.path).toBe('/login')
    vi.restoreAllMocks()
  })
})
