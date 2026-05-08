<template>
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-gray-50/80 dark:bg-zinc-950/80 backdrop-blur-sm" aria-modal="true" role="dialog">
    <div class="w-full max-w-md p-8 bg-white dark:bg-zinc-900 rounded-3xl shadow-2xl border border-gray-100 dark:border-zinc-800">
      <div class="mb-10 text-center">
        <div class="w-16 h-16 bg-black dark:bg-white rounded-2xl flex items-center justify-center mx-auto mb-6 shadow-xl transform rotate-3">
          <svg viewBox="0 0 24 24" class="w-10 h-10 text-white dark:text-black" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" />
            <path d="M12 7l-3.5 8" />
            <path d="M12 7l3.5 8" />
            <path d="M9.5 12h5" />
          </svg>
        </div>
        <h2 class="text-3xl font-black text-gray-900 dark:text-zinc-50 tracking-tight mb-2">AgentRQ</h2>
        <p class="text-gray-500 dark:text-zinc-400 text-sm leading-relaxed">
          Autonomous Workspace Pipeline
        </p>
      </div>

      <div class="space-y-4">
        <div v-if="loadingConfig" class="flex justify-center py-6">
          <svg class="animate-spin h-6 w-6 text-gray-500" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
          </svg>
        </div>

        <template v-else>
          <a v-if="!rootLoginEnabled" href="/api/v1/auth/google/login"
            class="w-full py-4 px-6 bg-white dark:bg-zinc-800 text-gray-700 dark:text-zinc-200 font-bold rounded-2xl border border-gray-200 dark:border-zinc-700 hover:bg-gray-50 dark:hover:bg-zinc-700 flex items-center justify-center gap-3 transform active:scale-[0.98] transition-all shadow-sm">
            <svg width="20" height="20" viewBox="0 0 24 24">
              <path d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z" fill="#4285F4"/>
              <path d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z" fill="#34A853"/>
              <path d="M5.84 14.1c-.22-.66-.35-1.36-.35-2.1s.13-1.44.35-2.1V7.06H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.94l3.66-2.84z" fill="#FBBC05"/>
              <path d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.06l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z" fill="#EA4335"/>
            </svg>
            Continue with Google
          </a>

          <div v-if="rootLoginEnabled" class="relative py-4">
            <div class="absolute inset-0 flex items-center">
              <div class="w-full border-t border-gray-100 dark:border-zinc-800"></div>
            </div>
            <div class="relative flex justify-center text-xs text-gray-500">
              <span class="bg-white dark:bg-zinc-900 px-2 font-bold">or use token</span>
            </div>
          </div>

          <form v-if="rootLoginEnabled" @submit.prevent="submitRootLogin" class="space-y-4">
            <input
              v-model="rootToken"
              type="password"
              placeholder="Paste access token..."
              class="w-full px-4 py-4 bg-gray-50 dark:bg-zinc-800/50 text-gray-900 dark:text-zinc-50 border border-gray-100 dark:border-zinc-700/50 rounded-2xl outline-none focus:ring-4 focus:ring-black/5 dark:focus:ring-white/10 focus:border-black dark:focus:border-white transition-all text-center placeholder:text-gray-500"
              required
            />
            <div v-if="errorMsg" class="p-4 bg-red-50 dark:bg-red-900/20 border border-red-100 dark:border-red-900/30 rounded-2xl">
              <p class="text-sm text-red-600 dark:text-red-400 font-medium text-center">{{ errorMsg }}</p>
            </div>
            <button
              type="submit"
              :disabled="loggingIn"
              class="w-full py-4 bg-gray-900 dark:bg-zinc-800 text-white dark:text-zinc-200 font-bold rounded-2xl hover:bg-black dark:hover:bg-zinc-700 transform active:scale-[0.98] transition-all shadow-sm disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {{ loggingIn ? 'Verifying...' : 'Access Pipeline' }}
            </button>
          </form>
        </template>

        <div class="mt-8 flex items-center justify-between pt-6 border-t border-gray-100 dark:border-zinc-800 text-[11px] font-bold text-gray-500 dark:text-zinc-500">
          <div class="flex gap-4">
            <a href="https://agentrq.com/tos" target="_blank" rel="noopener" class="hover:text-black dark:hover:text-white transition-colors">Terms</a>
            <a href="https://agentrq.com/privacy" target="_blank" rel="noopener" class="hover:text-black dark:hover:text-white transition-colors">Privacy</a>
          </div>
          <span class="opacity-40">&copy; 2026 AgentRQ</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'

const router = useRouter()
const rootLoginEnabled = ref(false)
const loadingConfig = ref(true)

const rootToken = ref('')
const loggingIn = ref(false)
const errorMsg = ref('')

onMounted(async () => {
  try {
    const res = await fetch('/api/v1/auth/config')
    const data = await res.json()
    if (data && typeof data.rootLoginEnabled === 'boolean') {
      rootLoginEnabled.value = data.rootLoginEnabled
    }
  } catch (err) {
    console.warn('Failed to fetch auth config:', err)
  } finally {
    loadingConfig.value = false
  }
})

const submitRootLogin = async () => {
  if (!rootToken.value) return
  loggingIn.value = true
  errorMsg.value = ''

  try {
    const res = await fetch('/api/v1/auth/root/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ rootToken: rootToken.value })
    })

    if (res.ok) {
      localStorage.setItem('request_fullscreen', 'true')
      window.location.href = '/'
    } else {
      const data = await res.json()
      errorMsg.value = data.error || 'Invalid Token'
    }
  } catch (err) {
    errorMsg.value = 'Connection Error'
  } finally {
    loggingIn.value = false
  }
}
</script>
