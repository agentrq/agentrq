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
  provider: '',
  model: '',
  cwd: '',
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
  readonly meta: { cwd?: string } | undefined
  readonly agentOptions: { provider?: string; model?: string } | undefined
  emitStatus(status: 'idle' | 'running'): void
}

function harness() {
  const warnings: string[] = []
  const spawned: SpawnedSession[] = []
  // Every agent `ctx.agents.get()`/`list()` can see: pre-seeded stand-ins for
  // an already-open chat, plus every agent `create()` spawns below.
  const registry = new Map<string, {
    id: string
    options: { provider?: string; model?: string }
    session: { id: string; header: { cwd: string | undefined } }
  }>()
  const renamedTitles: Array<{ sessionId: string; title: string }> = []
  const workspaces = new Map<string, { path: string; attached: string[]; attachSession: (id: string) => Promise<void> }>()
  const persistedHeaders: Array<{ id: string; createdAt: number; cwd?: string }> = []
  let sessionTitleAvailable = true
  let workspaceRegistryAvailable = true
  let sessionPersistenceAvailable = true
  let listFailure: Error | undefined
  let persistedSeq = 0
  // A bare-bones stand-in for cordis' scope-filtered dispatch: agent-scoped
  // listeners (registered on `agent.ctx`) see only that agent's events, while
  // top-level listeners (registered directly on `ctx`) see every agent's.
  const globalStatusListeners: Array<(payload: { agent: unknown; status: string }) => void> = []
  let spawnCount = 0
  let liveAgentCount = 0

  /** Shared spawn logic behind both `create()` (fresh id) and `resume()` (an existing one). */
  function spawnAgent(id: string, meta: { cwd?: string } | undefined, agentOptions: { provider?: string; model?: string } | undefined): { agent: unknown; dispose: () => Promise<void> } {
    const deliveries: Delivery[] = []
    const statusListeners: Array<(payload: { agent: unknown; status: string }) => void> = []
    const disposedListeners: Array<() => void> = []
    const record = (route: Delivery['route']) => (message: { content: readonly { type: string; text?: string }[] }) => {
      deliveries.push({ route, text: message.content.map(block => block.text ?? '').join('') })
    }

    const session: SpawnedSession = {
      id,
      deliveries,
      status: 'idle',
      disposed: false,
      meta,
      agentOptions,
      emitStatus(status) {
        session.status = status
        const payload = { agent, status }
        for (const listener of statusListeners) listener(payload)
        for (const listener of globalStatusListeners) listener(payload)
      },
    }

    const agent = {
      id,
      get status() { return session.status },
      options: agentOptions ?? {},
      session: { id, header: { cwd: meta?.cwd } },
      ctx: {
        on: (event: string, listener: (payload: unknown) => void) => {
          if (event === 'agent/status') statusListeners.push(listener as (payload: { agent: unknown; status: string }) => void)
          if (event === 'agent/disposed') disposedListeners.push(listener as () => void)
          return () => {}
        },
      },
      followup: record('followup'),
      inject: record('inject'),
    }

    spawned.push(session)
    registry.set(id, agent)
    return {
      agent,
      dispose: async (): Promise<void> => {
        session.disposed = true
        registry.delete(id)
        for (const listener of disposedListeners) listener()
      },
    }
  }

  const resumeCalls: string[] = []
  const resumeFailures: (Error | undefined)[] = []

  const agentsService = {
    create: async (options: {
      meta?: { cwd?: string }
      agentOptions?: { provider?: string; model?: string }
    }): Promise<{ agent: unknown; dispose: () => Promise<void> }> => {
      spawnCount += 1
      return spawnAgent(`spawned-${spawnCount}`, options.meta, options.agentOptions)
    },
    resume: async (options: { resumeSessionId: string }): Promise<{ agent: unknown; dispose: () => Promise<void> }> => {
      resumeCalls.push(options.resumeSessionId)
      const failure = resumeFailures.shift()
      if (failure !== undefined) throw failure
      const header = persistedHeaders.find(candidate => candidate.id === options.resumeSessionId)
      return spawnAgent(options.resumeSessionId, header?.cwd === undefined ? {} : { cwd: header.cwd }, undefined)
    },
    list: () => [...registry.values()],
    get: (id: string) => registry.get(id),
    withoutInitiator: <T>(operation: () => T): T => operation(),
  }

  function sessionTitleService() {
    if (!sessionTitleAvailable) return undefined
    return {
      rename: (session: { id: string }, title: string) => {
        if (title === '') throw new Error('title normalizes to empty')
        renamedTitles.push({ sessionId: session.id, title })
      },
    }
  }

  function workspaceRegistryService() {
    if (!workspaceRegistryAvailable) return undefined
    return {
      resolveByPath: async (path: string) => workspaces.get(path),
      create: async (path: string) => {
        const existing = workspaces.get(path)
        if (existing !== undefined) return existing
        const attached: string[] = []
        const workspace = { path, attached, attachSession: async (sessionId: string) => { attached.push(sessionId) } }
        workspaces.set(path, workspace)
        return workspace
      },
    }
  }

  function sessionPersistenceService() {
    if (!sessionPersistenceAvailable) return undefined
    return {
      list: async () => {
        if (listFailure !== undefined) {
          const failure = listFailure
          listFailure = undefined
          throw failure
        }
        return persistedHeaders.map(header => ({ ...header }))
      },
    }
  }

  const ctx = {
    logger: { warn: (message: string) => { warnings.push(message) } },
    agents: agentsService,
    on: (event: string, listener: (payload: { agent: unknown; status: string }) => void) => {
      if (event === 'agent/status') globalStatusListeners.push(listener)
      return () => {}
    },
    // Mirrors cordis' real `ctx.get(name)`: the one accessor documented to
    // return `undefined` for a service outside `inject`, rather than a
    // direct `ctx.sessionTitle`/`ctx.workspaceRegistry` property read.
    get: (name: string) => {
      if (name === 'sessionTitle') return sessionTitleService()
      if (name === 'workspaceRegistry') return workspaceRegistryService()
      if (name === 'sessionPersistence') return sessionPersistenceService()
      return undefined
    },
  }

  /** Seed a stand-in for a task session that survived a previous process lifetime. */
  function addPersistedSession(taskId: string, options: { cwd?: string; createdAt?: number } = {}): string {
    persistedSeq += 1
    const id = `agentrq-task-${taskId}-${persistedSeq}`
    persistedHeaders.push({ id, createdAt: options.createdAt ?? persistedSeq, ...(options.cwd === undefined ? {} : { cwd: options.cwd }) })
    return id
  }

  /** Seed a stand-in for an already-open, non-task chat session. */
  function addLiveAgent(options: { provider?: string; model?: string }, cwd = '/home/user/project'): string {
    liveAgentCount += 1
    const id = `live-${liveAgentCount}`
    registry.set(id, { id, options, session: { id, header: { cwd } } })
    return id
  }

  /** Simulate one live agent (by id) starting a turn, as the activity tracker observes it. */
  function markActive(agentId: string): void {
    const agent = registry.get(agentId)
    for (const listener of globalStatusListeners) listener({ agent, status: 'running' })
  }

  /** Simulate a live agent going away (closed tab, disposed session, …). */
  function removeLiveAgent(agentId: string): void {
    registry.delete(agentId)
  }

  const queue: (AgentRqTask | undefined)[] = []
  const failures: (Error | undefined)[] = []
  const statuses = new Map<string, string>()
  const fetchTaskFailures: (Error | undefined)[] = []
  let starts = 0
  let fetchTaskCalls = 0
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
      fetchTaskCalls += 1
      const failure = fetchTaskFailures.shift()
      if (failure !== undefined) throw failure
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
    fetchTaskFailures,
    renamedTitles,
    workspaces,
    resumeCalls,
    resumeFailures,
    addLiveAgent,
    markActive,
    removeLiveAgent,
    addPersistedSession,
    starts: () => starts,
    fetchTaskCalls: () => fetchTaskCalls,
    createCalls: () => spawnCount,
    setConnected: (value: boolean) => { connected = value },
    setSessionTitleAvailable: (value: boolean) => { sessionTitleAvailable = value },
    setWorkspaceRegistryAvailable: (value: boolean) => { workspaceRegistryAvailable = value },
    setSessionPersistenceAvailable: (value: boolean) => { sessionPersistenceAvailable = value },
    setListFailure: (error: Error) => { listFailure = error },
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

  it('opens a task session with an existing agent\'s cwd and model, when config sets neither', async () => {
    const h = harness()
    h.addLiveAgent({ provider: 'anthropic', model: 'claude' }, '/home/user/project')
    const manager = h.manager()

    await manager.deliverPush({ chatId: 't1', text: 'ping', user: 'human' })

    // NOT process.cwd(): under `dsh web` that's the dsh profile's own
    // directory, not the project the reference agent actually works in.
    expect(h.spawned[0]?.meta?.cwd).toBe('/home/user/project')
    expect(h.spawned[0]?.agentOptions).toEqual({ provider: 'anthropic', model: 'claude' })

    await manager.dispose()
  })

  it('falls back to the dsh process cwd only when no other agent has ever been live', async () => {
    const h = harness()
    const manager = h.manager()

    await manager.deliverPush({ chatId: 't1', text: 'ping', user: 'human' })

    expect(h.spawned[0]?.meta?.cwd).toBe(process.cwd())

    await manager.dispose()
  })

  it('leaves agentOptions undefined with no config and no other live agent to copy', async () => {
    const h = harness()
    const manager = h.manager()

    await manager.deliverPush({ chatId: 't1', text: 'ping', user: 'human' })

    expect(h.spawned[0]?.agentOptions).toBeUndefined()

    await manager.dispose()
  })

  it('prefers explicit config over an existing agent\'s model/provider/cwd', async () => {
    const h = harness()
    h.addLiveAgent({ provider: 'openai', model: 'gpt' })
    const manager = h.manager({ ...CONFIG, provider: 'anthropic', model: 'claude', cwd: '/srv/repo' })

    await manager.deliverPush({ chatId: 't1', text: 'ping', user: 'human' })

    expect(h.spawned[0]?.meta?.cwd).toBe('/srv/repo')
    expect(h.spawned[0]?.agentOptions).toEqual({ provider: 'anthropic', model: 'claude' })

    await manager.dispose()
  })

  it('omits provider when only model is configured', async () => {
    const h = harness()
    const manager = h.manager({ ...CONFIG, model: 'claude' })

    await manager.deliverPush({ chatId: 't1', text: 'ping', user: 'human' })

    expect(h.spawned[0]?.agentOptions).toEqual({ model: 'claude' })

    await manager.dispose()
  })

  it('copies the most recently active agent\'s model, not just the first one registered', async () => {
    const h = harness()
    const older = h.addLiveAgent({ provider: 'openai', model: 'gpt' })
    const newer = h.addLiveAgent({ provider: 'anthropic', model: 'claude' })
    const manager = h.manager()
    await manager.start()

    // The older agent had a turn a while ago; the newer one just started one.
    h.markActive(older)
    h.markActive(newer)
    await manager.deliverPush({ chatId: 't1', text: 'ping', user: 'human' })

    expect(h.spawned[0]?.agentOptions).toEqual({ provider: 'anthropic', model: 'claude' })

    await manager.dispose()
  })

  it('falls back to registration order when no agent has been observed active yet', async () => {
    const h = harness()
    h.addLiveAgent({ provider: 'openai', model: 'gpt' })
    h.addLiveAgent({ provider: 'anthropic', model: 'claude' })
    const manager = h.manager()
    await manager.start()

    // Neither agent has had a turn since the manager started watching.
    await manager.deliverPush({ chatId: 't1', text: 'ping', user: 'human' })

    expect(h.spawned[0]?.agentOptions).toEqual({ provider: 'openai', model: 'gpt' })

    await manager.dispose()
  })

  it('ignores a most-recently-active agent that is no longer live', async () => {
    const h = harness()
    const gone = h.addLiveAgent({ provider: 'openai', model: 'gpt' })
    h.addLiveAgent({ provider: 'anthropic', model: 'claude' })
    const manager = h.manager()
    await manager.start()

    h.markActive(gone)
    h.removeLiveAgent(gone)
    await manager.deliverPush({ chatId: 't1', text: 'ping', user: 'human' })

    expect(h.spawned[0]?.agentOptions).toEqual({ provider: 'anthropic', model: 'claude' })

    await manager.dispose()
  })

  it('pins a push-opened session\'s title to the task\'s title, fetched via getTask', async () => {
    const h = harness()
    h.statuses.set('t1', 'notstarted')
    const manager = h.manager()

    await manager.deliverPush({ chatId: 't1', text: 'Next assigned task:\nTitle: title t1', user: 'human' })

    expect(h.renamedTitles).toEqual([{ sessionId: 'spawned-1', title: 'title t1' }])
    // The framing itself is untouched — the title is set out of band, not
    // prefixed onto the message the model sees.
    expect(h.spawned[0]?.deliveries[0]?.text.startsWith('[AGENTRQ]')).toBe(true)

    await manager.dispose()
  })

  it('pins a startup-caught-up task\'s session title from its own task, with no extra getTask call', async () => {
    const h = harness()
    h.queue.push(task('t1'))
    const manager = h.manager()

    await manager.start()

    expect(h.renamedTitles).toEqual([{ sessionId: 'spawned-1', title: 'title t1' }])
    expect(h.fetchTaskCalls()).toBe(0)

    await manager.dispose()
  })

  it('leaves the session untitled when no title is available', async () => {
    const h = harness()
    const manager = h.manager()

    await manager.deliverPush({ chatId: 't1', text: 'ping', user: 'human' })

    expect(h.renamedTitles).toEqual([])
    expect(h.spawned[0]?.deliveries[0]?.text.startsWith('[AGENTRQ]')).toBe(true)

    await manager.dispose()
  })

  it('leaves the session untitled when the title lookup fails', async () => {
    const h = harness()
    h.fetchTaskFailures.push(new Error('workspace unreachable'))
    const manager = h.manager()

    await manager.deliverPush({ chatId: 't1', text: 'ping', user: 'human' })

    expect(h.renamedTitles).toEqual([])
    expect(h.warnings.join('\n')).toContain('workspace unreachable')

    await manager.dispose()
  })

  it('does not touch the title when sessionTitle is unavailable in this deployment', async () => {
    const h = harness()
    h.setSessionTitleAvailable(false)
    h.statuses.set('t1', 'notstarted')
    const manager = h.manager()

    await manager.deliverPush({ chatId: 't1', text: 'ping', user: 'human' })

    expect(h.spawned).toHaveLength(1)
    expect(h.renamedTitles).toEqual([])

    await manager.dispose()
  })

  it('groups a new session into the workspace already owning its cwd', async () => {
    const h = harness()
    h.addLiveAgent({ provider: 'anthropic', model: 'claude' }, '/home/user/project')
    const manager = h.manager()

    await manager.deliverPush({ chatId: 't1', text: 'ping', user: 'human' })
    await flush()

    const workspace = h.workspaces.get('/home/user/project')
    expect(workspace?.attached).toEqual(['spawned-1'])

    await manager.dispose()
  })

  it('creates a workspace for a cwd no workspace owns yet, then groups the session into it', async () => {
    const h = harness()
    const manager = h.manager({ ...CONFIG, cwd: '/srv/fresh-repo' })

    await manager.deliverPush({ chatId: 't1', text: 'ping', user: 'human' })
    await flush()

    const workspace = h.workspaces.get('/srv/fresh-repo')
    expect(workspace?.attached).toEqual(['spawned-1'])

    await manager.dispose()
  })

  it('does not touch grouping when workspaceRegistry is unavailable in this deployment', async () => {
    const h = harness()
    h.setWorkspaceRegistryAvailable(false)
    const manager = h.manager()

    await manager.deliverPush({ chatId: 't1', text: 'ping', user: 'human' })

    expect(h.spawned).toHaveLength(1)
    expect(h.workspaces.size).toBe(0)

    await manager.dispose()
  })

  it('resumes a task\'s session that survived a restart, instead of opening a new one', async () => {
    const h = harness()
    const persistedId = h.addPersistedSession('t1', { cwd: '/home/user/project' })
    const manager = h.manager()
    await manager.start()

    await manager.deliverPush({ chatId: 't1', text: 'ping', user: 'human' })

    expect(h.resumeCalls).toEqual([persistedId])
    expect(h.createCalls()).toBe(0)
    expect(h.spawned).toHaveLength(1)
    expect(h.spawned[0]?.id).toBe(persistedId)

    await manager.dispose()
  })

  it('applies grouping and title to a resumed session, same as a freshly created one', async () => {
    const h = harness()
    h.addPersistedSession('t1', { cwd: '/home/user/project' })
    h.statuses.set('t1', 'notstarted')
    const manager = h.manager()
    await manager.start()

    await manager.deliverPush({ chatId: 't1', text: 'ping', user: 'human' })

    expect(h.workspaces.get('/home/user/project')?.attached).toEqual([h.resumeCalls[0]])
    expect(h.renamedTitles).toEqual([{ sessionId: h.resumeCalls[0], title: 'title t1' }])

    await manager.dispose()
  })

  it('falls back to opening a fresh session when resuming a persisted one fails', async () => {
    const h = harness()
    h.addPersistedSession('t1', { cwd: '/home/user/project' })
    h.resumeFailures.push(new Error('session log corrupted'))
    const manager = h.manager()
    await manager.start()

    await manager.deliverPush({ chatId: 't1', text: 'ping', user: 'human' })

    expect(h.resumeCalls).toHaveLength(1)
    expect(h.createCalls()).toBe(1)
    expect(h.spawned).toHaveLength(1)
    expect(h.warnings.join('\n')).toContain('session log corrupted')

    await manager.dispose()
  })

  it('keeps the most recently created persisted session when duplicates exist for one task', async () => {
    const h = harness()
    h.addPersistedSession('t1', { createdAt: 1 })
    const newer = h.addPersistedSession('t1', { createdAt: 2 })
    const manager = h.manager()
    await manager.start()

    await manager.deliverPush({ chatId: 't1', text: 'ping', user: 'human' })

    expect(h.resumeCalls).toEqual([newer])

    await manager.dispose()
  })

  it('does not resume anything when sessionPersistence is unavailable in this deployment', async () => {
    const h = harness()
    h.setSessionPersistenceAvailable(false)
    h.addPersistedSession('t1')
    const manager = h.manager()
    await manager.start()

    await manager.deliverPush({ chatId: 't1', text: 'ping', user: 'human' })

    expect(h.resumeCalls).toEqual([])
    expect(h.createCalls()).toBe(1)

    await manager.dispose()
  })

  it('starts normally even when scanning persisted sessions fails', async () => {
    const h = harness()
    h.addPersistedSession('t1')
    h.setListFailure(new Error('storage unreachable'))
    const manager = h.manager()

    await manager.start()
    await manager.deliverPush({ chatId: 't1', text: 'ping', user: 'human' })

    expect(h.warnings.join('\n')).toContain('storage unreachable')
    expect(h.resumeCalls).toEqual([])
    expect(h.createCalls()).toBe(1)

    await manager.dispose()
  })
})
