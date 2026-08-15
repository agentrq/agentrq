/**
 * The `agentrq_autopull` management tool.
 *
 * Task CRUD already reaches the model as `mcp__agentrq__*` through the harness
 * MCP bridge; this tool covers only what that bridge cannot express — the
 * plugin's own polling state, and an on-demand dequeue that returns the task as
 * a tool result instead of waiting for the next tick.
 *
 * @module @agentrq/dsh-plugin-agentrq
 */

import type { Context } from '@deepseek-ai/cordis'
import { defineTool } from '@deepseek-ai/dsh-tools'
import type { AgentRqRuntime } from './runtime.js'

/** Actions the model may take on the auto-pull runtime. */
const ACTIONS = ['status', 'pause', 'resume', 'pull_now'] as const

/**
 * Register the tool in one agent's scope.
 *
 * @param agentCtx - the agent-scoped context that owns the registration.
 * @param runtime - that agent's auto-pull runtime.
 * @returns a disposer that unregisters the tool.
 */
export function registerAutoPullTool(agentCtx: Context, runtime: AgentRqRuntime): () => void {
  return agentCtx.tools.register(defineTool({
    name: 'agentrq_autopull',
    description: [
      'Inspect or steer automatic delivery of AgentRQ work into this session.',
      'The workspace pushes tasks and messages on its own; this tool does not fetch them on a timer.',
      '"status" reports the workspace connection and whether delivery is on;',
      '"pause" and "resume" stop and restart delivery into this session;',
      '"pull_now" dequeues the next task assigned to you right away and returns it.',
      'Task content, replies, and status changes go through the mcp__agentrq__* tools, not this one.',
    ].join(' '),
    parameters: {
      action: {
        type: 'string',
        enum: ACTIONS,
        required: true,
        description: 'status | pause | resume | pull_now',
      },
    },
    output: {
      schema: {
        type: 'object',
        additionalProperties: false,
        properties: {
          active: { type: 'boolean', required: true, description: 'Whether workspace pushes are reaching this session.' },
          configured: { type: 'boolean', required: true, description: 'Whether delivery is enabled in configuration.' },
          connected: { type: 'boolean', required: true, description: 'Whether the workspace session is established.' },
          lastDeliveredTaskId: {
            oneOf: [{ type: 'string' }, { type: 'null' }],
            required: true,
            description: 'Task id most recently handed to this session, or null.',
          },
          task: {
            oneOf: [
              {
                type: 'object',
                additionalProperties: false,
                properties: {
                  id: { type: 'string', required: true, description: 'Base62 task id.' },
                  title: { type: 'string', required: true, description: 'Task title.' },
                  status: { type: 'string', required: true, description: 'Task status at fetch time.' },
                  text: { type: 'string', required: true, description: 'The workspace rendering of the task.' },
                },
              },
              { type: 'null' },
            ],
            required: true,
            description: 'The dequeued task for "pull_now", or null when the queue was empty or the action was not a pull.',
          },
        },
      },
      render: (args, value) => {
        if (args.action !== 'pull_now') {
          const state = value.active ? 'on' : value.configured ? 'paused' : 'disabled'
          const link = value.connected ? 'connected' : 'reconnecting'
          return [{ type: 'text', text: `AgentRQ delivery is ${state}; workspace session ${link}.` }]
        }
        if (value.task === null) return [{ type: 'text', text: 'AgentRQ queue is empty; no task assigned to you.' }]
        return [{ type: 'text', text: value.task.text }]
      },
    },
    async execute(args, exec) {
      if (args.action === 'pull_now') {
        const task = await runtime.pullNow(exec.signal)
        return { ...runtime.status(), task: task === undefined ? null : { id: task.id, title: task.title, status: task.status, text: task.text } }
      }
      const status = args.action === 'pause'
        ? runtime.pause()
        : args.action === 'resume'
          ? runtime.resume()
          : runtime.status()
      return { ...status, task: null }
    },
  }))
}
