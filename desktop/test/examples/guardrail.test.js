// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { readFileSync } from 'node:fs'

import { describe, it, expect, vi } from 'vitest'

import {
  DEFAULT_RULES,
  apply,
  commandIn,
  extraRules,
  refuse,
  settingsFrom,
  settingsView,
} from '../../../examples/extensions/guardrail/index.js'
import { checkCompatibility, parseManifest } from '../../src/main/extensions/manifest.js'
import { CONSENT, availableConsents, createToolCallReview } from '../../src/main/extensions/tool-calls.js'
import manifest from '../../../examples/extensions/guardrail/agentrq-extension.json'

import { describeAsk } from '../../../frontend/src/composables/useExtensionGrant.js'
import { normaliseView } from '../../../frontend/src/composables/useExtensionView.js'

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

  /**
   * `app.getVersion()` reads the package version, so the build that *adds* an
   * API still reports the version it shipped under. A floor above that makes an
   * example uninstallable on the very build containing it — folder installs
   * included, since `installLocal` refuses an incompatible candidate — which is
   * exactly what a floor of ">=0.7" did here while the app was 0.6.2.
   */
  it('can be installed on the build that contains it', () => {
    const { version } = JSON.parse(
      readFileSync(new URL('../../package.json', import.meta.url), 'utf8'),
    )
    const { manifest: parsed } = parseManifest(manifest)

    const { compatible, reasons } = checkCompatibility(parsed, {
      appVersion: version,
      workspaceTools: [],
      supervisorTools: [],
    })

    expect(reasons).toEqual([])
    expect(compatible).toBe(true)
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
    const rules = extraRules('terraform destroy\nkubectl delete')

    expect(refuse('terraform destroy -auto-approve', rules)).toContain('you asked to refuse')
    expect(refuse('kubectl delete ns prod', rules)).toContain('you asked to refuse')
    expect(refuse('terraform plan', rules)).toBe('')
  })

  it('has none by default', () => {
    expect(extraRules(undefined)).toEqual([])
    expect(extraRules('')).toEqual([])
    expect(extraRules(' \n  \n')).toEqual([])
  })

  /**
   * One per line rather than comma-separated: a shell command very often has a
   * comma in it, and the separator should not be a character the thing being
   * matched routinely contains.
   */
  it('does not split a command on its own commas', () => {
    const rules = extraRules('aws s3 rm --exclude a,b')

    expect(rules).toHaveLength(1)
    expect(refuse('aws s3 rm --exclude a,b s3://x', rules)).toContain('you asked to refuse')
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

/** The extension, wired to a real dispatcher, deciding real requests. */
function install({ patterns = '', builtins = true } = {}) {
  const entries = []
  const tabs = []
  // One workspace's stored settings, in the shape this extension invented.
  // Nothing about it is AgentRQ's, which is the point of `ctx.storage`.
  const stored = new Map()
  if (patterns || builtins === false) stored.set('rules', { patterns, builtins })

  const at = {
    get: async (key) => stored.get(key),
    set: async (key, value) => { stored.set(key, value); return { ok: true } },
  }
  const ctx = {
    name: 'guardrail',
    hooks: { add: (entry) => entries.push({ ...entry, owner: 'guardrail' }) },
    ui: { add: (entry) => tabs.push({ ...entry, owner: 'guardrail' }) },
    storage: { workspace: () => at },
    logger: { info: vi.fn(), warn: vi.fn() },
  }
  apply(ctx)

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

  return { dispatcher, sendVerdict, event, ctx, tabs, stored }
}

describe('as the app runs it', () => {

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

  it('picks up the patterns its user typed on the settings tab', async () => {
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


/**
 * The settings tab.
 *
 * The extension decides what a setting *is* — here, a block of plain-text
 * patterns and a switch — and AgentRQ draws it. Nothing in the extension styles
 * anything, and there is no way for it to.
 */
describe('the settings tab', () => {
  const tabOf = (tabs) => tabs.find((entry) => entry.surface === 'workspace-settings-tab')

  it('is contributed as a tab, not a page or an action', () => {
    const { tabs } = install()

    expect(tabOf(tabs)).toMatchObject({ id: 'rules', surface: 'workspace-settings-tab', label: 'Guardrail' })
  })

  it('draws something the renderer will accept', () => {
    const drawn = normaliseView(settingsView({}))

    expect(drawn.ok).toBe(true)
    expect(drawn.view.values).toEqual({ patterns: '', builtins: true })
  })

  it('fills the boxes from what is stored for this workspace', async () => {
    const { tabs } = install({ patterns: 'terraform destroy', builtins: false })

    const drawn = normaliseView(await tabOf(tabs).view({ workspaceId: 'ws1' }))

    expect(drawn.ok).toBe(true)
    expect(drawn.view.values).toEqual({ patterns: 'terraform destroy', builtins: false })
  })

  it('saves what was submitted, and says so', async () => {
    const { tabs, stored } = install()

    const drawn = await tabOf(tabs).view({
      workspaceId: 'ws1',
      action: 'save',
      values: { patterns: 'kubectl delete', builtins: true },
    })

    expect(stored.get('rules')).toEqual({ patterns: 'kubectl delete', builtins: true })
    expect(normaliseView(drawn).view.nodes.some((node) => node.value === 'Saved.')).toBe(true)
  })

  /**
   * Storage refuses rather than truncating, so somebody who pasted a very long
   * list has to be told it was not saved — otherwise they discover it by it not
   * being in force.
   */
  it('says when what was submitted could not be stored', async () => {
    const { tabs, ctx } = install()
    ctx.storage.workspace = () => ({
      get: async () => ({}),
      set: async () => ({ ok: false, reason: 'That is too large.' }),
    })

    const drawn = await tabOf(tabs).view({ workspaceId: 'ws1', action: 'save', values: {} })

    expect(normaliseView(drawn).view.nodes[0]).toMatchObject({ tone: 'critical', value: 'That is too large.' })
  })

  it('takes a rules block and a switch, and nothing else', () => {
    expect(settingsFrom({ patterns: 'a', builtins: true, sneaky: 1 })).toEqual({ patterns: 'a', builtins: true })
    expect(settingsFrom({})).toEqual({ patterns: '', builtins: true })
    expect(settingsFrom({ builtins: false }).builtins).toBe(false)
    expect(settingsFrom()).toEqual({ patterns: '', builtins: true })
  })

  /** Turning the built-ins off leaves only what somebody typed. */
  it('lets a workspace refuse only its own list', async () => {
    const { dispatcher, sendVerdict, event } = install({ patterns: 'terraform destroy', builtins: false })

    await dispatcher.handle(event('rm -rf /'))
    expect(sendVerdict).not.toHaveBeenCalled()

    await dispatcher.handle(event('terraform destroy'))
    expect(sendVerdict).toHaveBeenCalledTimes(1)
  })
})
