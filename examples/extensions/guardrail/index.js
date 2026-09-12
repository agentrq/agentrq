// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * Guardrail — the example for the one registry that answers back.
 *
 * Every other extension in here *contributes* something: a page, a menu row, a
 * schedule. This one is asked a question. When an agent wants to run something
 * it cannot run unattended, the app stops, hands the pending call to whoever may
 * review it, and takes the answer as the user's own.
 *
 * It refuses a short list of commands that cannot be undone, and it can do
 * nothing else — its manifest says `"hooks": { "toolCall": "deny" }`, so the
 * install screen never offers the rung where it could approve one.
 *
 * ## Why an example that can only refuse is the right first one
 *
 * Refusing costs a tool call. Approving costs whatever the tool call does. An
 * extension that can only refuse is wrong in one direction — it blocks
 * something you wanted, you see the prompt, you turn it off from its row — and
 * an extension that can approve is wrong in the direction nobody finds out
 * about. Ask for `decide` when you genuinely need it, and expect to be read
 * more carefully for it.
 *
 * ## What this is not
 *
 * **It is not a security control, and nothing about it should be described as
 * one.** It runs in the desktop app, so with the app closed every request waits
 * for a person exactly as it did before this was installed. It reads the command
 * the harness *previewed*, which is a string that was easy to produce rather
 * than a promise about what will execute. And it is a list of patterns: an agent
 * writing `rm` with an environment variable, a shell function, or a script it
 * generated a moment earlier walks straight past it.
 *
 * What it is worth is the ordinary case — an agent about to do something
 * irreversible for a sensible-looking reason at two in the morning — where a
 * flat "no" costs one round trip and saves a restore from backup.
 *
 * ## Asks for nothing
 *
 * No workspace tools, no supervisor tools, no network. Everything it decides, it
 * decides from the request it was handed. That matters here more than anywhere:
 * a reviewer that phoned home with every tool call your agents run would be
 * reading your whole session, and there is no permission on the install screen
 * that would stop it.
 *
 * ## Its settings are its own, and per workspace
 *
 * The extra rules are not manifest config — they are a block of text with a
 * shape this extension invented, written on a tab it draws itself, stored under
 * `ctx.storage.workspace(id)`. Two workspaces can have entirely different rules
 * without this file knowing how to key anything, and an uninstall takes both
 * away, because the container is AgentRQ's even though the contents never are.
 */

/**
 * ## A note on `engines`
 *
 * `>=0.6`, which is the build this shipped in rather than the version that
 * first had the APIs — because `app.getVersion()` reads the package version,
 * and the branch that added `hooks`, `ctx.storage` and the settings tab was
 * still 0.6.2. A floor of `>=0.7` would have made the example uninstallable on
 * the very build that contains it, including for anyone reviewing it.
 *
 * The cost is an older 0.6.x, where this installs and then refuses to load with
 * *"This extension asks for something that does not exist: hooks"* — which is a
 * sentence somebody can act on, and the better of the two failures.
 */

export const name = 'guardrail'

/**
 * Two registries: the one that answers questions, and the one that draws the
 * screen where its rules are written.
 *
 * `ctx.storage` is not in here and never needs to be — like `ctx.mcp` it is
 * always present, because it is not a registry and nothing is contributed to
 * it.
 */
export const inject = ['hooks', 'ui']

/**
 * Commands refused by default, each with the reason a person needs to hear.
 *
 * Kept as a list of `{ pattern, because }` rather than one large regular
 * expression, because the message is half the value: "refused" tells an agent
 * nothing it can act on, and "this deletes a directory tree and cannot be
 * undone" tells it to ask for something narrower.
 */
export const DEFAULT_RULES = Object.freeze([
  { pattern: /\brm\s+(-[a-zA-Z]*[rR][a-zA-Z]*\s+|-[a-zA-Z]*f[a-zA-Z]*\s+)/, because: 'this deletes a directory tree, and nothing undoes it' },
  { pattern: /\bgit\s+push\b[^\n]*(--force\b|(?<![a-zA-Z-])-f\b)/, because: 'a force push can destroy commits nobody else has a copy of' },
  { pattern: /\bgit\s+reset\s+--hard\b/, because: 'this throws away uncommitted work with no way back' },
  { pattern: /\bgit\s+clean\s+-[a-zA-Z]*[dfx]/, because: 'this deletes untracked files, which are the ones not in any history' },
  { pattern: /\b(drop|truncate)\s+(table|database|schema)\b/i, because: 'this destroys data in a database' },
  { pattern: /\bmkfs(\.\w+)?\b|\bdd\s+[^\n]*\bof=\/dev\//, because: 'this writes over a disk' },
  { pattern: /:\(\)\s*\{.*\};\s*:/, because: 'this is a fork bomb' },
])

/** Tool names that run a shell. The same three the server's auto-allow knows. */
const SHELL_TOOLS = new Set(['Bash', 'shell_execute', 'execute_command'])

/**
 * The command a request is about, or '' when it is not about one.
 *
 * `inputPreview` arrives as the JSON string the harness sent, and is parsed here
 * rather than upstream on purpose: the host hands over exactly what the server
 * wrote, so a reviewer can see the real arguments instead of somebody's
 * half-parsed idea of them.
 *
 * A preview that will not parse is not a reason to refuse. It is a harness
 * describing its arguments some other way, and refusing everything it sends
 * would make this extension look broken while teaching its user to switch it
 * off.
 */
export function commandIn(request) {
  if (!SHELL_TOOLS.has(request?.toolName)) return ''
  try {
    const input = JSON.parse(request.inputPreview || '{}')
    return typeof input?.command === 'string' ? input.command : ''
  } catch {
    return ''
  }
}

/**
 * Extra patterns somebody typed on this workspace's settings tab.
 *
 * Each is escaped and matched literally rather than compiled as a regex. A
 * settings box that quietly accepts regular expressions is a settings box where
 * a stray `(` is an exception thrown from inside a review, and where `.`
 * silently means "any character" to somebody who typed a file name.
 *
 * One per line rather than comma-separated, because a shell command very often
 * has a comma in it and the separator should not be a character the thing being
 * matched routinely contains.
 */
export function extraRules(patterns) {
  return String(patterns ?? '')
    .split('\n')
    .map((entry) => entry.trim())
    .filter(Boolean)
    .map((literal) => ({
      pattern: new RegExp(literal.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')),
      because: `it matches "${literal}", which you asked to refuse`,
    }))
}

/** Where this workspace's own settings live. One key, one shape, ours. */
const SETTINGS = 'rules'

/**
 * The settings tab, as a description rather than as markup.
 *
 * This is the whole point of the surface: the extension decides what a setting
 * *is* — here, a block of plain-text patterns and a switch — and AgentRQ draws
 * it with its own controls. Nothing in this file styles anything, and there is
 * no way for it to.
 *
 * The same handler answers both the first draw and the submit, told apart by
 * `context.action`. One entry point rather than two is what keeps a form from
 * needing its own registration, its own lookup and its own failure modes.
 */
export function settingsView(saved, { saved: justSaved = false } = {}) {
  return {
    title: 'Guardrail',
    nodes: [
      {
        type: 'text',
        tone: 'muted',
        value:
          'Commands matching these are refused before you are asked about them. ' +
          'One per line, matched literally. This only works while AgentRQ is open.',
      },
      {
        type: 'form',
        action: 'save',
        submit: 'Save rules',
        children: [
          {
            type: 'field',
            key: 'patterns',
            label: 'Also refuse, in this workspace',
            input: 'multiline',
            value: saved.patterns ?? '',
            placeholder: 'terraform destroy\nkubectl delete',
          },
          {
            type: 'toggle',
            key: 'builtins',
            label: 'Also refuse the built-in list (rm -rf, force pushes, drop table, …)',
            value: saved.builtins !== false,
          },
        ],
      },
      justSaved ? { type: 'text', tone: 'positive', value: 'Saved.' } : { type: 'text', tone: 'muted', value: '' },
    ],
  }
}

/**
 * What a submitted tab means, as a value to store.
 *
 * Separate from the handler so the rule — a toggle is a yes or no, a text area
 * is text, and nothing else survives — can be read and tested without a store
 * or a workspace anywhere near it.
 */
export function settingsFrom(values = {}) {
  return {
    patterns: String(values.patterns ?? ''),
    builtins: values.builtins !== false,
  }
}

/**
 * Why this command is refused, or '' to leave it to the user.
 *
 * The abstention is the important half. This answers for somebody, so anything
 * it is not sure about has to go back to them — the alternative is an extension
 * that quietly narrows what its user is ever asked about.
 */
export function refuse(command, rules) {
  const hit = rules.find((rule) => rule.pattern.test(command))
  return hit ? hit.because : ''
}

export function apply(ctx) {
  /**
   * The rules in force for one workspace, built fresh on every request.
   *
   * Read rather than cached, because the settings tab can change them while an
   * agent is working — and a reviewer answering from a cache would go on
   * refusing something somebody had just allowed, with nothing on any screen
   * saying why.
   */
  async function rulesFor(workspaceId) {
    const saved = (await ctx.storage.workspace(workspaceId).get(SETTINGS)) ?? {}
    const extra = extraRules(saved.patterns)
    return saved.builtins === false ? extra : [...DEFAULT_RULES, ...extra]
  }

  ctx.ui.add({
    id: 'rules',
    surface: 'workspace-settings-tab',
    label: 'Guardrail',
    /**
     * Draw the tab, and save it when it is submitted.
     *
     * `context.workspaceId` is what makes this per workspace on both sides: it
     * decides which settings are read, and which are written. The extension
     * never keys the storage itself — `ctx.storage.workspace(id)` does, so the
     * container knows the data is per-workspace and can act on it.
     */
    async view(context) {
      const at = ctx.storage.workspace(context.workspaceId)

      if (context.action === 'save') {
        const written = await at.set(SETTINGS, settingsFrom(context.values))
        // Reported rather than swallowed: storage refuses rather than
        // truncating, and somebody who pasted a very long list needs to be told
        // it was not saved rather than discovering it was not in force.
        if (!written.ok) {
          return { title: 'Guardrail', nodes: [{ type: 'text', tone: 'critical', value: written.reason }] }
        }
        return settingsView(await at.get(SETTINGS), { saved: true })
      }

      return settingsView((await at.get(SETTINGS)) ?? {})
    },
  })

  ctx.hooks.add({
    id: 'irreversible-shell',
    /**
     * Called with the pending request, and answering one of three things.
     *
     * `{ behavior: 'deny' }` refuses it. `{ behavior: 'allow' }` would approve
     * it — this extension never says that, and could not act on it if it did,
     * because its manifest asked only to refuse. Returning nothing abstains,
     * and the user is asked exactly as they would have been.
     */
    async review(request) {
      const command = commandIn(request)
      if (!command) return undefined

      const because = refuse(command, await rulesFor(request.workspaceId))
      if (!because) return undefined

      ctx.logger.info(`refusing ${request.toolName}: ${because}`)
      // The reason is why *this* refusal happened, and today it reaches the log
      // and the desktop's own record of the decision — the verdict the agent is
      // sent carries the behaviour and nothing else. Written as a sentence
      // anyway, and worth writing that way: it is the line somebody reads when
      // they are working out why a command they wanted never ran.
      return { behavior: 'deny', reason: because }
    },
  })
}
