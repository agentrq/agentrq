/**
 * Plugin configuration schema.
 *
 * Everything two deployments might reasonably set differently is a config
 * field, per the harness configuration guidance: nothing tunable is hardcoded.
 *
 * @module @agentrq/dsh-plugin-agentrq
 */

import Schema from '@deepseek-ai/schemastery'

/** Reconnection backoff for a dropped workspace session. */
export interface ReconnectConfig {
  /** Delay before the first retry, in milliseconds. */
  initialDelayMs: number
  /** Ceiling for the exponential backoff, in milliseconds. */
  maxDelayMs: number
}

/** Resolved plugin configuration. */
export interface Config {
  /**
   * The workspace's AgentRQ MCP endpoint. Copy it from Workspace Settings —
   * the URL there already carries `?token=…`, which is how AgentRQ
   * authenticates a headless client.
   */
  url: string
  /**
   * Optional bearer token, for deployments that prefer an `Authorization`
   * header over the `?token=` query parameter. Empty means "the URL carries
   * its own credential".
   */
  token: string
  /**
   * Whether to mount the MCP bridge that gives the model AgentRQ's tools.
   *
   * The plugin mounts one `@deepseek-ai/dsh-mcp-client` instance itself, so a
   * deployment configures the workspace endpoint once. Set false only to mount
   * that bridge as your own row — a second instance on the same `serverName`
   * fails at load.
   */
  mountBridge: boolean
  /**
   * Namespace the bridged AgentRQ tools are registered under: the model sees
   * `mcp__<serverName>__reply` and friends. The working-agreement section and
   * every framing derive their tool names from this, so the two can never drift.
   */
  serverName: string
  /**
   * Whether the workspace's pushes — new tasks, the periodic next-task
   * reminder, status checks, and the human's messages — are delivered into the
   * session as they arrive.
   */
  deliverPushes: boolean
  /**
   * Whether to dequeue one task at startup. The workspace re-pushes an
   * unclaimed task on its own schedule, so this only shortens the wait for
   * work that predates the connection.
   */
  catchUpOnStart: boolean
  /** Reconnection backoff for a dropped workspace session. */
  reconnect: ReconnectConfig
  /**
   * Whether to contribute the AgentRQ working-agreement system-prompt section.
   * Turn it off when a deployment states the same protocol in its own persona.
   */
  guidance: boolean
  /** Per-request timeout for AgentRQ tool calls, in milliseconds. */
  requestTimeoutMs: number
  /**
   * Provider route for each task's dedicated session. Empty means "copy
   * whatever an already-live agent in this process is using" — a task session
   * created with no provider/model at all fails every turn (the persona
   * assembly has no value for `{{model}}`), so this and `model` exist
   * specifically to avoid that when no other session happens to be live yet.
   */
  provider: string
  /** Model id for each task's dedicated session. Empty defers to `provider`'s fallback. */
  model: string
  /**
   * Working directory for each task's dedicated session. Empty uses the dsh
   * process's own cwd — the same directory an interactively opened session
   * gets, and what a capable UI groups sessions by.
   */
  cwd: string
}

export const Config = Schema.object({
  url: Schema.string().required().description('AgentRQ workspace MCP endpoint, including its ?token= credential.'),
  token: Schema.string().default('').description('Optional bearer token, when the URL carries no ?token= credential.'),
  mountBridge: Schema.boolean().default(true).description('Mount the MCP bridge that gives the model AgentRQ\'s tools.'),
  serverName: Schema.string().default('agentrq').description('Namespace for the bridged tools: mcp__<serverName>__reply, and so on.'),
  deliverPushes: Schema.boolean().default(true).description('Deliver the workspace\'s tasks and messages into the live session.'),
  catchUpOnStart: Schema.boolean().default(true).description('Dequeue one task at startup, for work that predates the connection.'),
  reconnect: Schema.object({
    initialDelayMs: Schema.number().min(100).default(1000).description('Delay before the first reconnect attempt.'),
    maxDelayMs: Schema.number().min(1000).default(900000).description('Ceiling for the reconnect backoff.'),
  }).default({ initialDelayMs: 1000, maxDelayMs: 900000 }),
  guidance: Schema.boolean().default(true).description('Contribute the AgentRQ working-agreement system-prompt section.'),
  requestTimeoutMs: Schema.number().min(1000).default(30000).description('Timeout for a single AgentRQ tool call.'),
  provider: Schema.string().default('').description('Provider route for each task\'s dedicated session. Empty copies an already-live agent\'s provider.'),
  model: Schema.string().default('').description('Model id for each task\'s dedicated session. Empty copies an already-live agent\'s model.'),
  cwd: Schema.string().default('').description('Working directory for each task\'s dedicated session. Empty uses the dsh process\'s own cwd.'),
})
