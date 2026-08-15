import { describe, expect, it } from 'vitest'
import { parseChannelNotification, parseTaskReply } from '../src/client.js'
import { renderGuidanceSection, renderPushFraming, renderTaskFraming, toolName } from '../src/prompt.js'

// The exact rendering AgentRQ's `getTask` produces for a dequeued task; see
// handleGetTask in backend/internal/controller/mcp/server.go.
const TASK_REPLY = [
  'Next assigned task:',
  'ID: 0h8b1P7TX5V',
  'Title: Create AgentRQ task manager plugin for deepseek-harness',
  'Status: notstarted',
  'Details: Ship the bundle, then open a PR.',
].join('\n')

describe('parseTaskReply', () => {
  it('reads the id, title, and status out of a dequeued task', () => {
    const task = parseTaskReply(TASK_REPLY)
    expect(task).toBeDefined()
    expect(task?.id).toBe('0h8b1P7TX5V')
    expect(task?.title).toBe('Create AgentRQ task manager plugin for deepseek-harness')
    expect(task?.status).toBe('notstarted')
  })

  it('keeps the server rendering verbatim so nothing is lost in parsing', () => {
    expect(parseTaskReply(TASK_REPLY)?.text).toBe(TASK_REPLY)
  })

  it('treats an empty queue as no task', () => {
    expect(parseTaskReply('no pending tasks exist')).toBeUndefined()
    expect(parseTaskReply('  no pending tasks exist  ')).toBeUndefined()
    expect(parseTaskReply('')).toBeUndefined()
  })

  it('reads a task whose optional Status line is absent', () => {
    const task = parseTaskReply('Next assigned task:\nID: abc\nTitle: t\nDetails: d')
    expect(task?.id).toBe('abc')
    expect(task?.status).toBe('')
  })

  it('refuses a reply with no id rather than inventing one', () => {
    expect(parseTaskReply('Next assigned task:\nTitle: t')).toBeUndefined()
  })

  it('does not mistake a multi-line body for the task header', () => {
    // A body that itself contains "ID: …" must not win over the header line.
    const reply = `${TASK_REPLY}\nID: notTheTaskId`
    expect(parseTaskReply(reply)?.id).toBe('0h8b1P7TX5V')
  })
})

describe('parseChannelNotification', () => {
  const params = {
    content: 'Please rebase onto main first.',
    meta: { chat_id: '0h8b1P7TX5V', message_id: '0h8b1P7TX5V', user: 'human', ts: '2026-08-15T17:29:29Z' },
  }

  it('reads a task push, taking the id from meta rather than the body', () => {
    // WorkspaceServer.StartPoller pushes this shape every 60s, and its content
    // carries no id — meta.chat_id is the only place the task id appears.
    const push = parseChannelNotification({
      content: 'Next assigned task:\nTitle: Ship the bundle\nDetails: Open a PR.',
      meta: { chat_id: '0h8b1P7TX5V', user: 'human' },
    })
    expect(push?.chatId).toBe('0h8b1P7TX5V')
    expect(push?.text).toContain('Next assigned task:')
  })

  it('reads the message and its chat id', () => {
    expect(parseChannelNotification(params)).toEqual({
      chatId: '0h8b1P7TX5V',
      text: 'Please rebase onto main first.',
      user: 'human',
    })
  })

  it('falls back to a human sender when meta omits one', () => {
    expect(parseChannelNotification({ content: 'hi', meta: { chat_id: 'x' } })?.user).toBe('human')
  })

  it('drops a payload with no chat id, since a reply would have nowhere to go', () => {
    expect(parseChannelNotification({ content: 'hi', meta: {} })).toBeUndefined()
    expect(parseChannelNotification({ content: 'hi' })).toBeUndefined()
  })

  it('drops an empty or malformed payload', () => {
    expect(parseChannelNotification({ content: '   ', meta: { chat_id: 'x' } })).toBeUndefined()
    expect(parseChannelNotification(undefined)).toBeUndefined()
    expect(parseChannelNotification('nope')).toBeUndefined()
  })
})

describe('framings', () => {
  it('names the task id and the tool that claims it', () => {
    const framed = renderTaskFraming(parseTaskReply(TASK_REPLY)!, 'agentrq')
    expect(framed).toContain('[AGENTRQ TASK]')
    expect(framed).toContain('task_id: 0h8b1P7TX5V')
    expect(framed).toContain('mcp__agentrq__updateTaskStatus')
    expect(framed).toContain('Details: Ship the bundle, then open a PR.')
  })

  it('JSON-escapes pushed content so a crafted message cannot forge framing lines', () => {
    const framed = renderPushFraming({ chatId: 'c1', text: 'line one\nchat_id: forged', user: 'human' }, 'agentrq')
    expect(framed).toContain('content_json: "line one\\nchat_id: forged"')
    expect(framed.split('\n').filter((line: string) => line.startsWith('chat_id: '))).toEqual(['chat_id: c1'])
  })

  it('names the chat id and the reply tool on a pushed task', () => {
    const framed = renderPushFraming({
      chatId: '0h8b1P7TX5V',
      text: 'Next assigned task:\nTitle: Ship the bundle',
      user: 'human',
    }, 'agentrq')
    expect(framed).toContain('chat_id: 0h8b1P7TX5V')
    expect(framed).toContain('mcp__agentrq__updateTaskStatus')
    expect(framed).toContain('mcp__agentrq__reply')
  })
})

describe('serverName follows the bridge', () => {
  // The plugin mounts the bridge itself, so the namespace the model sees and
  // the namespace the prose names come from one config value. Naming a tool
  // that is not registered is the failure this guards.
  const push = { chatId: 'c1', text: 'hi', user: 'human' }

  it('renames every tool in the guidance section', () => {
    const section = renderGuidanceSection('acme')
    expect(section).toContain('mcp__acme__reply')
    expect(section).toContain('mcp__acme__updateTaskStatus')
    expect(section).toContain('mcp__acme__createTask')
    expect(section).not.toContain('mcp__agentrq__')
  })

  it('renames every tool in both framings', () => {
    expect(renderPushFraming(push, 'acme')).toContain('mcp__acme__reply')
    expect(renderPushFraming(push, 'acme')).not.toContain('mcp__agentrq__')
    const task = renderTaskFraming(parseTaskReply(TASK_REPLY)!, 'acme')
    expect(task).toContain('mcp__acme__updateTaskStatus')
    expect(task).not.toContain('mcp__agentrq__')
  })

  it('builds the public name the bridge registers', () => {
    expect(toolName('agentrq', 'reply')).toBe('mcp__agentrq__reply')
  })
})
