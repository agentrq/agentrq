/**
 * AgentRQ workspace client.
 *
 * The harness already bridges AgentRQ's tools to the model through
 * `@deepseek-ai/dsh-mcp-client`; this is the plugin's *own* connection, and its
 * job is to stay connected. AgentRQ pushes work over
 * `notifications/claude/channel` — a task created for this agent
 * (`handler/api/task.go`) and, every 60 seconds, the next unclaimed task or a
 * status check for the ongoing one (`WorkspaceServer.StartPoller`). Nothing
 * arrives while the session is down, so reconnection is the load-bearing part,
 * not request scheduling.
 *
 * Modelled on `acp-gateway/src/mcpClient.ts`, which consumes the same channel.
 *
 * @module @agentrq/dsh-plugin-agentrq
 */

import { Client } from '@modelcontextprotocol/sdk/client/index.js'
import { StreamableHTTPClientTransport } from '@modelcontextprotocol/sdk/client/streamableHttp.js'
import type { Transport } from '@modelcontextprotocol/sdk/shared/transport.js'

/** The MCP notification AgentRQ pushes for tasks and human messages alike. */
export const CHANNEL_NOTIFICATION_METHOD = 'notifications/claude/channel'

/** Server reply when the queue holds nothing for this agent. */
const EMPTY_QUEUE_REPLY = 'no pending tasks exist'

/** One task dequeued from the workspace queue by an explicit `getTask`. */
export interface AgentRqTask {
  /** Base62 task id, as AgentRQ reports it. */
  readonly id: string
  /** Task title, empty when the server omitted the line. */
  readonly title: string
  /** Task status at fetch time, empty when the server omitted the line. */
  readonly status: string
  /**
   * The server's own rendering of the task, verbatim. The plugin hands this to
   * the model rather than a reassembled copy, so nothing is lost in parsing.
   */
  readonly text: string
}

/**
 * One push from the workspace.
 *
 * The channel carries new task assignments, the periodic "next assigned task"
 * reminder, status-check prompts, and messages a human typed into a thread.
 * The plugin does not try to tell them apart: like the gateway, it forwards the
 * content as written and lets the model read it.
 */
export interface ChannelMessage {
  /** Task id the push belongs to; also the `chat_id` the `reply` tool wants. */
  readonly chatId: string
  /** Content as the workspace wrote it. */
  readonly text: string
  /** Sender label supplied by AgentRQ. */
  readonly user: string
}

/** Reconnection behavior for the workspace session. */
export interface ReconnectOptions {
  /** Delay before the first retry, in milliseconds. */
  readonly initialDelayMs: number
  /** Ceiling for the exponential backoff, in milliseconds. */
  readonly maxDelayMs: number
}

/** Options for constructing an {@link AgentRqClient}. */
export interface AgentRqClientOptions {
  /** Workspace MCP endpoint, including any `?token=` credential. */
  readonly url: string
  /** Bearer token, or empty when the URL carries its own credential. */
  readonly token: string
  /** Timeout for a single tool call, in milliseconds. */
  readonly requestTimeoutMs: number
  /** Reconnection backoff for a dropped session. */
  readonly reconnect: ReconnectOptions
  /** Called for every push the workspace delivers. */
  readonly onChannelMessage: (message: ChannelMessage) => void
  /** Called when a connection attempt fails, for process-local diagnostics. */
  readonly onConnectionError: (error: unknown) => void
}

/** Read the text blocks out of an MCP tool result. */
function joinTextContent(result: unknown): string {
  if (typeof result !== 'object' || result === null) return ''
  const content = (result as { content?: unknown }).content
  if (!Array.isArray(content)) return ''
  return content
    .filter((block): block is { type: 'text'; text: string } =>
      typeof block === 'object' && block !== null
      && (block as { type?: unknown }).type === 'text'
      && typeof (block as { text?: unknown }).text === 'string')
    .map(block => block.text)
    .join('\n')
}

/** Pull one `Key: value` header line out of the server's task rendering. */
function readField(text: string, field: string): string {
  const match = new RegExp(`^${field}: (.*)$`, 'm').exec(text)
  return match?.[1]?.trim() ?? ''
}

/**
 * Interpret a `getTask` reply.
 *
 * @param text - joined text content of the tool result.
 * @returns the task, or undefined when the queue is empty or unparseable.
 */
export function parseTaskReply(text: string): AgentRqTask | undefined {
  const trimmed = text.trim()
  if (trimmed === '' || trimmed === EMPTY_QUEUE_REPLY) return undefined
  const id = readField(trimmed, 'ID')
  if (id === '') return undefined
  return { id, title: readField(trimmed, 'Title'), status: readField(trimmed, 'Status'), text: trimmed }
}

/**
 * Interpret a `notifications/claude/channel` payload.
 *
 * `SendChannelNotification` puts the task id in `meta.chat_id` for every push,
 * so the id never has to be recovered from the content.
 */
export function parseChannelNotification(params: unknown): ChannelMessage | undefined {
  if (typeof params !== 'object' || params === null) return undefined
  const { content, meta } = params as { content?: unknown; meta?: unknown }
  if (typeof content !== 'string' || content.trim() === '') return undefined
  const chatId = typeof meta === 'object' && meta !== null
    ? (meta as { chat_id?: unknown }).chat_id
    : undefined
  if (typeof chatId !== 'string' || chatId === '') return undefined
  const user = typeof meta === 'object' && meta !== null
    ? (meta as { user?: unknown }).user
    : undefined
  return { chatId, text: content, user: typeof user === 'string' ? user : 'human' }
}

/**
 * One supervised AgentRQ workspace session.
 *
 * `start()` opens it and keeps it open: a closed transport or an unrecoverable
 * transport error schedules a reconnect with exponential backoff, because a
 * session that stays down silently stops delivering work.
 */
export class AgentRqClient {
  private client: Client | undefined
  private transport: StreamableHTTPClientTransport | undefined
  private opening: Promise<void> | undefined
  private retryTimer: ReturnType<typeof setTimeout> | undefined
  private attempt = 0
  private closed = false

  constructor(private readonly options: AgentRqClientOptions) {}

  /** Whether a session is currently established. */
  get connected(): boolean {
    return this.client !== undefined
  }

  /**
   * Open the session, and keep reopening it for as long as the client lives.
   *
   * @returns once the first attempt settles; a failure is reported through
   * `onConnectionError` and retried, not thrown.
   */
  async start(): Promise<void> {
    await this.ensureConnected().catch(() => {
      // `ensureConnected` already reported and scheduled the retry.
    })
  }

  /**
   * Open the session if it is not already open.
   *
   * @throws when this attempt fails; a retry is scheduled either way.
   */
  async ensureConnected(): Promise<void> {
    if (this.closed) throw new Error('agentrq client disposed')
    if (this.client !== undefined) return
    await (this.opening ??= this.open().finally(() => { this.opening = undefined }))
  }

  /** Dequeue the next task assigned to this agent, if any. */
  async fetchNextTask(signal: AbortSignal): Promise<AgentRqTask | undefined> {
    return parseTaskReply(await this.callTool('getTask', {}, signal))
  }

  /**
   * Call one AgentRQ tool and return its joined text content.
   *
   * @param name - raw AgentRQ tool name.
   * @param args - JSON arguments for the tool.
   * @param signal - caller cancellation.
   * @returns the joined text blocks of the result.
   * @throws when the connection or the call fails.
   */
  async callTool(name: string, args: Record<string, unknown>, signal: AbortSignal): Promise<string> {
    await this.ensureConnected()
    const client = this.client
    if (client === undefined) throw new Error('agentrq session is not connected')
    const result = await client.callTool(
      { name, arguments: args },
      undefined,
      { signal, timeout: this.options.requestTimeoutMs },
    )
    if ((result as { isError?: unknown }).isError === true) {
      throw new Error(joinTextContent(result) || `agentrq tool "${name}" failed`)
    }
    return joinTextContent(result)
  }

  /** Close the session and stop reconnecting. */
  async dispose(): Promise<void> {
    this.closed = true
    if (this.retryTimer !== undefined) {
      clearTimeout(this.retryTimer)
      this.retryTimer = undefined
    }
    await this.teardown()
  }

  private async open(): Promise<void> {
    await this.teardown()
    if (this.closed) throw new Error('agentrq client disposed')

    const transport = this.createTransport()
    const client = new Client({ name: 'dsh-plugin-agentrq', version: '0.2.0' })
    client.fallbackNotificationHandler = async notification => {
      if (notification.method !== CHANNEL_NOTIFICATION_METHOD) return
      const message = parseChannelNotification(notification.params)
      if (message !== undefined) this.options.onChannelMessage(message)
    }

    // A dropped stream is the failure that matters: no session, no pushes.
    transport.onclose = () => { this.handleLost(new Error('workspace session closed')) }
    transport.onerror = (error: Error) => {
      // The SDK retries a recoverable SSE gap itself; these two mean the
      // session is gone and only a fresh connection recovers it.
      const detail = error.message
      if (detail.includes('Failed to reconnect SSE stream') || detail.includes('Not Found')) {
        this.handleLost(error)
      }
    }

    try {
      await client.connect(transport as Transport)
    } catch (error: unknown) {
      this.options.onConnectionError(error)
      this.scheduleRetry()
      throw error
    }

    if (this.closed) {
      await client.close().catch(() => {})
      throw new Error('agentrq client disposed')
    }
    this.client = client
    this.transport = transport
    this.attempt = 0
  }

  /** Drop the current session and schedule a fresh one. */
  private handleLost(error: unknown): void {
    if (this.closed || this.client === undefined) return
    this.options.onConnectionError(error)
    void this.teardown().finally(() => { this.scheduleRetry() })
  }

  private scheduleRetry(): void {
    if (this.closed || this.retryTimer !== undefined) return
    const delay = Math.min(
      this.options.reconnect.initialDelayMs * 2 ** this.attempt,
      this.options.reconnect.maxDelayMs,
    )
    this.attempt += 1
    this.retryTimer = setTimeout(() => {
      this.retryTimer = undefined
      void this.ensureConnected().catch(() => {
        // Reported and rescheduled inside `open`.
      })
    }, delay)
    // A reconnect timer must never be the only thing keeping the process alive.
    this.retryTimer.unref?.()
  }

  private createTransport(): StreamableHTTPClientTransport {
    const headers = this.options.token === ''
      ? undefined
      : { Authorization: `Bearer ${this.options.token}` }
    return new StreamableHTTPClientTransport(new URL(this.options.url), {
      // Transport-level SSE resumption; the supervisor above handles the cases
      // it gives up on.
      reconnectionOptions: {
        maxRetries: 100,
        initialReconnectionDelay: this.options.reconnect.initialDelayMs,
        maxReconnectionDelay: this.options.reconnect.maxDelayMs,
        reconnectionDelayGrowFactor: 2,
      },
      ...(headers === undefined ? {} : { requestInit: { headers } }),
    })
  }

  private async teardown(): Promise<void> {
    const transport = this.transport
    const client = this.client
    this.transport = undefined
    this.client = undefined
    if (transport !== undefined) {
      // Detach before closing: the close we are about to perform must not look
      // like a lost session and start a reconnect.
      transport.onclose = () => {}
      transport.onerror = () => {}
    }
    if (client !== undefined) {
      try {
        await client.close()
      } catch {
        // Closing an already-broken session has nothing left to fix.
      }
    }
  }
}
