// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { computed, ref } from 'vue'

/**
 * The Pano: the account's board of markdown docs, written by every workspace's
 * agents and by the person. The board and the doc page are thin; every rule
 * they follow is here, where it is tested.
 */

// The server's limits, mirrored so the page can refuse before uploading. The
// server checks them again whatever is sent.
export const PANO_DEFAULT_TTL_DAYS = 30
export const PANO_MAX_FILE_BYTES = 2 * 1024 * 1024

// A linked file's name, as the server accepts one: one plain path segment.
const LINKED_NAME = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/

/**
 * The post-it colors, in the server's order, the first the default: Tailwind's
 * 50 tint of each hue on a light page and its 200 on a dark one, always with
 * dark text, so a note reads the same either way. The 200 is the swatch, so
 * the choices tell apart; the 600 marks checkmarks, bars and avatars, and the
 * 800/900 is the title's ink.
 */
// `ink` is the color's own deep shade, for a title and the author's mark;
// `tape` the strip that holds the note up.
export const PANO_COLORS = [
  { name: 'yellow', label: 'Canary yellow', card: 'bg-[#fffbeb] dark:bg-[#fde68a]', swatch: 'bg-[#fde68a]', ink: 'text-[#713f12]', inkBg: 'bg-[#d97706]', inkHex: '#d97706', tape: 'bg-[#fef08a]/80 dark:bg-[#fcd34d]/80' },
  { name: 'pink', label: 'Blush pink', card: 'bg-[#fff1f2] dark:bg-[#fecdd3]', swatch: 'bg-[#fecdd3]', ink: 'text-[#881337]', inkBg: 'bg-[#e11d48]', inkHex: '#e11d48', tape: 'bg-[#fecdd3]/80 dark:bg-[#fda4af]/80' },
  { name: 'green', label: 'Mint green', card: 'bg-[#f0fdf4] dark:bg-[#bbf7d0]', swatch: 'bg-[#bbf7d0]', ink: 'text-[#14532d]', inkBg: 'bg-[#16a34a]', inkHex: '#16a34a', tape: 'bg-[#bbf7d0]/80 dark:bg-[#86efac]/80' },
  { name: 'blue', label: 'Sky blue', card: 'bg-[#eff6ff] dark:bg-[#bfdbfe]', swatch: 'bg-[#bfdbfe]', ink: 'text-[#1e40af]', inkBg: 'bg-[#2563eb]', inkHex: '#2563eb', tape: 'bg-[#bfdbfe]/80 dark:bg-[#93c5fd]/80' },
  { name: 'orange', label: 'Peach orange', card: 'bg-[#fff7ed] dark:bg-[#fed7aa]', swatch: 'bg-[#fed7aa]', ink: 'text-[#7c2d12]', inkBg: 'bg-[#ea580c]', inkHex: '#ea580c', tape: 'bg-[#fed7aa]/80 dark:bg-[#fdba74]/80' },
  { name: 'purple', label: 'Lilac purple', card: 'bg-[#faf5ff] dark:bg-[#e9d5ff]', swatch: 'bg-[#e9d5ff]', ink: 'text-[#6b21a8]', inkBg: 'bg-[#9333ea]', inkHex: '#9333ea', tape: 'bg-[#e9d5ff]/80 dark:bg-[#d8b4fe]/80' },
]

/** A color by name; an unknown one is yellow. */
export function panoColor(name) {
  return PANO_COLORS.find((c) => c.name === name) || PANO_COLORS[0]
}

/** The classes of a doc's post-it; an unknown color is yellow. */
export function panoColorClass(color) {
  return panoColor(color).card
}

/** Docs whose title holds the query, ignoring case; every doc for none. */
export function filterPanoDocs(docs, query) {
  const q = String(query || '').trim().toLowerCase()
  if (!q) return docs
  return docs.filter((d) => String(d.title || '').toLowerCase().includes(q))
}

/** Who wrote something: "You" for the person, else the workspace's name. */
export function panoAuthor(workspaceId, workspaces = []) {
  if (!workspaceId) return 'You'
  const w = workspaces.find((ws) => ws.id === workspaceId)
  return w?.name || `Workspace ${workspaceId}`
}

const DAY = 24 * 60 * 60 * 1000

/** How long a doc has left, in whole days, as its card says it. */
export function expiresLabel(expiresAt, now = Date.now()) {
  const left = Math.floor((new Date(expiresAt).getTime() - now) / DAY)
  if (!(left > 0)) return 'Expires today'
  return left === 1 ? 'Expires in 1 day' : `Expires in ${left} days`
}

/**
 * A file's name made into one a markdown link can name, as the server asks
 * of a linked file: spaces and other characters become '-'.
 */
export function panoLinkName(filename) {
  const name = String(filename || '')
    .replace(/[^A-Za-z0-9._-]+/g, '-')
    .replace(/^[^A-Za-z0-9]+/, '')
    .slice(0, 128)
  return LINKED_NAME.test(name) ? name : 'file'
}

/** The markdown with a link to a file added on a line of its own. */
export function addFileLink(markdown, name, mimeType) {
  const link = String(mimeType || '').startsWith('image/') ? `![${name}](${name})` : `[${name}](${name})`
  const text = String(markdown || '')
  if (!text) return link
  return text.endsWith('\n') ? `${text}${link}` : `${text}\n\n${link}`
}

/** A browser File as the API takes one: its content base64. */
export async function readPanoUpload(file, filename = file.name) {
  const bytes = new Uint8Array(await file.arrayBuffer())
  let binary = ''
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  }
  return { filename, mimeType: file.type || '', data: btoa(binary) }
}

// A link's target as a file name: no "./", query or fragment, decoded.
function linkTarget(value) {
  let target = String(value || '').replace(/^\.\//, '')
  const cut = target.search(/[?#]/)
  if (cut >= 0) target = target.slice(0, cut)
  try {
    return decodeURIComponent(target)
  } catch {
    return target
  }
}

/**
 * Rendered markdown with its links and images to the doc's linked files
 * pointed at their public links, which is how the server names them.
 */
export function linkPanoFiles(html, files = []) {
  if (!files.length) return html
  const urls = new Map(files.filter((f) => f.url).map((f) => [f.filename, f.url]))
  const holder = document.createElement('div')
  holder.innerHTML = html
  for (const [selector, attr] of [['img[src]', 'src'], ['a[href]', 'href']]) {
    for (const el of holder.querySelectorAll(selector)) {
      const url = urls.get(linkTarget(el.getAttribute(attr)))
      if (url) el.setAttribute(attr, url)
    }
  }
  return holder.innerHTML
}

/** A date the way the Pano shows one: "Oct 13, 14:05". */
export function panoDate(iso) {
  const at = new Date(iso)
  const time = at.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', hour12: false })
  return `${at.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })}, ${time}`
}

/** How long ago something was: "just now", "5m ago", "2h ago", "3d ago". */
export function timeAgo(iso, now = Date.now()) {
  const minutes = Math.floor((now - new Date(iso).getTime()) / 60000)
  if (!(minutes >= 1)) return 'just now'
  if (minutes < 60) return `${minutes}m ago`
  if (minutes < 24 * 60) return `${Math.floor(minutes / 60)}h ago`
  return `${Math.floor(minutes / (24 * 60))}d ago`
}

/** The letter a name's round mark shows. */
export function panoInitial(name) {
  return (String(name || '').trim()[0] || '?').toUpperCase()
}

// What each formatting button does: wrap the selection, or start its lines.
const FORMATS = {
  bold: { wrap: '**', empty: 'bold' },
  italic: { wrap: '_', empty: 'italic' },
  code: { wrap: '`', empty: 'code' },
  list: { prefix: '- ' },
  checkbox: { prefix: '- [ ] ' },
}

/**
 * Applies a formatting button to the markdown's selection [start, end): bold,
 * italic and code wrap it (a word to replace, when nothing is selected); list
 * and checkbox start each selected line. Returns the new markdown and the
 * selection to restore.
 */
export function formatMarkdown(value, start, end, kind) {
  const f = FORMATS[kind]
  const text = String(value || '')
  if (f.wrap) {
    const inner = text.slice(start, end) || f.empty
    const at = start + f.wrap.length
    return { value: text.slice(0, start) + f.wrap + inner + f.wrap + text.slice(end), start: at, end: at + inner.length }
  }
  const lineStart = text.lastIndexOf('\n', start - 1) + 1
  const lines = text.slice(lineStart, end).split('\n').map((line) => f.prefix + line)
  const block = lines.join('\n')
  return { value: text.slice(0, lineStart) + block + text.slice(end), start: lineStart + block.length, end: lineStart + block.length }
}

// A task-list item, as the server counts one: a list marker, then [ ] or [x].
const TASK_ITEM = /^([ \t]*(?:[-*+]|\d{1,9}[.)])[ \t]+\[)([ xX])(\](?:[ \t]|$))/

/**
 * The markdown with its task-list item number `index` (from 0, in the order
 * they render) checked or unchecked; fenced code holds none, as on the server.
 * Unchanged when there is no such item.
 */
export function togglePanoCheckbox(markdown, index, checked) {
  const lines = String(markdown || '').split('\n')
  let fence = ''
  let seen = 0
  for (let i = 0; i < lines.length; i++) {
    const trimmed = lines[i].replace(/^ +/, '')
    if (fence) {
      if (trimmed.startsWith(fence)) fence = ''
      continue
    }
    if (trimmed.startsWith('```') || trimmed.startsWith('~~~')) {
      fence = trimmed.slice(0, 3)
      continue
    }
    const m = TASK_ITEM.exec(lines[i])
    if (!m) continue
    if (seen++ === index) {
      lines[i] = m[1] + (checked ? 'x' : ' ') + m[3] + lines[i].slice(m[0].length)
      return lines.join('\n')
    }
  }
  return lines.join('\n')
}

/** Rendered markdown with its task-list checkboxes made clickable. */
export function panoCheckable(html) {
  return String(html || '').replace(/<input\b[^>]*>/g, (tag) => (tag.includes('type="checkbox"') ? tag.replace(/ disabled(="[^"]*")?/, '') : tag))
}

/** Which of a container's checkboxes was clicked, from 0, or -1 for none. */
export function panoCheckboxIndex(container, target) {
  if (!target?.matches?.('input[type="checkbox"]')) return -1
  return [...container.querySelectorAll('input[type="checkbox"]')].indexOf(target)
}

/** How much of a doc's checklist is done, as a whole percentage. */
export function checklistPercent(doc) {
  return doc?.checklistTotal ? Math.round((100 * doc.checklistDone) / doc.checklistTotal) : 0
}

/**
 * Checks or unchecks a doc's checklist item from the board, where the doc
 * carries only its excerpt: it is read, changed and written back.
 */
export async function checkPanoItem(doc, index, checked, { api }) {
  const { doc: full } = await api.getPanoDoc(doc.id)
  const res = await api.updatePanoDoc(doc.id, { title: full.title, markdown: togglePanoCheckbox(full.markdown, index, checked), ttlDays: 0, color: '', files: [] })
  return res.doc
}

/**
 * Gives a doc another color. The list's docs carry no markdown, so it is read
 * first and written back as it is; like any save, this one restarts its life.
 */
export async function recolorPanoDoc(doc, color, { api }) {
  const { doc: full } = await api.getPanoDoc(doc.id)
  const res = await api.updatePanoDoc(doc.id, { title: full.title, markdown: full.markdown, ttlDays: 0, color, files: [] })
  return res.doc
}

/** A file's size the way the page shows it. */
export function formatFileSize(bytes) {
  const n = Number(bytes) || 0
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / (1024 * 1024)).toFixed(1)} MB`
}

/** Whether the person wrote a doc, which they alone can turn into a task. */
export function isOwnPanoDoc(doc) {
  return !doc?.createdByWorkspaceId
}

/**
 * The body of the task a doc becomes: its markdown, then where it came from,
 * so the agent can read the doc's files and comments with the pano tool.
 */
export function panoTaskBody(doc) {
  return `${doc.markdown}\n\n---\nFrom the Pano doc \`${doc.id}\`: read it with the pano tool for its files and comments.`
}

/**
 * Turns a doc into a task for a workspace's agent, and returns the task. The
 * list's docs carry no markdown, so it is read first; the doc is kept.
 */
export async function convertPanoDocToTask(doc, workspaceId, { api }) {
  const full = doc.markdown === undefined ? (await api.getPanoDoc(doc.id)).doc : doc
  const res = await api.createTask(workspaceId, full.title, panoTaskBody(full))
  return res.task
}

/** The board: every doc of the account, filtered by title as the person types. */
export function usePanoBoard({ api }) {
  const docs = ref([])
  const loading = ref(false)
  const error = ref('')
  const query = ref('')
  const shown = computed(() => filterPanoDocs(docs.value, query.value))

  // quiet reloads behind a live update without the loading state.
  async function load({ quiet = false } = {}) {
    if (!quiet) loading.value = true
    try {
      const res = await api.listPanoDocs()
      docs.value = res.docs || []
      error.value = ''
    } catch (err) {
      error.value = err.message || 'Failed to fetch the Pano'
    } finally {
      loading.value = false
    }
  }

  return { docs, loading, error, query, shown, load }
}

/**
 * One doc: read, edited, commented on and given attachments. `id` is null for
 * a doc not written yet, which opens in the editor.
 */
export function usePanoDoc(id, { api, color = PANO_COLORS[0].name }) {
  const doc = ref(null)
  const loading = ref(false)
  const error = ref('')
  const busy = ref(false)
  const editing = ref(!id)
  const draft = ref({ title: '', markdown: '', ttlDays: PANO_DEFAULT_TTL_DAYS, color: panoColor(color).name, files: [] })

  async function load({ quiet = false } = {}) {
    if (!id) return
    if (!quiet) loading.value = true
    try {
      const res = await api.getPanoDoc(id)
      doc.value = res.doc
      error.value = ''
    } catch (err) {
      error.value = err.status === 404 ? 'This Pano doc does not exist, or it has expired.' : err.message || 'Failed to fetch the Pano doc'
    } finally {
      loading.value = false
    }
  }

  function startEditing() {
    const d = doc.value
    draft.value = { title: d.title, markdown: d.markdown, ttlDays: d.ttlDays, color: d.color, files: [] }
    editing.value = true
  }

  function cancelEditing() {
    editing.value = false
  }

  /**
   * Adds a file to the draft, linked from the markdown so the save accepts it.
   * Refuses one over the size limit, rather than have the upload fail.
   */
  async function addDraftFile(file) {
    if (file.size > PANO_MAX_FILE_BYTES) {
      throw new Error(`${file.name} is over the ${formatFileSize(PANO_MAX_FILE_BYTES)} limit`)
    }
    const name = panoLinkName(file.name)
    const upload = await readPanoUpload(file, name)
    const d = draft.value
    d.files = [...d.files.filter((f) => f.filename !== name), upload]
    d.markdown = addFileLink(d.markdown, name, upload.mimeType)
  }

  function removeDraftFile(filename) {
    draft.value.files = draft.value.files.filter((f) => f.filename !== filename)
  }

  /** Saves the draft and returns the doc's id. */
  async function save() {
    busy.value = true
    try {
      const d = draft.value
      const body = { title: d.title, markdown: d.markdown, ttlDays: Number(d.ttlDays) || 0, color: d.color, files: d.files }
      const res = await (id ? api.updatePanoDoc(id, body) : api.createPanoDoc(body))
      editing.value = false
      if (id) {
        await load({ quiet: true })
      }
      return res.doc.id
    } finally {
      busy.value = false
    }
  }

  /** Checks or unchecks a checklist item of the doc as it is shown. */
  async function checkItem(index, checked) {
    busy.value = true
    try {
      const d = doc.value
      await api.updatePanoDoc(id, { title: d.title, markdown: togglePanoCheckbox(d.markdown, index, checked), ttlDays: 0, color: '', files: [] })
      await load({ quiet: true })
    } finally {
      busy.value = false
    }
  }

  async function remove() {
    busy.value = true
    try {
      await api.deletePanoDoc(id)
    } finally {
      busy.value = false
    }
  }

  async function comment(body) {
    busy.value = true
    try {
      await api.addPanoComment(id, body)
      await load({ quiet: true })
    } finally {
      busy.value = false
    }
  }

  async function attach(files) {
    for (const f of files) {
      if (f.size > PANO_MAX_FILE_BYTES) throw new Error(`${f.name} is over the ${formatFileSize(PANO_MAX_FILE_BYTES)} limit`)
    }
    busy.value = true
    try {
      const uploads = await Promise.all([...files].map((f) => readPanoUpload(f)))
      await api.attachPanoFiles(id, uploads)
      await load({ quiet: true })
    } finally {
      busy.value = false
    }
  }

  return {
    doc, loading, error, busy, editing, draft,
    load, startEditing, cancelEditing, addDraftFile, removeDraftFile, save, checkItem, remove, comment, attach,
  }
}
