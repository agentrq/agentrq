import type { Context } from '@deepseek-ai/cordis'
import type { Agent } from '@deepseek-ai/dsh-agent'
import { describe, expect, it } from 'vitest'
import type { AgentRqClient, AgentRqTask } from '../src/client.js'
import type { Config } from '../src/config.js'
import { AgentRqRuntime } from '../src/runtime.js'

const CONFIG: Config = {
  url: 'https://workspace.mcp.example/mcp?token=t',
  token: '',
  mountBridge: false,
  serverName: 'agentrq',
  deliverPushes: true,
  catchUpOnStart: true,
  scope: 'single-agent',
  reconnect: { initialDelayMs: 1000, maxDelayMs: 900000 },
  guidance: true,
  requestTimeoutMs: 30000,
}

function task(id: string): AgentRqTask {
  return { id, title: `title ${id}`, status: 'notstarted', text: `Next assigned task:\nID: ${id}` }
}

/** Text of every message queued on the agent, in order, tagged by route. */
type Delivery = { route: 'followup' | 'inject'; text: string }

function harness() {
  const deliveries: Delivery[] = []
  const warnings: string[] = []

  const record = (route: Delivery['route']) => (message: { content: readonly { type: string; text?: string }[] }) => {
    const text = message.content.map(block => block.text ?? '').join('')
    deliveries.push({ route, text })
  }

  const agent = {
    id: 'session-1',
    status: 'idle' as 'idle' | 'running',
    followup: record('followup'),
    inject: record('inject'),
  }

  const ctx = {
    logger: { warn: (message: string) => { warnings.push(message) } },
    agents: {
      get: () => agent,
      roots: () => [agent],
      withoutInitiator: <T>(operation: () => T): T => operation(),
    },
  }

  const queue: (AgentRqTask | undefined)[] = []
  const failures: (Error | undefined)[] = []
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
  }

  return {
    deliveries,
    warnings,
    queue,
    failures,
    agent,
    starts: () => starts,
    setConnected: (value: boolean) => { connected = value },
    runtime: (config: Config = CONFIG) => new AgentRqRuntime(
      ctx as unknown as Context,
      agent as unknown as Agent,
      client as unknown as AgentRqClient,
      config,
    ),
  }
}

describe('AgentRqRuntime', () => {
  it('opens the workspace session on start', async () => {
    const h = harness()
    const runtime = h.runtime()

    await runtime.start()

    expect(h.starts()).toBe(1)
    await runtime.dispose()
  })

  it('claims a waiting task at startup, for work that predates the connection', async () => {
    const h = harness()
    h.queue.push(task('t1'))
    const runtime = h.runtime()

    await runtime.start()

    expect(h.deliveries).toHaveLength(1)
    expect(h.deliveries[0]?.route).toBe('followup')
    expect(h.deliveries[0]?.text).toContain('task_id: t1')
    expect(runtime.status().lastDeliveredTaskId).toBe('t1')

    await runtime.dispose()
  })

  it('skips the startup check when catch-up is off', async () => {
    const h = harness()
    h.queue.push(task('t1'))
    const runtime = h.runtime({ ...CONFIG, catchUpOnStart: false })

    await runtime.start()

    expect(h.starts()).toBe(1)
    expect(h.deliveries).toHaveLength(0)

    await runtime.dispose()
  })

  it('contains a failed startup check, since the workspace re-pushes anyway', async () => {
    const h = harness()
    h.failures.push(new Error('workspace unreachable'))
    const runtime = h.runtime()

    await runtime.start()

    expect(h.deliveries).toHaveLength(0)
    expect(h.warnings.join('\n')).toContain('workspace unreachable')

    await runtime.dispose()
  })

  it('wakes an idle agent with a push and injects into a running one', async () => {
    const h = harness()
    const runtime = h.runtime()

    runtime.deliverPush({ chatId: 'c1', text: 'ping while idle', user: 'human' })
    h.agent.status = 'running'
    runtime.deliverPush({ chatId: 'c1', text: 'ping while running', user: 'human' })

    expect(h.deliveries.map(delivery => delivery.route)).toEqual(['followup', 'inject'])
    expect(h.deliveries[0]?.text).toContain('ping while idle')
    expect(h.deliveries[1]?.text).toContain('chat_id: c1')

    await runtime.dispose()
  })

  it('forwards a pushed task without classifying it', async () => {
    const h = harness()
    const runtime = h.runtime()

    // Exactly what WorkspaceServer.StartPoller pushes for a pending task.
    runtime.deliverPush({
      chatId: '0h8b1P7TX5V',
      text: 'Next assigned task:\nTitle: Ship the bundle\nDetails: Open a PR.',
      user: 'human',
    })

    expect(h.deliveries).toHaveLength(1)
    expect(h.deliveries[0]?.text).toContain('Next assigned task:')
    expect(runtime.status().lastDeliveredTaskId).toBe('0h8b1P7TX5V')

    await runtime.dispose()
  })

  it('drops the workspace re-push of an unclaimed task', async () => {
    const h = harness()
    const runtime = h.runtime()
    const push = { chatId: 't1', text: 'Next assigned task:\nTitle: Ship it', user: 'human' }

    // The server repeats this every 60s until the agent claims the task.
    runtime.deliverPush(push)
    runtime.deliverPush({ ...push })
    runtime.deliverPush({ ...push })

    expect(h.deliveries).toHaveLength(1)

    await runtime.dispose()
  })

  it('delivers a genuinely new message on a task it has already seen', async () => {
    const h = harness()
    const runtime = h.runtime()

    runtime.deliverPush({ chatId: 't1', text: 'Next assigned task:\nTitle: Ship it', user: 'human' })
    runtime.deliverPush({ chatId: 't1', text: 'Rebase onto main first.', user: 'human' })

    expect(h.deliveries).toHaveLength(2)
    expect(h.deliveries[1]?.text).toContain('Rebase onto main first.')

    await runtime.dispose()
  })

  it('does not confuse identical text on two different tasks', async () => {
    const h = harness()
    const runtime = h.runtime()

    runtime.deliverPush({ chatId: 't1', text: 'ping', user: 'human' })
    runtime.deliverPush({ chatId: 't2', text: 'ping', user: 'human' })

    expect(h.deliveries).toHaveLength(2)

    await runtime.dispose()
  })

  it('stops delivering while paused and resumes on request', async () => {
    const h = harness()
    const runtime = h.runtime()

    expect(runtime.pause().active).toBe(false)
    runtime.deliverPush({ chatId: 't1', text: 'while paused', user: 'human' })
    expect(h.deliveries).toHaveLength(0)

    expect(runtime.resume().active).toBe(true)
    runtime.deliverPush({ chatId: 't1', text: 'after resume', user: 'human' })
    expect(h.deliveries).toHaveLength(1)

    await runtime.dispose()
  })

  it('never delivers when delivery is disabled in configuration', async () => {
    const h = harness()
    const runtime = h.runtime({ ...CONFIG, deliverPushes: false })

    runtime.deliverPush({ chatId: 't1', text: 'ignored', user: 'human' })

    expect(h.deliveries).toHaveLength(0)
    expect(runtime.status().active).toBe(false)

    await runtime.dispose()
  })

  it('reports the workspace connection state', async () => {
    const h = harness()
    const runtime = h.runtime()
    expect(runtime.status().connected).toBe(true)

    h.setConnected(false)
    expect(runtime.status().connected).toBe(false)

    await runtime.dispose()
  })

  it('returns the dequeued task to an explicit pull instead of queuing a turn', async () => {
    const h = harness()
    h.queue.push(task('t1'))
    const runtime = h.runtime({ ...CONFIG, catchUpOnStart: false })

    const pulled = await runtime.pullNow(new AbortController().signal)

    expect(pulled?.id).toBe('t1')
    expect(h.deliveries).toHaveLength(0)
    expect(runtime.status().lastDeliveredTaskId).toBe('t1')

    await runtime.dispose()
  })

  it('does not re-deliver a task the model already pulled by hand', async () => {
    const h = harness()
    h.queue.push(task('t1'))
    const runtime = h.runtime({ ...CONFIG, catchUpOnStart: false })

    const pulled = await runtime.pullNow(new AbortController().signal)
    // The workspace keeps pushing it until the model claims it.
    runtime.deliverPush({ chatId: 't1', text: pulled!.text, user: 'human' })

    expect(h.deliveries).toHaveLength(0)

    await runtime.dispose()
  })

  it('stops delivering once disposed', async () => {
    const h = harness()
    const runtime = h.runtime()
    await runtime.start()
    await runtime.dispose()

    runtime.deliverPush({ chatId: 't1', text: 'too late', user: 'human' })

    expect(h.deliveries).toHaveLength(0)
  })
})
