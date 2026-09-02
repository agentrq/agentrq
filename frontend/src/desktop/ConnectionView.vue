<template>
  <!-- The window's drag handle. This screen is the first thing a macOS user
       sees, and with the title bar hidden there is otherwise nothing on the
       window to grab. A sibling of the dialog rather than a child: it is part
       of the window, not part of the dialog. -->
  <div v-if="isMac" class="app-drag fixed top-0 inset-x-0 h-10 z-[60]" aria-hidden="true"></div>

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
          Connect to your workspace server
        </p>
      </div>

      <form class="space-y-4" @submit.prevent="connect">
        <input
          ref="inputRef"
          v-model="serverUrl"
          type="text"
          inputmode="url"
          spellcheck="false"
          autocapitalize="off"
          placeholder="https://app.agentrq.com"
          :disabled="busy"
          class="w-full px-4 py-4 bg-gray-50 dark:bg-zinc-800/50 text-gray-900 dark:text-zinc-50 border border-gray-100 dark:border-zinc-700/50 rounded-2xl outline-none focus:ring-4 focus:ring-black/5 dark:focus:ring-white/10 focus:border-black dark:focus:border-white transition-all text-center placeholder:text-gray-500 disabled:opacity-50"
          required
        />

        <div v-if="errorMsg" class="p-4 bg-red-50 dark:bg-red-900/20 border border-red-100 dark:border-red-900/30 rounded-2xl">
          <p class="text-sm text-red-600 dark:text-red-400 font-medium text-center">{{ errorMsg }}</p>
        </div>

        <button
          type="submit"
          :disabled="busy || !serverUrl.trim()"
          class="w-full py-4 bg-gray-900 dark:bg-zinc-800 text-white dark:text-zinc-200 font-bold rounded-2xl hover:bg-black dark:hover:bg-zinc-700 transform active:scale-[0.98] transition-all shadow-sm disabled:opacity-50 disabled:cursor-not-allowed"
        >
          {{ connecting ? 'Connecting...' : 'Connect' }}
        </button>

        <button
          v-if="canCancel"
          type="button"
          :disabled="busy"
          @click="cancel"
          class="w-full py-4 text-gray-600 dark:text-zinc-400 font-bold rounded-2xl border border-gray-100 dark:border-zinc-800 hover:bg-gray-50 dark:hover:bg-zinc-800/50 transform active:scale-[0.98] transition-all disabled:opacity-50 disabled:cursor-not-allowed"
        >
          {{ cancelling ? 'Going back...' : 'Cancel' }}
        </button>
      </form>

      <p class="mt-6 text-center text-xs text-gray-500 dark:text-zinc-500 leading-relaxed">
        Point this at the AgentRQ server you run — on this machine, your network, or a hosted instance.
      </p>

      <div class="mt-8 flex items-center justify-between pt-6 border-t border-gray-100 dark:border-zinc-800 text-[11px] font-bold text-gray-500 dark:text-zinc-500">
        <span class="opacity-70">Desktop</span>
        <span class="opacity-40">&copy; 2026 AgentRQ</span>
      </div>
    </div>
  </div>
</template>

<script setup>
/**
 * First-run connection screen for the desktop app: which AgentRQ server to talk
 * to. The browser build is served *by* a server and can never need this, so the
 * view is never added to the shared route table — the desktop bootstrap mounts
 * it directly when nothing is configured yet.
 *
 * It lives under frontend/ rather than desktop/ for one concrete reason:
 * Tailwind's source detection is rooted at the stylesheet in frontend/src, so a
 * class used only in desktop/ is silently dropped from the bundle. Pointing an
 * `@source` at desktop/ is not an option either — the production Docker build
 * copies only ./frontend, so that path would not exist there. Keeping the file
 * here is what makes it actually get styled. Do not move it.
 */
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

import { useConnectionCancel } from './useConnectionCancel'

const props = defineProps({
  /** Prefilled when the user is switching away from a server they had. */
  initialUrl: { type: String, default: '' },
  /**
   * Whether there is a profile to go back to.
   *
   * The screen is reached two ways. On a first run it is the only thing the
   * app can show, and there is nowhere to go. After "Add profile" it is a step
   * the user chose to take, and one they must be able to back out of — so the
   * shell decides, and this says which of the two it is.
   */
  canCancel: { type: Boolean, default: false },
  /**
   * Whether the shell hid the title bar, which it does only on macOS. The
   * card is centred, so nothing here collides with the traffic lights — the
   * window just needs somewhere to be grabbed by.
   */
  isMac: { type: Boolean, default: false },
})

const serverUrl = ref(props.initialUrl || 'https://app.agentrq.com')
const connecting = ref(false)
const errorMsg = ref('')
const inputRef = ref(null)

const { cancelling, run: runCancel } = useConnectionCancel()

/** Either action ends this window, so neither should be offered twice. */
const busy = computed(() => connecting.value || cancelling.value)

// Escape is what a dialog is expected to answer to, and this one has been
// modal over the whole window since before there was any way out of it.
function onKeydown(event) {
  if (event.key === 'Escape') cancel()
}

onMounted(() => {
  inputRef.value?.focus()
  inputRef.value?.select()
  if (props.canCancel) window.addEventListener('keydown', onKeydown)
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown)
})

/**
 * Abandon this profile and return to the one it was added from.
 *
 * The half-made profile is discarded by the shell rather than left in the
 * switcher: it points at no server, so it can do nothing, and there is no UI
 * to delete it later.
 */
async function cancel() {
  if (!props.canCancel || busy.value) return

  // Cleared while the request is in flight, so a stale failure is not left on
  // screen beside a button that is already disabled.
  errorMsg.value = ''
  errorMsg.value = await runCancel()
}

async function connect() {
  // `busy`, not `connecting`: pressing Enter would otherwise still submit
  // while the window is on its way out.
  if (busy.value) return

  connecting.value = true
  errorMsg.value = ''

  try {
    // The main process validates by probing the server before it stores
    // anything, so a typo is caught here rather than surfacing later as a
    // mysterious failure to sign in.
    const result = await window.agentrq.connection.save(serverUrl.value)
    if (!result.ok) {
      errorMsg.value = result.reason
      return
    }
    // On success the main process reloads the window, so this view goes away
    // on its own — leave the button disabled until it does.
  } catch (err) {
    errorMsg.value = String(err?.message ?? err)
  } finally {
    connecting.value = false
  }
}
</script>
