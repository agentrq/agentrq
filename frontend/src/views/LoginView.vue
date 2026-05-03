<template>
  <div class="min-h-screen flex items-center justify-center p-6 bg-white font-inter text-zinc-900 leading-relaxed antialiased font-medium">
    <div class="max-w-md w-full p-10 bg-zinc-50 rounded-[2rem] border border-gray-100 flex flex-col items-center">
      
      <!-- App Icon from hasmcp-app style -->
      <div class="w-16 h-16 mb-8 flex items-center justify-center rounded-2xl bg-zinc-900 text-white shadow-xl shadow-black/10">
        <svg viewBox="0 0 24 24" class="w-10 h-10" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" />
          <path d="M12 7l-3.5 8" />
          <path d="M12 7l3.5 8" />
          <path d="M9.5 12h5" />
        </svg>
      </div>

      <h1 class="text-2xl font-black text-gray-900 mb-2 tracking-tight">AgentRQ</h1>
      <p class="text-[13px] font-medium text-gray-400 text-center mb-10 uppercase tracking-widest leading-relaxed">
        Autonomous Workspace Pipeline
      </p>

      <!-- Loading State -->
      <div v-if="loadingConfig" class="flex justify-center w-full py-4 text-gray-400">
        <svg class="animate-spin h-6 w-6" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
          <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
          <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
        </svg>
      </div>

      <template v-else>
        <!-- Google Login -->
        <a v-if="!rootLoginEnabled" href="/api/v1/auth/google/login" class="flex items-center justify-center w-full bg-white hover:bg-zinc-50 text-gray-900 font-black text-[11px] uppercase tracking-[0.2em] py-4 px-6 rounded-lg border border-gray-200 transition-all duration-300 group active:scale-95">
          <svg class="w-4 h-4 mr-4 group-hover:scale-110 transition-transform" viewBox="0 0 24 24">
            <path d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z" fill="#4285F4"/>
            <path d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z" fill="#34A853"/>
            <path d="M5.84 14.1c-.22-.66-.35-1.36-.35-2.1s.13-1.44.35-2.1V7.06H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.94l3.66-2.84z" fill="#FBBC05"/>
            <path d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z" fill="#EA4335"/>
          </svg>
          Continue with Google
        </a>

        <!-- Root Login Fallback -->
        <form v-else @submit.prevent="submitRootLogin" class="w-full flex flex-col gap-4">
          <input
            v-model="rootToken"
            type="password"
            placeholder="ACCESS TOKEN"
            class="w-full bg-white text-gray-900 text-[11px] font-black tracking-[0.2em] uppercase py-4 px-6 rounded-lg border border-gray-200 outline-none focus:ring-2 focus:ring-zinc-900 transition-all placeholder:text-gray-400 text-center"
            required
          />
          <button
            type="submit"
            :disabled="loggingIn"
            class="flex items-center justify-center w-full bg-zinc-900 hover:bg-zinc-800 text-white font-black text-[11px] uppercase tracking-[0.2em] py-4 px-6 rounded-lg transition-all duration-300 active:scale-95 disabled:opacity-50"
          >
            {{ loggingIn ? '...' : 'Access Pipeline' }}
          </button>
          <div v-if="errorMsg" class="text-red-500 text-[11px] font-black uppercase tracking-widest text-center mt-2">
            {{ errorMsg }}
          </div>
        </form>
      </template>

      <div class="mt-12 flex items-center justify-between w-full px-2 text-[11px] uppercase font-black tracking-widest text-gray-500">
        <div class="flex gap-6">
          <a href="https://agentrq.com/tos" target="_blank" rel="noopener" class="hover:text-black transition-colors underline decoration-gray-200 underline-offset-4">Terms</a>
          <a href="https://agentrq.com/privacy" target="_blank" rel="noopener" class="hover:text-black transition-colors underline decoration-gray-200 underline-offset-4">Privacy</a>
        </div>
        <span class="opacity-40">&copy; 2026 AgentRQ</span>
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
