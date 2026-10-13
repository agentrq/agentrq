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
 * The post-it colors, in the server's order, the first the default, as the
 * Pano's design draws them. Each is a pale paper (`card`), the same in both
 * themes, written on in its own ink: `title`, `body` and the footer's `muted`.
 * Its label is one step deeper than the paper (`tag`; as hex `tagHex` and
 * `tagInkHex`), the bar runs on its `track`, and checkmarks and bars are its
 * accent (`inkBg`/`inkHex`). `avatar` marks the author, `tape` holds the note
 * up, `swatch` shows the paper in a picker and `dot` the color in a filter.
 */
export const PANO_COLORS = [
  { name: 'yellow', label: 'Canary yellow', card: 'bg-[#fffbeb]', swatch: 'bg-[#fef08a]', dot: 'bg-[#facc15]', title: 'text-[#713f12]', body: 'text-[#542d0c]', muted: 'text-[#854d0e]', tag: 'bg-[#fef08a] text-[#713f12]', tagHex: '#fef08a', tagInkHex: '#713f12', ink: 'text-[#713f12]', inkBg: 'bg-[#b15f00]', inkHex: '#b15f00', avatar: 'bg-[#b15f00]', track: 'bg-[#fef9c3]', tape: 'bg-[#fef08a]/80' },
  { name: 'pink', label: 'Pink', card: 'bg-[#fdf2f8]', swatch: 'bg-[#fbcfe8]', dot: 'bg-[#f472b6]', title: 'text-[#9d174d]', body: 'text-[#831843]', muted: 'text-[#be185d]', tag: 'bg-[#fce7f3] text-[#be185d]', tagHex: '#fce7f3', tagInkHex: '#be185d', ink: 'text-[#be185d]', inkBg: 'bg-[#db2777]', inkHex: '#db2777', avatar: 'bg-[#be185d]', track: 'bg-[#fce7f3]', tape: 'bg-[#fbcfe8]/80' },
  { name: 'green', label: 'Mint green', card: 'bg-[#f0fdf4]', swatch: 'bg-[#bbf7d0]', dot: 'bg-[#86efac]', title: 'text-[#166534]', body: 'text-[#14532d]', muted: 'text-[#15803d]', tag: 'bg-[#dcfce7] text-[#15803d]', tagHex: '#dcfce7', tagInkHex: '#15803d', ink: 'text-[#15803d]', inkBg: 'bg-[#16a34a]', inkHex: '#16a34a', avatar: 'bg-[#15803d]', track: 'bg-[#dcfce7]', tape: 'bg-[#bbf7d0]/80' },
  { name: 'blue', label: 'Sky blue', card: 'bg-[#eff6ff]', swatch: 'bg-[#bfdbfe]', dot: 'bg-[#93c5fd]', title: 'text-[#1e40af]', body: 'text-[#1e3a8a]', muted: 'text-[#1e40af]', tag: 'bg-[#dbeafe] text-[#1e40af]', tagHex: '#dbeafe', tagInkHex: '#1e40af', ink: 'text-[#1e40af]', inkBg: 'bg-[#2563eb]', inkHex: '#2563eb', avatar: 'bg-[#1e40af]', track: 'bg-[#dbeafe]', tape: 'bg-[#bfdbfe]/80' },
  { name: 'orange', label: 'Coral', card: 'bg-[#fff1f2]', swatch: 'bg-[#fecdd3]', dot: 'bg-[#fda4af]', title: 'text-[#881337]', body: 'text-[#4c0519]', muted: 'text-[#9f1239]', tag: 'bg-[#ffe4e6] text-[#be123c]', tagHex: '#ffe4e6', tagInkHex: '#be123c', ink: 'text-[#be123c]', inkBg: 'bg-[#e11d48]', inkHex: '#e11d48', avatar: 'bg-[#be123c]', track: 'bg-[#ffe4e6]', tape: 'bg-[#fecdd3]/80' },
  { name: 'purple', label: 'Lavender', card: 'bg-[#faf5ff]', swatch: 'bg-[#e9d5ff]', dot: 'bg-[#d8b4fe]', title: 'text-[#6b21a8]', body: 'text-[#581c87]', muted: 'text-[#7e22ce]', tag: 'bg-[#f3e8ff] text-[#7e22ce]', tagHex: '#f3e8ff', tagInkHex: '#7e22ce', ink: 'text-[#7e22ce]', inkBg: 'bg-[#9333ea]', inkHex: '#9333ea', avatar: 'bg-[#7e22ce]', track: 'bg-[#f3e8ff]', tape: 'bg-[#e9d5ff]/80' },
  { name: 'amber', label: 'Golden amber', card: 'bg-[#fef3c7]', swatch: 'bg-[#fde68a]', dot: 'bg-[#fbbf24]', title: 'text-[#92400e]', body: 'text-[#451a03]', muted: 'text-[#92400e]', tag: 'bg-[#fde68a] text-[#92400e]', tagHex: '#fde68a', tagInkHex: '#92400e', ink: 'text-[#92400e]', inkBg: 'bg-[#b45309]', inkHex: '#b45309', avatar: 'bg-[#b45309]', track: 'bg-[#fde68a]', tape: 'bg-[#fcd34d]/80' },
]

/** A color by name; an unknown one is yellow. */
export function panoColor(name) {
  return PANO_COLORS.find((c) => c.name === name) || PANO_COLORS[0]
}

// The angles a note leans at, in degrees, as paper stuck up by hand does.
const TILTS = [-2, -1.4, -0.8, 0.7, 1.3, 1.9]

/**
 * How far a doc's post-it leans, picked from its id so a note keeps its angle
 * wherever the board puts it.
 */
export function panoTilt(id) {
  let h = 0
  for (const ch of String(id || '')) h = (h * 31 + ch.charCodeAt(0)) >>> 0
  return TILTS[h % TILTS.length]
}

/** The classes of a doc's post-it; an unknown color is yellow. */
export function panoColorClass(color) {
  return panoColor(color).card
}

/** Docs whose title holds the query, ignoring case, in the color picked; every doc for neither. */
export function filterPanoDocs(docs, query, color = '') {
  const q = String(query || '').trim().toLowerCase()
  if (!q && !color) return docs
  return docs.filter((d) => String(d.title || '').toLowerCase().includes(q) && (!color || panoColor(d.color).name === color))
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

// The line of task-list item number `index` (from 0, in the order they
// render) and its match, or null; fenced code holds none, as on the server.
function findTaskItem(lines, index) {
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
    if (m && seen++ === index) return { line: i, match: m }
  }
  return null
}

/**
 * The markdown with its task-list item number `index` checked or unchecked.
 * Unchanged when there is no such item.
 */
export function togglePanoCheckbox(markdown, index, checked) {
  const lines = String(markdown || '').split('\n')
  const item = findTaskItem(lines, index)
  if (item) {
    const { line, match: m } = item
    lines[line] = m[1] + (checked ? 'x' : ' ') + m[3] + lines[line].slice(m[0].length)
  }
  return lines.join('\n')
}

// Who a checklist item is assigned to: a link to the task, titled with its
// status, closing its line. The server keeps the status and the box in step.
const ASSIGNEE = /[ \t]*→ \[@[^\]\n]*\]\(\/workspaces\/[^/)\s]+\/tasks\/[^/)\s]+(?: "[a-z]*")?\)[ \t]*$/
const TASK_PATH = /^\/workspaces\/[^/"\s]+\/tasks\/[^/"\s]+$/

/** The text of task-list item number `index`, without its assignee; '' when there is none. */
export function panoItemText(markdown, index) {
  const lines = String(markdown || '').split('\n')
  const item = findTaskItem(lines, index)
  return item ? lines[item.line].slice(item.match[0].length).replace(ASSIGNEE, '').trim() : ''
}

/**
 * The markdown with task-list item number `index` assigned to a workspace's
 * task: ` → [@name](/workspaces/<id>/tasks/<id> "notstarted")` ends its line,
 * in place of an earlier assignee. An agent reading the doc sees who has it, and where.
 */
export function assignPanoItem(markdown, index, name, path) {
  const lines = String(markdown || '').split('\n')
  const item = findTaskItem(lines, index)
  if (item) {
    const label = String(name).replace(/[[\]\n]/g, '')
    lines[item.line] = `${lines[item.line].replace(ASSIGNEE, '')} → [@${label}](${path} "notstarted")`
  }
  return lines.join('\n')
}

// The desktop top bar's menu icon.
const DOTS = '<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><circle cx="12" cy="5" r="1.75"></circle><circle cx="12" cy="12" r="1.75"></circle><circle cx="12" cy="19" r="1.75"></circle></svg>'

/** What a task's status reads as, on an assignee. */
export const PANO_TASK_STATUSES = {
  notstarted: 'Not started',
  ongoing: 'Ongoing',
  blocked: 'Blocked',
  completed: 'Completed',
  done: 'Completed',
  rejected: 'Rejected',
}

/**
 * Rendered markdown with each assignee — a link to a task, which the renderer
 * leaves as text — made a chip with the task's status: a link that opens the
 * task, or with `link: false` (inside a card that is a link) a plain chip.
 */
export function panoAssignees(html, { link = true } = {}) {
  return String(html || '').replace(/(?:→ )?<span class="md-dead-link" title="([^"]*)"(?: data-link-title="([^"]*)")?>([^<]*)<\/span>/g, (span, href, status = '', text) => {
    if (!TASK_PATH.test(href)) return span
    const known = Object.hasOwn(PANO_TASK_STATUSES, status) ? status : 'notstarted'
    const title = `${PANO_TASK_STATUSES[known]} · task ${href.split('/').pop()}`
    const inner = `<span class="pano-status" aria-hidden="true"></span>${text}<span class="pano-status-label">${PANO_TASK_STATUSES[known]}</span>`
    return link
      ? `<a class="pano-assignee" href="${href}" data-pano-task="${href}" data-status="${known}" title="${title}">${inner}</a>`
      : `<span class="pano-assignee" data-status="${known}" title="${title}">${inner}</span>`
  })
}

/** Rendered markdown with a menu button on each checklist item, to assign it. */
export function panoItemMenus(html) {
  return String(html || '').replace(/<input\b[^>]*type="checkbox"[^>]*>/g, (tag) => `${tag}<button type="button" class="pano-assign" data-pano-assign="" title="Assign to a workspace" aria-label="Assign to a workspace" aria-haspopup="menu">${DOTS}</button>`)
}

/**
 * The body of the task a checklist item becomes: what it is, and where it is
 * from. The item follows the task's status on its own, so the agent is not
 * asked to tick it.
 */
export function panoItemTaskBody(doc, item) {
  return `${item}\n\n---\nA checklist item of the Pano doc \`${doc.id}\` ("${doc.title}"); read it with the pano tool for its context, files and comments. The item follows this task's status, and is ticked when it is completed.`
}

/**
 * Where an @mention is being typed: the `@` before the caret, at a word's
 * start, and what follows it. Null when the caret is not in one.
 */
export function mentionQuery(text, caret) {
  const m = /(?:^|\s)@([^\s@]{0,40})$/.exec(String(text || '').slice(0, caret))
  return m ? { start: caret - m[1].length - 1, query: m[1] } : null
}

/** The workspaces an @mention being typed could name, those starting with it first. */
export function mentionMatches(workspaces, query, limit = 6) {
  const q = String(query || '').toLowerCase()
  const named = (workspaces || []).filter((w) => w.name.toLowerCase().includes(q))
  return [...named.filter((w) => w.name.toLowerCase().startsWith(q)), ...named.filter((w) => !w.name.toLowerCase().startsWith(q))].slice(0, limit)
}

/** The text with the @mention typed from `start` to `caret` replaced by a workspace's name. */
export function insertMention(text, start, caret, name) {
  const value = String(text || '')
  const mention = `@${name} `
  return { value: value.slice(0, start) + mention + value.slice(caret), caret: start + mention.length }
}

/** The workspaces a comment @mentions by their whole name, each once. */
export function mentionedWorkspaces(text, workspaces) {
  const body = String(text || '')
  return (workspaces || []).filter((w) => w.name && new RegExp(`(?:^|\\s)@${escapeRegExp(w.name)}(?![\\w-])`).test(body))
}

function escapeRegExp(text) {
  return text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

/** A rendered comment with each workspace it @mentions highlighted, outside its tags. */
export function panoMentions(html, workspaces) {
  const names = (workspaces || []).map((w) => w.name).filter(Boolean).sort((a, b) => b.length - a.length)
  if (!names.length) return String(html || '')
  const escaped = names.map((n) => escapeRegExp(escapeHtml(n)))
  const mention = new RegExp(`@(${escaped.join('|')})(?![\\w-])`, 'g')
  return String(html || '').split(/(<[^>]*>)/).map((part) => (part.startsWith('<') ? part : part.replace(mention, '<span class="pano-mention">@$1</span>'))).join('')
}

function escapeHtml(text) {
  return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
}

/** The body of the task an @mention in a comment becomes. */
export function panoMentionTaskBody(doc, comment) {
  const quoted = comment.split('\n').map((line) => `> ${line}`).join('\n')
  return `${quoted}\n\n---\nYou were mentioned in a comment on the Pano doc \`${doc.id}\` ("${doc.title}"). Read the doc with the pano tool and \`includeComments\`, then answer with a comment on it.`
}

// The server makes at most one task a second for an account.
const PING_SPACING_MS = 1100
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))

/**
 * Gives each workspace a comment mentions a task to answer it, a second apart
 * as the server allows. Returns the tasks made, and the workspaces it could not
 * ping with why: the comment is posted by then, so one failure stops nothing.
 */
export async function pingPanoWorkspaces(doc, comment, workspaces, { api, wait = sleep }) {
  const tasks = []
  const failed = []
  for (const [i, w] of workspaces.entries()) {
    if (i > 0) await wait(PING_SPACING_MS)
    try {
      const res = await api.createTask(w.id, `Pano: ${doc.title}`, panoMentionTaskBody(doc, comment))
      tasks.push(res.task)
    } catch (err) {
      failed.push({ workspace: w, error: err.message || 'failed' })
    }
  }
  return { tasks, failed }
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

/** The board: every doc of the account, filtered by title as the person types, and by color. */
export function usePanoBoard({ api }) {
  const docs = ref([])
  const loading = ref(false)
  const error = ref('')
  const query = ref('')
  const color = ref('')
  const shown = computed(() => filterPanoDocs(docs.value, query.value, color.value))

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

  return { docs, loading, error, query, color, shown, load }
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

  /**
   * Assigns checklist item number `index` to a workspace: a task for its agent,
   * linked from the item. Returns the task.
   */
  async function assignItem(index, workspace) {
    busy.value = true
    try {
      const d = doc.value
      const item = panoItemText(d.markdown, index)
      const { task } = await api.createTask(workspace.id, item.slice(0, 200), panoItemTaskBody(d, item))
      const markdown = assignPanoItem(d.markdown, index, workspace.name, `/workspaces/${workspace.id}/tasks/${task.id}`)
      await api.updatePanoDoc(id, { title: d.title, markdown, ttlDays: 0, color: '', files: [] })
      await load({ quiet: true })
      return task
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

  /** Posts a comment, then pings each workspace it mentions; see pingPanoWorkspaces. */
  async function comment(body, mentions = [], { wait } = {}) {
    busy.value = true
    try {
      await api.addPanoComment(id, body)
      await load({ quiet: true })
      return await pingPanoWorkspaces(doc.value, body, mentions, { api, wait })
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
    load, startEditing, cancelEditing, addDraftFile, removeDraftFile, save, checkItem, assignItem, remove, comment, attach,
  }
}
