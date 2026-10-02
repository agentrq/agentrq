<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<template>
  <aside ref="asideRef" data-side-panel :data-full="panel.state.full || undefined"
         :class="['relative h-full min-h-0 flex py-4 pr-4', panel.state.full ? 'flex-1 min-w-0' : 'shrink-0']"
         :style="panel.state.full ? null : { width: `${width}px` }">

    <!-- The drag handle: the whole gap between the main column and the panel
         is grabbable, and a grip in the middle of it is always showing, so
         that the edge can be dragged is visible before anyone hovers it. -->
    <div v-if="!panel.state.full" data-side-panel-handle role="separator" aria-orientation="vertical" aria-label="Resize side panel"
         title="Drag to resize · double-click to reset"
         class="group absolute inset-y-4 -left-4 w-4 cursor-col-resize flex items-center justify-center touch-none"
         @pointerdown="startDrag" @dblclick="panel.resetWidth()">
      <div :class="['absolute inset-y-0 w-px transition-colors', dragging ? 'bg-gray-400 dark:bg-zinc-500' : 'bg-transparent group-hover:bg-gray-300 dark:group-hover:bg-zinc-700']"></div>
      <div data-side-panel-grip
           :class="['relative w-1 h-10 rounded-full transition-colors', dragging ? 'bg-gray-500 dark:bg-zinc-400' : 'bg-gray-300 dark:bg-zinc-700 group-hover:bg-gray-400 dark:group-hover:bg-zinc-500']"></div>
    </div>

    <div class="flex-1 min-w-0 flex flex-col rounded-sm bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 overflow-hidden">

      <!-- Toolbar -->
      <div class="shrink-0 flex items-center gap-1 px-2 py-1.5 border-b border-gray-100 dark:border-zinc-800">
        <button type="button" data-side-panel-back title="Back" :disabled="!nav.canGoBack" @click="goBack" :class="iconButton">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M15 19l-7-7 7-7" /></svg>
        </button>
        <button type="button" data-side-panel-forward title="Forward" :disabled="!nav.canGoForward" @click="goForward" :class="iconButton">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7" /></svg>
        </button>
        <button type="button" data-side-panel-reload :title="nav.loading ? 'Stop' : 'Reload'" :disabled="!hasPage" @click="reloadOrStop" :class="iconButton">
          <svg v-if="nav.loading" class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
          <svg v-else class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>
        </button>

        <form class="flex-1 min-w-0" @submit.prevent="go">
          <input ref="addressRef" v-model="address" data-side-panel-address type="text" spellcheck="false" autocomplete="off"
                 aria-label="Address" placeholder="Search or enter an address"
                 @focus="$event.target.select()"
                 :class="['w-full h-7 px-2.5 rounded-md bg-gray-100 dark:bg-zinc-800 text-xs text-gray-800 dark:text-zinc-100 placeholder:text-gray-400 dark:placeholder:text-zinc-500 outline-none focus-visible:ring-2 focus-visible:ring-gray-300 dark:focus-visible:ring-zinc-600', invalid ? 'ring-2 ring-red-300 dark:ring-red-800' : '']" />
        </form>

        <button type="button" data-side-panel-permissions-toggle :aria-pressed="showPermissions"
                :title="siteDecided ? 'Site permissions · this site has some' : 'Site permissions'"
                @click="togglePermissions" :class="[iconButton, siteDecided || showPermissions ? 'text-gray-900 dark:text-white' : '']">
          <svg class="w-4 h-4" :fill="siteDecided ? 'currentColor' : 'none'" :fill-opacity="siteDecided ? 0.15 : 1" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M12 3l7 3v5c0 4.5-3 8.5-7 10-4-1.5-7-5.5-7-10V6l7-3z" /></svg>
        </button>
        <button type="button" data-side-panel-external title="Open in browser" :disabled="!isWebPage" @click="openExternal" :class="iconButton">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14" /></svg>
        </button>
        <button type="button" data-side-panel-full :title="panel.state.full ? 'Collapse to the side' : 'Expand to the whole window'"
                :aria-pressed="panel.state.full" @click="panel.toggleFull()" :class="iconButton">
          <svg v-if="panel.state.full" class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M9 4v5H4m11-5v5h5M9 20v-5H4m11 5v-5h5" /></svg>
          <svg v-else class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M4 9V4h5m6 0h5v5M4 15v5h5m6 0h5v-5" /></svg>
        </button>
        <button type="button" data-side-panel-close title="Close side panel" @click="panel.close()" :class="iconButton">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
        </button>
      </div>

      <!-- A page asking for a permission. Under the toolbar, where the page it
           came from is named, so it cannot be mistaken for the page itself. -->
      <div v-if="question" data-side-panel-permission role="alertdialog" :aria-label="questionText"
           class="shrink-0 flex items-center gap-3 px-3 py-2 border-b border-amber-200 dark:border-amber-900/60 bg-amber-50 dark:bg-amber-950/30">
        <svg class="w-4 h-4 shrink-0 text-amber-600 dark:text-amber-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M12 3l7 3v5c0 4.5-3 8.5-7 10-4-1.5-7-5.5-7-10V6l7-3z" /></svg>
        <p class="flex-1 min-w-0 text-[11px] font-medium text-gray-800 dark:text-zinc-100">{{ questionText }}</p>
        <button type="button" data-side-panel-permission-block @click="answer('block')" :class="textButton">Block</button>
        <button type="button" data-side-panel-permission-allow @click="answer('allow')"
                class="px-3 py-1.5 rounded-md bg-black dark:bg-white text-[11px] font-semibold text-white dark:text-black hover:bg-gray-800 dark:hover:bg-zinc-200 transition-colors">Allow</button>
      </div>

      <!-- The page. The element is created in script rather than declared
           here: its `src` has to be set before it is attached, and a
           `<webview>` is not an element the template compiler knows. -->
      <div class="relative flex-1 min-h-0">
        <div ref="hostRef" :class="['absolute inset-0', hasPage && !failure ? '' : 'invisible', dragging ? 'pointer-events-none' : '']"></div>

        <SidePanelPermissions v-if="showPermissions" :sites="sites" @remove="removeDecision" @close="showPermissions = false" />

        <div v-if="!hasPage" data-side-panel-empty class="absolute inset-0 flex flex-col items-center justify-center gap-2 px-8 text-center">
          <p class="text-xs font-semibold text-gray-700 dark:text-zinc-200">Nothing open</p>
          <p class="text-[11px] text-gray-500 dark:text-zinc-400">Type an address above, or click a link in a task.</p>
        </div>

        <div v-else-if="failure" data-side-panel-error class="absolute inset-0 flex flex-col items-center justify-center gap-3 px-8 text-center bg-white dark:bg-zinc-900">
          <p class="text-xs font-semibold text-gray-700 dark:text-zinc-200">This page could not be loaded</p>
          <p class="text-[11px] text-gray-500 dark:text-zinc-400 break-words max-w-full">{{ failure }}</p>
          <div class="flex items-center gap-2">
            <button type="button" data-side-panel-retry @click="retry" :class="textButton">Retry</button>
            <button v-if="isWebPage" type="button" @click="openExternal" :class="textButton">Open in browser</button>
          </div>
        </div>
      </div>
    </div>
  </aside>
</template>

<script setup>
/**
 * The desktop app's side panel.
 *
 * A `<webview>`, not a native view laid over the window, so the app's own
 * menus, dialogs and toasts still draw on top of it. What it may do is decided
 * in the main process (`desktop/src/main/side-panel/`), which overwrites every
 * guest's preferences before it is attached; nothing set here can loosen that.
 */
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { isPanelUrl, normaliseAddress, panelWidth, useSidePanel } from '../composables/useSidePanel'
import { describeQuestion, hasDecisions } from '../composables/useSitePermissions'
import SidePanelPermissions from './SidePanelPermissions.vue'

const props = defineProps({
  /** Test seam: what makes the guest element. */
  createGuest: { type: Function, default: () => document.createElement('webview') },
  /** Test seam: the shell's bridge. */
  bridge: { type: Object, default: () => globalThis.window?.agentrq?.sidePanel },
})

const panel = useSidePanel()

const iconButton = 'shrink-0 w-7 h-7 rounded-md flex items-center justify-center text-gray-500 dark:text-zinc-400 hover:bg-gray-100 dark:hover:bg-zinc-800 hover:text-gray-900 dark:hover:text-white disabled:opacity-40 disabled:pointer-events-none transition-colors outline-none focus-visible:ring-2 focus-visible:ring-gray-300 dark:focus-visible:ring-zinc-600'
const textButton = 'px-3 py-1.5 rounded-md border border-gray-200 dark:border-zinc-800 text-[11px] font-semibold text-gray-700 dark:text-zinc-300 hover:bg-gray-50 dark:hover:bg-zinc-800 transition-colors'

const asideRef = ref(null)
const hostRef = ref(null)
const addressRef = ref(null)
const address = ref(panel.state.url)
const invalid = ref(false)
const failure = ref('')
const dragging = ref(false)
const nav = reactive({ canGoBack: false, canGoForward: false, loading: false })

/** The guest element, once it exists. */
let guest = null
/** The URL the guest is showing, which is not always the one typed. */
const current = ref(panel.state.url)

const hasPage = computed(() => Boolean(current.value))

// Permissions: the question a page is asking, and what every site has been
// answered — which also lights the shield for a site that has something.
const question = ref(null)
const questionText = computed(() => (question.value ? describeQuestion(question.value) : ''))
const sites = ref([])
const showPermissions = ref(false)
const siteDecided = computed(() => hasDecisions(sites.value, current.value))

async function refreshSites() {
  sites.value = (await props.bridge?.permissions?.()) ?? []
}

async function answer(decision) {
  const asked = question.value
  question.value = null
  await props.bridge?.answerPermission?.(asked.id, decision)
  await refreshSites()
}

async function removeDecision(origin, permission) {
  sites.value = (await props.bridge?.removePermission?.(origin, permission)) ?? []
}

function togglePermissions() {
  showPermissions.value = !showPermissions.value
  if (showPermissions.value) refreshSites()
}

const stops = []
const isWebPage = computed(() => /^https?:/i.test(current.value))

/**
 * How much room the panel and the main column share: the two of them, and not
 * the sidebar beside them. Their sum does not change as the edge is dragged,
 * only when the window or the sidebar does.
 */
function measure() {
  const aside = asideRef.value
  const shared = (aside?.previousElementSibling?.offsetWidth ?? 0) + (aside?.offsetWidth ?? 0)
  return shared || aside?.parentElement?.clientWidth || globalThis.innerWidth || Infinity
}

/** Kept current as the window resizes, so a panel that fills it follows it. */
const room = ref(Infinity)
const width = computed(() => panelWidth(panel.state.width, room.value))

function refreshNav() {
  nav.canGoBack = Boolean(guest?.canGoBack?.())
  nav.canGoForward = Boolean(guest?.canGoForward?.())
}

/** Load `url` in the guest, creating the guest on first use. */
function show(url) {
  failure.value = ''
  current.value = url
  address.value = url
  if (!guest) {
    mountGuest(url)
    return
  }
  guest.loadURL(url)
}

function mountGuest(url) {
  guest = props.createGuest()
  guest.setAttribute('src', url)
  // White, as a browser's canvas is: a guest is transparent by default, and a
  // page that sets no background of its own would otherwise be dark text on
  // the app's dark theme.
  guest.className = 'absolute inset-0 w-full h-full bg-white'
  guest.addEventListener('did-start-loading', () => { nav.loading = true })
  guest.addEventListener('did-stop-loading', () => { nav.loading = false; refreshNav() })
  guest.addEventListener('did-navigate', onNavigate)
  guest.addEventListener('did-navigate-in-page', (event) => { if (event.isMainFrame) onNavigate(event) })
  guest.addEventListener('did-fail-load', (event) => {
    // -3 is "aborted": a navigation replaced by another, which is not a failure.
    if (event.errorCode === -3 || event.isMainFrame === false) return
    failure.value = event.errorDescription || `Error ${event.errorCode}`
    nav.loading = false
  })
  hostRef.value.append(guest)
}

function onNavigate(event) {
  current.value = event.url
  if (document.activeElement !== addressRef.value) address.value = event.url
  failure.value = ''
  panel.setUrl(event.url)
  refreshNav()
}

function goBack() {
  guest?.goBack()
}

function goForward() {
  guest?.goForward()
}

function go() {
  const url = normaliseAddress(address.value)
  invalid.value = !url
  if (url) show(url)
}

function reloadOrStop() {
  if (nav.loading) guest?.stop()
  else guest?.reload()
}

function retry() {
  failure.value = ''
  if (guest) guest.reload()
}

function openExternal() {
  if (isWebPage.value) props.bridge?.openExternal(current.value)
}

/** A page the shell or a link asked for, after the panel is already showing. */
watch(() => panel.state.url, (url) => {
  if (url && url !== current.value && isPanelUrl(url)) show(url)
})

// Dragging. Pointer capture keeps the events coming once the pointer is over
// the guest, and the guest is made transparent to the pointer as well: it is a
// separate page, and would otherwise swallow the drag the moment it got there.
let dragStart = null

function startDrag(event) {
  if (event.button !== 0) return
  event.preventDefault()
  dragStart = { x: event.clientX, width: width.value }
  dragging.value = true
  event.currentTarget.setPointerCapture?.(event.pointerId)
  globalThis.addEventListener('pointermove', onDrag)
  globalThis.addEventListener('pointerup', endDrag)
}

function onDrag(event) {
  // The handle is on the panel's left edge: moving left widens it.
  panel.setWidth(dragStart.width + (dragStart.x - event.clientX), room.value)
}

function endDrag() {
  dragging.value = false
  dragStart = null
  globalThis.removeEventListener('pointermove', onDrag)
  globalThis.removeEventListener('pointerup', endDrag)
}

function onWindowResize() {
  room.value = measure()
}

// The sidebar collapsing changes the room without the window resizing.
let observer = null

onMounted(() => {
  room.value = measure()
  const bridge = props.bridge
  stops.push(bridge?.onPermissionRequest?.((asked) => { question.value = asked }))
  stops.push(bridge?.onPermissionSettled?.((id) => {
    if (question.value?.id === id) question.value = null
    refreshSites()
  }))
  refreshSites()
  if (globalThis.ResizeObserver && asideRef.value?.previousElementSibling) {
    observer = new ResizeObserver(onWindowResize)
    observer.observe(asideRef.value.previousElementSibling)
  }
  if (current.value) mountGuest(current.value)
  else addressRef.value?.focus()
  globalThis.addEventListener('resize', onWindowResize)
})

onUnmounted(() => {
  endDrag()
  for (const stop of stops) stop?.()
  observer?.disconnect()
  globalThis.removeEventListener('resize', onWindowResize)
})

defineExpose({ show })
</script>
