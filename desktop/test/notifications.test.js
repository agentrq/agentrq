// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, it, expect, vi } from 'vitest'

import {
  DEDUPE_WINDOW_MS,
  NOTIFIABLE_TYPES,
  SELF_ACTION_WINDOW_MS,
  createNotificationGate,
  createSelfActionGate,
  badgeFor,
  createUnreadCounter,
  mapEventToNotification,
  shouldNotify,
  taskIdFromSelfActionRequest,
  truncate,
} from '../src/main/notifications.js'

/** A task view as the event stream delivers it. */
const task = (over = {}) => ({
  id: '0hua6QI7nXN',
  workspaceId: '0ZzhYQG2qtl',
  title: 'Ship the desktop app',
  status: 'ongoing',
  createdBy: 'agent',
  ...over,
})

const event = (type, over = {}) => ({ type, payload: task(over) })
const names = { workspaceName: () => 'agentrq-code' }

describe('truncate', () => {
  it('leaves short text alone', () => {
    expect(truncate('short', 60)).toBe('short')
  })

  it('cuts long text and marks it', () => {
    expect(truncate('x'.repeat(70), 60)).toBe(`${'x'.repeat(60)}…`)
  })

  it('handles missing text', () => {
    expect(truncate(undefined, 10)).toBe('')
    expect(truncate(null, 10)).toBe('')
  })
})

describe('shouldNotify', () => {
  it('accepts the event types worth interrupting someone for', () => {
    for (const type of NOTIFIABLE_TYPES) {
      expect(shouldNotify(event(type))).toBe(true)
    }
  })

  it('ignores UI-only signals', () => {
    // agent.connected drives a status dot; task.deleted has nothing to show.
    expect(shouldNotify(event('agent.connected'))).toBe(false)
    expect(shouldNotify(event('task.deleted'))).toBe(false)
  })

  it('ignores the user\'s own actions', () => {
    // Mirrors the push controller, which notifies only on agent activity —
    // being told about your own click is noise, not news.
    expect(shouldNotify(event('task.created', { createdBy: 'human' }))).toBe(false)
    expect(shouldNotify(event('status.updated', { createdBy: 'human' }))).toBe(false)
  })

  it('always notifies for a reply, which only an agent sends', () => {
    expect(shouldNotify(event('reply.received', { createdBy: 'human' }))).toBe(true)
  })

  it('stays silent for a muted workspace', () => {
    expect(shouldNotify(event('task.created'), { mutedWorkspaces: ['0ZzhYQG2qtl'] })).toBe(false)
    expect(shouldNotify(event('task.created'), { mutedWorkspaces: ['other'] })).toBe(true)
  })

  it('rejects malformed events rather than raising an empty notification', () => {
    expect(shouldNotify(null)).toBe(false)
    expect(shouldNotify({})).toBe(false)
    expect(shouldNotify({ type: 'task.created' })).toBe(false)
    expect(shouldNotify({ type: 'task.created', payload: 'nope' })).toBe(false)
    expect(shouldNotify(event('task.created', { workspaceId: undefined }))).toBe(false)
    expect(shouldNotify(event('task.created', { id: undefined }))).toBe(false)
  })
})

describe('mapEventToNotification', () => {
  it('words a new task the way the browser notification does', () => {
    expect(mapEventToNotification(event('task.created'), names)).toEqual({
      title: 'New task: Ship the desktop app',
      body: 'agentrq-code',
      route: '/workspaces/0ZzhYQG2qtl',
      tag: 'task-create-0hua6QI7nXN',
    })
  })

  it('words a status change with the status in caps', () => {
    expect(mapEventToNotification(event('status.updated', { status: 'completed' }), names)).toMatchObject({
      title: 'Task COMPLETED: Ship the desktop app',
      route: '/workspaces/0ZzhYQG2qtl',
      tag: 'task-status-0hua6QI7nXN',
    })
  })

  it('treats a plain task update the same as a status change', () => {
    expect(mapEventToNotification(event('task.updated'), names).title).toBe('Task ONGOING: Ship the desktop app')
  })

  it('routes a reply to the task itself, not the workspace', () => {
    expect(mapEventToNotification(event('reply.received'), names)).toEqual({
      title: 'Reply on: Ship the desktop app',
      body: 'agentrq-code',
      route: '/workspaces/0ZzhYQG2qtl/tasks/0hua6QI7nXN',
      tag: 'reply-0hua6QI7nXN',
    })
  })

  it('truncates long titles at the same lengths the backend uses', () => {
    const long = 'y'.repeat(100)
    expect(mapEventToNotification(event('task.created', { title: long }), names).title)
      .toBe(`New task: ${'y'.repeat(60)}…`)
    expect(mapEventToNotification(event('status.updated', { title: long }), names).title)
      .toBe(`Task ONGOING: ${'y'.repeat(50)}…`)
    expect(mapEventToNotification(event('reply.received', { title: long }), names).title)
      .toBe(`Reply on: ${'y'.repeat(55)}…`)
  })

  it('falls back to the product name rather than showing a raw workspace id', () => {
    expect(mapEventToNotification(event('task.created')).body).toBe('AgentRQ')
    expect(mapEventToNotification(event('task.created'), { workspaceName: () => '' }).body).toBe('AgentRQ')
  })

  it('returns nothing for an event that should not notify', () => {
    expect(mapEventToNotification(event('agent.connected'), names)).toBeNull()
    expect(mapEventToNotification(event('task.created'), { ...names, mutedWorkspaces: ['0ZzhYQG2qtl'] })).toBeNull()
  })

  it('copes with a status that is missing', () => {
    expect(mapEventToNotification(event('status.updated', { status: undefined }), names).title)
      .toBe('Task : Ship the desktop app')
  })
})

describe('taskIdFromSelfActionRequest', () => {
  it('reads the task id out of a reply request', () => {
    expect(taskIdFromSelfActionRequest('POST', '/api/v1/workspaces/ws1/tasks/t1/reply')).toBe('t1')
  })

  it('reads the task id out of a respond request', () => {
    expect(taskIdFromSelfActionRequest('POST', '/api/v1/workspaces/ws1/tasks/t1/respond')).toBe('t1')
  })

  it('is case-insensitive on the method, matching how Electron reports it', () => {
    expect(taskIdFromSelfActionRequest('post', '/api/v1/workspaces/ws1/tasks/t1/reply')).toBe('t1')
  })

  it('ignores a GET on the same path', () => {
    // Fetching a task's detail is not sending anything.
    expect(taskIdFromSelfActionRequest('GET', '/api/v1/workspaces/ws1/tasks/t1/reply')).toBeNull()
  })

  it('ignores requests that are not a reply or respond', () => {
    expect(taskIdFromSelfActionRequest('POST', '/api/v1/workspaces/ws1/tasks/t1/status')).toBeNull()
    expect(taskIdFromSelfActionRequest('POST', '/api/v1/workspaces/ws1/tasks')).toBeNull()
    expect(taskIdFromSelfActionRequest('PATCH', '/api/v1/workspaces/ws1/tasks/t1/reply')).toBeNull()
  })

  it('does not match a reply nested under something else', () => {
    expect(taskIdFromSelfActionRequest('POST', '/api/v1/workspaces/ws1/tasks/t1/reply/extra')).toBeNull()
  })
})

describe('who the payload says spoke', () => {
  const withMessages = (type, messages, over = {}) => ({
    type,
    payload: { id: 't1', workspaceId: 'w1', title: 'Ship it', createdBy: 'human', messages, ...over },
  })

  // Replying from a browser, a phone or Slack while the desktop app is open.
  // The self-action gate cannot help — it only knows what *this* instance sent
  // — so the payload has to say, and it does.
  it('says nothing about a reply the human wrote somewhere else', () => {
    const event = withMessages('reply.received', [{ id: 'm1', sender: 'human', text: 'any update?' }])

    expect(mapEventToNotification(event)).toBeNull()
  })

  it('still announces the agent answering', () => {
    const event = withMessages('reply.received', [{ id: 'm1', sender: 'agent', text: 'on it' }])

    expect(mapEventToNotification(event)?.title).toContain('Reply on: Ship it')
  })

  // Without messages there is nothing to read, so the old reading stands:
  // a reply is the agent's, and a task event is judged by who created it.
  it('falls back to the old reading when the payload carries no messages', () => {
    expect(mapEventToNotification({ type: 'reply.received', payload: { id: 't1', workspaceId: 'w1', title: 'T' } })).not.toBeNull()
    expect(mapEventToNotification({ type: 'task.created', payload: { id: 't1', workspaceId: 'w1', title: 'T', createdBy: 'agent' } })).not.toBeNull()
    expect(mapEventToNotification({ type: 'task.created', payload: { id: 't1', workspaceId: 'w1', title: 'T', createdBy: 'human' } })).toBeNull()
  })

  // One agent reply publishes two events — `task.updated` directly and
  // `reply.received` through the forwarder. Tagged by task they were two tags
  // for one message and both fired; tagged by the message the dedupe gate
  // collapses them, which is what it is for.
  it('tags both halves of one reply with the message they are about', () => {
    const messages = [{ id: 'm9', sender: 'agent', text: 'done' }]

    const reply = mapEventToNotification(withMessages('reply.received', messages))
    const updated = mapEventToNotification(withMessages('task.updated', messages))

    expect(reply.tag).toBe('m9')
    expect(updated.tag).toBe('m9')
    expect(createNotificationGate().allow(reply.tag)).toBe(true)
  })

  // A message row carrying no id names nothing, so the task-shaped tag stands
  // rather than every such event colliding on one empty tag and being deduped
  // into silence.
  it('keeps a tag of its own when the newest message has no id', () => {
    const event = withMessages('reply.received', [{ sender: 'agent', text: 'done' }])

    expect(mapEventToNotification(event).tag).toBe('reply-t1')
  })

  it('keeps a tag of its own when there is no message to name', () => {
    const reply = mapEventToNotification({ type: 'reply.received', payload: { id: 't1', workspaceId: 'w1', title: 'T' } })
    const created = mapEventToNotification({ type: 'task.created', payload: { id: 't1', workspaceId: 'w1', title: 'T', createdBy: 'agent' } })

    expect(reply.tag).toBe('reply-t1')
    expect(created.tag).toBe('task-create-t1')
  })
})

describe('createSelfActionGate', () => {
  /**
   * An event as the stream delivers it. `sender` is what settles whether this
   * is this desktop's own reply coming back or somebody else's news — the
   * payload carries the messages, so the gate reads rather than guesses.
   */
  const selfEcho = (id, type, sender = 'human') => ({
    type,
    payload: { id, messages: [{ sender, text: 'hi' }] },
  })

  it('reports no recent self-action for a task never marked', () => {
    expect(createSelfActionGate().isRecentSelfAction(selfEcho('t1', 'reply.received'))).toBe(false)
  })

  it('reports a recent self-action right after it is marked', () => {
    const gate = createSelfActionGate()
    gate.markSelf('t1')
    expect(gate.isRecentSelfAction(selfEcho('t1', 'reply.received'))).toBe(true)
  })

  it('recognises task.updated as the other type a reply/respond can echo as', () => {
    const gate = createSelfActionGate()
    gate.markSelf('t1')
    expect(gate.isRecentSelfAction(selfEcho('t1', 'task.updated'))).toBe(true)
  })

  it('does not mute a type a reply/respond could never produce', () => {
    // task.created and status.updated are never the result of sending a
    // reply, so a real one of either must still notify even for a task this
    // desktop instance just replied to.
    const gate = createSelfActionGate()
    gate.markSelf('t1')
    expect(gate.isRecentSelfAction(selfEcho('t1', 'task.created'))).toBe(false)
    expect(gate.isRecentSelfAction(selfEcho('t1', 'status.updated'))).toBe(false)
  })

  it('does not mark a task when given no id', () => {
    const gate = createSelfActionGate()
    gate.markSelf(null)
    gate.markSelf(undefined)
    expect(gate.isRecentSelfAction(selfEcho(null, 'reply.received'))).toBe(false)
  })

  it('does not confuse one task for another', () => {
    const gate = createSelfActionGate()
    gate.markSelf('t1')
    expect(gate.isRecentSelfAction(selfEcho('t2', 'reply.received'))).toBe(false)
  })

  it('expires the mute once the window passes', () => {
    let clock = 0
    const gate = createSelfActionGate({ windowMs: 1000, now: () => clock })

    gate.markSelf('t1')
    clock = 999
    expect(gate.isRecentSelfAction(selfEcho('t1', 'reply.received'))).toBe(true)
    clock = 1000
    expect(gate.isRecentSelfAction(selfEcho('t1', 'reply.received'))).toBe(false)
  })

  // The bug this gate caused. A human replies, the gate arms for ten seconds,
  // and the agent answers in two — which is most of them. Keyed on the clock
  // alone that reply was this desktop's own echo and was dropped; the status
  // change that followed arrived after the window and still notified, which is
  // exactly the difference that got reported.
  it('does not mute the agent answering inside the window', () => {
    let clock = 0
    const gate = createSelfActionGate({ windowMs: 10000, now: () => clock })

    gate.markSelf('t1')
    clock = 2000

    expect(gate.isRecentSelfAction(selfEcho('t1', 'reply.received', 'agent'))).toBe(false)
    // And the human's own message in that same window is still the echo it was.
    expect(gate.isRecentSelfAction(selfEcho('t1', 'reply.received', 'human'))).toBe(true)
  })

  // Older events, and anything the forwarder publishes without them, fall back
  // to the window rather than notifying on everything.
  it('still uses the window when the payload carries no messages', () => {
    const gate = createSelfActionGate()
    gate.markSelf('t1')

    expect(gate.isRecentSelfAction({ type: 'reply.received', payload: { id: 't1' } })).toBe(true)
    expect(gate.isRecentSelfAction({ type: 'reply.received', payload: { id: 't1', messages: [] } })).toBe(true)
    // A message row that names nobody is not the agent, so it falls back too
    // rather than reading as somebody else's news.
    expect(gate.isRecentSelfAction({ type: 'reply.received', payload: { id: 't1', messages: [{}] } })).toBe(true)
    expect(gate.isRecentSelfAction({ type: 'reply.received', payload: { id: 't1', messages: [null] } })).toBe(true)
  })

  it('holds up against an event that is barely one', () => {
    const gate = createSelfActionGate()

    expect(gate.isRecentSelfAction(undefined)).toBe(false)
    expect(gate.isRecentSelfAction({})).toBe(false)
  })

  it('lets a genuine later reply on the same task notify again', () => {
    // The mute must not persist forever just because the task was replied to
    // once; a real agent reply after the window is news again.
    let clock = 0
    const gate = createSelfActionGate({ windowMs: 1000, now: () => clock })

    gate.markSelf('t1')
    clock = 2000
    expect(gate.isRecentSelfAction(selfEcho('t1', 'reply.received'))).toBe(false)
  })

  it('forgets a stale mark even when only markSelf is ever called for it again', () => {
    // markSelf fires on every reply/respond regardless of what the stream
    // echoes back, so isRecentSelfAction may never run for a given task (a
    // muted workspace, say). Pruning has to happen from markSelf too, or the
    // map would grow for the life of the process.
    let clock = 0
    const gate = createSelfActionGate({ windowMs: 1000, now: () => clock })

    gate.markSelf('stale-task')
    clock = 5000
    gate.markSelf('other-task')

    clock = 5001
    expect(gate.isRecentSelfAction(selfEcho('stale-task', 'reply.received'))).toBe(false)
    expect(gate.isRecentSelfAction(selfEcho('other-task', 'reply.received'))).toBe(true)
  })

  it('has a sane default window', () => {
    expect(SELF_ACTION_WINDOW_MS).toBe(10000)
  })
})

describe('createNotificationGate', () => {
  it('lets the first notification for a tag through', () => {
    expect(createNotificationGate().allow('task-create-1')).toBe(true)
  })

  it('collapses the backend\'s duplicate publish of one event', () => {
    // A single task creation is published twice — once by the REST handler and
    // once by the CRUD-event consumer — carrying the same tag both times.
    const gate = createNotificationGate()

    expect(gate.allow('task-create-1')).toBe(true)
    expect(gate.allow('task-create-1')).toBe(false)
  })

  it('does not collapse different tags', () => {
    const gate = createNotificationGate()

    expect(gate.allow('task-create-1')).toBe(true)
    expect(gate.allow('task-create-2')).toBe(true)
  })

  it('allows the same tag again once the window has passed', () => {
    // A task genuinely updated twice, minutes apart, is two pieces of news.
    let clock = 0
    const gate = createNotificationGate({ windowMs: 1000, now: () => clock })

    expect(gate.allow('task-status-1')).toBe(true)
    clock = 999
    expect(gate.allow('task-status-1')).toBe(false)
    clock = 1000
    expect(gate.allow('task-status-1')).toBe(true)
  })

  it('forgets old tags rather than growing without bound', () => {
    let clock = 0
    const gate = createNotificationGate({ windowMs: 100, now: () => clock })

    for (let i = 0; i < 50; i += 1) {
      gate.allow(`tag-${i}`)
      clock += 10
    }
    // Well past the window, so the early entries must have been pruned; the
    // first tag being allowed again is the observable proof.
    clock += 1000
    expect(gate.allow('tag-0')).toBe(true)
  })

  it('has a sane default window', () => {
    expect(DEDUPE_WINDOW_MS).toBe(10000)
  })
})

describe('createUnreadCounter', () => {
  it('counts up and publishes each change', () => {
    const setBadge = vi.fn()
    const counter = createUnreadCounter({ setBadge })

    expect(counter.increment()).toBe(1)
    expect(counter.increment()).toBe(2)
    expect(counter.value).toBe(2)
    expect(setBadge).toHaveBeenNthCalledWith(2, 2)
  })

  it('clears back to zero', () => {
    const setBadge = vi.fn()
    const counter = createUnreadCounter({ setBadge })

    counter.increment()
    expect(counter.clear()).toBe(0)
    expect(setBadge).toHaveBeenLastCalledWith(0)
  })

  it('does not republish a clear when already at zero', () => {
    // The window emits focus constantly; repainting an unchanged badge each
    // time is pointless work.
    const setBadge = vi.fn()
    const counter = createUnreadCounter({ setBadge })

    counter.clear()
    counter.clear()

    expect(setBadge).not.toHaveBeenCalled()
  })
})

describe('badgeFor', () => {
  it('shows nothing at zero', () => {
    expect(badgeFor(0, 'darwin')).toEqual({ badge: '', overlay: null })
    expect(badgeFor(-1, 'darwin')).toEqual({ badge: '', overlay: null })
    expect(badgeFor(0, 'win32')).toEqual({ badge: '', overlay: null })
  })

  it('puts the number on the dock on macOS and Linux', () => {
    expect(badgeFor(3, 'darwin')).toEqual({ badge: '3', overlay: null })
    expect(badgeFor(3, 'linux')).toEqual({ badge: '3', overlay: null })
  })

  it('uses a taskbar overlay on Windows, which has no numeric badge', () => {
    expect(badgeFor(3, 'win32')).toEqual({
      badge: '',
      overlay: { count: 3, description: '3 unread notifications' },
    })
  })

  it('gets the singular right in the overlay description, which is read aloud', () => {
    expect(badgeFor(1, 'win32').overlay.description).toBe('1 unread notification')
  })
})
