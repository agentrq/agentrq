<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
-->
<script setup>
/**
 * A terminal attached to an agent session.
 *
 * Deliberately thin: everything that could be wrong — framing, reconnection,
 * the resize debounce, replay handling — lives in `useTerminalSession` and is
 * tested there. What is left here is wiring xterm to it, which is the part
 * that needs a real browser to mean anything.
 */
import { ref, onMounted, onBeforeUnmount, computed } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { useTerminalSession, RESIZE_DEBOUNCE_MS } from '../composables/useTerminalSession'
import { terminalSocketUrl } from '../api'

const props = defineProps({
  sessionId: { type: String, required: true },
})
const emit = defineEmits(['exit'])

const host = ref(null)
const status = ref('connecting')
const viewers = ref([])
const self = ref(0)

let term = null
let fit = null
let session = null
let observer = null

/**
 * Two browsers on one terminal is allowed — that is how one person shows
 * another what is happening — but it is never left implicit. Somebody typing
 * into a terminal that answers with keystrokes they did not press should be
 * able to see why.
 *
 * Which entry is us comes from the server rather than by matching on name:
 * the same person in two tabs is two identical names, and there is no way to
 * tell them apart from the list alone.
 */
const others = computed(() => viewers.value.filter((_, i) => i !== self.value))

const statusLabel = computed(
  () =>
    ({
      connecting: 'Connecting',
      connected: 'Connected',
      reconnecting: 'Reconnecting',
      disconnected: 'Disconnected',
      closed: 'Closed',
    })[status.value] ?? status.value
)

/** A presence announcement arrives as a control frame on the same socket. */
function handleControl(payload) {
  try {
    const message = JSON.parse(new TextDecoder().decode(payload))
    if (message.op !== 'presence') return
    viewers.value = message.body?.viewers ?? []
    self.value = message.body?.you ?? 0
  } catch {
    // A control frame this build does not understand is not worth breaking
    // the terminal over.
  }
}

onMounted(async () => {
  term = new Terminal({
    convertEol: false,
    cursorBlink: true,
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
    fontSize: 13,
  })
  fit = new FitAddon()
  term.loadAddon(fit)
  term.open(host.value)
  fit.fit()

  const url = await terminalSocketUrl(props.sessionId)

  session = useTerminalSession({
    sessionId: props.sessionId,
    connect: () => new WebSocket(url),
    onOutput: (bytes) => term.write(bytes),
    onReplay: () => term.reset(),
    onExit: (code) => emit('exit', code),
    onStatus: (s) => (status.value = s),
    onControl: handleControl,
  })

  // Exactly the bytes the key produced. Esc is 0x1b and gets no special
  // handling, which is what makes the next key nobody has thought about work
  // too.
  term.onData((data) => session.sendInput(data))
  session.open()
  session.sendResize(term.cols, term.rows)

  observer = new ResizeObserver(() => {
    fit.fit()
    session.sendResize(term.cols, term.rows)
  })
  observer.observe(host.value)
})

onBeforeUnmount(() => {
  observer?.disconnect()
  session?.close()
  term?.dispose()
})
</script>

<template>
  <div class="flex flex-col rounded-xl border border-gray-100 bg-black">
    <div class="flex items-center gap-3 border-b border-gray-800 px-3 py-2">
      <span class="text-[11px] font-black uppercase tracking-widest text-gray-400">
        {{ statusLabel }}
      </span>
      <span
        v-if="others.length > 0"
        class="text-[11px] font-black uppercase tracking-widest text-amber-400"
      >
        Shared with {{ others.join(', ') }}
      </span>
    </div>
    <div ref="host" class="min-h-64 flex-1 p-2" />
  </div>
</template>
