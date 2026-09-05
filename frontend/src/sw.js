import { precacheAndRoute, cleanupOutdatedCaches } from 'workbox-precaching'
import { registerRoute } from 'workbox-routing'
import { CacheFirst, NetworkFirst } from 'workbox-strategies'
import { CacheableResponsePlugin } from 'workbox-cacheable-response'

import {
  attachmentUrlsForWorkspace,
  budgetFor,
  evictionPlan,
  isAttachmentRequest,
  isCacheableApiRead,
  withinSizeCap,
} from './composables/useAttachmentCache'

self.addEventListener('message', event => {
  if (event.data && event.data.type === 'SKIP_WAITING') self.skipWaiting()
  // Clearing a workspace has to reach the attachment bytes too, and only the
  // worker can reach the cache they live in.
  if (event.data && event.data.type === 'FORGET_WORKSPACE') {
    event.waitUntil(forgetWorkspace(event.data.workspaceId))
  }
})

precacheAndRoute(self.__WB_MANIFEST)
cleanupOutdatedCaches()

const ATTACHMENT_CACHE = 'attachment-cache'

/**
 * Attachment bytes.
 *
 * CacheFirst because an attachment is immutable: its id names that exact file,
 * so a cached copy can never be out of date. This is also what makes them work
 * offline with no component change — the page asks for the same URL it always
 * has, and the worker answers it.
 */
registerRoute(
  ({ url }) => isAttachmentRequest(url.pathname),
  new CacheFirst({
    cacheName: ATTACHMENT_CACHE,
    plugins: [
      new CacheableResponsePlugin({ statuses: [0, 200] }),
      {
        // Too large to be worth the space. Returning null tells Workbox to
        // serve the response without keeping it.
        cacheWillUpdate: async ({ response }) => (withinSizeCap(response) ? response : null),
        // Serving an entry makes it the newest, which is what turns Cache
        // Storage's insertion order into a recency list with nothing else to
        // keep in step.
        cachedResponseWillBeUsed: async ({ cache, request, cachedResponse }) => {
          if (cachedResponse) {
            await cache.delete(request)
            await cache.put(request, cachedResponse.clone())
          }
          return cachedResponse
        },
        cacheDidUpdate: async ({ cacheName }) => enforceBudget(cacheName),
      },
    ],
  })
)

/**
 * Everything else under /api/ that nothing else claims.
 *
 * Deliberately no longer the task reads: the local database owns those now, and
 * a second cache with a different lifetime answering the same question disagrees
 * with it in a way that looks like a database bug. What is left — the workspace
 * and user endpoints — is what the shell needs to render at all.
 */
registerRoute(
  ({ url }) => isCacheableApiRead(url.pathname),
  new NetworkFirst({
    cacheName: 'api-cache',
    networkTimeoutSeconds: 10,
    plugins: [new CacheableResponsePlugin({ statuses: [0, 200] })],
  })
)

/** Drop the least recently used attachments until the cache fits its budget. */
async function enforceBudget(cacheName) {
  const cache = await caches.open(cacheName)
  const requests = await cache.keys()
  const entries = await Promise.all(
    requests.map(async request => {
      const response = await cache.match(request)
      const declared = Number(response?.headers?.get('content-length'))
      return { url: request.url, size: Number.isFinite(declared) ? declared : 0 }
    })
  )

  // The platform is not knowable from inside the worker, and only the web build
  // registers one at all — so the web budget is the only one that applies here.
  for (const url of evictionPlan(entries, budgetFor('web'))) await cache.delete(url)
}

/** Forget one workspace's cached attachment bytes. */
async function forgetWorkspace(workspaceId) {
  const cache = await caches.open(ATTACHMENT_CACHE)
  const urls = (await cache.keys()).map(request => request.url)
  for (const url of attachmentUrlsForWorkspace(urls, workspaceId)) await cache.delete(url)
}

self.addEventListener('push', event => {
  const data = event.data?.json() ?? {}
  const title = data.title || 'AgentRQ'
  const options = {
    body: data.body || '',
    icon: '/pwa-192x192.png',
    badge: '/pwa-192x192.png',
    data: { url: data.url || '/' },
    tag: data.tag || 'agentrq',
    renotify: true,
  }
  event.waitUntil(self.registration.showNotification(title, options))
})

self.addEventListener('notificationclick', event => {
  event.notification.close()
  const url = event.notification.data?.url || '/'
  event.waitUntil(
    clients.matchAll({ type: 'window', includeUncontrolled: true }).then(clientList => {
      for (const client of clientList) {
        if (client.url.endsWith(url) && 'focus' in client) return client.focus()
      }
      if (clients.openWindow) return clients.openWindow(url)
    })
  )
})
