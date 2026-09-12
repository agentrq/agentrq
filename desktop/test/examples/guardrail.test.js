// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, it, expect, vi } from 'vitest'

import {
  DEFAULT_RULES,
  apply,
  commandIn,
  extraRules,
  refuse,
} from '../../../examples/extensions/guardrail/index.js'
import { parseManifest } from '../../src/main/extensions/manifest.js'
import { CONSENT, availableConsents, createToolCallReview } from '../../src/main/extensions/tool-calls.js'
import manifest from '../../../examples/extensions/guardrail/agentrq-extension.json'

import { describeAsk } from '../../../frontend/src/composables/useExtensionGrant.js'

/**
 * The example for the registry that answers back.
 *
 * Everything else an extension does is a contribution the app draws. This is
 * asked a question, and its answer stands in for the user's — so the tests here
 * are as much about what it *declines* to answer as about what it refuses.
 */

const shell = (command, toolName = 'Bash') => ({
  workspaceId: 'ws1',
  taskId: 't1',
  requestId: 'req-1',
  toolName,
  description: 'run a command',
  inputPreview: JSON.stringify({ command }),
})

describe('the manifest', () => {
  it('is one', () => {
    expect(parseManifest(manifest).ok).toBe(true)
  })

  /**
   * The whole point of the example. Its manifest asks only to refuse, so the
   * install screen never offers the rung where it could approve something — and
   * that ceiling is set by its author, in public, rather than by the screen.
   */
  it('asks only to refuse, so the screen cannot offer more', () => {
    const { manifest: parsed } = parseManifest(manifest)

    expect(parsed.hooks).toEqual({ toolCall: 'deny' })
    expect(availableConsents(parsed)).toEqual([CONSENT.none, CONSENT.deny])
  })

  /**
   * A reviewer that phoned home with every tool call your agents run would be
   * reading the whole session, and no permission on the install screen would
   * stop it. This one asks for nothing, so there is nothing to read it with.
   */
  it('asks for nothing else at all', () => {
    const { manifest: parsed } = parseManifest(manifest)

    expect(parsed.mcp).toEqual({ workspace: [], supervisor: [] })
    expect(parsed.net).toEqual([])
  })

  // It wants no tools, so the old rule — "ask when it wants a permission" —
  // would have installed it with no screen and no consent at all.
  it('still has something to ask a person about', () => {
    const ask = describeAsk(parseManifest(manifest).manifest)

    expect(ask.level).toBe('none')
    expect(ask.reviewsToolCalls).toBe(true)
  })
})

describe('commandIn', () => {
  it('reads the command out of a shell request', () => {
    expect(commandIn(shell('rm -rf /tmp/x'))).toBe('rm -rf /tmp/x')
    expect(commandIn(shell('ls', 'shell_execute'))).toBe('ls')
    expect(commandIn(shell('ls', 'execute_command'))).toBe('ls')
  })

  it('has nothing to say about a tool that is not a shell', () => {
    expect(commandIn(shell('rm -rf /', 'Read'))).toBe('')
    expect(commandIn(undefined)).toBe('')
  })

  /**
   * A preview that will not parse is a harness describing its arguments some
   * other way. Refusing everything it sends would make this look broken and
   * teach its user to switch it off.
   */
  it('abstains on a preview it cannot read rather than refusing it', () => {
    expect(commandIn({ toolName: 'Bash', inputPreview: 'rm -rf /' })).toBe('')
    expect(commandIn({ toolName: 'Bash', inputPreview: '' })).toBe('')
    expect(commandIn({ toolName: 'Bash', inputPreview: '{"cmd":"ls"}' })).toBe('')
    expect(commandIn({ toolName: 'Bash', inputPreview: '{"command":42}' })).toBe('')
  })
})

describe('refuse', () => {
  const because = (command) => refuse(command, DEFAULT_RULES)

  it('refuses the things that cannot be undone', () => {
    expect(because('rm -rf node_modules')).toContain('nothing undoes it')
    expect(because('rm -fr /tmp/x')).toContain('nothing undoes it')
    expect(because('git push --force origin main')).toContain('force push')
    expect(because('git push -f')).toContain('force push')
    expect(because('git reset --hard HEAD~3')).toContain('uncommitted work')
    expect(because('git clean -fd')).toContain('untracked files')
    expect(because('DROP TABLE users')).toContain('destroys data')
    expect(because('psql -c "drop database app"')).toContain('destroys data')
    expect(because('mkfs.ext4 /dev/sda1')).toContain('writes over a disk')
    expect(because('dd if=/dev/zero of=/dev/sda')).toContain('writes over a disk')
    expect(because(':(){ :|:& };:')).toContain('fork bomb')
  })

  /**
   * The abstention is the important half: this answers for somebody, so
   * anything it is not sure about goes back to them. An extension that quietly
   * narrows what its user is ever asked about is worse than no extension.
   */
  it('leaves ordinary work to the user', () => {
    expect(because('rm /tmp/one-file')).toBe('')
    expect(because('git push origin main')).toBe('')
    expect(because('git status')).toBe('')
    expect(because('npm run build')).toBe('')
    expect(because('grep -rf patterns.txt src/')).toBe('')
    expect(because('SELECT * FROM users')).toBe('')
  })
})

describe('extraRules', () => {
  it('adds what the user asked to refuse', () => {
    const rules = extraRules('terraform destroy, kubectl delete')

    expect(refuse('terraform destroy -auto-approve', rules)).toContain('you asked to refuse')
    expect(refuse('kubectl delete ns prod', rules)).toContain('you asked to refuse')
    expect(refuse('terraform plan', rules)).toBe('')
  })

  it('has none by default', () => {
    expect(extraRules(undefined)).toEqual([])
    expect(extraRules('')).toEqual([])
    expect(extraRules(' , ')).toEqual([])
  })

  /**
   * Matched literally rather than compiled. A settings box that quietly accepts
   * regular expressions is one where a stray bracket throws from inside a
   * review, and where `.` means "any character" to somebody who typed a
   * filename.
   */
  it('takes a pattern literally, brackets and all', () => {
    const rules = extraRules('deploy (prod)')

    expect(() => refuse('anything', rules)).not.toThrow()
    expect(refuse('deploy (prod) --now', rules)).toContain('you asked to refuse')
    expect(refuse('deploy xprodx', rules)).toBe('')
  })
})

describe('as the app runs it', () => {
  /** The extension, wired to a real dispatcher, deciding real requests. */
  function install({ patterns } = {}) {
    const entries = []
    const ctx = {
      name: 'guardrail',
      hooks: { add: (entry) => entries.push({ ...entry, owner: 'guardrail' }) },
      logger: { info: vi.fn(), warn: vi.fn() },
    }
    apply(ctx, { patterns })

    const sendVerdict = vi.fn(async () => ({ ok: true }))
    const dispatcher = createToolCallReview({
      reviewers: () => entries,
      grantFor: () => ({ workspaces: ['ws1'], hooks: { toolCall: CONSENT.deny } }),
      allows: () => ({ ok: true }),
      sendVerdict,
      logger: { info: vi.fn(), warn: vi.fn() },
    })

    const event = (command) => ({
      payload: {
        workspaceId: 'ws1',
        id: 't1',
        messages: [
          {
            metadata: {
              type: 'permission_request',
              status: 'pending',
              requestId: `req-${command}`,
              toolName: 'Bash',
              inputPreview: JSON.stringify({ command }),
            },
          },
        ],
      },
    })

    return { dispatcher, sendVerdict, event, ctx }
  }

  it('refuses a destructive command before anybody is asked', async () => {
    const { dispatcher, sendVerdict, event } = install()

    await dispatcher.handle(event('rm -rf /'))

    expect(sendVerdict).toHaveBeenCalledWith(
      expect.objectContaining({ toolName: 'Bash' }),
      expect.objectContaining({ behavior: 'deny', by: 'guardrail', reason: expect.stringContaining('undoes') }),
    )
  })

  it('sends nothing for a command it has no opinion about', async () => {
    const { dispatcher, sendVerdict, event } = install()

    await dispatcher.handle(event('git status'))

    expect(sendVerdict).not.toHaveBeenCalled()
  })

  it('picks up the patterns its user configured', async () => {
    const { dispatcher, sendVerdict, event } = install({ patterns: 'terraform destroy' })

    await dispatcher.handle(event('terraform destroy'))

    expect(sendVerdict).toHaveBeenCalledWith(
      expect.anything(),
      expect.objectContaining({ behavior: 'deny' }),
    )
  })

  it('says what it refused, in its own log', async () => {
    const { dispatcher, event, ctx } = install()

    await dispatcher.handle(event('git reset --hard'))

    expect(ctx.logger.info).toHaveBeenCalledWith(expect.stringContaining('refusing Bash'))
  })
})
