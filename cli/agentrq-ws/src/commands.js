// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { readFileSync } from 'node:fs'
import { basename, join } from 'node:path'

import { readAttachment, resolveOutputPath, writeAttachment } from './attachments.js'
import { UserError } from './errors.js'

/** Statuses the workspace accepts, mirrored from isValidTaskStatus on the backend. */
export const TASK_STATUSES = ['notstarted', 'ongoing', 'completed', 'rejected', 'cron', 'blocked']

const asArray = (value) => (value === undefined ? [] : Array.isArray(value) ? value : [value])

/** Read the whole of stdin, so `--body -` and piped text work. */
export async function readStdin(stream) {
  const chunks = []
  for await (const chunk of stream) chunks.push(chunk)
  return Buffer.concat(chunks.map((c) => Buffer.from(c))).toString('utf8')
}

/**
 * Resolve text that may be given inline, read from a file, or piped in.
 *
 * Task bodies and memory contents are routinely longer than a shell argument
 * wants to be, so every place that takes prose takes `@path` or `-` as well.
 */
export async function resolveText(value, { stdin, what }) {
  if (value === undefined) return undefined
  if (value === '-') {
    if (!stdin) throw new UserError(`no stdin to read ${what} from`)
    return await readStdin(stdin)
  }
  if (typeof value === 'string' && value.startsWith('@')) {
    const path = value.slice(1)
    try {
      return readFileSync(path, 'utf8')
    } catch (err) {
      throw new UserError(`cannot read ${what} from ${path}: ${err.message}`)
    }
  }
  return value
}

/** Build the attachment array for a tool call from repeated --attach flags. */
export function collectAttachments(values) {
  const paths = asArray(values.attach)
  if (paths.length === 0) return undefined
  return paths.map((path) => readAttachment(path))
}

/** Parse `question=answer` pairs for publishEvent's faq. */
export function parseFaq(values) {
  const pairs = asArray(values.faq)
  if (pairs.length === 0) return undefined
  return pairs.map((pair) => {
    const at = String(pair).indexOf('=')
    if (at <= 0) {
      throw new UserError(`--faq expects question=answer, got "${pair}"`)
    }
    return { question: pair.slice(0, at), answer: pair.slice(at + 1) }
  })
}

/**
 * Build an elicitation schema from repeated `--field name[:type[:description]]`.
 *
 * The protocol restricts these to a flat object of primitives, which is what
 * makes describing one on a command line reasonable at all. `--schema @file`
 * stays available for anything this shorthand cannot say.
 */
export function buildRequestedSchema(values) {
  const fields = asArray(values.field)
  if (fields.length === 0) return undefined
  const properties = {}
  const required = []
  for (const field of fields) {
    const [name, type = 'string', ...rest] = String(field).split(':')
    if (!name) throw new UserError(`--field expects a name, got "${field}"`)
    if (!['string', 'number', 'integer', 'boolean'].includes(type)) {
      throw new UserError(`--field "${name}" has unsupported type "${type}" (string, number, integer, boolean)`)
    }
    properties[name] = { type }
    if (rest.length > 0) properties[name].description = rest.join(':')
    required.push(name)
  }
  return { type: 'object', properties, required }
}

function requirePositional(positionals, index, name) {
  const value = positionals[index]
  if (value === undefined || value === '') throw new UserError(`missing required argument <${name}>`)
  return value
}

/** Adds --limit and --offset to a search's arguments, as whole numbers. */
function withPaging(ctx, args) {
  for (const key of ['limit', 'offset']) {
    if (ctx.values[key] === undefined) continue
    const n = Number(ctx.values[key])
    if (!Number.isInteger(n) || n < 0) throw new UserError(`--${key} must be a whole number, 0 or more`)
    args[key] = n
  }
  return args
}

/**
 * `getAttachment` answers with the attachment's name and type, and its public
 * link — the default — or, for one saved before attachments had links, its
 * content as base64. A link is fetched, so the file arrives either way.
 */
async function downloadAttachment(ctx, attachmentId, taskId, out) {
  const { text } = await ctx.client.callAction('task', 'getAttachment', { attachmentId, taskId }, ['getAttachment', { attachmentId, taskId }])
  let attachment
  try {
    attachment = JSON.parse(text)
  } catch {
    throw new UserError(`unexpected answer for attachment ${attachmentId}: ${text}`)
  }
  const content = attachment.url ? await fetchAttachment(ctx, attachment.url) : attachment.data
  if (!content || !content.length) {
    throw new UserError(`attachment ${attachmentId} is empty or was not found on task ${taskId}`)
  }
  const path = resolveOutputPath(attachment.filename || attachmentId, out, { cwd: ctx.cwd })
  return writeAttachment(path, content)
}

async function fetchAttachment(ctx, url) {
  const fetchImpl = ctx.fetch || globalThis.fetch
  let response
  try {
    response = await fetchImpl(url)
  } catch (err) {
    throw new UserError(`cannot download ${url}: ${err.message}`)
  }
  if (!response.ok) throw new UserError(`cannot download ${url}: HTTP ${response.status}`)
  return Buffer.from(await response.arrayBuffer())
}

/** The Pano's post-it colors, mirrored from the backend. */
export const PANO_COLORS = ['yellow', 'pink', 'green', 'blue', 'orange', 'purple', 'amber']

/** Files for the pano tool from repeated --file flags: an attachment without its id. */
function panoFiles(paths) {
  const list = asArray(paths)
  if (list.length === 0) return undefined
  return list.map((path) => {
    const { filename, mimeType, data } = readAttachment(path)
    return { filename, mimeType, data }
  })
}

/**
 * The pano tool answers with the doc as JSON, or with a refusal's text. With
 * idOnly a doc prints as its id, which is all a write needs to hand on.
 */
function printPanoDoc(result, { idOnly = false } = {}) {
  let doc
  try {
    doc = JSON.parse(result.text)
  } catch {
    return result
  }
  return { text: idOnly ? doc.id : result.text, data: doc }
}

/**
 * Writes a doc's markdown to <out>/<id>/<id>.md and downloads its linked files
 * beside it, so the markdown's links resolve on disk, and its attachments to
 * <out>/<id>/attachments/. Comments go to comments.json. Prints the paths only.
 */
async function downloadPanoDoc(ctx, doc, out) {
  if (!doc?.id) throw new UserError('unexpected answer from the pano tool')
  const dir = resolveOutputPath(doc.id, out.endsWith('/') ? out : `${out}/`, { cwd: ctx.cwd })
  const paths = [writeAttachment(join(dir, `${doc.id}.md`), Buffer.from(doc.markdown || '')).path]
  const downloads = [
    ...(doc.files || []).map((f) => [f, dir]),
    ...(doc.attachments || []).map((f) => [f, join(dir, 'attachments')]),
  ]
  for (const [file, into] of downloads) {
    if (!file.url) continue
    paths.push(writeAttachment(join(into, basename(file.filename)), await fetchAttachment(ctx, file.url)).path)
  }
  if (doc.comments) {
    paths.push(writeAttachment(join(dir, 'comments.json'), Buffer.from(JSON.stringify(doc.comments, null, 2))).path)
  }
  return { text: paths.join('\n'), data: { ...doc, paths } }
}

export const COMMANDS = [
  {
    path: ['workspace'],
    summary: 'Show the workspace title and mission',
    usage: 'agentrq-ws workspace',
    async run(ctx) {
      return ctx.client.callTool('getWorkspace', {})
    },
  },
  {
    path: ['task', 'get'],
    summary: "Fetch a task by id, optionally with its conversation",
    usage: 'agentrq-ws task get <taskId> [--conversation] [--cursor N] [--limit N]',
    options: {
      conversation: { type: 'boolean', short: 'c', description: "Include the task's chat history" },
      cursor: { type: 'string', description: 'Message pagination offset (default 0)' },
      limit: { type: 'string', description: 'Maximum messages to return (default 5)' },
    },
    async run(ctx) {
      const payload = {
        taskId: requirePositional(ctx.positionals, 0, 'taskId'),
        includeConversation: ctx.values.conversation || undefined,
        cursor: ctx.values.cursor ? Number(ctx.values.cursor) : undefined,
        limit: ctx.values.limit ? Number(ctx.values.limit) : undefined,
      }
      return ctx.client.callAction('task', 'get', payload, ['getTask', payload])
    },
  },
  {
    path: ['task', 'next'],
    summary: 'Take the next not-started task (dequeues the work queue)',
    usage: 'agentrq-ws task next [--conversation]',
    options: {
      conversation: { type: 'boolean', short: 'c', description: "Include the task's chat history" },
    },
    async run(ctx) {
      // Kept as its own verb rather than a bare `task get`: this call mutates
      // the queue, and a command that silently claims work is a bad surprise.
      const payload = { includeConversation: ctx.values.conversation || undefined }
      return ctx.client.callAction('task', 'get', payload, ['getTask', payload])
    },
  },
  {
    path: ['task', 'create'],
    summary: 'Create a task',
    usage: 'agentrq-ws task create <title> [--body TEXT|@file|-] [--clear-context] [--attach PATH]...',
    options: {
      body: { type: 'string', short: 'b', description: 'Task details (@file or - for stdin)' },
      assignee: { type: 'string', description: "'human' or 'agent' (default agent)" },
      cron: { type: 'string', description: "5-field cron schedule, e.g. '30 * * * *'" },
      event: { type: 'string', description: 'Event id to publish when the task completes' },
      'clear-context': {
        type: 'boolean',
        description: 'Send /clear to the agent before it picks this task up',
      },
      attach: { type: 'string', multiple: true, description: 'File to attach (repeatable)' },
    },
    async run(ctx) {
      const title = requirePositional(ctx.positionals, 0, 'title')
      const body = await resolveText(ctx.values.body, { stdin: ctx.stdin, what: 'the task body' })
      const payload = {
        title,
        body: body ?? '',
        assignee: ctx.values.assignee,
        cronSchedule: ctx.values.cron,
        eventId: ctx.values.event,
        // Only sent when asked for. Absent means "use the workspace default",
        // and a literal false would override that with an opinion the caller
        // never expressed.
        clearContext: ctx.values['clear-context'] || undefined,
        attachments: collectAttachments(ctx.values),
      }
      return ctx.client.callAction('task', 'create', payload, ['createTask', payload])
    },
  },
  {
    path: ['task', 'status'],
    summary: 'Update a task status',
    usage: `agentrq-ws task status <taskId> <${TASK_STATUSES.join('|')}>`,
    async run(ctx) {
      const taskId = requirePositional(ctx.positionals, 0, 'taskId')
      const status = requirePositional(ctx.positionals, 1, 'status')
      if (!TASK_STATUSES.includes(status)) {
        throw new UserError(`unknown status "${status}" (expected one of: ${TASK_STATUSES.join(', ')})`)
      }
      return ctx.client.callAction('task', 'updateStatus', { taskId, status }, ['updateTaskStatus', { taskId, status }])
    },
  },
  {
    path: ['reply'],
    summary: 'Send a message to a task, optionally with files attached',
    usage: 'agentrq-ws reply <taskId> <text|@file|-> [--attach PATH]...',
    options: {
      attach: { type: 'string', multiple: true, description: 'File to attach (repeatable)' },
    },
    async run(ctx) {
      const taskId = requirePositional(ctx.positionals, 0, 'taskId')
      const raw = requirePositional(ctx.positionals, 1, 'text')
      const text = await resolveText(raw, { stdin: ctx.stdin, what: 'the reply text' })
      const attachments = collectAttachments(ctx.values)
      return ctx.client.callAction('task', 'reply', { taskId, text, attachments }, ['reply', { chatId: taskId, text, attachments }])
    },
  },
  {
    path: ['task', 'help'],
    summary: "Show the task tool's actions, or one action's payload",
    usage: 'agentrq-ws task help [action]',
    async run(ctx) {
      return actionHelp(ctx, 'task')
    },
  },
  {
    path: ['attachment', 'get'],
    summary: 'Download an attachment to a file (no base64, ever)',
    usage: 'agentrq-ws attachment get <attachmentId> --task <taskId> [--out DIR|FILE]',
    options: {
      task: { type: 'string', short: 't', description: 'The task holding the attachment (required)' },
      out: { type: 'string', short: 'o', description: 'Destination directory or file (default: OS temp dir)' },
    },
    async run(ctx) {
      const attachmentId = requirePositional(ctx.positionals, 0, 'attachmentId')
      if (!ctx.values.task) throw new UserError('--task <taskId> is required to locate the attachment')
      const { path, bytes } = await downloadAttachment(ctx, attachmentId, ctx.values.task, ctx.values.out)
      return { text: path, data: { path, bytes } }
    },
  },
  {
    path: ['memory', 'load'],
    summary: 'Read a workspace memory (defaults to the index)',
    usage: 'agentrq-ws memory load [name]',
    async run(ctx) {
      return ctx.client.callTool('loadMemory', { name: ctx.positionals[0] })
    },
  },
  {
    path: ['memory', 'save'],
    summary: 'Replace a workspace memory',
    usage: 'agentrq-ws memory save [name] --content TEXT|@file|-',
    options: {
      content: { type: 'string', short: 'C', description: 'The full new content (@file or - for stdin)' },
    },
    async run(ctx) {
      if (ctx.values.content === undefined) throw new UserError('--content is required (use @file or - to read from stdin)')
      const content = await resolveText(ctx.values.content, { stdin: ctx.stdin, what: 'the memory content' })
      return ctx.client.callTool('saveMemory', { name: ctx.positionals[0], content })
    },
  },
  {
    path: ['memory', 'delete'],
    summary: 'Delete a workspace memory',
    usage: 'agentrq-ws memory delete [name]',
    async run(ctx) {
      return ctx.client.callTool('deleteMemory', { name: ctx.positionals[0] })
    },
  },
  {
    path: ['skill', 'search'],
    summary: 'Find the skills this workspace can use, by name or description',
    usage: 'agentrq-ws skill search [q] [--limit N] [--offset N]',
    options: {
      limit: { type: 'string', description: 'How many to return (at most 100; default all)' },
      offset: { type: 'string', description: 'How many matches to skip' },
    },
    async run(ctx) {
      const args = {}
      if (ctx.positionals.length) args.q = ctx.positionals.join(' ')
      return ctx.client.callTool('searchSkills', withPaging(ctx, args))
    },
  },
  {
    path: ['skill', 'load'],
    summary: 'Read a skill file (skill://<name> reads its SKILL.md)',
    usage: 'agentrq-ws skill load <uri>',
    async run(ctx) {
      return ctx.client.callTool('loadSkill', { uri: requirePositional(ctx.positionals, 0, 'uri') })
    },
  },
  {
    path: ['skill', 'save'],
    summary: 'Replace a file of one of this workspace\'s skills',
    usage: 'agentrq-ws skill save <uri> --content TEXT|@file|-',
    options: {
      content: { type: 'string', short: 'C', description: 'The full new content (@file or - for stdin)' },
    },
    async run(ctx) {
      const uri = requirePositional(ctx.positionals, 0, 'uri')
      if (ctx.values.content === undefined) throw new UserError('--content is required (use @file or - to read from stdin)')
      const content = await resolveText(ctx.values.content, { stdin: ctx.stdin, what: 'the skill file' })
      return ctx.client.callTool('saveSkill', { uri, content })
    },
  },
  {
    path: ['skill', 'delete'],
    summary: 'Delete a skill (skill://<name>) or one of its files',
    usage: 'agentrq-ws skill delete <uri>',
    async run(ctx) {
      return ctx.client.callTool('deleteSkill', { uri: requirePositional(ctx.positionals, 0, 'uri') })
    },
  },
  {
    path: ['pano', 'save'],
    summary: 'Create a Pano doc, or replace one; prints its id',
    usage: 'agentrq-ws pano save [id] --title TEXT --markdown TEXT|@file|- [--file PATH]... [--ttl-days N] [--color NAME]',
    options: {
      title: { type: 'string', short: 'T', description: "The doc's title (required)" },
      markdown: { type: 'string', short: 'M', description: "The doc's full markdown (@file or - for stdin; required)" },
      file: { type: 'string', multiple: true, description: 'A linked file, linked from the markdown by its file name (repeatable)' },
      'ttl-days': { type: 'string', description: 'Days the doc lives after this save (default: 30, or its own)' },
      color: { type: 'string', description: `Its post-it's color: ${PANO_COLORS.join(', ')}` },
    },
    async run(ctx) {
      if (ctx.values.title === undefined) throw new UserError('--title is required')
      if (ctx.values.markdown === undefined) throw new UserError('--markdown is required (use @file or - to read from stdin)')
      const markdown = await resolveText(ctx.values.markdown, { stdin: ctx.stdin, what: 'the markdown' })
      const color = ctx.values.color
      if (color !== undefined && !PANO_COLORS.includes(color)) {
        throw new UserError(`unknown color "${color}" (expected one of: ${PANO_COLORS.join(', ')})`)
      }
      let ttlDays
      if (ctx.values['ttl-days'] !== undefined) {
        ttlDays = Number(ctx.values['ttl-days'])
        if (!Number.isInteger(ttlDays) || ttlDays < 1) throw new UserError('--ttl-days must be a whole number, 1 or more')
      }
      return printPanoDoc(await pano(ctx, 'save', {
        id: ctx.positionals[0],
        title: ctx.values.title,
        markdown,
        ttlDays,
        color,
        files: panoFiles(ctx.values.file),
      }), { idOnly: true })
    },
  },
  {
    path: ['pano', 'read'],
    summary: 'Read a Pano doc, or download it with its files to a directory',
    usage: 'agentrq-ws pano read <id> [--comments] [--attachments] [--out DIR]',
    options: {
      comments: { type: 'boolean', short: 'c', description: "Include the doc's comments" },
      attachments: { type: 'boolean', short: 'a', description: "Include the doc's attachments" },
      out: { type: 'string', short: 'o', description: 'Write the markdown and download every file here; prints only their paths' },
    },
    async run(ctx) {
      const id = requirePositional(ctx.positionals, 0, 'id')
      const result = await pano(ctx, 'read', {
        id,
        includeComments: ctx.values.comments || undefined,
        includeAttachments: ctx.values.attachments || undefined,
      })
      if (!ctx.values.out) return result
      return downloadPanoDoc(ctx, printPanoDoc(result).data, ctx.values.out)
    },
  },
  {
    path: ['pano', 'comment'],
    summary: 'Comment on a Pano doc',
    usage: 'agentrq-ws pano comment <id> <text|@file|->',
    async run(ctx) {
      const id = requirePositional(ctx.positionals, 0, 'id')
      const raw = requirePositional(ctx.positionals, 1, 'text')
      const comment = await resolveText(raw, { stdin: ctx.stdin, what: 'the comment' })
      return pano(ctx, 'comment', { id, comment })
    },
  },
  {
    path: ['pano', 'attach'],
    summary: 'Attach files to a Pano doc; they need no link',
    usage: 'agentrq-ws pano attach <id> --file PATH...',
    options: {
      file: { type: 'string', multiple: true, description: 'A file to attach (repeatable)' },
    },
    async run(ctx) {
      const id = requirePositional(ctx.positionals, 0, 'id')
      const files = panoFiles(ctx.values.file)
      if (!files) throw new UserError('--file is required')
      return printPanoDoc(await pano(ctx, 'attach', { id, files }), { idOnly: true })
    },
  },
  {
    path: ['pano', 'delete'],
    summary: 'Delete a Pano doc',
    usage: 'agentrq-ws pano delete <id>',
    async run(ctx) {
      return pano(ctx, 'delete', { id: requirePositional(ctx.positionals, 0, 'id') })
    },
  },
  {
    path: ['pano', 'help'],
    summary: "Show the pano tool's actions, or one action's payload",
    usage: 'agentrq-ws pano help [action]',
    async run(ctx) {
      return actionHelp(ctx, 'pano')
    },
  },
  {
    path: ['event', 'publish'],
    summary: 'Publish a named event',
    usage: 'agentrq-ws event publish <name> [--payload TEXT|@file|-] [--task ID] [--faq Q=A]...',
    options: {
      payload: { type: 'string', short: 'p', description: 'What happened (@file or - for stdin)' },
      task: { type: 'string', short: 't', description: 'The task this publish completes' },
      faq: { type: 'string', multiple: true, description: 'question=answer pair (repeatable)' },
    },
    async run(ctx) {
      const name = requirePositional(ctx.positionals, 0, 'name')
      const payload = await resolveText(ctx.values.payload, { stdin: ctx.stdin, what: 'the payload' })
      return ctx.client.callTool('publishEvent', {
        name,
        payload,
        taskId: ctx.values.task,
        faq: parseFaq(ctx.values),
      })
    },
  },
  {
    path: ['ask'],
    summary: 'Ask the human a question and wait for the answer',
    usage: 'agentrq-ws ask <taskId> <message> [--field name[:type[:description]]]... [--url URL]',
    options: {
      mode: { type: 'string', short: 'm', description: "'form' or 'url' (inferred when omitted)" },
      field: { type: 'string', multiple: true, description: 'Form field, e.g. branch:string:Which branch' },
      schema: { type: 'string', description: 'Full requestedSchema as JSON (@file supported)' },
      url: { type: 'string', short: 'u', description: 'Link to show the human (mode=url)' },
      timeout: { type: 'string', description: 'Seconds to wait, max 3600 (default 3600)' },
    },
    async run(ctx) {
      const taskId = requirePositional(ctx.positionals, 0, 'taskId')
      const message = requirePositional(ctx.positionals, 1, 'message')
      let requestedSchema = buildRequestedSchema(ctx.values)
      if (ctx.values.schema !== undefined) {
        const raw = await resolveText(ctx.values.schema, { stdin: ctx.stdin, what: 'the schema' })
        try {
          requestedSchema = JSON.parse(raw)
        } catch (err) {
          throw new UserError(`--schema is not valid JSON: ${err.message}`)
        }
      }
      // The mode is almost always implied by which of the two was given, and
      // making somebody state it as well is ceremony.
      const mode = ctx.values.mode || (ctx.values.url ? 'url' : 'form')
      if (mode === 'url' && !ctx.values.url) throw new UserError('mode=url needs --url')
      if (mode === 'form' && !requestedSchema) {
        throw new UserError('mode=form needs at least one --field (or --schema)')
      }
      const payload = {
        taskId,
        message,
        mode,
        requestedSchema,
        url: ctx.values.url,
        timeoutSeconds: ctx.values.timeout ? Number(ctx.values.timeout) : undefined,
      }
      return ctx.client.callAction('task', 'elicit', payload, ['elicit', payload])
    },
  },
  {
    path: ['site-tools'],
    summary: 'List the websites shared with this workspace and their tools, ranked by q or filtered by a pattern',
    usage: 'agentrq-ws site-tools [q] [--pattern REGEX] [--limit N] [--offset N]',
    options: {
      pattern: { type: 'string', description: "A regular expression a tool's name or description must match" },
      limit: { type: 'string', description: 'How many tools to return (at most 100; default all)' },
      offset: { type: 'string', description: 'How many matches to skip' },
    },
    async run(ctx) {
      const args = {}
      if (ctx.positionals.length) args.q = ctx.positionals.join(' ')
      if (ctx.values.pattern !== undefined) args.pattern = ctx.values.pattern
      return ctx.client.callTool('listSiteTools', withPaging(ctx, args))
    },
  },
  {
    path: ['site-tool'],
    summary: "Show a shared website's tool, with its input schema",
    usage: 'agentrq-ws site-tool <site> <tool>',
    async run(ctx) {
      const site = requirePositional(ctx.positionals, 0, 'site')
      const tool = requirePositional(ctx.positionals, 1, 'tool')
      return ctx.client.callTool('getSiteToolDefinition', { site, tool })
    },
  },
  {
    path: ['call-site-tool'],
    summary: "Run a shared website's tool in the human's Chrome",
    usage: "agentrq-ws call-site-tool <site> <tool> --task <taskId> [--args '{\"k\":\"v\"}'|@file|-]",
    options: {
      task: { type: 'string', short: 't', description: 'The task you are working on; approvals are asked there' },
      args: { type: 'string', short: 'a', description: 'Tool arguments as JSON (@file or - for stdin)' },
    },
    async run(ctx) {
      const site = requirePositional(ctx.positionals, 0, 'site')
      const tool = requirePositional(ctx.positionals, 1, 'tool')
      if (!ctx.values.task) throw new UserError('--task <taskId> is required')
      return ctx.client.callTool('callSiteTool', {
        taskId: ctx.values.task,
        site,
        tool,
        arguments: await readArgs(ctx),
      })
    },
  },
  {
    path: ['tools'],
    summary: 'List the tools this workspace server offers',
    usage: 'agentrq-ws tools',
    async run(ctx) {
      const tools = await ctx.client.listTools()
      return {
        text: tools.map((tool) => `${tool.name}\n  ${(tool.description || '').split('\n')[0]}`).join('\n'),
        data: tools,
      }
    },
  },
  {
    path: ['call'],
    summary: 'Call any workspace tool directly with JSON arguments',
    usage: "agentrq-ws call <tool> [--args '{\"k\":\"v\"}'|@file|-]",
    options: {
      args: { type: 'string', short: 'a', description: 'Tool arguments as JSON (@file or - for stdin)' },
    },
    async run(ctx) {
      // An escape hatch, so a tool added to the server tomorrow is reachable
      // today without waiting for this CLI to grow a verb for it.
      const name = requirePositional(ctx.positionals, 0, 'tool')
      return ctx.client.callTool(name, await readArgs(ctx))
    },
  },
]

/** The server's own help for an action tool, for one action when named. */
function actionHelp(ctx, tool) {
  const topic = ctx.positionals[0]
  return ctx.client.callTool(tool, { action: 'help', payload: topic ? { topic } : undefined })
}

/** One pano call; a server from before payloads takes its fields beside action. */
function pano(ctx, action, payload) {
  return ctx.client.callAction('pano', action, payload, ['pano', { action, ...payload }])
}

/** --args as JSON, from inline, @file or stdin; nothing given is {}. */
async function readArgs(ctx) {
  const raw = await resolveText(ctx.values.args, { stdin: ctx.stdin, what: 'the arguments' })
  if (raw === undefined || String(raw).trim() === '') return {}
  try {
    return JSON.parse(raw)
  } catch (err) {
    throw new UserError(`--args is not valid JSON: ${err.message}`)
  }
}

/**
 * Match a leading path that names a family rather than a command — `task`,
 * `memory` — so it can be answered with that family's commands instead of
 * "unknown command", which is a dead end for somebody exploring.
 */
export function findGroup(argv) {
  const segments = []
  for (const arg of argv) {
    if (String(arg).startsWith('-')) break
    segments.push(arg)
  }
  if (segments.length === 0) return null

  const members = COMMANDS.filter(
    (command) =>
      command.path.length > segments.length &&
      segments.every((segment, i) => command.path[i] === segment),
  )
  return members.length > 0 ? { segments, members } : null
}

/**
 * Match argv against the command table, longest path first so `task get` wins
 * over a hypothetical `task`.
 */
export function findCommand(argv) {
  const sorted = [...COMMANDS].sort((a, b) => b.path.length - a.path.length)
  for (const command of sorted) {
    if (command.path.every((segment, i) => argv[i] === segment)) {
      return { command, rest: argv.slice(command.path.length) }
    }
  }
  return { command: null, rest: argv }
}
