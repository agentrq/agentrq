// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import assert from 'node:assert/strict'
import { mkdtempSync, readFileSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'node:test'

import {
  COMMANDS,
  TASK_STATUSES,
  buildRequestedSchema,
  collectAttachments,
  findCommand,
  parseFaq,
  readStdin,
  resolveText,
} from '../src/commands.js'
import { UserError } from '../src/errors.js'
import { stdinOf } from './fake-server.js'

const scratch = () => mkdtempSync(join(tmpdir(), 'agentrq-ws-cmd-'))

/**
 * A client that records what the command layer asked the server to do. An
 * action call is recorded as its tool with the action beside the payload, and
 * the call it would make on an older server is kept in `legacy`.
 */
function stubClient(replies = {}) {
  const calls = []
  const legacy = []
  return {
    calls,
    legacy,
    async callAction(tool, action, payload, old) {
      legacy.push(old)
      const reply = replies[action] ?? replies[tool]
      calls.push({ name: tool, args: { action, ...payload } })
      const text = typeof reply === 'function' ? reply({ action, ...payload }) : (reply ?? 'ok')
      return { text, result: { content: [{ type: 'text', text }] } }
    },
    async callTool(name, args) {
      calls.push({ name, args })
      const reply = replies[name]
      const text = typeof reply === 'function' ? reply(args) : (reply ?? 'ok')
      return { text, result: { content: [{ type: 'text', text }] } }
    },
    async listTools() {
      calls.push({ name: 'tools/list' })
      return replies.tools || []
    },
  }
}

const commandNamed = (path) => {
  const found = COMMANDS.find((c) => c.path.join(' ') === path)
  assert.ok(found, `no command ${path}`)
  return found
}

/** Run a command the way the CLI would. */
function invoke(path, { positionals = [], values = {}, client = stubClient(), stdin, cwd, fetch } = {}) {
  return commandNamed(path).run({ client, positionals, values, stdin, cwd, fetch, env: {} })
}

test('readStdin reads the whole stream', async () => {
  assert.equal(await readStdin(stdinOf('piped text')), 'piped text')
})

test('resolveText passes an inline value through', async () => {
  assert.equal(await resolveText('inline', { what: 'x' }), 'inline')
  assert.equal(await resolveText(undefined, { what: 'x' }), undefined)
})

test('resolveText reads - from stdin', async () => {
  // Prose does not belong in a shell argument.
  assert.equal(await resolveText('-', { stdin: stdinOf('from stdin'), what: 'x' }), 'from stdin')
})

test('resolveText refuses - with no stdin', async () => {
  await assert.rejects(() => resolveText('-', { what: 'the body' }), /no stdin to read the body from/)
})

test('resolveText reads @file', async () => {
  const dir = scratch()
  const path = join(dir, 'body.md')
  writeFileSync(path, '# from a file')
  assert.equal(await resolveText(`@${path}`, { what: 'x' }), '# from a file')
})

test('resolveText explains an unreadable @file', async () => {
  await assert.rejects(() => resolveText('@/nope/missing.md', { what: 'the body' }), /cannot read the body from/)
})

test('collectAttachments turns paths into attachments', () => {
  const dir = scratch()
  const one = join(dir, 'a.txt')
  const two = join(dir, 'b.png')
  writeFileSync(one, 'a')
  writeFileSync(two, 'b')

  assert.equal(collectAttachments({}), undefined)
  assert.deepEqual(collectAttachments({ attach: one }).map((a) => a.filename), ['a.txt'])
  assert.deepEqual(collectAttachments({ attach: [one, two] }).map((a) => a.mimeType), ['text/plain', 'image/png'])
})

test('parseFaq reads question=answer pairs', () => {
  assert.equal(parseFaq({}), undefined)
  assert.deepEqual(parseFaq({ faq: 'why=because' }), [{ question: 'why', answer: 'because' }])
  assert.deepEqual(parseFaq({ faq: ['a=b=c'] }), [{ question: 'a', answer: 'b=c' }])
})

test('parseFaq refuses a pair with no answer', () => {
  assert.throws(() => parseFaq({ faq: 'nope' }), /expects question=answer/)
  assert.throws(() => parseFaq({ faq: '=orphan' }), /expects question=answer/)
})

test('buildRequestedSchema builds a flat schema from --field', () => {
  assert.equal(buildRequestedSchema({}), undefined)
  assert.deepEqual(buildRequestedSchema({ field: ['branch', 'count:integer:How many'] }), {
    type: 'object',
    properties: {
      branch: { type: 'string' },
      count: { type: 'integer', description: 'How many' },
    },
    required: ['branch', 'count'],
  })
})

test('buildRequestedSchema keeps a colon inside a description', () => {
  const schema = buildRequestedSchema({ field: ['url:string:Where: exactly'] })
  assert.equal(schema.properties.url.description, 'Where: exactly')
})

test('buildRequestedSchema refuses what the protocol will not carry', () => {
  // Elicitation schemas are flat primitives; anything else fails at the server.
  assert.throws(() => buildRequestedSchema({ field: ['thing:object'] }), /unsupported type "object"/)
  assert.throws(() => buildRequestedSchema({ field: [':string'] }), /expects a name/)
})

test('workspace asks for the mission', async () => {
  const client = stubClient({ getWorkspace: 'Workspace: agentrq-code' })
  const result = await invoke('workspace', { client })
  assert.equal(result.text, 'Workspace: agentrq-code')
  assert.deepEqual(client.calls[0], { name: 'getWorkspace', args: {} })
})

test('task get passes the id and the conversation options', async () => {
  const client = stubClient()
  await invoke('task get', {
    positionals: ['0isnjTCkpW5'],
    values: { conversation: true, cursor: '10', limit: '25' },
    client,
  })
  assert.deepEqual(client.calls[0], {
    name: 'task',
    args: { action: 'get', taskId: '0isnjTCkpW5', includeConversation: true, cursor: 10, limit: 25 },
  })
  // An older server is asked with the tool this action used to be.
  assert.deepEqual(client.legacy[0], ['getTask', { taskId: '0isnjTCkpW5', includeConversation: true, cursor: 10, limit: 25 }])
})

test('task get leaves the options out when unasked', async () => {
  const client = stubClient()
  await invoke('task get', { positionals: ['0isnjTCkpW5'], client })
  assert.deepEqual(client.calls[0].args, {
    action: 'get',
    taskId: '0isnjTCkpW5',
    includeConversation: undefined,
    cursor: undefined,
    limit: undefined,
  })
})

test('task get requires a task id', async () => {
  await assert.rejects(() => invoke('task get'), /missing required argument <taskId>/)
})

test('task next sends no id, which is what dequeues the queue', async () => {
  const client = stubClient()
  await invoke('task next', { values: { conversation: true }, client })
  assert.equal(client.calls[0].name, 'task')
  assert.equal(client.calls[0].args.action, 'get')
  assert.equal(client.legacy[0][0], 'getTask')
  assert.equal(client.calls[0].args.taskId, undefined)
  assert.equal(client.calls[0].args.includeConversation, true)
})

test('task create sends every optional field it was given', async () => {
  const dir = scratch()
  const attachment = join(dir, 'diagram.png')
  writeFileSync(attachment, 'png')
  const client = stubClient()

  await invoke('task create', {
    positionals: ['Ship the CLI'],
    values: {
      body: 'the details',
      assignee: 'human',
      cron: '30 * * * *',
      event: '0evt123',
      attach: [attachment],
    },
    client,
  })

  const { args } = client.calls[0]
  assert.equal(args.action, 'create')
  assert.equal(client.legacy[0][0], 'createTask')
  assert.equal(args.title, 'Ship the CLI')
  assert.equal(args.body, 'the details')
  assert.equal(args.assignee, 'human')
  assert.equal(args.cronSchedule, '30 * * * *')
  assert.equal(args.eventId, '0evt123')
  assert.equal(args.attachments[0].filename, 'diagram.png')
})

test('task create defaults the body to empty rather than omitting it', async () => {
  const client = stubClient()
  await invoke('task create', { positionals: ['Just a title'], client })
  assert.equal(client.calls[0].args.body, '')
})

test('task create reads a body from stdin', async () => {
  const client = stubClient()
  await invoke('task create', {
    positionals: ['Title'],
    values: { body: '-' },
    client,
    stdin: stdinOf('piped body'),
  })
  assert.equal(client.calls[0].args.body, 'piped body')
})

test('task create requires a title', async () => {
  await assert.rejects(() => invoke('task create'), /missing required argument <title>/)
})

test('task status accepts the statuses the backend accepts', async () => {
  for (const status of TASK_STATUSES) {
    const client = stubClient()
    await invoke('task status', { positionals: ['0isnjTCkpW5', status], client })
    assert.deepEqual(client.calls[0].args, { action: 'updateStatus', taskId: '0isnjTCkpW5', status })
    assert.deepEqual(client.legacy[0], ['updateTaskStatus', { taskId: '0isnjTCkpW5', status }])
  }
})

test('task status rejects a status the backend would reject, listing the real ones', async () => {
  // Catching this locally saves a round trip and tells the user the options.
  await assert.rejects(
    () => invoke('task status', { positionals: ['0isnjTCkpW5', 'done'] }),
    /unknown status "done".*notstarted, ongoing/s,
  )
})

test('task status requires both arguments', async () => {
  await assert.rejects(() => invoke('task status', { positionals: ['0isnjTCkpW5'] }), /<status>/)
})

test('reply sends text and attachments by path', async () => {
  const dir = scratch()
  const log = join(dir, 'run.log')
  writeFileSync(log, 'log line')
  const client = stubClient()

  await invoke('reply', { positionals: ['0isnjTCkpW5', 'done'], values: { attach: [log] }, client })

  const { args } = client.calls[0]
  assert.equal(args.action, 'reply')
  assert.equal(args.taskId, '0isnjTCkpW5')
  assert.equal(args.text, 'done')
  // The reply tool called the task its chat.
  assert.equal(client.legacy[0][0], 'reply')
  assert.equal(client.legacy[0][1].chatId, '0isnjTCkpW5')
  assert.equal(args.attachments.length, 1)
  assert.equal(Buffer.from(args.attachments[0].data, 'base64').toString(), 'log line')
})

test('reply reads its text from a file', async () => {
  const dir = scratch()
  const path = join(dir, 'msg.md')
  writeFileSync(path, 'from a file')
  const client = stubClient()
  await invoke('reply', { positionals: ['0isnjTCkpW5', `@${path}`], client })
  assert.equal(client.calls[0].args.text, 'from a file')
})

test('reply requires a task and text', async () => {
  await assert.rejects(() => invoke('reply', { positionals: [] }), /<taskId>/)
  await assert.rejects(() => invoke('reply', { positionals: ['0isnjTCkpW5'] }), /<text>/)
})

test('attachment get fetches the link it is given and names the file', async () => {
  // getAttachment answers with the link by default, and the file's own name.
  const dir = scratch()
  const client = stubClient({
    getAttachment: JSON.stringify({ filename: 'report.pdf', mimeType: 'application/pdf', url: 'https://agentrq.example/storage/artifacts/x' }),
  })
  const fetched = []
  const fetch = async (url) => {
    fetched.push(url)
    return { ok: true, status: 200, arrayBuffer: async () => new TextEncoder().encode('%PDF-1.4').buffer }
  }

  const result = await invoke('attachment get', {
    positionals: ['att-1'],
    values: { task: '0isnjTCkpW5', out: dir },
    client,
    fetch,
  })

  assert.equal(result.text, join(dir, 'report.pdf'))
  assert.equal(result.data.bytes, 8)
  assert.equal(readFileSync(result.text, 'utf8'), '%PDF-1.4')
  // One call, with the default format: nothing else is needed for the name.
  assert.deepEqual(client.calls, [{ name: 'task', args: { action: 'getAttachment', attachmentId: 'att-1', taskId: '0isnjTCkpW5' } }])
  assert.deepEqual(client.legacy, [['getAttachment', { attachmentId: 'att-1', taskId: '0isnjTCkpW5' }]])
  assert.deepEqual(fetched, ['https://agentrq.example/storage/artifacts/x'])
})

test('attachment get decodes the content of one with no link', async () => {
  const dir = scratch()
  const client = stubClient({
    getAttachment: JSON.stringify({ filename: '', mimeType: '', data: Buffer.from('bytes').toString('base64') }),
  })
  const result = await invoke('attachment get', {
    positionals: ['att-unknown'],
    values: { task: '0isnjTCkpW5', out: dir },
    client,
  })
  // With no name, the file is named after its id.
  assert.equal(result.text, join(dir, 'att-unknown'))
  assert.equal(readFileSync(result.text, 'utf8'), 'bytes')
})

test('attachment get explains a link it cannot fetch', async () => {
  const client = stubClient({ getAttachment: JSON.stringify({ filename: 'a', url: 'https://x/a' }) })
  const values = { task: '0isnjTCkpW5', out: scratch() }
  await assert.rejects(
    () => invoke('attachment get', { positionals: ['a'], values, client, fetch: async () => ({ ok: false, status: 404 }) }),
    /cannot download https:\/\/x\/a: HTTP 404/,
  )
  await assert.rejects(
    () =>
      invoke('attachment get', {
        positionals: ['a'],
        values,
        client,
        fetch: async () => {
          throw new Error('offline')
        },
      }),
    /cannot download https:\/\/x\/a: offline/,
  )
})

test('attachment get refuses an answer that is not an attachment', async () => {
  const client = stubClient({ getAttachment: 'not json' })
  await assert.rejects(
    () => invoke('attachment get', { positionals: ['a'], values: { task: '0isnjTCkpW5' }, client }),
    /unexpected answer for attachment a: not json/,
  )
})

test('attachment get needs the task holding the attachment', async () => {
  await assert.rejects(
    () => invoke('attachment get', { positionals: ['att-1'] }),
    /--task <taskId> is required/,
  )
  await assert.rejects(() => invoke('attachment get', { values: { task: 'x' } }), /<attachmentId>/)
})

test('attachment get reports an attachment that is not there', async () => {
  const client = stubClient({ getAttachment: JSON.stringify({ filename: 'gone', data: '' }) })
  await assert.rejects(
    () => invoke('attachment get', { positionals: ['gone'], values: { task: '0isnjTCkpW5' }, client }),
    /is empty or was not found/,
  )
})

test('memory load defaults to the index', async () => {
  const client = stubClient()
  await invoke('memory load', { client })
  assert.deepEqual(client.calls[0].args, { name: undefined })

  await invoke('memory load', { positionals: ['toolchain.md'], client })
  assert.deepEqual(client.calls[1].args, { name: 'toolchain.md' })
})

test('memory save needs content and takes it from anywhere', async () => {
  const client = stubClient()
  await assert.rejects(() => invoke('memory save', { positionals: ['notes.md'] }), /--content is required/)

  await invoke('memory save', {
    positionals: ['notes.md'],
    values: { content: '-' },
    stdin: stdinOf('the whole note'),
    client,
  })
  assert.deepEqual(client.calls[0].args, { name: 'notes.md', content: 'the whole note' })
})

test('memory delete defaults to the index', async () => {
  const client = stubClient()
  await invoke('memory delete', { client })
  assert.deepEqual(client.calls[0].args, { name: undefined })
})

test('skill search passes the query and paging on', async () => {
  const client = stubClient()
  await invoke('skill search', { client })
  await invoke('skill search', { positionals: ['pull', 'request'], values: { limit: '20', offset: '40' }, client })
  assert.deepEqual(
    client.calls.map((c) => c.args),
    [{}, { q: 'pull request', limit: 20, offset: 40 }],
  )
  await assert.rejects(() => invoke('skill search', { values: { limit: '-1' } }), /--limit must be a whole number/)
  await assert.rejects(() => invoke('skill search', { values: { offset: 'x' } }), /--offset must be a whole number/)
})

test('skill load and delete name the skill by URI', async () => {
  const client = stubClient()
  await invoke('skill load', { positionals: ['skill://tdd'], client })
  await invoke('skill delete', { positionals: ['skill://tdd/notes.md'], client })
  assert.deepEqual(
    client.calls.map((c) => [c.name, c.args]),
    [
      ['loadSkill', { uri: 'skill://tdd' }],
      ['deleteSkill', { uri: 'skill://tdd/notes.md' }],
    ],
  )
  await assert.rejects(() => invoke('skill load', {}), /<uri>/)
  await assert.rejects(() => invoke('skill delete', {}), /<uri>/)
})

test('skill save needs a URI and content, and takes the content from anywhere', async () => {
  const client = stubClient()
  await assert.rejects(() => invoke('skill save', { values: { content: 'x' } }), /<uri>/)
  await assert.rejects(() => invoke('skill save', { positionals: ['skill://tdd'] }), /--content is required/)

  await invoke('skill save', {
    positionals: ['skill://tdd/SKILL.md'],
    values: { content: '-' },
    stdin: stdinOf('---\ndescription: d\n---\n'),
    client,
  })
  assert.deepEqual(client.calls[0].args, { uri: 'skill://tdd/SKILL.md', content: '---\ndescription: d\n---\n' })
})

test('event publish sends the payload, task and faq', async () => {
  const client = stubClient()
  await invoke('event publish', {
    positionals: ['build.finished'],
    values: { payload: 'green', task: '0isnjTCkpW5', faq: ['why=it passed'] },
    client,
  })
  assert.deepEqual(client.calls[0].args, {
    name: 'build.finished',
    payload: 'green',
    taskId: '0isnjTCkpW5',
    faq: [{ question: 'why', answer: 'it passed' }],
  })
})

test('event publish requires a name', async () => {
  await assert.rejects(() => invoke('event publish'), /<name>/)
})

test('ask infers form mode from --field', async () => {
  const client = stubClient()
  await invoke('ask', {
    positionals: ['0isnjTCkpW5', 'Which branch?'],
    values: { field: ['branch'], timeout: '60' },
    client,
  })
  const { args } = client.calls[0]
  assert.equal(args.mode, 'form')
  assert.equal(args.timeoutSeconds, 60)
  assert.deepEqual(args.requestedSchema.required, ['branch'])
})

test('ask infers url mode from --url', async () => {
  const client = stubClient()
  await invoke('ask', {
    positionals: ['0isnjTCkpW5', 'Approve please'],
    values: { url: 'https://example.com' },
    client,
  })
  assert.equal(client.calls[0].args.mode, 'url')
})

test('ask takes a full schema as JSON', async () => {
  const client = stubClient()
  const schema = { type: 'object', properties: { ok: { type: 'boolean' } } }
  await invoke('ask', {
    positionals: ['0isnjTCkpW5', 'Question'],
    values: { schema: JSON.stringify(schema) },
    client,
  })
  assert.deepEqual(client.calls[0].args.requestedSchema, schema)
})

test('ask refuses an unusable question', async () => {
  await assert.rejects(
    () => invoke('ask', { positionals: ['0isnjTCkpW5', 'Q'], values: { mode: 'url' } }),
    /mode=url needs --url/,
  )
  await assert.rejects(
    () => invoke('ask', { positionals: ['0isnjTCkpW5', 'Q'] }),
    /mode=form needs at least one --field/,
  )
  await assert.rejects(
    () => invoke('ask', { positionals: ['0isnjTCkpW5', 'Q'], values: { schema: '{oops' } }),
    /--schema is not valid JSON/,
  )
  await assert.rejects(() => invoke('ask', { positionals: ['0isnjTCkpW5'] }), /<message>/)
})

test('tools lists what the server offers, one line each', async () => {
  const client = stubClient({
    tools: [
      { name: 'getWorkspace', description: 'Returns the workspace title.\nSecond line ignored.' },
      { name: 'bare' },
    ],
  })
  const result = await invoke('tools', { client })
  assert.equal(result.text, 'getWorkspace\n  Returns the workspace title.\nbare\n  ')
  assert.equal(result.data.length, 2)
})

test('call reaches a tool this CLI has no verb for', async () => {
  // The escape hatch: a tool added to the server tomorrow works today.
  const client = stubClient({ futureTool: 'done' })
  const result = await invoke('call', {
    positionals: ['futureTool'],
    values: { args: '{"k":"v"}' },
    client,
  })
  assert.equal(result.text, 'done')
  assert.deepEqual(client.calls[0], { name: 'futureTool', args: { k: 'v' } })
})

test('call defaults to no arguments', async () => {
  const client = stubClient()
  await invoke('call', { positionals: ['getWorkspace'], client })
  assert.deepEqual(client.calls[0].args, {})

  await invoke('call', { positionals: ['getWorkspace'], values: { args: '  ' }, client })
  assert.deepEqual(client.calls[1].args, {})
})

test('call refuses arguments that are not JSON, and needs a tool name', async () => {
  await assert.rejects(
    () => invoke('call', { positionals: ['t'], values: { args: '{oops' } }),
    /--args is not valid JSON/,
  )
  await assert.rejects(() => invoke('call', { positionals: [] }), /<tool>/)
})

test('findCommand prefers the longer path', () => {
  // `task get` must win over anything matching just `task`.
  assert.deepEqual(findCommand(['task', 'get', '0isnjTCkpW5']).command.path, ['task', 'get'])
  assert.deepEqual(findCommand(['task', 'get', '0isnjTCkpW5']).rest, ['0isnjTCkpW5'])
  assert.deepEqual(findCommand(['workspace']).command.path, ['workspace'])
  assert.equal(findCommand(['nonsense']).command, null)
  assert.deepEqual(findCommand(['nonsense']).rest, ['nonsense'])
})

test('every command is documented and reachable', () => {
  for (const command of COMMANDS) {
    assert.ok(command.summary, `${command.path.join(' ')} needs a summary`)
    assert.ok(command.usage.startsWith('agentrq-ws'), `${command.path.join(' ')} needs a usage line`)
    assert.equal(findCommand(command.path).command, command)
  }
})

test('site-tools passes the query, pattern and paging on', async () => {
  const client = stubClient()
  await invoke('site-tools', { client })
  await invoke('site-tools', {
    positionals: ['search', 'issues'],
    values: { pattern: '^search', limit: '20', offset: '40' },
    client,
  })
  assert.deepEqual(client.calls, [
    { name: 'listSiteTools', args: {} },
    { name: 'listSiteTools', args: { q: 'search issues', pattern: '^search', limit: 20, offset: 40 } },
  ])
  await assert.rejects(() => invoke('site-tools', { values: { limit: '2.5' } }), /--limit must be a whole number/)
})

test('site-tool asks for one tool of one site', async () => {
  const client = stubClient()
  await invoke('site-tool', { positionals: ['https://github.com', 'search'], client })
  assert.deepEqual(client.calls, [{ name: 'getSiteToolDefinition', args: { site: 'https://github.com', tool: 'search' } }])
  await assert.rejects(() => invoke('site-tool', {}), /<site>/)
  await assert.rejects(() => invoke('site-tool', { positionals: ['https://github.com'] }), /<tool>/)
})

test('call-site-tool sends the site, tool, task and parsed arguments', async () => {
  const client = stubClient()
  await invoke('call-site-tool', {
    positionals: ['https://github.com', 'search'],
    values: { task: '0isnjTCkpW5', args: '{"q":"agentrq"}' },
    client,
  })
  assert.deepEqual(client.calls, [{
    name: 'callSiteTool',
    args: { taskId: '0isnjTCkpW5', site: 'https://github.com', tool: 'search', arguments: { q: 'agentrq' } },
  }])
})

test('call-site-tool reads arguments from stdin, and defaults them to {}', async () => {
  const client = stubClient()
  await invoke('call-site-tool', {
    positionals: ['https://github.com', 'star'],
    values: { task: '0isnjTCkpW5', args: '-' },
    stdin: stdinOf('{"repo":"agentrq"}'),
    client,
  })
  await invoke('call-site-tool', { positionals: ['https://github.com', 'star'], values: { task: '0isnjTCkpW5' }, client })
  assert.deepEqual(client.calls.map((c) => c.args.arguments), [{ repo: 'agentrq' }, {}])
})

test('call-site-tool refuses invalid JSON, a missing task, site or tool', async () => {
  const task = { task: '0isnjTCkpW5' }
  await assert.rejects(
    () => invoke('call-site-tool', { positionals: ['https://github.com', 'star'], values: { ...task, args: '{oops' } }),
    (err) => err instanceof UserError && /--args is not valid JSON/.test(err.message),
  )
  await assert.rejects(() => invoke('call-site-tool', { positionals: ['https://github.com', 'star'] }), /--task <taskId> is required/)
  await assert.rejects(() => invoke('call-site-tool', { values: task }), /<site>/)
  await assert.rejects(() => invoke('call-site-tool', { positionals: ['https://github.com'], values: task }), /<tool>/)
})

test('the CLI covers every tool the workspace server offers', () => {
  // The point of the package: anything an agent can do here, a person can do
  // from a shell. `call` is the catch-all, so a tool may be reached that way.
  const covered = new Set([
    'task',
    'getWorkspace',
    'publishEvent',
    'loadMemory',
    'saveMemory',
    'deleteMemory',
    'searchSkills',
    'loadSkill',
    'saveSkill',
    'deleteSkill',
    'listSiteTools',
    'getSiteToolDefinition',
    'callSiteTool',
    'pano',
  ])
  const source = readFileSync(new URL('../src/commands.js', import.meta.url), 'utf8')
  for (const tool of covered) {
    assert.ok(source.includes(`callTool('${tool}'`) || source.includes(`callAction('${tool}'`), `no command calls ${tool}`)
  }
})

const PANO_DOC = {
  id: '0kGVgW2hMzR',
  title: 'Plan',
  markdown: '# Plan\n\n![chart](chart.png)',
  files: [{ id: 'f1', filename: 'chart.png', url: 'https://agentrq.test/storage/pano/u-1/pano-0kGVgW2hMzR/f1' }],
  attachments: [{ id: 'a1', filename: 'notes.txt', url: 'https://agentrq.test/storage/pano/u-1/pano-0kGVgW2hMzR/a1' }],
  comments: [{ id: 'c1', author: 'human', body: 'Looks good.' }],
}

test('pano save sends the doc with its linked files, and prints only the id', async () => {
  const dir = scratch()
  const chart = join(dir, 'chart.png')
  writeFileSync(chart, 'png-bytes')
  writeFileSync(join(dir, 'plan.md'), '# Plan\n\n![chart](chart.png)')
  const client = stubClient({ pano: JSON.stringify(PANO_DOC) })
  const result = await invoke('pano save', {
    positionals: ['0kGVgW2hMzR'],
    values: { title: 'Plan', markdown: `@${join(dir, 'plan.md')}`, file: [chart], 'ttl-days': '14', color: 'blue' },
    client,
  })
  assert.equal(result.text, '0kGVgW2hMzR')
  assert.deepEqual(result.data, PANO_DOC)
  assert.deepEqual(client.calls[0], {
    name: 'pano',
    args: {
      action: 'save',
      id: '0kGVgW2hMzR',
      title: 'Plan',
      markdown: '# Plan\n\n![chart](chart.png)',
      ttlDays: 14,
      color: 'blue',
      files: [{ filename: 'chart.png', mimeType: 'image/png', data: Buffer.from('png-bytes').toString('base64') }],
    },
  })
})

test('pano save creates a doc from stdin, and passes a refusal on as it is', async () => {
  const client = stubClient({ pano: 'chart.png is not linked from the markdown' })
  const result = await invoke('pano save', { values: { title: 'New', markdown: '-' }, stdin: stdinOf('piped'), client })
  assert.equal(result.text, 'chart.png is not linked from the markdown')
  assert.deepEqual(client.calls[0].args, { action: 'save', id: undefined, title: 'New', markdown: 'piped', ttlDays: undefined, color: undefined, files: undefined })
})

test('pano save refuses a missing title or markdown, an unknown color and a bad ttl', async () => {
  await assert.rejects(() => invoke('pano save', { values: { markdown: 'x' } }), /--title is required/)
  await assert.rejects(() => invoke('pano save', { values: { title: 'x' } }), /--markdown is required/)
  await assert.rejects(() => invoke('pano save', { values: { title: 'x', markdown: 'x', color: 'red' } }), /unknown color "red"/)
  for (const ttl of ['0', '1.5', 'soon']) {
    await assert.rejects(() => invoke('pano save', { values: { title: 'x', markdown: 'x', 'ttl-days': ttl } }), /--ttl-days must be a whole number/)
  }
})

test('pano read prints the doc, with comments and attachments when asked', async () => {
  const client = stubClient({ pano: JSON.stringify(PANO_DOC) })
  const result = await invoke('pano read', { positionals: ['0kGVgW2hMzR'], values: { comments: true, attachments: true }, client })
  assert.equal(result.text, JSON.stringify(PANO_DOC))
  await invoke('pano read', { positionals: ['0kGVgW2hMzR'], client })
  assert.deepEqual(client.calls.map((c) => c.args), [
    { action: 'read', id: '0kGVgW2hMzR', includeComments: true, includeAttachments: true },
    { action: 'read', id: '0kGVgW2hMzR', includeComments: undefined, includeAttachments: undefined },
  ])
  await assert.rejects(() => invoke('pano read', {}), /<id>/)
})

test('pano read --out writes the markdown and downloads every file beside it', async () => {
  const dir = scratch()
  const fetched = []
  const fetch = async (url) => {
    fetched.push(url)
    return { ok: true, arrayBuffer: async () => Buffer.from(`bytes of ${url.slice(-2)}`) }
  }
  const client = stubClient({ pano: JSON.stringify(PANO_DOC) })
  const result = await invoke('pano read', { positionals: ['0kGVgW2hMzR'], values: { out: dir, comments: true, attachments: true }, client, fetch })
  const docDir = join(dir, '0kGVgW2hMzR')
  const paths = [join(docDir, '0kGVgW2hMzR.md'), join(docDir, 'chart.png'), join(docDir, 'attachments', 'notes.txt'), join(docDir, 'comments.json')]
  assert.equal(result.text, paths.join('\n'))
  assert.equal(readFileSync(paths[0], 'utf8'), PANO_DOC.markdown)
  assert.equal(readFileSync(paths[1], 'utf8'), 'bytes of f1')
  assert.equal(readFileSync(paths[2], 'utf8'), 'bytes of a1')
  assert.deepEqual(JSON.parse(readFileSync(paths[3], 'utf8')), PANO_DOC.comments)
  assert.equal(fetched.length, 2)
})

test('pano read --out skips a file without a link, and refuses an answer that is no doc', async () => {
  const dir = scratch()
  const doc = { id: 'd1', title: 't', files: [{ filename: 'gone.png' }] }
  const result = await invoke('pano read', { positionals: ['d1'], values: { out: `${dir}/` }, client: stubClient({ pano: JSON.stringify(doc) }) })
  assert.equal(result.text, join(dir, 'd1', 'd1.md'))
  assert.equal(readFileSync(join(dir, 'd1', 'd1.md'), 'utf8'), '')
  await assert.rejects(
    () => invoke('pano read', { positionals: ['d1'], values: { out: dir }, client: stubClient({ pano: 'Pano doc not found' }) }),
    /unexpected answer from the pano tool/,
  )
})

test('pano comment, attach and delete', async () => {
  const dir = scratch()
  const notes = join(dir, 'notes.txt')
  writeFileSync(notes, 'release notes')
  const client = stubClient({ pano: (args) => (args.action === 'attach' ? JSON.stringify(PANO_DOC) : 'ok') })
  await invoke('pano comment', { positionals: ['d1', 'Ship it.'], client })
  const attached = await invoke('pano attach', { positionals: ['d1'], values: { file: notes }, client })
  assert.equal(attached.text, '0kGVgW2hMzR')
  await invoke('pano delete', { positionals: ['d1'], client })
  assert.deepEqual(client.calls.map((c) => c.args), [
    { action: 'comment', id: 'd1', comment: 'Ship it.' },
    { action: 'attach', id: 'd1', files: [{ filename: 'notes.txt', mimeType: 'text/plain', data: Buffer.from('release notes').toString('base64') }] },
    { action: 'delete', id: 'd1' },
  ])
  await assert.rejects(() => invoke('pano comment', { positionals: ['d1'] }), /<text>/)
  await assert.rejects(() => invoke('pano attach', { positionals: ['d1'] }), /--file is required/)
  await assert.rejects(() => invoke('pano attach', {}), /<id>/)
  await assert.rejects(() => invoke('pano delete', {}), /<id>/)
})

test('task help and pano help ask the server, for one action when named', async () => {
  const client = stubClient()
  await invoke('task help', { client })
  await invoke('task help', { positionals: ['create'], client })
  await invoke('pano help', { positionals: ['save'], client })
  assert.deepEqual(client.calls, [
    { name: 'task', args: { action: 'help', payload: undefined } },
    { name: 'task', args: { action: 'help', payload: { topic: 'create' } } },
    { name: 'pano', args: { action: 'help', payload: { topic: 'save' } } },
  ])
})

test('every pano call keeps its flat form for an older server', async () => {
  const client = stubClient()
  await invoke('pano delete', { positionals: ['d1'], client })
  assert.deepEqual(client.legacy, [['pano', { action: 'delete', id: 'd1' }]])
})

test("ask is the task tool's elicit action", async () => {
  const client = stubClient()
  await invoke('ask', { positionals: ['0isnjTCkpW5', 'Ship it?'], values: { url: 'https://x.test' }, client })
  assert.equal(client.calls[0].name, 'task')
  assert.equal(client.calls[0].args.action, 'elicit')
  assert.equal(client.legacy[0][0], 'elicit')
})
