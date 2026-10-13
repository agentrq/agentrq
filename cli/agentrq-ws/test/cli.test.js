// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import assert from 'node:assert/strict'
import { mkdtempSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'node:test'

import { run } from '../src/cli.js'
import { COMMANDS } from '../src/commands.js'
import { commandHelp, mainHelp } from '../src/help.js'
import { VERSION } from '../src/version.js'
import { collect, fakeFetch, stdinOf, withSession } from './fake-server.js'

const ENV = { AGENTRQ_WS_URL: 'https://workspace.test' }

/** Run the CLI with everything stubbed, returning what it printed and its code. */
async function cli(argv, { createClient, env = ENV, stdin, cwd = process.cwd() } = {}) {
  const stdout = collect()
  const stderr = collect()
  const code = await run({ argv, stdout, stderr, stdin, cwd, env, createClient })
  return { code, out: stdout.text, err: stderr.text }
}

/** A client that answers every tool with the same text. */
const clientSaying = (text, onCall) => () => ({
  async callTool(name, args) {
    if (onCall) onCall(name, args)
    return { text, result: { content: [{ type: 'text', text }], structured: { name } } }
  },
  async callAction(tool, action, payload) {
    return this.callTool(tool, { action, payload })
  },
  async listTools() {
    return [{ name: 'getWorkspace', description: 'x' }]
  },
  async close() {},
})

test('no arguments prints the main help', async () => {
  const { code, out } = await cli([])
  assert.equal(code, 0)
  assert.match(out, /AgentRQ workspace client/)
  assert.match(out, /Usage: agentrq-ws <command>/)
})

test('help lists every command', async () => {
  // This is the entry point the package tells people to run first, so it has
  // to actually name the whole surface.
  const { out } = await cli(['help'])
  for (const command of COMMANDS) {
    assert.ok(out.includes(command.path.join(' ')), `help omits ${command.path.join(' ')}`)
  }
})

test('help <command> explains that command', async () => {
  const { code, out } = await cli(['help', 'attachment', 'get'])
  assert.equal(code, 0)
  assert.match(out, /--task/)
  assert.match(out, /default: OS temp dir/)
})

test('help for an unknown command fails rather than showing nothing', async () => {
  const { code, err } = await cli(['help', 'nonsense'])
  assert.equal(code, 1)
  assert.match(err, /unknown command "nonsense"/)
})

test('--help on a command shows that command', async () => {
  const { code, out } = await cli(['task', 'create', '--help'])
  assert.equal(code, 0)
  assert.match(out, /Usage: agentrq-ws task create/)
})

test('the version is reported every way it is asked for', async () => {
  for (const argv of [['--version'], ['-V'], ['version'], ['workspace', '--version']]) {
    const { code, out } = await cli(argv)
    assert.equal(code, 0)
    assert.equal(out.trim(), VERSION)
  }
})

test('an unknown command suggests the real ones', async () => {
  const { code, err } = await cli(['taks', 'get'])
  assert.equal(code, 1)
  assert.match(err, /unknown command "taks get"/)
  assert.match(err, /agentrq-ws help/)
})

test('a bad flag is reported with the command usage', async () => {
  const { code, err } = await cli(['workspace', '--no-such-flag'])
  assert.equal(code, 1)
  assert.match(err, /Usage: agentrq-ws workspace/)
})

test('a command prints the server text and exits zero', async () => {
  const { code, out } = await cli(['workspace'], { createClient: clientSaying('Workspace: agentrq-code') })
  assert.equal(code, 0)
  assert.equal(out, 'Workspace: agentrq-code\n')
})

test('--json prints the structured result instead', async () => {
  const { code, out } = await cli(['workspace', '--json'], { createClient: clientSaying('text') })
  assert.equal(code, 0)
  assert.deepEqual(JSON.parse(out), { content: [{ type: 'text', text: 'text' }], structured: { name: 'workspace' } })
})

test('--json falls back to the text when a command returns no raw result', async () => {
  const dir = mkdtempSync(join(tmpdir(), 'agentrq-ws-cli-'))
  const createClient = () => ({
    async callAction() {
      return { text: JSON.stringify({ filename: 'note.txt', mimeType: 'text/plain', data: Buffer.from('hi').toString('base64') }) }
    },
    async close() {},
  })
  const { code, out } = await cli(['attachment', 'get', 'a1', '--task', 't1', '--out', dir, '--json'], {
    createClient,
  })
  assert.equal(code, 0)
  assert.deepEqual(JSON.parse(out), { path: join(dir, 'note.txt'), bytes: 2 })
})

test('a command that prints nothing prints nothing', async () => {
  const { code, out } = await cli(['workspace'], { createClient: clientSaying('') })
  assert.equal(code, 0)
  assert.equal(out, '')
})

test('a server refusal is reported as a message, not a stack', async () => {
  const createClient = () => ({
    async callAction() {
      const { ServerError } = await import('../src/errors.js')
      throw new ServerError('invalid taskId format')
    },
    async close() {},
  })
  const { code, err } = await cli(['task', 'get', 'bogus'], { createClient })
  assert.equal(code, 1)
  assert.equal(err, 'agentrq-ws: invalid taskId format\n')
})

test('a usage mistake is reported as a message, not a stack', async () => {
  const { code, err } = await cli(['task', 'status', '0isnjTCkpW5', 'done'], {
    createClient: clientSaying('ok'),
  })
  assert.equal(code, 1)
  assert.match(err, /unknown status "done"/)
  assert.doesNotMatch(err, /at /, 'a mistyped status is not a crash')
})

test('an unexpected error keeps its stack, because that one is a bug', async () => {
  const createClient = () => ({
    async callAction() {
      throw new TypeError('undefined is not a function')
    },
    async close() {},
  })
  const { code, err } = await cli(['workspace'], { createClient })
  assert.equal(code, 1)
  assert.match(err, /unexpected error/)
  assert.match(err, /TypeError/)
})

test('the session is closed even when the command fails', async () => {
  // Otherwise every failed invocation leaks a session on the server.
  let closed = 0
  const createClient = () => ({
    async callAction() {
      throw new Error('server unavailable')
    },
    async close() {
      closed += 1
    },
  })
  await cli(['workspace'], { createClient })
  assert.equal(closed, 1)
})

test('a client without close is still fine', async () => {
  const createClient = () => ({
    async callAction() {
      return { text: 'ok' }
    },
  })
  const { code } = await cli(['workspace'], { createClient })
  assert.equal(code, 0)
})

test('a missing .mcp.json is explained before anything is sent', async () => {
  const empty = mkdtempSync(join(tmpdir(), 'agentrq-ws-nowhere-'))
  let created = false
  const { code, err } = await cli(['workspace'], {
    env: {},
    cwd: empty,
    createClient: () => {
      created = true
      return { async callAction() {}, async close() {} }
    },
  })
  assert.equal(code, 1)
  assert.match(err, /no \.mcp\.json found/)
  assert.equal(created, false, 'no connection is attempted without a server')
})

test('the whole path works against a server, from .mcp.json to printed text', async () => {
  // End to end through the real client and the real config loader: only the
  // socket is a stand-in.
  const dir = mkdtempSync(join(tmpdir(), 'agentrq-ws-e2e-'))
  writeFileSync(
    join(dir, '.mcp.json'),
    JSON.stringify({ mcpServers: { 'agentrq-workspace': { url: 'https://workspace.test' } } }),
  )
  const fetchImpl = withSession(fakeFetch({ onCall: () => 'Workspace: agentrq-code' }))
  const { McpClient } = await import('../src/mcp.js')

  const { code, out } = await cli(['workspace'], {
    env: {},
    cwd: dir,
    createClient: (server) => new McpClient({ ...server, fetchImpl }),
  })
  assert.equal(code, 0)
  assert.equal(out, 'Workspace: agentrq-code\n')
})

test('stdin reaches a command that asks for it', async () => {
  let seen
  const { code } = await cli(['memory', 'save', 'notes.md', '--content', '-'], {
    stdin: stdinOf('remembered'),
    createClient: clientSaying('saved', (name, args) => {
      seen = args
    }),
  })
  assert.equal(code, 0)
  assert.deepEqual(seen, { action: 'save', payload: { name: 'notes.md', content: 'remembered' } })
})

test('--server picks between several configured servers', async () => {
  const dir = mkdtempSync(join(tmpdir(), 'agentrq-ws-multi-'))
  writeFileSync(
    join(dir, '.mcp.json'),
    JSON.stringify({ mcpServers: { 'agentrq-a': { url: 'https://a' }, 'agentrq-b': { url: 'https://b' } } }),
  )
  let url
  const { code } = await cli(['workspace', '--server', 'agentrq-b'], {
    env: {},
    cwd: dir,
    createClient: (server) => {
      url = server.url
      return { async callAction() { return { text: 'ok' } }, async close() {} }
    },
  })
  assert.equal(code, 0)
  assert.equal(url, 'https://b')
})

test('mainHelp and commandHelp render options, including a command with none', () => {
  assert.match(mainHelp(), /Environment:/)
  const noOptions = COMMANDS.find((c) => !c.options)
  assert.ok(noOptions, 'expected at least one command with no options of its own')
  const help = commandHelp(noOptions)
  assert.match(help, /Global options:/)
  assert.doesNotMatch(help, /^Options:/m)
  assert.match(commandHelp(COMMANDS.find((c) => c.options)), /^Options:/m)
})

test('with no injected client the CLI builds a real one and talks to the URL', async () => {
  // Covers the default wiring: everything above stubs createClient, so without
  // this the production path from argv to socket is never exercised.
  const { code, err } = await cli(['workspace'], {
    env: { AGENTRQ_WS_URL: 'http://127.0.0.1:1/unreachable' },
  })
  assert.equal(code, 1)
  assert.match(err, /cannot reach the workspace server at http:\/\/127\.0\.0\.1:1\/unreachable/)
})

test('the package entry point exports the public surface', async () => {
  const api = await import('../src/index.js')
  assert.equal(typeof api.run, 'function')
  assert.equal(typeof api.McpClient, 'function')
  assert.equal(typeof api.resolveServer, 'function')
  assert.equal(api.PROTOCOL_VERSION, '2025-06-18')
  assert.ok(Array.isArray(api.COMMANDS))
  assert.equal(api.VERSION, VERSION)
})

test('every command answers --help, and none of them needs a server to do it', async () => {
  // Asked for explicitly: --help on each subcommand. It must also come back
  // before any connection is attempted, so it works with no .mcp.json at all.
  const empty = mkdtempSync(join(tmpdir(), 'agentrq-ws-help-'))
  for (const command of COMMANDS) {
    const { code, out } = await cli([...command.path, '--help'], { env: {}, cwd: empty })
    assert.equal(code, 0, `${command.path.join(' ')} --help exited ${code}`)
    assert.match(out, new RegExp(`Usage: agentrq-ws ${command.path.join(' ')}`))
    assert.ok(out.includes(command.summary), `${command.path.join(' ')} --help omits its summary`)
  }
})

test('every command answers -h as well', async () => {
  const empty = mkdtempSync(join(tmpdir(), 'agentrq-ws-help-'))
  for (const command of COMMANDS) {
    const { code, out } = await cli([...command.path, '-h'], { env: {}, cwd: empty })
    assert.equal(code, 0)
    assert.match(out, /Usage: agentrq-ws/)
  }
})

test('a command family lists what it holds instead of saying unknown command', async () => {
  const { code, out } = await cli(['task'])
  assert.equal(code, 1, 'an incomplete command is still an error')
  assert.match(out, /Usage: agentrq-ws task <command>/)
  assert.match(out, /task create/)
  assert.doesNotMatch(out, /reply/, 'only the family is listed')
})

test('a command family asked for help exits zero', async () => {
  for (const argv of [['task', '--help'], ['memory', '-h']]) {
    const { code, out } = await cli(argv)
    assert.equal(code, 0)
    assert.match(out, /Commands:/)
  }
})

test('help <family> explains the family', async () => {
  const { code, out } = await cli(['help', 'memory'])
  assert.equal(code, 0)
  assert.match(out, /memory load/)
  assert.match(out, /memory save/)
  assert.match(out, /memory delete/)
})

test('findGroup matches only a real family prefix', async () => {
  const { findGroup } = await import('../src/commands.js')
  assert.equal(findGroup(['task']).members.length, 5)
  assert.equal(findGroup(['attachment']).members.length, 1)
  assert.equal(findGroup(['workspace']), null, 'a leaf command is not a family')
  assert.equal(findGroup(['nonsense']), null)
  assert.equal(findGroup(['--help']), null)
  assert.equal(findGroup([]), null)
})
