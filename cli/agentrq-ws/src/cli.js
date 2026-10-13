// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { parseArgs } from 'node:util'

import { COMMANDS, findCommand, findGroup } from './commands.js'
import { resolveServer } from './config.js'
import { ServerError, UserError } from './errors.js'
import { GLOBAL_OPTIONS, commandHelp, groupHelp, mainHelp } from './help.js'
import { McpClient } from './mcp.js'
import { VERSION } from './version.js'

/** Strip the `description` keys parseArgs does not understand. */
function optionSpec(options) {
  const spec = {}
  for (const [name, { description, ...rest }] of Object.entries(options || {})) {
    spec[name] = rest
  }
  return spec
}

/**
 * Run the CLI.
 *
 * Everything the process touches — argv, streams, cwd, env, and how a client is
 * made — arrives as an argument, so the whole command surface is testable
 * without spawning a process or opening a socket.
 */
export async function run({
  argv = [],
  stdout = process.stdout,
  stderr = process.stderr,
  stdin = process.stdin,
  cwd = process.cwd(),
  env = process.env,
  createClient = (server) => new McpClient(server),
} = {}) {
  const write = (stream, text) => stream.write(`${text}\n`)

  try {
    // `--help "memory load"` reads as `help memory load`: a leading help flag
    // asks for the help of whatever follows, rather than being an unknown command.
    // A quoted "memory load" is one argument, so split it into words.
    if (argv[0] === '--help' || argv[0] === '-h') {
      argv = ['help', ...argv.slice(1).flatMap((arg) => arg.split(/\s+/).filter(Boolean))]
    }
    if (argv.length === 0 || argv[0] === 'help') {
      const target = argv.slice(1)
      if (target.length > 0) {
        const { command } = findCommand(target)
        if (command) {
          write(stdout, commandHelp(command))
          return 0
        }
        const group = findGroup(target)
        if (group) {
          write(stdout, groupHelp(group))
          return 0
        }
        throw new UserError(`unknown command "${target.join(' ')}"`)
      }
      write(stdout, mainHelp())
      return 0
    }
    if (argv[0] === '--version' || argv[0] === '-V' || argv[0] === 'version') {
      write(stdout, VERSION)
      return 0
    }

    const { command, rest } = findCommand(argv)
    if (!command) {
      // `task` on its own names a family, not a command. Listing what it holds
      // is the useful answer; git and docker both behave this way, and it
      // still exits non-zero unless help was what was actually asked for.
      const group = findGroup(argv)
      if (group) {
        write(stdout, groupHelp(group))
        return argv.some((arg) => arg === '--help' || arg === '-h') ? 0 : 1
      }
      throw new UserError(
        `unknown command "${argv.join(' ')}".\n` +
          `Try one of: ${COMMANDS.map((c) => c.path.join(' ')).join(', ')}\n` +
          'Run `agentrq-ws help` for the full list.',
      )
    }

    let values
    let positionals
    try {
      ;({ values, positionals } = parseArgs({
        args: rest,
        options: { ...optionSpec(GLOBAL_OPTIONS), ...optionSpec(command.options) },
        allowPositionals: true,
      }))
    } catch (err) {
      throw new UserError(`${err.message}\n\nUsage: ${command.usage}`)
    }

    if (values.help) {
      write(stdout, commandHelp(command))
      return 0
    }
    if (values.version) {
      write(stdout, VERSION)
      return 0
    }

    const server = resolveServer({ cwd, configPath: values.config, serverName: values.server, env })
    const client = createClient(server)

    let outcome
    try {
      outcome = await command.run({ client, values, positionals, stdin, cwd, env, server })
    } finally {
      if (typeof client.close === 'function') await client.close()
    }

    if (values.json) {
      write(stdout, JSON.stringify(outcome.data ?? outcome.result ?? { text: outcome.text }, null, 2))
    } else if (outcome.text) {
      write(stdout, outcome.text)
    }
    return 0
  } catch (err) {
    if (err instanceof UserError || err instanceof ServerError) {
      write(stderr, `agentrq-ws: ${err.message}`)
      return err.exitCode || 1
    }
    write(stderr, `agentrq-ws: unexpected error: ${err && err.stack ? err.stack : err}`)
    return 1
  }
}
