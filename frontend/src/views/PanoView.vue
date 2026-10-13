<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
/**
 * The Pano board: every doc of the account as a post-it, showing only its
 * opening. Thin on purpose: the rules are in `usePano`, where they are tested.
 */
import { onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { storeToRefs } from 'pinia'
import * as api from '../api'
import {
  usePanoBoard, panoAuthor, panoColor, panoTilt, panoInitial, timeAgo, expiresLabel, isOwnPanoDoc, recolorPanoDoc, PANO_COLORS,
  panoCheckable, panoCheckboxIndex, checkPanoItem,
} from '../composables/usePano'
import PanoTaskButton from '../components/PanoTaskButton.vue'
import PanoProgress from '../components/PanoProgress.vue'
import DeleteModal from '../components/DeleteModal.vue'
import { renderMarkdown } from '../utils/markdown'
import { useWorkspaceStore } from '../stores/workspaceStore'
import { useToasts } from '../composables/useToasts'
import { useEventBus } from '../useEventBus'
import { onWebMCPChange } from '../composables/useWebMCPChanges'

const router = useRouter()
const { docs, loading, error, query, shown, load } = usePanoBoard({ api })
const { workspaces } = storeToRefs(useWorkspaceStore())
const { notifySuccess, notifyError } = useToasts()
const { connect, disconnect, onEvent } = useEventBus(undefined, { buffer: false })

// The doc whose color menu is open, and the one waiting on the delete dialog.
const recoloring = ref(null)
const pendingDelete = ref(null)

// N starts a note, unless the person is typing somewhere.
function onKey(event) {
  if (event.key !== 'n' && event.key !== 'N') return
  if (event.metaKey || event.ctrlKey || event.altKey) return
  if (event.target.closest?.('input, textarea, select, [contenteditable="true"]')) return
  event.preventDefault()
  router.push('/pano/new')
}

onMounted(async () => {
  window.addEventListener('keydown', onKey)
  await load()
  // Agents write the Pano from every workspace; their docs show as they land.
  onEvent((event) => {
    if (event.type === 'pano.changed') load({ quiet: true })
  })
  connect()
})
onUnmounted(() => {
  window.removeEventListener('keydown', onKey)
  disconnect()
})
onWebMCPChange(() => load({ quiet: true }))

async function recolor(doc, color) {
  recoloring.value = null
  if (doc.color === color) return
  try {
    await recolorPanoDoc(doc, color, { api })
    await load({ quiet: true })
  } catch (err) {
    notifyError(err.message)
  }
}

// A checkbox on a card checks its item off, and does not open the doc.
// By the time a click is handled the box shows its new state; preventing the
// default puts it back until the save comes through.
async function onExcerptClick(doc, event) {
  const index = panoCheckboxIndex(event.currentTarget, event.target)
  if (index < 0) return
  event.preventDefault()
  event.stopPropagation()
  try {
    await checkPanoItem(doc, index, event.target.checked, { api })
    await load({ quiet: true })
  } catch (err) {
    notifyError(err.message)
  }
}

async function onDelete() {
  const doc = pendingDelete.value
  pendingDelete.value = null
  try {
    await api.deletePanoDoc(doc.id)
    notifySuccess('Doc deleted')
    await load({ quiet: true })
  } catch (err) {
    notifyError(err.message)
  }
}

const action = 'p-1 rounded-sm opacity-60 hover:opacity-100 hover:bg-black/10 transition-opacity'
</script>

<template>
  <div class="flex flex-col h-full w-full overflow-y-auto custom-scrollbar">
    <div class="w-full px-4 py-2 mb-4 shrink-0 flex flex-row items-start justify-between gap-4">
      <div class="flex flex-col min-w-0 flex-1">
        <h1 class="text-lg md:text-2xl font-black text-gray-800 dark:text-zinc-200 truncate leading-tight">Pano</h1>
        <p class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5">
          Docs every workspace's agents share. An agent posts one and passes its id to the agents that need it.
        </p>
      </div>
    </div>

    <div class="px-4 pb-10 space-y-5 rounded-xl [background-image:radial-gradient(rgba(0,0,0,0.13)_1px,transparent_1px)] dark:[background-image:radial-gradient(rgba(255,255,255,0.09)_1px,transparent_1px)] [background-size:18px_18px]">
      <div class="flex flex-wrap items-center gap-x-4 gap-y-2">
        <input v-model="query" type="search" data-test="pano-search" placeholder="Filter by title"
          class="w-full sm:max-w-sm px-3 py-2 text-xs bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700 rounded-lg text-gray-900 dark:text-zinc-100 placeholder-gray-300 dark:placeholder-zinc-600 focus:outline-none focus:border-gray-400 dark:focus:border-zinc-500" />
        <p v-if="query.trim() && docs.length" data-test="pano-filter-summary" class="flex items-center gap-3 text-[10px] font-bold uppercase tracking-widest text-gray-400 dark:text-zinc-500">
          <span>Showing {{ shown.length }} of {{ docs.length }}</span>
          <button type="button" data-test="pano-filter-reset" class="normal-case tracking-normal text-[11px] text-gray-600 dark:text-zinc-300 hover:underline" @click="query = ''">Reset</button>
        </p>
      </div>

      <p v-if="loading" class="text-[11px] text-gray-400 dark:text-zinc-500">Loading the Pano…</p>
      <p v-else-if="error" data-test="pano-error" class="text-[11px] font-bold text-red-600 dark:text-red-400">{{ error }}</p>

      <div v-else class="grid gap-x-5 gap-y-7 pt-2 grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5">
        <!-- A new note, first on the board; a dot starts it in that color. -->
        <div data-test="pano-new-card"
          class="relative flex flex-col items-center justify-center gap-3 h-48 sm:h-72 p-5 rounded-md border border-dashed border-gray-300 dark:border-zinc-700 bg-white/60 dark:bg-zinc-900/60 text-center">
          <router-link to="/pano/new" data-test="pano-new" class="flex flex-col items-center gap-3 group">
            <span class="flex items-center justify-center w-12 h-12 rounded-xl bg-[#ffdcb5] text-[#7a3d06] group-hover:scale-105 transition-transform">
              <svg class="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M12 4.5v15m7.5-7.5h-15" /></svg>
            </span>
            <span class="text-base font-black text-gray-800 dark:text-zinc-100">New note</span>
          </router-link>
          <p class="text-[11px] text-gray-500 dark:text-zinc-400">{{ docs.length ? 'Pick its paper, or press N' : 'Nothing on the Pano yet. Agents post here with the pano tool, or write the first note.' }}</p>
          <div class="flex gap-1.5 px-2 py-1.5 rounded-full bg-gray-100 dark:bg-zinc-800">
            <router-link v-for="c in PANO_COLORS" :key="c.name" :to="`/pano/new?color=${c.name}`" :data-test="`pano-new-${c.name}`"
              :title="c.label" :aria-label="`New ${c.label} note`" :class="c.swatch"
              class="w-5 h-5 rounded-full border border-black/10 hover:scale-110 transition-transform"></router-link>
          </div>
        </div>

        <p v-if="docs.length && !shown.length" data-test="pano-no-match" class="self-center text-[11px] text-gray-500 dark:text-zinc-400">No doc's title matches “{{ query }}”.</p>

        <article v-for="doc in shown" :key="doc.id" data-test="pano-card"
          :style="{ '--pano-ink': panoColor(doc.color).inkHex, '--pano-tilt': `${panoTilt(doc.id)}deg` }"
          :class="[panoColor(doc.color).card, { 'z-20': recoloring === doc.id }]"
          class="pano-paper group relative flex flex-col min-w-0 h-72 rounded-md border border-black/[0.06] text-[#1e293b] shadow-[0_1px_2px_rgba(0,0,0,0.06),0_10px_20px_-12px_rgba(0,0,0,0.25)] dark:shadow-[0_2px_4px_rgba(0,0,0,0.3),0_16px_32px_-12px_rgba(0,0,0,0.45)] rotate-[var(--pano-tilt)] hover:rotate-0 focus-within:rotate-0 hover:z-10 focus-within:z-20 hover:shadow-[0_1px_2px_rgba(0,0,0,0.08),0_14px_28px_-12px_rgba(0,0,0,0.35)] transition-[rotate,box-shadow] duration-200">
          <!-- A leaning card is a stacking context of its own, so the one under it
               would cover its open menu: hover, focus and an open menu raise it. -->
          <span aria-hidden="true" :class="panoColor(doc.color).tape" class="absolute -top-2.5 left-1/2 -translate-x-1/2 -rotate-2 w-20 h-5 rounded-[2px]"></span>

          <router-link :to="`/pano/${doc.id}`" data-test="pano-open" class="flex flex-col flex-1 min-h-0 px-5 pt-6 pb-2 focus:outline-none">
            <h2 :class="panoColor(doc.color).ink" class="text-lg font-black leading-snug line-clamp-2 break-words">{{ doc.title }}</h2>
            <div class="relative mt-2 flex-1 min-h-0 overflow-hidden">
              <div data-test="pano-excerpt" class="md-body text-[13px] leading-relaxed [&_img]:hidden"
                v-html="panoCheckable(renderMarkdown(doc.excerpt))" @click="onExcerptClick(doc, $event)"></div>
            </div>
            <PanoProgress :doc="doc" class="mt-3 shrink-0" />
          </router-link>

          <footer class="flex items-center gap-2 px-5 py-3 border-t border-black/10 text-[11px] text-[#334155] min-w-0">
            <span :class="panoColor(doc.color).inkBg" :title="panoAuthor(doc.updatedByWorkspaceId, workspaces)"
              class="shrink-0 flex items-center justify-center w-6 h-6 rounded-full text-[10px] font-black text-white">{{ panoInitial(panoAuthor(doc.updatedByWorkspaceId, workspaces)) }}</span>
            <span class="min-w-0 flex-1 truncate" :title="expiresLabel(doc.expiresAt)">
              <span class="font-bold">{{ panoAuthor(doc.updatedByWorkspaceId, workspaces) }}</span> · {{ timeAgo(doc.updatedAt) }}
            </span>
            <router-link :to="`/pano/${doc.id}?edit=1`" data-test="pano-card-edit" title="Edit" aria-label="Edit" :class="action">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="m16.862 4.487 1.687-1.688a1.875 1.875 0 1 1 2.652 2.652L6.832 19.82a4.5 4.5 0 0 1-1.897 1.13l-2.685.8.8-2.685a4.5 4.5 0 0 1 1.13-1.897L16.863 4.487Z" /></svg>
            </router-link>
            <div class="relative" v-click-outside="() => { if (recoloring === doc.id) recoloring = null }">
              <button type="button" data-test="pano-card-color" title="Color" aria-label="Color" :aria-expanded="recoloring === doc.id" :class="action"
                @click="recoloring = recoloring === doc.id ? null : doc.id">
                <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M4.098 19.902a3.75 3.75 0 0 0 5.304 0l6.401-6.402M6.75 21A3.75 3.75 0 0 1 3 17.25V4.125C3 3.504 3.504 3 4.125 3h5.25c.621 0 1.125.504 1.125 1.125v4.072M6.75 21a3.75 3.75 0 0 0 3.75-3.75V8.197M6.75 21h13.125c.621 0 1.125-.504 1.125-1.125v-5.25c0-.621-.504-1.125-1.125-1.125h-4.072M10.5 8.197l2.88-2.88c.438-.439 1.15-.439 1.59 0l3.712 3.713c.44.44.44 1.152 0 1.59l-2.879 2.88M6.75 17.25h.008v.008H6.75v-.008Z" /></svg>
              </button>
              <div v-if="recoloring === doc.id" role="menu" data-test="pano-card-colors"
                class="absolute right-0 bottom-full mb-1 z-30 flex gap-1.5 p-2 rounded-lg bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700">
                <button v-for="c in PANO_COLORS" :key="c.name" type="button" role="menuitemradio" :aria-checked="doc.color === c.name"
                  :data-test="`pano-card-color-${c.name}`" :title="c.label" :aria-label="c.label" :class="[c.swatch, doc.color === c.name ? 'ring-2 ring-black dark:ring-white ring-offset-1 dark:ring-offset-zinc-900' : '']"
                  class="w-5 h-5 rounded-full border border-black/10" @click="recolor(doc, c.name)"></button>
              </div>
            </div>
            <button type="button" data-test="pano-card-delete" title="Delete" aria-label="Delete" :class="action" @click="pendingDelete = doc">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="m14.74 9-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 0 1-2.244 2.077H8.084a2.25 2.25 0 0 1-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 0 0-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 0 1 3.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 0 0-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 0 0-7.5 0" /></svg>
            </button>
            <PanoTaskButton v-if="isOwnPanoDoc(doc)" :doc="doc" button-class="!text-current opacity-60 hover:opacity-100" />
          </footer>
        </article>
      </div>
    </div>

    <DeleteModal
      :show="!!pendingDelete"
      title="Delete Doc"
      :message="`Delete '${pendingDelete?.title}' with its files and comments? Every workspace loses it. This cannot be undone.`"
      @close="pendingDelete = null"
      @confirm="onDelete"
    />
  </div>
</template>
