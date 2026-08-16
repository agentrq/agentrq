/**
 * AgentRQ task manager for DeepSeek Harness.
 *
 * One row, one endpoint. The plugin mounts `@deepseek-ai/dsh-mcp-client` as a
 * child so the workspace URL is configured once, and owns the parts a
 * model-facing bridge cannot do on its own — the AgentRQ working agreement as
 * a system-prompt section, and a supervised workspace session that opens a
 * fresh, dedicated dsh session for each task AgentRQ pushes (a new task, the
 * periodic next-task reminder, or a human's message), so one task's history
 * never rides along in another's conversation.
 *
 * Lifecycle is effect-scoped: disposal closes the workspace session and every
 * still-open task session, and unregisters the section and tools. HMR
 * hot-swaps by disposing the old instance and applying a new one.
 *
 * @module @agentrq/dsh-plugin-agentrq
 */

import type { Context } from '@deepseek-ai/cordis'
// Side-effect type imports: these declaration-merge `tools` and `systemPrompt`
// onto `Context`, and `agent` onto the agent registry surface.
import type {} from '@deepseek-ai/dsh-tools'
import type {} from '@deepseek-ai/dsh-system-prompt'
import * as mcpClient from '@deepseek-ai/dsh-mcp-client'
import { AgentRqClient } from './client.js'
import type { Config } from './config.js'
import { GUIDANCE_SECTION_NAME, GUIDANCE_SECTION_ORDER, renderGuidanceSection } from './prompt.js'
import { TaskSessionManager } from './sessions.js'
import { registerAutoPullTool } from './tools.js'

export type { AgentRqTask, ChannelMessage, ReconnectOptions } from './client.js'
export type { ReconnectConfig } from './config.js'
export type { DeliveryStatus } from './sessions.js'
export { AgentRqClient, parseChannelNotification, parseTaskReply } from './client.js'
export { renderGuidanceSection, renderPushFraming, renderTaskFraming, toolName } from './prompt.js'
// Cordis reads the exported schema to validate `config` and fill defaults; the
// re-export carries both the schema value and the `Config` type.
export { Config } from './config.js'

/** Cordis function-plugin name used by loader diagnostics. */
export const name = 'agentrq'

/** Services required before this plugin loads. */
export const inject = ['agents', 'tools', 'systemPrompt']

/**
 * Wire up the workspace connection and the tools that reach it.
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

  ctx.effect(() => {
    let manager: TaskSessionManager | undefined
    const client = new AgentRqClient({
      url: config.url,
      token: config.token,
      requestTimeoutMs: config.requestTimeoutMs,
      reconnect: config.reconnect,
      onChannelMessage: message => { void manager?.deliverPush(message) },
      onConnectionError: error => {
        ctx.logger.warn(`agentrq: workspace session: ${
          error instanceof Error ? error.message : String(error)}`)
      },
    })
    manager = new TaskSessionManager(ctx, client, config)
    const owned = manager

    const disposeTool = registerAutoPullTool(ctx, owned)
    // Connecting and the startup catch-up are async; the effect's disposer is
    // registered synchronously, so teardown always finds this manager.
    void owned.start().catch(() => {
      // `start` reports its own failures and the client keeps retrying.
    })

    return async () => {
      disposeTool()
      await owned.dispose()
    }
  }, 'agentrq.lifecycle()')
}
