// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, afterEach } from 'vitest'
import { createApp, h, nextTick } from 'vue'
import { createMemoryHistory, createRouter, RouterView, useRoute } from 'vue-router'
import { routes } from '../src/app'
import { pageKey } from '../src/pageKey'

describe('pageKey', () => {
  it('keys a remount route by its path', () => {
    expect(pageKey({ path: '/events/a', meta: { remount: true } })).toBe('/events/a')
  })

  it('leaves every other route unkeyed', () => {
    expect(pageKey({ path: '/workspaces/a', meta: {} })).toBeUndefined()
    expect(pageKey({ path: '/workspaces/a' })).toBeUndefined()
  })

  it('marks the pages that read their params once', () => {
    const remounted = routes.filter(r => r.meta?.remount).map(r => r.path)
    expect(remounted).toEqual([
      '/workspaces/:id/tasks/new',
      '/workspaces/:id/tasks/:taskId/edit',
      '/events/:id',
      '/machines/:id',
      '/skills/:name',
      '/sessions/:id',
      '/workflows/:id',
    ])
  })
})

// App.vue's page slot, around pages that read `route.params` once in setup, as
// the detail views do.
describe('the page slot', () => {
  let app
  afterEach(() => app?.unmount())

  async function mountAt(table, path) {
    let setups = 0
    const OnceView = {
      setup() {
        setups++
        const id = useRoute().params.id
        return () => h('p', id)
      },
    }
    const router = createRouter({
      history: createMemoryHistory(),
      routes: table.map(r => ({ ...r, component: OnceView, children: r.children?.map(c => ({ ...c, component: OnceView })) })),
    })
    router.push(path)
    await router.isReady()
    app = createApp({
      render: () => h(RouterView, null, {
        default: ({ Component, route }) => h(Component, { key: pageKey(route) }),
      }),
    })
    app.use(router)
    const el = document.createElement('div')
    app.mount(el)
    return { el, router, setups: () => setups }
  }

  it('draws the event navigated to from another event', async () => {
    const { el, router, setups } = await mountAt(routes.filter(r => r.path === '/events/:id'), '/events/a')
    expect(el.textContent).toBe('a')
    await router.push('/events/b')
    await nextTick()
    expect(el.textContent).toBe('b')
    expect(setups()).toBe(2)
  })

  it('keeps an unmarked page mounted across a param change', async () => {
    const { el, router, setups } = await mountAt([{ path: '/workspaces/:id' }], '/workspaces/a')
    await router.push('/workspaces/b')
    await nextTick()
    expect(el.textContent).toBe('a')
    expect(setups()).toBe(1)
  })
})
