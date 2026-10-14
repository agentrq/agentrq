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
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { storeToRefs } from 'pinia'
import * as api from '../api'
import {
  usePanoBoard, panoAuthor, panoColor, panoTilt, panoInitial, timeAgo, expiresLabel, isOwnPanoDoc, recolorPanoDoc, PANO_COLORS,
  panoCheckable, panoCheckboxIndex, checkPanoItem, panoAssignees, usePanoChat, docMentionQuery, docMentionMatches,
} from '../composables/usePano'
import PanoTaskButton from '../components/PanoTaskButton.vue'
import PanoProgress from '../components/PanoProgress.vue'
import ComposerBox from '../components/ComposerBox.vue'
import ComposerNotice from '../components/ComposerNotice.vue'
import TaskOptionsTools from '../components/TaskOptionsTools.vue'
import TaskAttachmentChips from '../components/TaskAttachmentChips.vue'
import DeleteModal from '../components/DeleteModal.vue'
import { renderMarkdown } from '../utils/markdown'
import { useWorkspaceStore } from '../stores/workspaceStore'
import { useToasts } from '../composables/useToasts'
import { useEventBus } from '../useEventBus'
import { onWebMCPChange } from '../composables/useWebMCPChanges'

const route = useRoute()
const router = useRouter()
const { docs, loading, error, query, color, shown, load } = usePanoBoard({ api })
const workspaceStore = useWorkspaceStore()
const { workspaces } = storeToRefs(workspaceStore)
const { notifySuccess, notifyError } = useToasts()
const { connect, disconnect, onEvent } = useEventBus(undefined, { buffer: false })

// The chat with the supervisor: a box under the board on a large screen. Once
// a message is sent its task opens in a card of its own beside the page,
// which App.vue draws (PanoChatColumn), as it does the desktop side panel.
const chat = usePanoChat({ api, store: workspaceStore })
const { supervisor, online: chatOnline, message: chatMessage, doc: chatDoc, sending: chatSending, opts: chatOpts, clearContextOffered } = chat
const chatInput = ref(null)
const chatOpen = computed(() => !!route.params.taskId)

// Typing # in the chat suggests the board's docs by title; picking one tags
// the chat with it, and the task it starts is linked to that doc.
const docMention = ref(null)
const docMentionIndex = ref(0)
const docOptions = computed(() => (docMention.value ? docMentionMatches(docs.value, docMention.value.query) : []))

function onChatInput() {
  docMention.value = docMentionQuery(chatMessage.value, chatInput.value.selectionStart)
  docMentionIndex.value = 0
}

async function pickDoc(doc) {
  const el = chatInput.value
  const { start } = docMention.value
  chatMessage.value = chatMessage.value.slice(0, start) + chatMessage.value.slice(el.selectionStart)
  chatDoc.value = { id: doc.id, title: doc.title }
  docMention.value = null
  await nextTick()
  el.focus()
  el.setSelectionRange(start, start)
}

function onChatKey(event) {
  const options = docOptions.value
  if (!options.length) return
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    docMentionIndex.value = (docMentionIndex.value + (event.key === 'ArrowDown' ? 1 : options.length - 1)) % options.length
  } else if ((event.key === 'Enter' && !event.metaKey && !event.ctrlKey) || event.key === 'Tab') {
    event.preventDefault()
    pickDoc(options[docMentionIndex.value])
  } else if (event.key === 'Escape') {
    event.preventDefault()
    docMention.value = null
  }
}

async function sendChat() {
  try {
    const task = await chat.send()
    if (task?.status === 'cron') notifySuccess('Task scheduled')
    else if (task) router.push(`/pano/chat/${task.workspaceId}/${task.id}`)
  } catch (err) {
    notifyError(err.message)
  }
}

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
  chat.ensure().catch((err) => notifyError(err.message))
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

// The filter pills: amber when picked, quiet otherwise.
const pillOn = 'bg-[#d97706] text-white border-transparent'
const pillOff = 'bg-white dark:bg-zinc-800/80 text-stone-700 dark:text-stone-300 border-stone-200 dark:border-zinc-700 hover:border-stone-300 dark:hover:border-zinc-600'
const action = 'p-1 rounded-sm opacity-60 hover:opacity-100 hover:bg-black/10 transition-opacity'
</script>

<template>
  <!-- The board scrolls above the chat box, which stays at the bottom. -->
  <div class="@container flex flex-col h-full w-full min-h-0">
  <div class="flex-1 min-h-0 overflow-y-auto custom-scrollbar">
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
        <!-- Quick filter pills: every color, or one. -->
        <div class="flex flex-wrap items-center gap-1.5" role="radiogroup" aria-label="Filter by color">
          <button type="button" role="radio" :aria-checked="!color" data-test="pano-filter-all" @click="color = ''"
            :class="!color ? pillOn : 'bg-stone-200 dark:bg-zinc-800 text-stone-700 dark:text-stone-300 border-transparent'" class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full border text-[11px] font-bold transition-colors">All</button>
          <button v-for="c in PANO_COLORS" :key="c.name" type="button" role="radio" :aria-checked="color === c.name" :data-test="`pano-filter-${c.name}`"
            @click="color = color === c.name ? '' : c.name" :class="color === c.name ? pillOn : pillOff" class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full border text-[11px] font-bold transition-colors">
            <span :class="c.dot" class="w-2.5 h-2.5 rounded-full" aria-hidden="true"></span>{{ c.label }}
          </button>
        </div>
        <p v-if="(query.trim() || color) && docs.length" data-test="pano-filter-summary" class="flex items-center gap-3 text-[10px] font-bold uppercase tracking-widest text-gray-400 dark:text-zinc-500">
          <span>Showing {{ shown.length }} of {{ docs.length }}</span>
          <button type="button" data-test="pano-filter-reset" class="normal-case tracking-normal text-[11px] text-gray-600 dark:text-zinc-300 hover:underline" @click="query = ''; color = ''">Reset</button>
        </p>
      </div>

      <p v-if="loading" class="text-[11px] text-gray-400 dark:text-zinc-500">Loading the Pano…</p>
      <p v-else-if="error" data-test="pano-error" class="text-[11px] font-bold text-red-600 dark:text-red-400">{{ error }}</p>

      <!-- Columns follow the board's own width, which the chat narrows. -->
      <div v-else class="grid gap-x-5 gap-y-7 pt-2 grid-cols-1 @min-[33rem]:grid-cols-2 @min-[57rem]:grid-cols-3 @min-[73rem]:grid-cols-4 @min-[89rem]:grid-cols-5">
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

        <p v-if="docs.length && !shown.length" data-test="pano-no-match" class="self-center text-[11px] text-gray-500 dark:text-zinc-400">{{ query.trim() ? `No doc's title matches “${query}”${color ? ' in this color' : ''}.` : 'No doc in this color.' }}</p>

        <article v-for="doc in shown" :key="doc.id" data-test="pano-card"
          :style="{ '--pano-ink': panoColor(doc.color).inkHex, '--pano-tag': panoColor(doc.color).tagHex, '--pano-tag-ink': panoColor(doc.color).tagInkHex, '--pano-tilt': `${panoTilt(doc.id)}deg` }"
          :class="[panoColor(doc.color).card, panoColor(doc.color).body, { 'z-20': recoloring === doc.id }]"
          class="pano-paper group relative flex flex-col min-w-0 h-72 rounded-md border border-black/[0.06] shadow-[0_1px_2px_rgba(0,0,0,0.06),0_10px_20px_-12px_rgba(0,0,0,0.25)] dark:shadow-[0_2px_4px_rgba(0,0,0,0.3),0_16px_32px_-12px_rgba(0,0,0,0.45)] rotate-[var(--pano-tilt)] hover:rotate-0 focus-within:rotate-0 hover:z-10 focus-within:z-20 hover:shadow-[0_1px_2px_rgba(0,0,0,0.08),0_14px_28px_-12px_rgba(0,0,0,0.35)] transition-[rotate,box-shadow] duration-200">
          <!-- A leaning card is a stacking context of its own, so the one under it
               would cover its open menu: hover, focus and an open menu raise it. -->
          <span aria-hidden="true" :class="panoColor(doc.color).tape" class="absolute -top-2.5 left-1/2 -translate-x-1/2 -rotate-2 w-20 h-5 rounded-[2px]"></span>

          <router-link :to="`/pano/${doc.id}`" data-test="pano-open" class="flex flex-col flex-1 min-h-0 px-5 pt-6 pb-2 focus:outline-none">
            <h2 :class="panoColor(doc.color).title" class="text-lg font-black leading-snug line-clamp-2 break-words">{{ doc.title }}</h2>
            <div class="relative mt-2 flex-1 min-h-0 overflow-hidden">
              <div data-test="pano-excerpt" class="md-body text-[13px] leading-relaxed [&_img]:hidden"
                v-html="panoAssignees(panoCheckable(renderMarkdown(doc.excerpt)), { link: false })" @click="onExcerptClick(doc, $event)"></div>
            </div>
            <PanoProgress :doc="doc" class="mt-3 shrink-0" />
          </router-link>

          <footer :class="panoColor(doc.color).muted" class="flex items-center gap-2 px-5 py-3 border-t border-black/10 text-[11px] min-w-0">
            <span :class="panoColor(doc.color).avatar" :title="panoAuthor(doc.updatedByWorkspaceId, workspaces)"
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

  </div>

    <!-- The chat box: large screens only, and only until a chat is open,
         whose own composer takes over. -->
    <form v-if="!chatOpen" data-test="pano-chat" class="@container hidden lg:block shrink-0 px-4 pt-3" @submit.prevent="sendChat">
      <div v-if="chatDoc" class="flex pb-2">
        <div data-test="pano-chat-doc" class="flex items-center text-[10px] bg-gray-50 dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 border border-gray-200 dark:border-zinc-700 rounded-md px-2.5 py-1 font-bold shadow-sm">
          <span class="truncate max-w-[240px]" :title="`The task is linked to ${chatDoc.title}`">#{{ chatDoc.title }}</span>
          <button type="button" data-test="pano-chat-doc-remove" title="Untag the doc" aria-label="Untag the doc" @click.prevent="chatDoc = null" class="ml-2 text-gray-500 hover:text-red-500 transition-colors">
            <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2.5" aria-hidden="true"><path d="M6 18L18 6M6 6l12 12"></path></svg>
          </button>
        </div>
      </div>
      <TaskAttachmentChips :attachments="chatOpts.attachments.value" @remove="(i) => chatOpts.attachments.value.splice(i, 1)" class="pb-2" />
      <div class="relative">
        <ul v-if="docOptions.length" data-test="pano-chat-doc-menu" role="listbox"
          class="absolute left-0 right-0 bottom-full mb-1 z-30 max-h-56 overflow-y-auto custom-scrollbar bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700 rounded-lg py-1 shadow-lg">
          <li v-for="(d, i) in docOptions" :key="d.id" role="option" :aria-selected="i === docMentionIndex" data-test="pano-chat-doc-option"
            @mousedown.prevent="pickDoc(d)" @mouseenter="docMentionIndex = i"
            :class="i === docMentionIndex ? 'bg-gray-100 dark:bg-zinc-800' : ''"
            class="px-3 py-1.5 text-xs text-gray-700 dark:text-zinc-200 truncate cursor-pointer">#{{ d.title }}</li>
        </ul>
        <ComposerBox v-model="chatMessage" data-test="pano-chat-input" dir="auto" :input-ref="(el) => { chatInput = el }"
          @keydown.meta.enter.prevent="sendChat" @keydown.ctrl.enter.prevent="sendChat"
          @input="onChatInput" @click="onChatInput" @keydown="onChatKey" @blur="docMention = null"
          :disabled="!chatOnline || chatSending"
          :placeholder="chatOnline ? `Ask ${supervisor.name} about the Pano, # to tag a doc... (Cmd ⌘ + Enter to send)` : 'Connect your supervisor agent to start chatting'"
          :send-disabled="!chatOnline || !chatMessage.trim() || chatSending">
          <template v-if="chatOnline" #tools>
            <TaskOptionsTools :opts="chatOpts" v-model:text="chatMessage" :workspace="supervisor" :workspace-id="supervisor.id"
              :clear-context-offered="clearContextOffered" :input-el="chatInput" />
          </template>
        </ComposerBox>
      </div>
      <ComposerNotice v-if="!chatOnline" data-test="pano-chat-offline" tone="red" pulse>
        Supervisor offline. Connect your supervisor agent to start chatting.
        <template #action>
          <router-link v-if="supervisor" :to="`/workspaces/${supervisor.id}`" data-test="pano-chat-connect"
            class="ml-auto text-[10px] font-bold text-red-700 dark:text-red-400 underline underline-offset-2 hover:no-underline">Connect</router-link>
        </template>
      </ComposerNotice>
    </form>

    <DeleteModal
      :show="!!pendingDelete"
      title="Delete Doc"
      :message="`Delete '${pendingDelete?.title}' with its files and comments? Every workspace loses it. This cannot be undone.`"
      @close="pendingDelete = null"
      @confirm="onDelete"
    />
  </div>
</template>
