// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { readFileSync } from 'node:fs'

import { indexAttachments, readAttachment, resolveOutputPath, writeAttachment } from './attachments.js'
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

/**
 * `downloadAttachment` returns base64 and nothing else, so the filename has to
 * come from the task. A task whose text does not mention the id still
 * downloads — under the id as a name — because refusing would be worse than a
 * plainly-named file.
 */
async function downloadAttachment(ctx, attachmentId, taskId, out) {
  const { text: taskText } = await ctx.client.callTool('getTask', {
    taskId,
    includeConversation: true,
    limit: 200,
  })
  const known = indexAttachments(taskText).get(attachmentId)
  const filename = (known && known.filename) || attachmentId

  const { text: base64 } = await ctx.client.callTool('downloadAttachment', { attachmentId, taskId })
  if (!base64.trim()) {
    throw new UserError(`attachment ${attachmentId} is empty or was not found on task ${taskId}`)
  }
  const path = resolveOutputPath(filename, out, { cwd: ctx.cwd })
  return writeAttachment(path, base64)
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
      const taskId = requirePositional(ctx.positionals, 0, 'taskId')
      return ctx.client.callTool('getTask', {
        taskId,
        includeConversation: ctx.values.conversation || undefined,
        cursor: ctx.values.cursor ? Number(ctx.values.cursor) : undefined,
        limit: ctx.values.limit ? Number(ctx.values.limit) : undefined,
      })
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
      return ctx.client.callTool('getTask', {
        includeConversation: ctx.values.conversation || undefined,
      })
    },
  },
  {
    path: ['task', 'create'],
    summary: 'Create a task',
    usage: 'agentrq-ws task create <title> [--body TEXT|@file|-] [--clear-context] [--attach PATH]...',
    options: {
      body: { type: 'string', short: 'b', description: 'Task details (@file or - for stdin)' },
      assignee: { type: 'string', description: "'human' or 'agent' (default agent)" },
      cron: { type: 'string', description: "5-field cron schedule in UTC, e.g. '30 * * * *'; prefix CRON_TZ=<zone> for another" },
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
      return ctx.client.callTool('createTask', {
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
      })
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
      return ctx.client.callTool('updateTaskStatus', { taskId, status })
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
      const chatId = requirePositional(ctx.positionals, 0, 'taskId')
      const raw = requirePositional(ctx.positionals, 1, 'text')
      const text = await resolveText(raw, { stdin: ctx.stdin, what: 'the reply text' })
      return ctx.client.callTool('reply', {
        chatId,
        text,
        attachments: collectAttachments(ctx.values),
      })
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
      for (const key of ['limit', 'offset']) {
        if (ctx.values[key] === undefined) continue
        const n = Number(ctx.values[key])
        if (!Number.isInteger(n) || n < 0) throw new UserError(`--${key} must be a whole number, 0 or more`)
        args[key] = n
      }
      return ctx.client.callTool('searchSkills', args)
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
      return ctx.client.callTool('elicit', {
        taskId,
        message,
        mode,
        requestedSchema,
        url: ctx.values.url,
        timeoutSeconds: ctx.values.timeout ? Number(ctx.values.timeout) : undefined,
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
      const raw = await resolveText(ctx.values.args, { stdin: ctx.stdin, what: 'the arguments' })
      let args = {}
      if (raw !== undefined && String(raw).trim() !== '') {
        try {
          args = JSON.parse(raw)
        } catch (err) {
          throw new UserError(`--args is not valid JSON: ${err.message}`)
        }
      }
      return ctx.client.callTool(name, args)
    },
  },
]

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
