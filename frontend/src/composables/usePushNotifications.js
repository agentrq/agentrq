import { ref } from 'vue'
import { API_BASE_URL } from '../api'

const isSubscribed = ref(false)

function urlBase64ToUint8Array(base64String) {
  const padding = '='.repeat((4 - (base64String.length % 4)) % 4)
  const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/')
  const rawData = atob(base64)
  return Uint8Array.from([...rawData].map(c => c.charCodeAt(0)))
}

async function fetchVAPIDPublicKey() {
  const res = await fetch(`${API_BASE_URL}/push/vapid-public-key`)
  if (!res.ok) return null
  const data = await res.json()
  return data.publicKey || null
}

async function getOrCreateBrowserSubscription(publicKey) {
  if (!('serviceWorker' in navigator) || !('PushManager' in window)) return null
  const permission = await Notification.requestPermission()
  if (permission !== 'granted') return null
  try {
    const reg = await getServiceWorkerRegistration()
    const existing = await reg.pushManager.getSubscription()
    if (existing) return existing
    return await reg.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: urlBase64ToUint8Array(publicKey),
    })
  } catch {
    return null
  }
}

async function getServiceWorkerRegistration() {
  const timeout = new Promise((_, reject) => setTimeout(() => reject(new Error('sw timeout')), 3000))
  return Promise.race([navigator.serviceWorker.ready, timeout])
}

async function getBrowserSubscription() {
  if (!('serviceWorker' in navigator) || !('PushManager' in window)) return null
  try {
    const reg = await getServiceWorkerRegistration()
    return await reg.pushManager.getSubscription()
  } catch {
    return null
  }
}

async function saveSubscription(subscription, workspaceId) {
  const key = subscription.getKey('p256dh')
  const auth = subscription.getKey('auth')
  await fetch(`${API_BASE_URL}/push/subscribe`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      endpoint: subscription.endpoint,
      keys: {
        p256dh: btoa(String.fromCharCode(...new Uint8Array(key))),
        auth: btoa(String.fromCharCode(...new Uint8Array(auth))),
      },
      workspaceId: workspaceId || '',
    }),
  })
}

async function deleteSubscription(endpoint) {
  await fetch(`${API_BASE_URL}/push/subscribe`, {
    method: 'DELETE',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ endpoint }),
  })
}

async function deleteSubscriptionByWorkspace(endpoint, workspaceId) {
  await fetch(`${API_BASE_URL}/push/subscription`, {
    method: 'DELETE',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ endpoint, workspaceId }),
  })
}

export function usePushNotifications() {
  async function subscribe(workspaceId) {
    const publicKey = await fetchVAPIDPublicKey()
    if (!publicKey) return

    try {
      const subscription = await getOrCreateBrowserSubscription(publicKey)
      if (!subscription) return
      await saveSubscription(subscription, workspaceId)
      isSubscribed.value = true
    } catch {
      // Silently ignore — push is optional
    }
  }

  async function unsubscribe() {
    try {
      const sub = await getBrowserSubscription()
      if (!sub) return
      await deleteSubscription(sub.endpoint)
      await sub.unsubscribe()
      isSubscribed.value = false
    } catch {
      // Silently ignore
    }
  }

  async function checkWorkspaceSubscription(workspaceId) {
    if (!workspaceId) return false
    try {
      const sub = await getBrowserSubscription()
      if (!sub) return false
      const params = new URLSearchParams({ workspaceId, endpoint: sub.endpoint })
      const res = await fetch(`${API_BASE_URL}/push/subscription?${params}`)
      if (!res.ok) return false
      const data = await res.json()
      return data.subscribed === true
    } catch {
      return false
    }
  }

  async function subscribeWorkspace(workspaceId) {
    const publicKey = await fetchVAPIDPublicKey()
    if (!publicKey) return false
    try {
      const subscription = await getOrCreateBrowserSubscription(publicKey)
      if (!subscription) return false
      await saveSubscription(subscription, workspaceId)
      return true
    } catch {
      return false
    }
  }

  async function unsubscribeWorkspace(workspaceId) {
    try {
      const sub = await getBrowserSubscription()
      if (!sub) return
      await deleteSubscriptionByWorkspace(sub.endpoint, workspaceId)
    } catch {
      // Silently ignore
    }
  }

  return { subscribe, unsubscribe, isSubscribed, checkWorkspaceSubscription, subscribeWorkspace, unsubscribeWorkspace }
}
