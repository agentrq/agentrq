// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { computed, ref, watch } from 'vue'
import { clampWidth } from './useSidePanel'
import { useTaskOptions } from './useTaskOptions'
import { useClearContext } from './useClearContext'

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

/**
 * The tasks a doc's rendered markdown points at, once each, in the order they
 * appear: an assigned item's and any other link to a task alike.
 */
export function panoRelatedTasks(html) {
  const page = document.createElement('div')
  page.innerHTML = html
  const seen = new Map()
  // A chip's path is always a task's, and its name sits between the dot and the
  // label. The old 'done' reads as the task status it stands for.
  for (const chip of page.querySelectorAll('a.pano-assignee[data-pano-task]')) {
    const path = chip.dataset.panoTask
    if (seen.has(path)) continue
    const [, , workspaceId, , id] = path.split('/')
    seen.set(path, { path, workspaceId, id, name: chip.childNodes[1].textContent, status: chip.dataset.status === 'done' ? 'completed' : chip.dataset.status })
  }
  return [...seen.values()]
}

/**
 * The doc's related tasks: those its markdown points at, then the other tasks
 * linked to it (a chat about it, or the task its agent was on), once each.
 * Every one takes its status from the doc's tasks, read with the doc, and a
 * linked task with no chip is named after its workspace.
 */
export function panoLinkedTasks(related, docTasks = [], workspaces = []) {
  const live = new Map((docTasks || []).map((t) => [`/workspaces/${t.workspaceId}/tasks/${t.taskId}`, t]))
  const out = (related || []).map((t) => (live.has(t.path) ? { ...t, status: live.get(t.path).status } : t))
  const seen = new Set(out.map((t) => t.path))
  for (const [path, t] of live) {
    if (seen.has(path)) continue
    const name = (workspaces || []).find((w) => w.id === t.workspaceId)?.name || ''
    out.push({ path, workspaceId: t.workspaceId, id: t.taskId, name: name && `@${name}`, status: t.status })
  }
  return out
}

/** Rendered markdown with a menu button on each checklist item, to assign it. */
export function panoItemMenus(html) {
  return String(html || '').replace(/<input\b[^>]*type="checkbox"[^>]*>/g, (tag) => `${tag}<button type="button" class="pano-assign" data-pano-assign="" title="Assign to a workspace" aria-label="Assign to a workspace" aria-haspopup="menu">${DOTS}</button>`)
}

/**
 * The body of the task a checklist item becomes: what it is, and where it is
 * from. The item follows the task's status on its own, so the agent is not
 * asked to tick it: completing the task does.
 */
export function panoItemTaskBody(doc, item) {
  return `${item}\n\n---\nA checklist item of the Pano doc \`${doc.id}\` ("${doc.title}"). Read the doc with the pano tool for its context, files and comments.`
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

/**
 * Where a #task is being typed right after a whole @mention: the workspace,
 * the `#` before the caret, and what follows it. Null when the caret is not in one.
 */
export function taskMentionQuery(text, caret, workspaces) {
  const before = String(text || '').slice(0, caret)
  for (const w of byLongestName(workspaces)) {
    const m = new RegExp(`(?:^|\\s)@${escapeRegExp(w.name)} #([A-Za-z0-9]{0,40})$`).exec(before)
    if (m) return { workspace: w, start: caret - m[1].length - 1, query: m[1] }
  }
  return null
}

/** A workspace's tasks a #task being typed could name, by id or title, ongoing first. */
export function taskMentionMatches(tasks, query, limit = 6) {
  const q = String(query || '').toLowerCase()
  const named = (tasks || []).filter((t) => t.id.toLowerCase().includes(q) || String(t.title || '').toLowerCase().includes(q))
  return [...named.filter((t) => t.status === 'ongoing'), ...named.filter((t) => t.status !== 'ongoing')].slice(0, limit)
}

/** The text with the mention typed from `start` to `caret` replaced by a workspace's name, or a task's id after `#`. */
export function insertMention(text, start, caret, name, sigil = '@') {
  const value = String(text || '')
  const mention = `${sigil}${name} `
  return { value: value.slice(0, start) + mention + value.slice(caret), caret: start + mention.length }
}

/**
 * What a comment @mentions: one entry for each `@workspace #task` it names,
 * and one for each workspace named whole with no task after it.
 */
export function commentMentions(text, workspaces) {
  const body = String(text || '')
  const mentions = []
  for (const w of workspaces || []) {
    if (!w.name) continue
    const name = escapeRegExp(w.name)
    const taskIds = new Set([...body.matchAll(new RegExp(`(?:^|\\s)@${name} #([A-Za-z0-9]+)(?![\\w-])`, 'g'))].map((m) => m[1]))
    for (const taskId of taskIds) mentions.push({ workspace: w, taskId })
    if (!taskIds.size && new RegExp(`(?:^|\\s)@${name}(?![\\w-])`).test(body)) mentions.push({ workspace: w })
  }
  return mentions
}

function byLongestName(workspaces) {
  return (workspaces || []).filter((w) => w.name).sort((a, b) => b.name.length - a.name.length)
}

function escapeRegExp(text) {
  return text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

/** A rendered comment with each workspace it @mentions, and the task named after it, highlighted outside its tags. */
export function panoMentions(html, workspaces) {
  const names = byLongestName(workspaces).map((w) => w.name)
  if (!names.length) return String(html || '')
  const escaped = names.map((n) => escapeRegExp(escapeHtml(n)))
  const mention = new RegExp(`@(?:${escaped.join('|')})(?: #[A-Za-z0-9]+)?(?![\\w-])`, 'g')
  return String(html || '').split(/(<[^>]*>)/).map((part) => (part.startsWith('<') ? part : part.replace(mention, '<span class="pano-mention">$&</span>'))).join('')
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
 * Pings each mention of a comment (see commentMentions), a second apart as the
 * server allows: a message on the task it names, or else a new task for the
 * workspace to answer it. Returns the tasks pinged, and the workspaces it could
 * not ping with why: the comment is posted by then, so one failure stops nothing.
 */
export async function pingPanoWorkspaces(doc, comment, mentions, { api, wait = sleep }) {
  const tasks = []
  const failed = []
  const body = panoMentionTaskBody(doc, comment)
  for (const [i, { workspace: w, taskId }] of mentions.entries()) {
    if (i > 0) await wait(PING_SPACING_MS)
    try {
      const res = taskId ? await api.respondToTask(w.id, taskId, 'text', body) : await api.createTask(w.id, `Pano: ${doc.title}`, body)
      tasks.push(res.task)
    } catch (err) {
      failed.push({ workspace: w, error: err.message || 'failed' })
    }
  }
  return { tasks, failed }
}

/**
 * Rendered markdown with its task-list checkboxes made clickable, except an
 * item's assigned to a task: its box follows the task, so it stays as the
 * task's status says.
 */
export function panoCheckable(html) {
  let n = 0
  const locked = new Set()
  if (String(html || '').includes('md-dead-link')) {
    const page = document.createElement('div')
    page.innerHTML = html
    page.querySelectorAll('input[type="checkbox"]').forEach((box, i) => {
      const item = box.closest('li')
      const link = item && [...item.querySelectorAll('.md-dead-link')].find((a) => a.closest('li') === item && TASK_PATH.test(a.title))
      if (link) locked.add(i)
    })
  }
  return String(html || '').replace(/<input\b[^>]*>/g, (tag) => {
    if (!tag.includes('type="checkbox"')) return tag
    if (locked.has(n++)) return tag.replace('<input', '<input title="Follows its task"')
    return tag.replace(/ disabled(="[^"]*")?/, '')
  })
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
  const res = await api.updatePanoDoc(doc.id, { title: full.title, markdown: togglePanoCheckbox(full.markdown, index, checked), ttlDays: 0, color: '', files: [], version: full.version })
  return res.doc
}

/**
 * Gives a doc another color. The list's docs carry no markdown, so it is read
 * first and written back as it is; like any save, this one restarts its life.
 */
export async function recolorPanoDoc(doc, color, { api }) {
  const { doc: full } = await api.getPanoDoc(doc.id)
  const res = await api.updatePanoDoc(doc.id, { title: full.title, markdown: full.markdown, ttlDays: 0, color, files: [], version: full.version })
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
  // Set when a write was refused because the doc changed since it was read.
  const conflict = ref(false)
  // The version the draft was started from: what its save must match.
  let draftVersion = 0

  /**
   * Runs a write on the doc. A refusal for a stale version reads the doc again
   * and says so, so what is shown is what a retry would change.
   */
  async function guarded(write) {
    try {
      return await write()
    } catch (err) {
      if (err.status === 409) {
        await load({ quiet: true })
        throw new Error('This note changed while you were working on it, and has been reloaded. Try again.')
      }
      throw err
    }
  }

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
    draftVersion = d.version
    conflict.value = false
    editing.value = true
  }

  function cancelEditing() {
    editing.value = false
    conflict.value = false
  }

  /** Reads the doc again and reopens the editor on it, dropping the draft. */
  async function reloadEditing() {
    await load({ quiet: true })
    if (doc.value) startEditing()
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
      let res
      try {
        res = await (id ? api.updatePanoDoc(id, { ...body, version: draftVersion }) : api.createPanoDoc(body))
      } catch (err) {
        // The draft stays in the editor, so nothing the person wrote is lost.
        if (err.status === 409) {
          conflict.value = true
          throw new Error('This note changed while you were editing it. Your changes are not saved: load the latest version, or copy your text first.')
        }
        throw err
      }
      conflict.value = false
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
      await guarded(() => api.updatePanoDoc(id, { title: d.title, markdown: togglePanoCheckbox(d.markdown, index, checked), ttlDays: 0, color: '', files: [], version: d.version }))
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
      await guarded(() => api.updatePanoDoc(id, { title: d.title, markdown, ttlDays: 0, color: '', files: [], version: d.version }))
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
    doc, loading, error, busy, editing, draft, conflict,
    load, startEditing, cancelEditing, reloadEditing, addDraftFile, removeDraftFile, save, checkItem, assignItem, remove, comment, attach,
  }
}

/** The workspace the Pano's chat talks to: the account's supervisor. */
export function panoSupervisor(workspaces = []) {
  return workspaces.find((w) => w.name === 'supervisor')
}

/** Whether a route shows a Pano chat: the one whose page has a `chat` view. */
export function isPanoChatRoute(route) {
  return !!route?.matched?.at(-1)?.components?.chat
}

/** How long a chat's first line may run as its task's title. */
export const PANO_CHAT_TITLE_MAX = 80

/**
 * The task a chat message starts: its first line is the title, and the body
 * says where it was written, so the supervisor knows the person is on the
 * board and what they probably want from it. A `doc` the person tagged is
 * named, for the agent to read first; the task is linked to it as it is made.
 */
export function panoChatTask(message, doc = null) {
  const text = message.trim()
  const firstLine = text.split('\n')[0].trim()
  const title = firstLine.length > PANO_CHAT_TITLE_MAX ? `${firstLine.slice(0, PANO_CHAT_TITLE_MAX - 1).trimEnd()}…` : firstLine
  const about = doc ? `It is about the Pano doc \`${doc.id}\` ("${doc.title}"): read it with the pano tool first. ` : ''
  const body = `${text}\n\n---\nSent from the Pano chat: the person is looking at the Pano board, the account's shared docs. ${about}` +
    'They may want a doc posted or changed, or to talk about one: read the board with the pano tool when it helps. Reply in this task.'
  return { title, body }
}

/**
 * Where a #doc is being typed in the chat box: the `#` before the caret, at a
 * word's start, and what follows it. Null when the caret is not in one.
 */
export function docMentionQuery(text, caret) {
  const m = /(?:^|\s)#([^\s#]{0,40})$/.exec(String(text || '').slice(0, caret))
  return m ? { start: caret - m[1].length - 1, query: m[1] } : null
}

/** The docs a #doc being typed could name, by title, those starting with it first. */
export function docMentionMatches(docs, query, limit = 6) {
  const q = String(query || '').toLowerCase()
  const named = (docs || []).filter((d) => String(d.title || '').toLowerCase().includes(q))
  const starts = (d) => d.title.toLowerCase().startsWith(q)
  return [...named.filter(starts), ...named.filter((d) => !starts(d))].slice(0, limit)
}

/**
 * The board's chat box. It talks to the supervisor workspace, which `ensure`
 * makes again when it was deleted, and sends only while its agent is
 * connected. A message starts a task there, which `send` returns.
 *
 * The task carries the same options as one from the New Task form (`opts`, a
 * `useTaskOptions`), starting from the supervisor's own defaults; the first
 * line of the message is its title, as the form's title field would be.
 */
export function usePanoChat({ api, store }) {
  const supervisor = computed(() => panoSupervisor(store.workspaces))
  const online = computed(() => !!supervisor.value?.agentConnected)
  const message = ref('')
  // The doc the person tagged with #, which the task is linked to.
  const doc = ref(null)
  const sending = ref(false)
  const opts = useTaskOptions()
  const clear = useClearContext({
    workspaceId: computed(() => supervisor.value?.id),
    fetchWorkspaceSession: api.fetchWorkspaceSession,
  })

  // The options the form offers, once there is a workspace to offer them for.
  // Both lists degrade to empty: a task needs no completion trigger.
  watch(() => supervisor.value?.id, async (id) => {
    if (!id) return
    opts.applyWorkspaceDefaults(supervisor.value)
    clear.load()
    const [events, workflows] = await Promise.all([
      Promise.resolve(api.fetchEvents?.()).catch(() => null),
      Promise.resolve(api.fetchWorkflows?.()).catch(() => null),
    ])
    opts.events.value = events?.events ?? []
    opts.workflows.value = workflows?.workflows ?? []
  }, { immediate: true })

  async function ensure() {
    if (supervisor.value) return
    await api.ensureSupervisorWorkspace()
    await store.fetchWorkspaces()
  }

  async function send() {
    const text = message.value.trim()
    if (!text || !online.value || sending.value) return null
    sending.value = true
    try {
      const { title, body } = panoChatTask(text, doc.value)
      const res = await api.createTask(
        supervisor.value.id, title, body,
        opts.assignee.value, opts.attachments.value,
        opts.scheduleType.value !== 'none' ? 'cron' : 'notstarted', opts.cronSchedule.value, opts.allowAllCommands.value,
        opts.selectedEventId.value, opts.selectedWorkflowId.value,
        clear.offered.value && opts.clearContext.value,
        doc.value?.id || '',
      )
      message.value = ''
      doc.value = null
      opts.reset(supervisor.value)
      return res.task
    } finally {
      sending.value = false
    }
  }

  return { supervisor, online, message, doc, sending, opts, clearContextOffered: clear.offered, ensure, send }
}

/** Where the chat column's dragged width is remembered, per device. */
export const PANO_CHAT_WIDTH_KEY = 'agentrq:pano-chat-width'
/** The chat column before it is dragged: a phone's width. */
export const PANO_CHAT_DEFAULT_WIDTH = 480

function localStore() {
  try {
    return globalThis.localStorage
  } catch {
    return null
  }
}

/**
 * The chat column's width, dragged as the desktop side panel's is and held to
 * the same limits (`clampWidth`): never under its minimum, and never leaving
 * the page beside it less than a phone's width. `width` is what was dragged,
 * or null for the default; storage that is missing or refuses only means the
 * width is not remembered.
 */
export function usePanoChatWidth({ storage = localStore() } = {}) {
  let saved = null
  try {
    saved = Number.parseInt(storage?.getItem(PANO_CHAT_WIDTH_KEY) ?? '', 10)
  } catch {
    saved = null
  }
  const width = ref(Number.isFinite(saved) ? saved : null)

  /** How wide the column is drawn, sharing `available` pixels with the page. */
  function drawn(available) {
    return clampWidth(width.value ?? PANO_CHAT_DEFAULT_WIDTH, available)
  }

  function set(value, available) {
    width.value = clampWidth(value, available)
    try {
      storage?.setItem(PANO_CHAT_WIDTH_KEY, String(width.value))
    } catch {
      // Not remembered, and nothing else lost.
    }
  }

  function reset() {
    width.value = null
    try {
      storage?.removeItem(PANO_CHAT_WIDTH_KEY)
    } catch {
      // As above.
    }
  }

  return { width, drawn, set, reset }
}
