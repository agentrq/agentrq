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
 * that does not depend on its content, and `useTerminalFit` refuses to act on
 * a measurement that has not actually changed.
 *
 * ## The terminal is fitted before its size is sent
 *
 * This used to run the other way round: the fit was deferred to an animation
 * frame, and the size was handed to the session in the same tick as `open()`
 * — so the first size to reach `sendResize` was xterm's default 80×24, and
 * the real one arrived a frame later.
 *
 * In practice that was almost certainly harmless, and it is worth knowing why
 * before anybody "simplifies" this back: `sendResize` keeps the last size in
 * `pendingSize` and re-sends it when the socket opens, and the socket cannot
 * open for at least a ticket fetch and a handshake — far longer than one
 * frame. So the machine got the corrected size anyway.
 *
 * It is ordered this way now because a correct first value should not depend
 * on losing a race, not because a bug was traced to it. The same goes for the
 * observer being attached before the session is built.
 */
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { useTerminalSession, VIEWER_SESSION } from '../composables/useTerminalSession'
import { useTerminalFit } from '../composables/useTerminalFit'
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
let fitter = null

/** Ask for a fit. Safe before the fitter exists, which the observer can be. */
const refit = () => fitter?.request()

onMounted(async () => {
  term = new Terminal({ ...TERMINAL_OPTIONS, theme: TERMINAL_THEME })
  fit = new FitAddon()
  term.loadAddon(fit)
  term.open(host.value)

  fitter = useTerminalFit({
    measure: () => (host.value ? { width: host.value.clientWidth, height: host.value.clientHeight } : null),
    // Asked before every fit, because it is the only thing that distinguishes
    // "the renderer has not measured a cell yet" from a real measurement —
    // `fit()` itself is silent about it.
    propose: () => fit?.proposeDimensions(),
    apply: () => {
      fit.fit()
      return { cols: term.cols, rows: term.rows }
    },
    onSize: (cols, rows) => session?.sendResize(cols, rows),
  })

  // Before the session is built, so that what fits after mount is in place
  // before anything can yield. Nothing in this body awaits today; the point is
  // that adding an await later should not be able to lose a layout change.
  observer = new ResizeObserver(refit)
  observer.observe(host.value)
  window.addEventListener('resize', refit)

  // Synchronously where it can be, so the size sent below is a measurement
  // rather than xterm's default.
  fitter.settle()

  session = useTerminalSession({
    // Not props.sessionId: that is a base62 string, and the frame header wants
    // a number the browser has no way to produce. The backend decides which
    // session this socket may drive and overwrites it.
    sessionId: VIEWER_SESSION,
    // Built per attempt, not once. The URL carries a ticket that expires in
    // about a minute — the socket cannot authenticate with the app's cookie —
    // so a URL computed here and kept would be refused by every reconnect
    // after the first minute, which is the failure nobody would think to look
    // for on a terminal left open all afternoon.
    connect: async () => new WebSocket(await terminalSocketUrl(props.sessionId)),
    onOutput: (bytes) => term.write(bytes),
    onReplay: () => term.reset(),
    onControl: (payload) => emit('control', payload),
    onExit: (code) => emit('exit', code),
    onStatus: (s) => emit('status', s),
    // Said out loud rather than retried in silence: an attempt that cannot
    // even be made is usually a permission or a server that is not there, and
    // a terminal that only says "Reconnecting" gives somebody nothing to act
    // on.
    onError: (err) => {
      failed.value = err ? err.message || 'Could not connect to that terminal.' : ''
    },
  })

  // Exactly the bytes the key produced. Esc is 0x1b and gets no special
  // handling, which is what makes the next key nobody has thought about work.
  term.onData((data) => {
    if (props.ended) return
    session.sendInput(data)
  })

  session.open()
  session.sendResize(term.cols, term.rows)
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', refit)
  fitter?.stop()
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
