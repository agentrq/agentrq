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
 */

export const name = 'guardrail'

/** The one registry it uses. `inject` is what decides its context. */
export const inject = ['hooks']

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
 * Extra patterns from the extension's settings, as regular expressions.
 *
 * Each is escaped and matched literally rather than compiled as a regex. A
 * settings box that quietly accepts regular expressions is a settings box where
 * a stray `(` is an exception thrown from inside a review, and where `.`
 * silently means "any character" to somebody who typed a file name.
 */
export function extraRules(patterns) {
  return String(patterns ?? '')
    .split(',')
    .map((entry) => entry.trim())
    .filter(Boolean)
    .map((literal) => ({
      pattern: new RegExp(literal.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')),
      because: `it matches "${literal}", which you asked to refuse`,
    }))
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

export function apply(ctx, config) {
  const rules = [...DEFAULT_RULES, ...extraRules(config?.patterns)]

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
    review(request) {
      const command = commandIn(request)
      if (!command) return undefined

      const because = refuse(command, rules)
      if (!because) return undefined

      ctx.logger.info(`refusing ${request.toolName}: ${because}`)
      // The reason travels with the verdict and is what the agent reads. It is
      // written to be acted on rather than apologised for: an agent told *why*
      // can come back with a narrower command, and one told "denied" asks the
      // same thing again.
      return { behavior: 'deny', reason: because }
    },
  })
}
