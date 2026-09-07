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

describe('createSelfActionGate', () => {
  it('reports no recent self-action for a task never marked', () => {
    expect(createSelfActionGate().isRecentSelfAction('t1', 'reply.received')).toBe(false)
  })

  it('reports a recent self-action right after it is marked', () => {
    const gate = createSelfActionGate()
    gate.markSelf('t1')
    expect(gate.isRecentSelfAction('t1', 'reply.received')).toBe(true)
  })

  it('recognises task.updated as the other type a reply/respond can echo as', () => {
    const gate = createSelfActionGate()
    gate.markSelf('t1')
    expect(gate.isRecentSelfAction('t1', 'task.updated')).toBe(true)
  })

  it('does not mute a type a reply/respond could never produce', () => {
    // task.created and status.updated are never the result of sending a
    // reply, so a real one of either must still notify even for a task this
    // desktop instance just replied to.
    const gate = createSelfActionGate()
    gate.markSelf('t1')
    expect(gate.isRecentSelfAction('t1', 'task.created')).toBe(false)
    expect(gate.isRecentSelfAction('t1', 'status.updated')).toBe(false)
  })

  it('does not mark a task when given no id', () => {
    const gate = createSelfActionGate()
    gate.markSelf(null)
    gate.markSelf(undefined)
    expect(gate.isRecentSelfAction(null, 'reply.received')).toBe(false)
  })

  it('does not confuse one task for another', () => {
    const gate = createSelfActionGate()
    gate.markSelf('t1')
    expect(gate.isRecentSelfAction('t2', 'reply.received')).toBe(false)
  })

  it('expires the mute once the window passes', () => {
    let clock = 0
    const gate = createSelfActionGate({ windowMs: 1000, now: () => clock })

    gate.markSelf('t1')
    clock = 999
    expect(gate.isRecentSelfAction('t1', 'reply.received')).toBe(true)
    clock = 1000
    expect(gate.isRecentSelfAction('t1', 'reply.received')).toBe(false)
  })

  it('lets a genuine later reply on the same task notify again', () => {
    // The mute must not persist forever just because the task was replied to
    // once; a real agent reply after the window is news again.
    let clock = 0
    const gate = createSelfActionGate({ windowMs: 1000, now: () => clock })

    gate.markSelf('t1')
    clock = 2000
    expect(gate.isRecentSelfAction('t1', 'reply.received')).toBe(false)
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
    expect(gate.isRecentSelfAction('stale-task', 'reply.received')).toBe(false)
    expect(gate.isRecentSelfAction('other-task', 'reply.received')).toBe(true)
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
