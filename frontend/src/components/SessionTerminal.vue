<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
-->
<script setup>
/**
 * A terminal attached to an agent session.
 *
 * Deliberately thin: the wire is `useTerminalSession` and the page's own state
 * is `useTerminalView`, both tested. What is left here is wiring xterm to
 * them, which is the part that needs a real browser to mean anything.
 *
 * ## It must never size itself
 *
 * The fit addon reads the host element's box and sets the terminal's rows to
 * match. If that box is content-sized, fitting makes it taller, which makes
 * the box taller, which fits again — the terminal grows until it has pushed
 * the page off the bottom of the screen. That is not hypothetical; it is what
 * this page did.
 *
 * So the host is a flex child with `min-h-0`, which gives it a definite height
 * that does not depend on its content, and the observer below refuses to act
 * on a measurement that has not actually changed.
 */
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { useTerminalSession } from '../composables/useTerminalSession'
import { TERMINAL_OPTIONS, TERMINAL_THEME } from '../composables/useTerminalView'
import { terminalSocketUrl } from '../api'

const props = defineProps({
  sessionId: { type: String, required: true },
  /** Stops the terminal accepting input once the session is over. */
  ended: { type: Boolean, default: false },
})
const emit = defineEmits(['status', 'control', 'exit'])

const host = ref(null)
const failed = ref('')

let term = null
let fit = null
let session = null
let observer = null
let frame = 0
let lastSize = ''

/**
 * Re-fit, at most once a frame, and only when it changes something.
 *
 * Both halves matter. The frame keeps a drag from fitting on every pixel, and
 * the comparison is what stops a fit that changed nothing from being fed back
 * in as a reason to fit again.
 */
function refit() {
  if (frame) return
  frame = requestAnimationFrame(() => {
    frame = 0
    if (!term || !fit || !host.value) return
    // A hidden element measures zero, and fitting to zero throws away the
    // terminal's dimensions for when it comes back.
    if (host.value.clientHeight < 2 || host.value.clientWidth < 2) return

    fit.fit()
    const size = `${term.cols}x${term.rows}`
    if (size === lastSize) return
    lastSize = size
    session?.sendResize(term.cols, term.rows)
  })
}

onMounted(async () => {
  term = new Terminal({ ...TERMINAL_OPTIONS, theme: TERMINAL_THEME })
  fit = new FitAddon()
  term.loadAddon(fit)
  term.open(host.value)
  refit()

  let url
  try {
    url = await terminalSocketUrl(props.sessionId)
  } catch (e) {
    failed.value = e?.message || 'Could not work out where the server is.'
    return
  }

  session = useTerminalSession({
    sessionId: props.sessionId,
    connect: () => new WebSocket(url),
    onOutput: (bytes) => term.write(bytes),
    onReplay: () => term.reset(),
    onControl: (payload) => emit('control', payload),
    onExit: (code) => emit('exit', code),
    onStatus: (s) => emit('status', s),
  })

  // Exactly the bytes the key produced. Esc is 0x1b and gets no special
  // handling, which is what makes the next key nobody has thought about work.
  term.onData((data) => {
    if (props.ended) return
    session.sendInput(data)
  })

  session.open()
  session.sendResize(term.cols, term.rows)

  observer = new ResizeObserver(refit)
  observer.observe(host.value)
  window.addEventListener('resize', refit)
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', refit)
  if (frame) cancelAnimationFrame(frame)
  observer?.disconnect()
  session?.close()
  term?.dispose()
})

/** Focus the terminal when somebody clicks anywhere in it. */
function focusTerminal() {
  term?.focus()
}

const surface = computed(() => TERMINAL_THEME.background)
</script>

<template>
  <!-- min-h-0 is what makes this a terminal rather than a growing box: without
       it a flex child refuses to shrink below its content, and the content is
       exactly what the fit addon is sizing from. -->
  <div
    class="flex flex-col min-h-0 flex-1 rounded-xl overflow-hidden border border-zinc-800"
    :style="{ backgroundColor: surface }"
    @click="focusTerminal"
  >
    <!-- The chrome is dark in both themes on purpose: a terminal is a
         terminal, and the app's own light surface around it is what tells you
         which is which. -->
    <div
      class="shrink-0 flex items-center gap-2 px-3 py-2 border-b border-zinc-800 bg-zinc-900/60"
    >
      <span class="flex gap-1.5">
        <span class="h-2.5 w-2.5 rounded-full bg-zinc-700" />
        <span class="h-2.5 w-2.5 rounded-full bg-zinc-700" />
        <span class="h-2.5 w-2.5 rounded-full bg-zinc-700" />
      </span>
      <slot name="title" />
      <div class="ml-auto flex items-center gap-3">
        <slot name="actions" />
      </div>
    </div>

    <p v-if="failed" class="px-3 py-2 text-[11px] text-red-400">{{ failed }}</p>

    <!-- The terminal's own box. flex-1 min-h-0 gives it a definite height that
         does not depend on what is inside it. -->
    <div ref="host" class="flex-1 min-h-0 px-2 py-1.5" />
  </div>
</template>

<style scoped>
/* xterm measures its own viewport, so it has to fill the host exactly rather
   than sit inside it at its natural size. */
:deep(.xterm),
:deep(.xterm-viewport),
:deep(.xterm-screen) {
  height: 100% !important;
}

/* The scrollbar xterm draws is the browser default, which on a dark terminal
   is a pale bar down the side. */
:deep(.xterm-viewport) {
  scrollbar-width: thin;
  scrollbar-color: #3f3f46 transparent;
}
:deep(.xterm-viewport)::-webkit-scrollbar {
  width: 10px;
}
:deep(.xterm-viewport)::-webkit-scrollbar-thumb {
  background-color: #3f3f46;
  border-radius: 6px;
}
:deep(.xterm-viewport)::-webkit-scrollbar-track {
  background: transparent;
}
</style>
