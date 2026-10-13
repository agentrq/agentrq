<!--
  Copyright 2026 Contextual, Inc. https://agentrq.com
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
/**
 * One Pano doc: its markdown, linked files, attachments and comments, and the
 * editor for it — or, at /pano/new, for a doc not written yet. Thin on purpose:
 * the rules are in `usePano`, where they are tested.
 */
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { storeToRefs } from 'pinia'
import * as api from '../api'
import {
  usePanoDoc, panoAuthor, panoColor, panoTilt, panoDate, expiresLabel, linkPanoFiles, formatFileSize, isOwnPanoDoc,
  panoCheckable, panoCheckboxIndex, formatMarkdown, panoAssignees, panoRelatedTasks, panoItemMenus, panoMentions,
  mentionQuery, mentionMatches, insertMention, mentionedWorkspaces, PANO_COLORS, PANO_TASK_STATUSES,
} from '../composables/usePano'
import { taskRingClass } from '../composables/useTaskStatusStyle'
import PanoTaskButton from '../components/PanoTaskButton.vue'
import PanoProgress from '../components/PanoProgress.vue'
import { renderMarkdown } from '../utils/markdown'
import { useWorkspaceStore } from '../stores/workspaceStore'
import { useEventBus } from '../useEventBus'
import { useToasts } from '../composables/useToasts'
import { onWebMCPChange } from '../composables/useWebMCPChanges'
import DeleteModal from '../components/DeleteModal.vue'

const route = useRoute()
const router = useRouter()
const id = route.path === '/pano/new' ? null : String(route.params.docId ?? route.params.id)
const {
  doc, loading, error, busy, editing, draft, conflict,
  load, startEditing, cancelEditing, reloadEditing, addDraftFile, removeDraftFile, save, checkItem, assignItem, remove, comment, attach,
} = usePanoDoc(id, { api, color: route.query.color })
const { workspaces } = storeToRefs(useWorkspaceStore())
const { notifySuccess, notifyError } = useToasts()
const { connect, disconnect, onEvent } = useEventBus(undefined, { buffer: false })

const commentText = ref('')
const commentInput = ref(null)
// The @mention being typed in the comment, and the workspaces it could name.
const mention = ref(null)
const mentionIndex = ref(0)
const mentionOptions = computed(() => (mention.value ? mentionMatches(workspaces.value, mention.value.query) : []))
// The open menu of a checklist item: which item, where, and its task if assigned.
const itemMenu = ref(null)
let itemMenuButton = null
const pendingDelete = ref(false)
const draftFileInput = ref(null)
const attachInput = ref(null)
const markdownInput = ref(null)
const preview = ref(false)
// Set while this page deletes the doc, so its own delete is not read back.
let deleting = false

// Read only inside v-else-if="doc", so a doc is always there.
const TASK_PATH = /^\/workspaces\/([^/]+)\/tasks\/([^/]+)$/
const assigned = computed(() => panoAssignees(panoCheckable(linkPanoFiles(renderMarkdown(doc.value.markdown), doc.value.files))))
const body = computed(() => panoItemMenus(assigned.value))
const relatedTasks = computed(() => (doc.value ? panoRelatedTasks(assigned.value) : []))
// The related tasks' own titles, by path, read once each; a task that cannot be
// read keeps the name its link carries.
const taskTitles = ref({})
const fetchedTasks = new Set()
watch(relatedTasks, (tasks) => {
  for (const t of tasks) {
    if (fetchedTasks.has(t.path)) continue
    fetchedTasks.add(t.path)
    api.getTask(t.workspaceId, t.id).then((res) => {
      if (res?.task?.title) taskTitles.value = { ...taskTitles.value, [t.path]: res.task.title }
    }).catch(() => {})
  }
})
const draftBody = computed(() => renderMarkdown(draft.value.markdown || '*Nothing written yet.*'))
const color = computed(() => panoColor(editing.value ? draft.value.color : doc.value?.color))
const canSave = computed(() => !busy.value && !!draft.value.title.trim() && !!draft.value.markdown.trim())

onMounted(async () => {
  if (!id) return
  await load()
  // The board's edit button opens the doc straight in the editor.
  if (route.query.edit && doc.value) startEditing()
  // An agent may comment or rewrite the doc while it is open; never over an edit.
  onEvent((event) => {
    if (event.type === 'pano.changed' && event.payload?.id === id && !editing.value && !deleting) load({ quiet: true })
  })
  connect()
})
onUnmounted(disconnect)
onWebMCPChange(() => { if (id && !editing.value) load({ quiet: true }) })

async function run(action, success) {
  try {
    const result = await action()
    if (success) notifySuccess(success)
    return result
  } catch (err) {
    notifyError(err.message)
    return undefined
  }
}

function edit() {
  preview.value = false
  startEditing()
}

async function onDraftFiles(event) {
  for (const file of event.target.files) await run(() => addDraftFile(file))
  event.target.value = ''
}

async function onSave() {
  if (!canSave.value) return
  const savedId = await run(save, id ? 'Doc saved' : 'Doc posted')
  if (savedId && !id) router.replace(`/pano/${savedId}`)
}

function onCancel() {
  if (id) cancelEditing()
  else router.push('/pano')
}

// ⌘S / Ctrl+S saves, Esc leaves the editor.
function onEditorKey(event) {
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 's') {
    event.preventDefault()
    onSave()
  } else if (event.key === 'Escape') {
    event.preventDefault()
    onCancel()
  }
}

// A formatting button wraps the selection or starts its lines, and keeps it selected.
async function format(kind) {
  preview.value = false
  await nextTick()
  const el = markdownInput.value
  const next = formatMarkdown(draft.value.markdown, el.selectionStart, el.selectionEnd, kind)
  draft.value.markdown = next.value
  await nextTick()
  el.focus()
  el.setSelectionRange(next.start, next.end)
}

// A checkbox in the note checks its item off, and saves; an item's menu button
// opens its menu, and an assignee opens its task.
// By the time a click is handled the box shows its new state; preventing the
// default puts it back until the save comes through.
async function onBodyClick(event) {
  const chip = event.target.closest('[data-pano-task]')
  if (chip) {
    event.preventDefault()
    openTask(chip.dataset.panoTask)
    return
  }
  const button = event.target.closest('[data-pano-assign]')
  if (button) {
    openItemMenu(event.currentTarget, button)
    return
  }
  const index = panoCheckboxIndex(event.currentTarget, event.target)
  if (index < 0) return
  event.preventDefault()
  await run(() => checkItem(index, event.target.checked))
}

// A task opens beside the doc on a large screen, where the Pano chat column
// has room for it, and as its own page on a small one.
function openTask(path) {
  const m = TASK_PATH.exec(path)
  const wide = globalThis.matchMedia?.('(min-width: 1024px)').matches
  router.push(m && wide ? `/pano/${id}/chat/${m[1]}/${m[2]}` : path)
}

// The menu opens under its button, in the paper's own coordinates.
function openItemMenu(container, button) {
  closeItemMenu()
  const item = button.parentElement
  const chip = [...item.querySelectorAll('a.pano-assignee')].find((a) => a.closest('li') === item)
  const paper = container.closest('article').getBoundingClientRect()
  const at = button.getBoundingClientRect()
  itemMenu.value = {
    index: panoCheckboxIndex(container, button.previousElementSibling),
    top: at.bottom - paper.top + 4,
    left: Math.max(at.left - paper.left, 8),
    task: chip && { path: chip.dataset.panoTask, id: chip.dataset.panoTask.split('/').pop(), status: chip.dataset.status, name: chip.childNodes[1].textContent },
  }
  itemMenuButton = button
  button.setAttribute('aria-expanded', 'true')
}

// Another item's button opens that item's menu instead.
function closeItemMenu(event) {
  if (event?.target?.closest?.('[data-pano-assign]')) return
  itemMenuButton?.setAttribute('aria-expanded', 'false')
  itemMenuButton = null
  itemMenu.value = null
}

async function onAssign(workspace) {
  const { index } = itemMenu.value
  closeItemMenu()
  const task = await run(() => assignItem(index, workspace))
  if (task) notifySuccess(`Assigned to ${workspace.name}`, null, { taskId: task.id, workspaceId: workspace.id })
}

// Typing @ suggests the workspaces to mention; arrows move, Enter or Tab picks.
function onCommentInput() {
  const el = commentInput.value
  mention.value = mentionQuery(commentText.value, el.selectionStart)
  mentionIndex.value = 0
}

async function pickMention(workspace) {
  const el = commentInput.value
  const next = insertMention(commentText.value, mention.value.start, el.selectionStart, workspace.name)
  commentText.value = next.value
  mention.value = null
  await nextTick()
  el.focus()
  el.setSelectionRange(next.caret, next.caret)
}

function onCommentKey(event) {
  const options = mentionOptions.value
  if (options.length) {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault()
      mentionIndex.value = (mentionIndex.value + (event.key === 'ArrowDown' ? 1 : options.length - 1)) % options.length
      return
    }
    if ((event.key === 'Enter' && !event.metaKey && !event.ctrlKey) || event.key === 'Tab') {
      event.preventDefault()
      pickMention(options[mentionIndex.value])
      return
    }
    if (event.key === 'Escape') {
      event.preventDefault()
      mention.value = null
      return
    }
  }
  if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
    event.preventDefault()
    onComment()
  }
}

// A comment pings each workspace it mentions with a task to answer it.
async function onComment() {
  const text = commentText.value.trim()
  if (!text) return
  const mentioned = mentionedWorkspaces(text, workspaces.value)
  const pinged = await run(() => comment(text, mentioned))
  if (!pinged) return
  commentText.value = ''
  mention.value = null
  const { tasks, failed } = pinged
  if (tasks.length) notifySuccess(tasks.length === 1 ? 'Pinged 1 workspace' : `Pinged ${tasks.length} workspaces`)
  for (const f of failed) notifyError(`Could not ping ${f.workspace.name}: ${f.error}`)
}

async function onAttach(event) {
  const files = [...event.target.files]
  event.target.value = ''
  if (files.length) await run(() => attach(files), files.length === 1 ? 'File attached' : 'Files attached')
}

async function onDelete() {
  pendingDelete.value = false
  deleting = true
  if (await run(() => remove().then(() => true), 'Doc deleted')) router.push('/pano')
  else deleting = false
}

const FORMAT_BUTTONS = [
  { kind: 'bold', label: 'Bold', path: 'M6.75 3.744h-.753v8.25h7.125a4.125 4.125 0 0 0 0-8.25H6.75Zm0 0v.38m0 16.122h6.747a4.5 4.5 0 0 0 0-9.001h-7.5v9h.753Zm0 0v-.37m0-15.751h6a3.75 3.75 0 1 1 0 7.5h-6m0-7.5v7.5m0 0v8.25m0-8.25h6.375a4.125 4.125 0 0 1 0 8.25H6.75m.747-15.38h4.875a3.375 3.375 0 0 1 0 6.75H7.497v-6.75Zm0 7.5h5.25a3.75 3.75 0 0 1 0 7.5h-5.25v-7.5Z' },
  { kind: 'italic', label: 'Italic', path: 'M5.248 20.246H9.05m0 0h3.696m-3.696 0 5.893-16.502m0 0h-3.697m3.697 0h3.803' },
  { kind: 'list', label: 'List', path: 'M8.25 6.75h12M8.25 12h12m-12 5.25h12M3.75 6.75h.007v.008H3.75V6.75Zm.375 0a.375.375 0 1 1-.75 0 .375.375 0 0 1 .75 0ZM3.75 12h.007v.008H3.75V12Zm.375 0a.375.375 0 1 1-.75 0 .375.375 0 0 1 .75 0Zm-.375 5.25h.007v.008H3.75v-.008Zm.375 0a.375.375 0 1 1-.75 0 .375.375 0 0 1 .75 0Z' },
  { kind: 'checkbox', label: 'Checklist item', path: 'M9 12.75 11.25 15 15 9.75M4.5 4.5h15v15h-15z' },
  { kind: 'code', label: 'Code', path: 'M17.25 6.75 22.5 12l-5.25 5.25m-10.5 0L1.5 12l5.25-5.25m7.5-3-4.5 16.5' },
]

const label = 'block text-[13px] font-black text-gray-800 dark:text-zinc-100 mb-2'
const field = 'w-full px-3 py-2 text-sm border border-gray-200 dark:border-zinc-700 rounded-lg bg-white dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 placeholder-gray-400 dark:placeholder-zinc-500 focus:outline-none focus:ring-2 focus:ring-black dark:focus:ring-white transition-all'
const primary = 'px-4 py-2 bg-black dark:bg-white text-white dark:text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:opacity-80 transition-all active:scale-95 disabled:opacity-50'
const secondary = 'px-4 py-2 bg-white dark:bg-zinc-800 text-gray-700 dark:text-zinc-300 border border-gray-200 dark:border-zinc-700 text-[11px] font-black uppercase tracking-widest rounded-lg hover:bg-gray-50 dark:hover:bg-zinc-700 transition-all active:scale-95 disabled:opacity-50'
const iconButton = 'flex items-center justify-center w-9 h-9 rounded-lg border border-gray-200 dark:border-zinc-700 bg-white dark:bg-zinc-800 !text-gray-600 dark:!text-zinc-300 hover:!text-black dark:hover:!text-white hover:bg-gray-50 dark:hover:bg-zinc-700 transition-colors'
const menuHeader = 'px-3 py-1.5 text-[9px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500'
const section = 'text-[10px] font-bold text-gray-400 dark:text-zinc-500 uppercase tracking-widest'
const paper = 'pano-paper relative rounded-md border border-black/[0.06] shadow-[0_1px_2px_rgba(0,0,0,0.06),0_16px_32px_-16px_rgba(0,0,0,0.3)] dark:shadow-[0_2px_4px_rgba(0,0,0,0.3),0_20px_40px_-16px_rgba(0,0,0,0.45)]'
const tool = 'flex items-center justify-center w-9 h-9 rounded-lg text-gray-600 dark:text-zinc-300 hover:bg-white dark:hover:bg-zinc-700 hover:text-black dark:hover:text-white transition-colors'
const trash = 'm14.74 9-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 0 1-2.244 2.077H8.084a2.25 2.25 0 0 1-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 0 0-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 0 1 3.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 0 0-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 0 0-7.5 0'
</script>

<template>
  <div class="flex flex-col h-full w-full overflow-y-auto custom-scrollbar">
    <div class="w-full px-4 py-2 mb-4 shrink-0 flex flex-row items-center justify-between gap-4">
      <router-link to="/pano" class="text-[10px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400 hover:text-gray-800 dark:hover:text-zinc-100">← Pano</router-link>
      <div v-if="doc && !editing" class="flex shrink-0 items-center gap-2">
        <PanoTaskButton v-if="isOwnPanoDoc(doc)" :doc="doc" :button-class="`${iconButton} !rounded-lg`" />
        <button type="button" data-test="pano-edit" title="Edit" aria-label="Edit" :class="iconButton" @click="edit">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="m16.862 4.487 1.687-1.688a1.875 1.875 0 1 1 2.652 2.652L6.832 19.82a4.5 4.5 0 0 1-1.897 1.13l-2.685.8.8-2.685a4.5 4.5 0 0 1 1.13-1.897L16.863 4.487Z" /></svg>
        </button>
        <button type="button" data-test="pano-delete" title="Delete" aria-label="Delete" :class="iconButton" class="hover:!text-red-600 dark:hover:!text-red-400" @click="pendingDelete = true">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" :d="trash" /></svg>
        </button>
      </div>
    </div>

    <div class="px-4 pb-10 min-w-0">
      <p v-if="loading" class="text-[11px] text-gray-400 dark:text-zinc-500">Loading…</p>
      <p v-else-if="error" data-test="pano-doc-error" class="text-[11px] font-bold text-red-600 dark:text-red-400">{{ error }}</p>

      <!-- The studio: the note as it will look on the left, its settings on the right. -->
      <form v-else-if="editing" data-test="pano-editor" @submit.prevent="onSave" @keydown="onEditorKey"
        class="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl overflow-hidden">
        <header class="flex items-center gap-3 px-5 py-4 border-b border-gray-100 dark:border-zinc-800">
          <span class="flex items-center justify-center w-9 h-9 rounded-lg bg-gray-100 dark:bg-zinc-800 text-gray-600 dark:text-zinc-300">
            <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="m16.862 4.487 1.687-1.688a1.875 1.875 0 1 1 2.652 2.652L10.582 16.07a4.5 4.5 0 0 1-1.897 1.13L6 18l.8-2.685a4.5 4.5 0 0 1 1.13-1.897l8.932-8.931Zm0 0L19.5 7.125M18 14v4.75A2.25 2.25 0 0 1 15.75 21H5.25A2.25 2.25 0 0 1 3 18.75V8.25A2.25 2.25 0 0 1 5.25 6H10" /></svg>
          </span>
          <div class="min-w-0 flex-1">
            <h1 data-test="pano-title" class="text-lg font-black text-gray-800 dark:text-zinc-100 leading-tight">{{ id ? 'Edit note' : 'New note' }}</h1>
            <p class="text-[11px] text-gray-500 dark:text-zinc-400 truncate">{{ id ? `Pano · ${doc?.title}` : 'Pano · every workspace can read it' }}</p>
          </div>
          <button type="button" data-test="pano-cancel" title="Cancel (Esc)" aria-label="Cancel" class="p-2 rounded-lg text-gray-500 hover:text-black dark:hover:text-white hover:bg-gray-100 dark:hover:bg-zinc-800" @click="onCancel">
            <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18 18 6M6 6l12 12" /></svg>
          </button>
        </header>

        <!-- Someone else saved the note while it was being edited: the draft is kept until the person chooses. -->
        <div v-if="conflict" data-test="pano-conflict" role="alert" class="flex flex-wrap items-center gap-3 px-5 py-3 border-b border-amber-200 dark:border-amber-900/50 bg-amber-50 dark:bg-amber-950/30">
          <p class="min-w-0 flex-1 text-[12px] font-bold text-amber-800 dark:text-amber-300">
            This note changed while you were editing it, so your changes were not saved. Copy what you need from your draft, then load the latest version, which replaces it.
          </p>
          <button type="button" data-test="pano-conflict-reload" :class="primary" :disabled="busy" @click="run(reloadEditing)">Load latest</button>
        </div>

        <div class="grid lg:grid-cols-[minmax(0,1fr)_340px]">
          <section class="p-5 sm:p-8 bg-gray-50/60 dark:bg-zinc-950/40 min-w-0">
            <div class="flex items-center justify-between mb-6">
              <span :class="section">Note surface</span>
              <div class="flex p-0.5 rounded-lg bg-gray-100 dark:bg-zinc-800 text-[11px] font-bold" role="tablist">
                <button type="button" role="tab" :aria-selected="!preview" data-test="pano-write-tab" @click="preview = false"
                  :class="!preview ? 'bg-white dark:bg-zinc-700 text-gray-900 dark:text-white' : 'text-gray-500 dark:text-zinc-400'" class="px-3 py-1 rounded-md">Write</button>
                <button type="button" role="tab" :aria-selected="preview" data-test="pano-preview-tab" @click="preview = true"
                  :class="preview ? 'bg-white dark:bg-zinc-700 text-gray-900 dark:text-white' : 'text-gray-500 dark:text-zinc-400'" class="px-3 py-1 rounded-md">Preview</button>
              </div>
            </div>
            <div :class="[paper, color.card, color.body]" :style="{ '--pano-ink': color.inkHex, '--pano-tag': color.tagHex, '--pano-tag-ink': color.tagInkHex, rotate: '-0.6deg' }" class="mx-auto max-w-2xl px-6 sm:px-8 pt-9 pb-6">
              <span aria-hidden="true" :class="color.tape" class="absolute -top-3 left-1/2 -translate-x-1/2 -rotate-2 w-24 h-6 rounded-[2px]"></span>
              <label for="pano-title-input" class="sr-only">Title</label>
              <input id="pano-title-input" v-model="draft.title" data-test="pano-title-input" maxlength="200" placeholder="Title"
                :class="color.title" class="w-full bg-transparent text-2xl font-black leading-snug placeholder-black/30 focus:outline-none" />
              <div class="my-3 border-t border-black/10"></div>
              <label for="pano-markdown-input" class="sr-only">Markdown</label>
              <textarea v-show="!preview" id="pano-markdown-input" ref="markdownInput" v-model="draft.markdown" data-test="pano-markdown-input" rows="14"
                class="w-full min-h-[18rem] bg-transparent text-[14px] leading-relaxed placeholder-black/35 resize-y focus:outline-none"
                placeholder="Write in markdown. - [ ] starts a checklist item.&#10;&#10;Files you add are linked here, like ![chart](chart.png)."></textarea>
              <div v-if="preview" data-test="pano-preview" class="md-body min-h-[18rem] text-[14px] [&_img]:max-w-full" v-html="draftBody"></div>
            </div>
          </section>

          <aside class="p-5 sm:p-6 space-y-6 border-t lg:border-t-0 lg:border-l border-gray-100 dark:border-zinc-800 min-w-0">
            <div>
              <div class="flex items-baseline justify-between">
                <span :class="label">Paper</span>
                <span data-test="pano-color-label" class="text-[11px] font-bold text-gray-500 dark:text-zinc-400">{{ color.label }}</span>
              </div>
              <div class="grid grid-cols-7 gap-2" role="radiogroup" aria-label="Color">
                <button v-for="c in PANO_COLORS" :key="c.name" type="button" role="radio" :aria-checked="draft.color === c.name" :title="c.label" :aria-label="c.label"
                  :data-test="`pano-color-${c.name}`" @click="draft.color = c.name"
                  :class="[c.swatch, draft.color === c.name ? `ring-2 ring-offset-2 ring-black dark:ring-white dark:ring-offset-zinc-900 ${c.ink}` : 'text-transparent']"
                  class="flex items-center justify-center h-11 rounded-lg border border-black/10 hover:scale-105 transition-transform">
                  <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="m4.5 12.75 6 6 9-13.5" /></svg>
                </button>
              </div>
            </div>

            <div>
              <span :class="label">Formatting</span>
              <div class="flex flex-wrap items-center gap-1 p-1 rounded-lg bg-gray-100 dark:bg-zinc-800">
                <button v-for="b in FORMAT_BUTTONS" :key="b.kind" type="button" :title="b.label" :aria-label="b.label" :data-test="`pano-format-${b.kind}`" :class="tool" @click="format(b.kind)">
                  <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" :d="b.path" /></svg>
                </button>
                <span class="mx-1 h-5 w-px bg-gray-300 dark:bg-zinc-600" aria-hidden="true"></span>
                <button type="button" title="Add a file" aria-label="Add a file" data-test="pano-add-file" :class="tool" @click="draftFileInput.click()">
                  <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="m18.375 12.739-7.693 7.693a4.5 4.5 0 0 1-6.364-6.364l10.94-10.94A3 3 0 1 1 19.5 7.372L8.552 18.32m.009-.01-.01.01m5.699-9.941-7.81 7.81a1.5 1.5 0 0 0 2.112 2.13" /></svg>
                </button>
                <input ref="draftFileInput" type="file" multiple class="hidden" data-test="pano-draft-file" @change="onDraftFiles" />
              </div>
              <p class="text-[11px] text-gray-400 dark:text-zinc-500 mt-2">A file is linked from the note as you add it, and dropped on save once nothing links it.</p>
              <ul v-if="draft.files.length" class="mt-2 space-y-1">
                <li v-for="f in draft.files" :key="f.filename" class="flex items-center gap-2 text-[12px] text-gray-700 dark:text-zinc-300 min-w-0">
                  <span class="min-w-0 truncate font-mono">{{ f.filename }}</span>
                  <button type="button" class="shrink-0 text-[10px] font-black uppercase tracking-widest text-gray-400 hover:text-red-600" @click="removeDraftFile(f.filename)">Remove</button>
                </li>
              </ul>
            </div>

            <div>
              <label :class="label" for="pano-ttl-input">Lives for</label>
              <div class="flex items-center gap-2">
                <input id="pano-ttl-input" v-model.number="draft.ttlDays" data-test="pano-ttl-input" type="number" min="1" :class="field" class="!w-24" />
                <span class="text-xs text-gray-500 dark:text-zinc-400">days after each save</span>
              </div>
            </div>

            <div class="pt-2 space-y-2">
              <div class="flex gap-2">
                <button type="submit" data-test="pano-save" :class="primary" class="flex-1 !py-3" :disabled="!canSave">{{ id ? 'Save changes' : 'Post note' }}</button>
                <button v-if="id" type="button" title="Delete" aria-label="Delete" data-test="pano-editor-delete" @click="pendingDelete = true"
                  class="px-3 rounded-lg bg-red-50 dark:bg-red-950/40 text-red-600 dark:text-red-400 hover:bg-red-100 dark:hover:bg-red-950/70">
                  <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" :d="trash" /></svg>
                </button>
              </div>
              <p class="text-center text-[10px] text-gray-400 dark:text-zinc-500">
                <kbd class="px-1 rounded bg-gray-100 dark:bg-zinc-800 font-mono">⌘ S</kbd> to save · <kbd class="px-1 rounded bg-gray-100 dark:bg-zinc-800 font-mono">Esc</kbd> to cancel
              </p>
            </div>
          </aside>
        </div>
      </form>

      <div v-else-if="doc" class="grid gap-8 lg:grid-cols-[minmax(0,1fr)_320px] items-start">
        <article :class="[paper, color.card, color.body]" :style="{ '--pano-ink': color.inkHex, '--pano-tag': color.tagHex, '--pano-tag-ink': color.tagInkHex, rotate: `${panoTilt(doc.id) / 4}deg` }" class="mt-3 px-6 sm:px-10 pt-10 pb-8 min-w-0">
          <span aria-hidden="true" :class="color.tape" class="absolute -top-3 left-1/2 -translate-x-1/2 -rotate-2 w-24 h-6 rounded-[2px]"></span>
          <h1 data-test="pano-title" :class="color.title" class="text-2xl md:text-3xl font-black leading-tight break-words">{{ doc.title }}</h1>
          <p data-test="pano-meta" :class="color.muted" class="mt-1 text-[11px]">
            By {{ panoAuthor(doc.createdByWorkspaceId, workspaces) }} · updated {{ panoDate(doc.updatedAt) }} by {{ panoAuthor(doc.updatedByWorkspaceId, workspaces) }}
          </p>
          <PanoProgress :doc="doc" class="mt-4 max-w-sm" />
          <div class="my-5 border-t border-black/10"></div>
          <div data-test="pano-body" class="md-body text-[14px] break-words [&_img]:max-w-full [&_img]:rounded" v-html="body" @click="onBodyClick"></div>
          <div v-if="itemMenu" v-click-outside="closeItemMenu" data-test="pano-item-menu" role="menu" @keydown.esc="closeItemMenu()"
            :style="{ top: `${itemMenu.top}px`, left: `${itemMenu.left}px` }"
            class="absolute z-30 w-64 max-h-72 overflow-y-auto custom-scrollbar bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700 rounded-lg py-1 text-left shadow-lg">
            <template v-if="itemMenu.task">
              <p :class="menuHeader">Assigned</p>
              <a :href="itemMenu.task.path" data-test="pano-item-task" role="menuitem" @click.prevent="openTask(itemMenu.task.path)" class="flex items-center gap-2 px-3 py-1.5 text-xs text-gray-700 dark:text-zinc-200 hover:bg-gray-100 dark:hover:bg-zinc-800 min-w-0">
                <span class="min-w-0 truncate font-bold">{{ itemMenu.task.name }}</span>
                <span class="shrink-0 text-[10px] text-gray-500 dark:text-zinc-400">{{ PANO_TASK_STATUSES[itemMenu.task.status] }}</span>
                <span class="ml-auto shrink-0 font-mono text-[10px] text-gray-400 dark:text-zinc-500">{{ itemMenu.task.id }}</span>
              </a>
              <div class="my-1 border-t border-gray-100 dark:border-zinc-800"></div>
            </template>
            <p :class="menuHeader">{{ itemMenu.task ? 'Reassign to' : 'Assign to' }}</p>
            <p v-if="!workspaces.length" class="px-3 py-1.5 text-[11px] text-gray-500 dark:text-zinc-400">No workspaces yet.</p>
            <button v-for="w in workspaces" :key="w.id" type="button" role="menuitem" data-test="pano-item-assign" :disabled="busy" @click="onAssign(w)"
              class="block w-full truncate px-3 py-1.5 text-left text-xs text-gray-700 dark:text-zinc-200 hover:bg-gray-100 dark:hover:bg-zinc-800 disabled:opacity-50">{{ w.name }}</button>
          </div>
        </article>

        <aside class="space-y-6 min-w-0">
          <dl class="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1.5 text-[12px]">
            <dt class="text-gray-400 dark:text-zinc-500">Id</dt><dd class="font-mono text-gray-700 dark:text-zinc-300 truncate">{{ doc.id }}</dd>
            <dt class="text-gray-400 dark:text-zinc-500">Expires</dt><dd class="text-gray-700 dark:text-zinc-300">{{ expiresLabel(doc.expiresAt) }}</dd>
            <dt class="text-gray-400 dark:text-zinc-500">Paper</dt><dd class="text-gray-700 dark:text-zinc-300">{{ color.label }}</dd>
          </dl>

          <section v-if="doc.files?.length" class="space-y-2">
            <h3 :class="section">Linked files</h3>
            <ul class="space-y-1">
              <li v-for="f in doc.files" :key="f.id" class="flex items-center gap-2 text-[12px] min-w-0">
                <a :href="f.url" target="_blank" rel="noopener" class="min-w-0 truncate font-mono text-gray-700 dark:text-zinc-300 underline decoration-dotted underline-offset-2">{{ f.filename }}</a>
                <span class="shrink-0 text-[10px] text-gray-400 dark:text-zinc-500">{{ formatFileSize(f.size) }}</span>
              </li>
            </ul>
          </section>

          <section class="space-y-2">
            <div class="flex items-center justify-between gap-2">
              <h3 :class="section">Attachments</h3>
              <button type="button" data-test="pano-attach" class="text-[10px] font-black uppercase tracking-widest text-gray-500 dark:text-zinc-400 hover:text-black dark:hover:text-white disabled:opacity-50" :disabled="busy" @click="attachInput.click()">+ Attach</button>
              <input ref="attachInput" type="file" multiple class="hidden" data-test="pano-attach-input" @change="onAttach" />
            </div>
            <p v-if="!doc.attachments?.length" class="text-[11px] text-gray-400 dark:text-zinc-500">No attachments.</p>
            <ul v-else data-test="pano-attachments" class="space-y-1">
              <li v-for="f in doc.attachments" :key="f.id" class="flex items-center gap-2 text-[12px] min-w-0">
                <a :href="f.url" target="_blank" rel="noopener" class="min-w-0 truncate font-mono text-gray-700 dark:text-zinc-300 underline decoration-dotted underline-offset-2">{{ f.filename }}</a>
                <span class="shrink-0 text-[10px] text-gray-400 dark:text-zinc-500">{{ formatFileSize(f.size) }} · {{ panoAuthor(f.workspaceId, workspaces) }}</span>
              </li>
            </ul>
          </section>

          <section v-if="relatedTasks.length" data-test="pano-related" class="hidden lg:block space-y-2">
            <h3 :class="section">Related tasks</h3>
            <ul class="space-y-1">
              <li v-for="t in relatedTasks" :key="t.path" class="flex items-center gap-2 text-[12px] min-w-0">
                <span class="w-3.5 h-3.5 rounded-full shrink-0 flex items-center justify-center border-[1.5px]" :class="taskRingClass(t.status)" data-test="pano-related-status" role="img" :aria-label="PANO_TASK_STATUSES[t.status]" :title="PANO_TASK_STATUSES[t.status]"><svg v-if="t.status === 'completed'" viewBox="0 0 24 24" fill="none" stroke="white" stroke-width="3.5" stroke-linecap="round" stroke-linejoin="round" class="w-[70%] h-[70%]"><path d="m5 12.5 4.5 4.5L19 7.5"/></svg></span>
                <a :href="t.path" data-test="pano-related-task" @click.prevent="openTask(t.path)"
                  class="min-w-0 truncate font-bold text-gray-700 dark:text-zinc-300 underline decoration-dotted underline-offset-2" :title="taskTitles[t.path] || t.name">{{ taskTitles[t.path] || t.name }}</a>
                <span data-test="pano-related-workspace" class="shrink-0 text-[10px] text-gray-400 dark:text-zinc-500">{{ panoAuthor(t.workspaceId, workspaces) }}</span>
              </li>
            </ul>
          </section>

          <section class="space-y-3">
            <h3 :class="section">Comments</h3>
            <p v-if="!doc.comments?.length" class="text-[11px] text-gray-400 dark:text-zinc-500">No comments yet.</p>
            <ul v-else data-test="pano-comments" class="space-y-3">
              <li v-for="c in doc.comments" :key="c.id" class="border-l-2 border-gray-200 dark:border-zinc-700 pl-3 min-w-0">
                <p class="text-[10px] text-gray-500 dark:text-zinc-400"><span class="font-bold text-gray-700 dark:text-zinc-200">{{ panoAuthor(c.workspaceId, workspaces) }}</span> · {{ panoDate(c.createdAt) }}</p>
                <div class="md-body text-[13px] text-gray-800 dark:text-zinc-200 break-words" v-html="panoMentions(renderMarkdown(c.body), workspaces)"></div>
              </li>
            </ul>
            <!-- The task chat's composer: a quiet box, the send button inside it. -->
            <form @submit.prevent="onComment"
              class="relative flex flex-col bg-white dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700 rounded-sm focus-within:border-gray-900 dark:focus-within:border-white transition-all min-w-0 shadow-sm">
              <ul v-if="mentionOptions.length" data-test="pano-mention-menu" role="listbox"
                class="absolute left-0 right-0 bottom-full mb-1 z-30 max-h-56 overflow-y-auto custom-scrollbar bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700 rounded-lg py-1 shadow-lg">
                <li v-for="(w, i) in mentionOptions" :key="w.id" role="option" :aria-selected="i === mentionIndex" data-test="pano-mention-option"
                  @mousedown.prevent="pickMention(w)" @mouseenter="mentionIndex = i"
                  :class="i === mentionIndex ? 'bg-gray-100 dark:bg-zinc-800' : ''"
                  class="px-3 py-1.5 text-xs text-gray-700 dark:text-zinc-200 truncate cursor-pointer">@{{ w.name }}</li>
              </ul>
              <textarea ref="commentInput" v-model="commentText" data-test="pano-comment-input" rows="2" placeholder="Comment, @ a workspace to ping its agent (⌘ + Enter to send)"
                @input="onCommentInput" @click="onCommentInput" @keydown="onCommentKey" @blur="mention = null"
                class="w-full px-3.5 pt-3 pb-1.5 text-[13px] font-medium text-gray-800 dark:text-zinc-200 bg-transparent outline-none border-none focus:outline-none focus:ring-0 resize-none min-h-[46px] max-h-[150px] custom-scrollbar"></textarea>
              <div class="flex justify-end px-3 pb-2 pt-1">
                <button type="submit" data-test="pano-comment" title="Comment" aria-label="Comment" :disabled="busy || !commentText.trim()"
                  class="h-6 w-6 rounded-full bg-black dark:bg-white text-white dark:text-zinc-900 hover:opacity-90 disabled:opacity-30 transition-all flex items-center justify-center shrink-0 shadow-sm">
                  <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M5 10l7-7m0 0l7 7m-7-7v18" /></svg>
                </button>
              </div>
            </form>
          </section>
        </aside>
      </div>
    </div>

    <DeleteModal
      :show="pendingDelete"
      title="Delete Doc"
      :message="`Delete '${doc?.title}' with its files and comments? Every workspace loses it. This cannot be undone.`"
      @close="pendingDelete = false"
      @confirm="onDelete"
    />
  </div>
</template>
