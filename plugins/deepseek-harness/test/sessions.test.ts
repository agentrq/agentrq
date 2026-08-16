import type { Context } from '@deepseek-ai/cordis'
import { describe, expect, it } from 'vitest'
import type { AgentRqClient, AgentRqTask } from '../src/client.js'
import type { Config } from '../src/config.js'
import { TaskSessionManager } from '../src/sessions.js'

const CONFIG: Config = {
  url: 'https://workspace.mcp.example/mcp?token=t',
  token: '',
  mountBridge: false,
  serverName: 'agentrq',
  deliverPushes: true,
  catchUpOnStart: true,
  reconnect: { initialDelayMs: 1000, maxDelayMs: 900000 },
  guidance: true,
  requestTimeoutMs: 30000,
}

function task(id: string, status = 'notstarted'): AgentRqTask {
  return { id, title: `title ${id}`, status, text: `Next assigned task:\nID: ${id}` }
}

/** Drain the microtask queue, for a fire-and-forget event listener's async chain. */
function flush(): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, 0))
}

/** Text of every message queued on one spawned session's agent, in order. */
type Delivery = { route: 'followup' | 'inject'; text: string }

/** One agent/session `ctx.agents.create()` spawned, with its own tiny event bus. */
interface SpawnedSession {
  readonly id: string
  readonly deliveries: Delivery[]
  status: 'idle' | 'running'
  disposed: boolean
  emitStatus(status: 'idle' | 'running'): void
}

function harness() {
  const warnings: string[] = []
  const spawned: SpawnedSession[] = []
  let spawnCount = 0

  const agentsService = {
    create: async (): Promise<{ agent: unknown; dispose: () => Promise<void> }> => {
      spawnCount += 1
      const id = `spawned-${spawnCount}`
      const deliveries: Delivery[] = []
      const statusListeners: Array<(payload: { status: string }) => void> = []
      const disposedListeners: Array<() => void> = []
      const record = (route: Delivery['route']) => (message: { content: readonly { type: string; text?: string }[] }) => {
        deliveries.push({ route, text: message.content.map(block => block.text ?? '').join('') })
      }

      const session: SpawnedSession = {
        id,
        deliveries,
        status: 'idle',
        disposed: false,
        emitStatus(status) {
          session.status = status
          for (const listener of statusListeners) listener({ status })
        },
      }

      const agent = {
        id,
        get status() { return session.status },
        ctx: {
          on: (event: string, listener: (payload: unknown) => void) => {
            if (event === 'agent/status') statusListeners.push(listener as (payload: { status: string }) => void)
            if (event === 'agent/disposed') disposedListeners.push(listener as () => void)
            return () => {}
          },
        },
        followup: record('followup'),
        inject: record('inject'),
      }

      spawned.push(session)
      return {
        agent,
        dispose: async (): Promise<void> => {
          session.disposed = true
          for (const listener of disposedListeners) listener()
        },
      }
    },
    withoutInitiator: <T>(operation: () => T): T => operation(),
  }

  const ctx = {
    logger: { warn: (message: string) => { warnings.push(message) } },
    agents: agentsService,
  }

  const queue: (AgentRqTask | undefined)[] = []
  const failures: (Error | undefined)[] = []
  const statuses = new Map<string, string>()
  let starts = 0
  let connected = true
  const client = {
    get connected() { return connected },
    start: async (): Promise<void> => { starts += 1 },
    dispose: async (): Promise<void> => { connected = false },
    fetchNextTask: async (): Promise<AgentRqTask | undefined> => {
      const failure = failures.shift()
      if (failure !== undefined) throw failure
      return queue.shift()
    },
    fetchTask: async (id: string): Promise<AgentRqTask | undefined> => {
      const status = statuses.get(id)
      return status === undefined ? undefined : task(id, status)
    },
  }

  return {
    warnings,
    spawned,
    queue,
    failures,
    statuses,
    starts: () => starts,
    setConnected: (value: boolean) => { connected = value },
    manager: (config: Config = CONFIG) => new TaskSessionManager(
      ctx as unknown as Context,
      client as unknown as AgentRqClient,
      config,
    ),
  }
}

describe('TaskSessionManager', () => {
  it('opens the workspace session on start', async () => {
    const h = harness()
    const manager = h.manager()

    await manager.start()

    expect(h.starts()).toBe(1)
    await manager.dispose()
  })

  it('claims a waiting task at startup by opening its own dedicated session', async () => {
    const h = harness()
    h.queue.push(task('t1'))
    const manager = h.manager()

    await manager.start()

    expect(h.spawned).toHaveLength(1)
    expect(h.spawned[0]?.deliveries).toHaveLength(1)
    expect(h.spawned[0]?.deliveries[0]?.route).toBe('followup')
    expect(h.spawned[0]?.deliveries[0]?.text).toContain('task_id: t1')
    expect(manager.status().lastDeliveredTaskId).toBe('t1')

    await manager.dispose()
  })

  it('skips the startup check when catch-up is off', async () => {
    const h = harness()
    h.queue.push(task('t1'))
    const manager = h.manager({ ...CONFIG, catchUpOnStart: false })

    await manager.start()

    expect(h.starts()).toBe(1)
    expect(h.spawned).toHaveLength(0)

    await manager.dispose()
  })

  it('contains a failed startup check, since the workspace re-pushes anyway', async () => {
    const h = harness()
    h.failures.push(new Error('workspace unreachable'))
    const manager = h.manager()

    await manager.start()

    expect(h.spawned).toHaveLength(0)
    expect(h.warnings.join('\n')).toContain('workspace unreachable')

    await manager.dispose()
  })

  it('opens one session for a new task and routes a later push for it into that same session', async () => {
    const h = harness()
    const manager = h.manager()

    await manager.deliverPush({ chatId: 'c1', text: 'ping while idle', user: 'human' })
    expect(h.spawned).toHaveLength(1)
    h.spawned[0]!.status = 'running'
    await manager.deliverPush({ chatId: 'c1', text: 'ping while running', user: 'human' })

    // Still one session: the second push found the first task's session live.
    expect(h.spawned).toHaveLength(1)
    expect(h.spawned[0]?.deliveries.map(delivery => delivery.route)).toEqual(['followup', 'inject'])
    expect(h.spawned[0]?.deliveries[1]?.text).toContain('chat_id: c1')

    await manager.dispose()
  })

  it('forwards a pushed task without classifying it', async () => {
    const h = harness()
    const manager = h.manager()

    // Exactly what WorkspaceServer.StartPoller pushes for a pending task.
    await manager.deliverPush({
      chatId: '0h8b1P7TX5V',
      text: 'Next assigned task:\nTitle: Ship the bundle\nDetails: Open a PR.',
      user: 'human',
    })

    expect(h.spawned).toHaveLength(1)
    expect(h.spawned[0]?.deliveries[0]?.text).toContain('Next assigned task:')
    expect(manager.status().lastDeliveredTaskId).toBe('0h8b1P7TX5V')

    await manager.dispose()
  })

  it('drops the workspace re-push of an unclaimed task without opening a second session', async () => {
    const h = harness()
    const manager = h.manager()
    const push = { chatId: 't1', text: 'Next assigned task:\nTitle: Ship it', user: 'human' }

    // The server repeats this every 60s until the agent claims the task.
    await manager.deliverPush(push)
    await manager.deliverPush({ ...push })
    await manager.deliverPush({ ...push })

    expect(h.spawned).toHaveLength(1)
    expect(h.spawned[0]?.deliveries).toHaveLength(1)

    await manager.dispose()
  })

  it('delivers a genuinely new message on a task into its existing session', async () => {
    const h = harness()
    const manager = h.manager()

    await manager.deliverPush({ chatId: 't1', text: 'Next assigned task:\nTitle: Ship it', user: 'human' })
    await manager.deliverPush({ chatId: 't1', text: 'Rebase onto main first.', user: 'human' })

    expect(h.spawned).toHaveLength(1)
    expect(h.spawned[0]?.deliveries).toHaveLength(2)
    expect(h.spawned[0]?.deliveries[1]?.text).toContain('Rebase onto main first.')

    await manager.dispose()
  })

  it('opens a separate session per task even with identical push content', async () => {
    const h = harness()
    const manager = h.manager()

    await manager.deliverPush({ chatId: 't1', text: 'ping', user: 'human' })
    await manager.deliverPush({ chatId: 't2', text: 'ping', user: 'human' })

    expect(h.spawned).toHaveLength(2)
    expect(h.spawned[0]?.deliveries).toHaveLength(1)
    expect(h.spawned[1]?.deliveries).toHaveLength(1)

    await manager.dispose()
  })

  it('closes a task session once the task reaches a terminal status', async () => {
    const h = harness()
    const manager = h.manager()

    await manager.deliverPush({ chatId: 't1', text: 'Next assigned task:\nTitle: Ship it', user: 'human' })
    expect(h.spawned).toHaveLength(1)
    const session = h.spawned[0]!

    h.statuses.set('t1', 'completed')
    session.emitStatus('idle')
    await flush()

    expect(session.disposed).toBe(true)

    // A later push for the same task id now opens a fresh session rather than
    // reusing the one that already closed.
    await manager.deliverPush({ chatId: 't1', text: 'a distinct later message', user: 'human' })
    expect(h.spawned).toHaveLength(2)

    await manager.dispose()
  })

  it('keeps a task session open when idle but not yet terminal, so a human reply lands in it', async () => {
    const h = harness()
    const manager = h.manager()

    await manager.deliverPush({ chatId: 't1', text: 'Next assigned task:\nTitle: Ship it', user: 'human' })
    const session = h.spawned[0]!

    // The agent asked a clarifying question and went idle waiting on a reply.
    h.statuses.set('t1', 'ongoing')
    session.emitStatus('idle')
    await flush()

    expect(session.disposed).toBe(false)

    await manager.deliverPush({ chatId: 't1', text: 'here is the answer', user: 'human' })
    expect(h.spawned).toHaveLength(1)
    expect(session.deliveries).toHaveLength(2)

    await manager.dispose()
  })

  it('stops delivering while paused and resumes on request', async () => {
    const h = harness()
    const manager = h.manager()

    expect(manager.pause().active).toBe(false)
    await manager.deliverPush({ chatId: 't1', text: 'while paused', user: 'human' })
    expect(h.spawned).toHaveLength(0)

    expect(manager.resume().active).toBe(true)
    await manager.deliverPush({ chatId: 't1', text: 'after resume', user: 'human' })
    expect(h.spawned).toHaveLength(1)

    await manager.dispose()
  })

  it('never delivers when delivery is disabled in configuration', async () => {
    const h = harness()
    const manager = h.manager({ ...CONFIG, deliverPushes: false })

    await manager.deliverPush({ chatId: 't1', text: 'ignored', user: 'human' })

    expect(h.spawned).toHaveLength(0)
    expect(manager.status().active).toBe(false)

    await manager.dispose()
  })

  it('reports the workspace connection state', async () => {
    const h = harness()
    const manager = h.manager()
    expect(manager.status().connected).toBe(true)

    h.setConnected(false)
    expect(manager.status().connected).toBe(false)

    await manager.dispose()
  })

  it('returns the dequeued task to an explicit pull instead of opening a session', async () => {
    const h = harness()
    h.queue.push(task('t1'))
    const manager = h.manager({ ...CONFIG, catchUpOnStart: false })

    const pulled = await manager.pullNow(new AbortController().signal)

    expect(pulled?.id).toBe('t1')
    expect(h.spawned).toHaveLength(0)
    expect(manager.status().lastDeliveredTaskId).toBe('t1')

    await manager.dispose()
  })

  it('does not re-deliver a task the model already pulled by hand', async () => {
    const h = harness()
    h.queue.push(task('t1'))
    const manager = h.manager({ ...CONFIG, catchUpOnStart: false })

    const pulled = await manager.pullNow(new AbortController().signal)
    // The workspace keeps pushing it until the model claims it.
    await manager.deliverPush({ chatId: 't1', text: pulled!.text, user: 'human' })

    expect(h.spawned).toHaveLength(0)

    await manager.dispose()
  })

  it('stops delivering once disposed', async () => {
    const h = harness()
    const manager = h.manager()
    await manager.start()
    await manager.dispose()

    await manager.deliverPush({ chatId: 't1', text: 'too late', user: 'human' })

    expect(h.spawned).toHaveLength(0)
  })

  it('closes every open task session on dispose', async () => {
    const h = harness()
    const manager = h.manager()

    await manager.deliverPush({ chatId: 't1', text: 'one', user: 'human' })
    await manager.deliverPush({ chatId: 't2', text: 'two', user: 'human' })
    expect(h.spawned).toHaveLength(2)

    await manager.dispose()

    expect(h.spawned.every(session => session.disposed)).toBe(true)
  })
})
