/**
 * The per-task AgentRQ session manager.
 *
 * AgentRQ decides *when* there is work — it pushes a task the moment one is
 * created for this agent, and re-pushes the next unclaimed task (or a status
 * check for the ongoing one) every 60 seconds from `WorkspaceServer.StartPoller`
 * — so this manager never asks. What it owns is *where* each push goes: every
 * task gets its own dedicated dsh session, opened the first time the task is
 * seen and closed once the task reaches a terminal status, so one task's
 * history never bleeds into another's.
 *
 * @module @agentrq/dsh-plugin-agentrq
 */

import type { Agent, AgentHandle } from '@deepseek-ai/dsh-agent'
import type { Context } from '@deepseek-ai/cordis'
import { createUserMessage } from '@deepseek-ai/dsh-llm'
import type { SessionId } from '@deepseek-ai/dsh-session'
import type { AgentRqClient, AgentRqTask, ChannelMessage } from './client.js'
import type { Config } from './config.js'
import { renderPushFraming, renderTaskFraming } from './prompt.js'

/** Source attribution carried by every message this plugin queues. */
const MESSAGE_SOURCE = { kind: 'plugin', plugin: 'agentrq' } as const

/**
 * How many `(task, content)` pairs to remember for repeat suppression.
 *
 * The workspace re-pushes an unclaimed task every 60 seconds with byte-identical
 * content, so without this a task's dedicated session would be woken once a
 * minute for work it has already been handed. Bounded because a long-lived
 * process sees many distinct tasks and this is a cache, not a ledger.
 */
const SEEN_LIMIT = 200

/** Task statuses that close a task's dedicated session once reached. */
const TERMINAL_STATUSES = new Set(['completed', 'rejected'])

/** What `agentrq_autopull` reports about the manager. */
export interface DeliveryStatus {
  /** Whether the workspace session is established right now. */
  readonly connected: boolean
  /** Whether pushes are configured to open dedicated task sessions. */
  readonly configured: boolean
  /** Whether pushes are opening dedicated task sessions (configured and not paused). */
  readonly active: boolean
  /** Task id most recently delivered, or null when none has been. */
  readonly lastDeliveredTaskId: string | null
}

/** Render an unknown thrown value for process-local diagnostics only. */
function renderThrown(value: unknown): string {
  return value instanceof Error ? value.message : String(value)
}

/**
 * Owns one workspace connection and, from it, every task's dedicated session.
 *
 * A task is unseen the first time a push for its `chatId` arrives; the
 * manager opens a fresh agent for it via `ctx.agents.create()`, seeded with
 * the framed push, and remembers the handle. Every later push for the same
 * task — the 60-second reminder, a status check, a human's reply — is routed
 * into that same session instead of whatever else happens to be live. The
 * session closes itself once the task's own status (read back through
 * `getTask`) reaches a terminal state, so a session's lifetime tracks its
 * task's lifetime rather than a human's terminal tab.
 */
export class TaskSessionManager {
  private readonly abort = new AbortController()
  private readonly seen = new Set<string>()
  private readonly sessions = new Map<string, AgentHandle>()
  private lastDeliveredTaskId: string | undefined
  private paused = false
  private stopping = false
  private spawnSeq = 0

  constructor(
    private readonly ctx: Context,
    private readonly client: AgentRqClient,
    private readonly config: Config,
  ) {}

  /** Open the workspace session and, optionally, claim any waiting task. */
  async start(): Promise<void> {
    await this.client.start()
    if (!this.config.catchUpOnStart || this.stopping) return
    try {
      const task = await this.client.fetchNextTask(this.abort.signal)
      if (task !== undefined) await this.deliverTask(task)
    } catch (error: unknown) {
      // The workspace re-pushes an unclaimed task on its own schedule, so a
      // failed catch-up costs latency, not work.
      this.warn('startup task check failed', error)
    }
  }

  /** Stop opening new sessions and close every task session still open. */
  async dispose(): Promise<void> {
    this.stopping = true
    this.abort.abort()
    const handles = [...this.sessions.values()]
    this.sessions.clear()
    await this.client.dispose()
    await Promise.allSettled(handles.map(handle => handle.dispose()))
  }

  /** Current manager state, for the management tool. */
  status(): DeliveryStatus {
    return {
      connected: this.client.connected,
      configured: this.config.deliverPushes,
      active: this.config.deliverPushes && !this.paused && !this.stopping,
      lastDeliveredTaskId: this.lastDeliveredTaskId ?? null,
    }
  }

  /** Stop opening or routing into task sessions; already-open ones stay live. */
  pause(): DeliveryStatus {
    this.paused = true
    return this.status()
  }

  /** Resume opening and routing into task sessions. */
  resume(): DeliveryStatus {
    this.paused = false
    return this.status()
  }

  /**
   * Dequeue the next task for an explicit request.
   *
   * The caller is a tool body, so the task travels back as the tool's own
   * result rather than through a dedicated session — the calling agent asked
   * by hand and reads the answer in its own conversation.
   *
   * @param signal - tool-call cancellation.
   * @returns the task, or undefined when the queue is empty.
   */
  async pullNow(signal: AbortSignal): Promise<AgentRqTask | undefined> {
    const task = await this.client.fetchNextTask(signal)
    if (task === undefined) return undefined
    this.remember(task.id, task.text)
    this.lastDeliveredTaskId = task.id
    return task
  }

  /**
   * Route one workspace push to its task's dedicated session.
   *
   * A new task, the periodic reminder, a status check, and a human's reply all
   * arrive on the same channel, and the manager forwards each as written — the
   * content is the message, and deciding what kind it is would only add a way
   * to be wrong.
   *
   * @param message - the push AgentRQ delivered.
   */
  async deliverPush(message: ChannelMessage): Promise<void> {
    if (!this.deliverable()) return
    // The workspace repeats an unclaimed task verbatim every minute.
    if (this.remember(message.chatId, message.text)) return
    this.lastDeliveredTaskId = message.chatId
    await this.routeToSession(message.chatId, renderPushFraming(message, this.config.serverName))
  }

  /** Route one task fetched by the manager itself, framed as a task hand-off. */
  private async deliverTask(task: AgentRqTask): Promise<void> {
    if (!this.deliverable()) return
    if (this.remember(task.id, task.text)) return
    this.lastDeliveredTaskId = task.id
    await this.routeToSession(task.id, renderTaskFraming(task, this.config.serverName))
  }

  /** Deliver framed text to a task's session, opening one if none is open yet. */
  private async routeToSession(taskId: string, framed: string): Promise<void> {
    const existing = this.sessions.get(taskId)
    if (existing !== undefined) {
      this.queue(existing.agent, framed)
      return
    }
    const handle = await this.openSession(taskId)
    if (handle === undefined) return
    this.sessions.set(taskId, handle)
    this.watch(taskId, handle)
    this.queue(handle.agent, framed)
  }

  /** Open a fresh dedicated agent/session for one task. */
  private async openSession(taskId: string): Promise<AgentHandle | undefined> {
    this.spawnSeq += 1
    try {
      return await this.ctx.agents.create({
        // `SessionId` brands a string at the type level only (no runtime
        // behavior), so a local cast avoids depending on `dsh-session` just
        // for its identity function.
        sessionId: `agentrq-task-${taskId}-${this.spawnSeq}` as SessionId,
        signal: this.abort.signal,
      })
    } catch (error: unknown) {
      this.warn(`could not open a dedicated session for task "${taskId}"`, error)
      return undefined
    }
  }

  /**
   * Close a task's session once its task reaches a terminal status.
   *
   * The session goes idle both between an in-progress task's steps and while
   * genuinely done, and only `getTask` tells the two apart — an idle agent
   * that asked the human a question and is waiting for a reply must keep its
   * session so that reply lands in the same conversation, not a new one.
   */
  private watch(taskId: string, handle: AgentHandle): void {
    handle.agent.ctx.on('agent/status', async ({ status }) => {
      if (status !== 'idle' || this.stopping) return
      if (this.sessions.get(taskId) !== handle) return
      if (!(await this.isTaskTerminal(taskId))) return
      if (this.sessions.get(taskId) !== handle) return
      this.sessions.delete(taskId)
      await handle.dispose().catch((error: unknown) => {
        this.warn(`could not close the session for task "${taskId}"`, error)
      })
    })
    handle.agent.ctx.on('agent/disposed', () => {
      if (this.sessions.get(taskId) === handle) this.sessions.delete(taskId)
    })
  }

  /** Whether the task's current status (read back from the workspace) is terminal. */
  private async isTaskTerminal(taskId: string): Promise<boolean> {
    try {
      const task = await this.client.fetchTask(taskId, this.abort.signal)
      return task !== undefined && TERMINAL_STATUSES.has(task.status.toLowerCase())
    } catch (error: unknown) {
      this.warn(`could not read status for task "${taskId}"`, error)
      return false
    }
  }

  /** Hand framed text to one task's agent on the route its current state allows. */
  private queue(agent: Agent, text: string): void {
    const framed = createUserMessage({
      content: [{ type: 'text', text }],
      source: MESSAGE_SOURCE,
    })
    try {
      this.ctx.agents.withoutInitiator(() => {
        if (agent.status === 'running') agent.inject(framed)
        else agent.followup(framed)
      })
    } catch (error: unknown) {
      this.warn('could not deliver a workspace push', error)
    }
  }

  /**
   * Record one `(task, content)` pair.
   *
   * @returns whether this exact content was already delivered for this task.
   */
  private remember(chatId: string, text: string): boolean {
    const key = `${chatId} ${text}`
    if (this.seen.has(key)) return true
    this.seen.add(key)
    if (this.seen.size > SEEN_LIMIT) {
      // Insertion-ordered, so the first key is the oldest.
      const oldest = this.seen.values().next()
      if (!oldest.done) this.seen.delete(oldest.value)
    }
    return false
  }

  /** Whether a push may open or reach a task session right now. */
  private deliverable(): boolean {
    return !this.stopping && !this.paused && this.config.deliverPushes
  }

  private warn(what: string, error: unknown): void {
    if (this.stopping) return
    this.ctx.logger.warn(`agentrq: ${what}: ${renderThrown(error)}`)
  }
}
