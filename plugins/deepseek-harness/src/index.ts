/**
 * AgentRQ task manager for DeepSeek Harness.
 *
 * One row, one endpoint. The plugin mounts `@deepseek-ai/dsh-mcp-client` as a
 * child so the workspace URL is configured once, and owns the parts a
 * model-facing bridge cannot do on its own — the AgentRQ working agreement as
 * a system-prompt section, and a supervised workspace session that delivers
 * AgentRQ's pushes (new tasks, the periodic next-task reminder, and the
 * human's messages) into the live agent.
 *
 * Lifecycle is effect-scoped: disposal stops every poller, closes every
 * workspace session, and unregisters the section and tools. HMR hot-swaps by
 * disposing the old instance and applying a new one.
 *
 * @module @agentrq/dsh-plugin-agentrq
 */

import type { Context } from '@deepseek-ai/cordis'
import type { Agent } from '@deepseek-ai/dsh-agent'
// Side-effect type imports: these declaration-merge `tools` and `systemPrompt`
// onto `Context`, and `agent` onto the agent registry surface.
import type {} from '@deepseek-ai/dsh-tools'
import type {} from '@deepseek-ai/dsh-system-prompt'
import * as mcpClient from '@deepseek-ai/dsh-mcp-client'
import { AgentRqClient } from './client.js'
import type { Config } from './config.js'
import { GUIDANCE_SECTION_NAME, GUIDANCE_SECTION_ORDER, renderGuidanceSection } from './prompt.js'
import { AgentRqRuntime } from './runtime.js'
import { registerAutoPullTool } from './tools.js'

export type { AgentRqTask, ChannelMessage, ReconnectOptions } from './client.js'
export type { DeliveryScope, ReconnectConfig } from './config.js'
export type { DeliveryStatus } from './runtime.js'
export { AgentRqClient, parseChannelNotification, parseTaskReply } from './client.js'
export { renderGuidanceSection, renderPushFraming, renderTaskFraming, toolName } from './prompt.js'
// Cordis reads the exported schema to validate `config` and fill defaults; the
// re-export carries both the schema value and the `Config` type.
export { Config } from './config.js'

/** Cordis function-plugin name used by loader diagnostics. */
export const name = 'agentrq'

/** Services required before this plugin loads. */
export const inject = ['agents', 'tools', 'systemPrompt']

/** Teardown for one agent's AgentRQ attachment. */
type AgentCleanup = () => void | Promise<void>

/**
 * Attach AgentRQ to root agents published after this plugin loads.
 *
 * @param ctx - the plugin's context.
 * @param config - validated plugin configuration.
 */
export function apply(ctx: Context, config: Config): void {
  // The bridge is a child fiber rather than a sibling row, so the workspace
  // endpoint is configured once and the two halves share one lifetime: our
  // disposal and HMR reload take the bridge with them.
  if (config.mountBridge) {
    ctx.plugin(mcpClient, {
      serverName: config.serverName,
      transport: 'streamable-http',
      url: config.url,
      // One timeout for every AgentRQ call, whether the model makes it through
      // the bridge or the plugin makes it on its own session.
      toolCallTimeoutMs: config.requestTimeoutMs,
      // The bridge activating with no tools is recoverable — it re-syncs on
      // reconnect — and failing activation would take the delivery half down
      // with it for a workspace that is merely slow to come up.
      failOnStartupError: false,
      // Empty unless a deployment prefers a bearer header; the endpoint's own
      // `?token=` credential is the usual path.
      headers: config.token === '' ? {} : { Authorization: `Bearer ${config.token}` },
    })
  }

  if (config.guidance) {
    ctx.systemPrompt.section({
      name: GUIDANCE_SECTION_NAME,
      order: GUIDANCE_SECTION_ORDER,
      text: renderGuidanceSection(config.serverName),
    })
  }

  const attachments = new Map<Agent, AgentCleanup>()
  let stopping = false

  ctx.effect(() => {
    const stopCreated = ctx.on('agent/created', ({ agent }) => {
      if (stopping || attachments.has(agent)) return
      if (!ctx.agents.roots().includes(agent)) return
      // AgentRQ broadcasts each push to every connected session, and one
      // workspace queue serves one worker. Under the default scope the first
      // live root agent holds the session, and a later one only inherits it
      // after that agent is gone.
      if (config.scope === 'single-agent' && attachments.size > 0) return

      let runtime: AgentRqRuntime | undefined
      const client = new AgentRqClient({
        url: config.url,
        token: config.token,
        requestTimeoutMs: config.requestTimeoutMs,
        reconnect: config.reconnect,
        onChannelMessage: message => { runtime?.deliverPush(message) },
        onConnectionError: error => {
          ctx.logger.warn(`agentrq: workspace session for agent "${agent.id}": ${
            error instanceof Error ? error.message : String(error)}`)
        },
      })
      runtime = new AgentRqRuntime(ctx, agent, client, config)
      const owned = runtime

      const cleanup: AgentCleanup = agent.ctx.effect(() => {
        const disposeTool = registerAutoPullTool(agent.ctx, owned)
        // Connecting and the startup catch-up are async; the effect's disposer
        // is registered synchronously, so teardown always finds this runtime.
        void owned.start().catch(() => {
          // `start` reports its own failures and the client keeps retrying.
        })
        return async () => {
          disposeTool()
          try {
            await owned.dispose()
          } finally {
            if (attachments.get(agent) === cleanup) attachments.delete(agent)
          }
        }
      }, 'agentrq.runtime()')

      attachments.set(agent, cleanup)
    })

    return async () => {
      stopping = true
      stopCreated()
      const cleanups = [...attachments.values()]
      attachments.clear()
      await Promise.allSettled(cleanups.map(cleanup => Promise.resolve(cleanup())))
    }
  }, 'agentrq.lifecycle()')
}
