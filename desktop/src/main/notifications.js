// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Turning event-stream traffic into native notifications.
 *
 * Web Push cannot work in Electron — there is no push service behind it — so
 * the desktop app produces the same user-facing behaviour from the SSE stream
 * it already has. The wording, the trigger rules and the click destinations
 * deliberately mirror the backend's push controller
 * (`backend/internal/controller/push/push.go`) so a user who moves between the
 * browser and the desktop app sees the same notifications for the same events.
 *
 * `frontend/src/composables/usePushNotifications.js` is untouched and still
 * serves the browser; the desktop renderer simply never calls it.
 *
 * One rule the push controller does not need: telling the human's own message
 * apart from the agent's. The payload **does** carry the task's messages — the
 * forwarder loads them before publishing — so `lastMessageSender` answers it
 * directly, and that is what `shouldNotify` gates on.
 *
 * `protocol.js` additionally reports this instance's own reply/respond calls
 * (`taskIdFromSelfActionRequest`, `createSelfActionGate`), which covers the
 * narrower case of an echo arriving before the write is visible. It is a
 * backstop, not the rule: keyed on time alone it swallowed agents that answered
 * quickly, which is the whole of the bug it once caused.
 *
 * Pure functions, so the mapping and mute rules are testable without Electron.
 */

/**
 * Event types that can produce a notification. Others are UI-only signals.
 *
 * A status change is announced from `task.status` alone: the server publishes
 * it only for the agent's own change. `status.updated` and `task.updated` cannot
 * say who changed the status, so judging them by who created the task or spoke
 * last dropped an agent's change on a human's task, announced the human's own
 * change on an agent's, and would now repeat `task.status`.
 */
export const NOTIFIABLE_TYPES = ['task.created', 'task.status', 'reply.received']

/** Matches the backend's truncation lengths so titles read identically. */
export function truncate(text, max) {
  const value = String(text ?? '')
  return value.length > max ? `${value.slice(0, max)}…` : value
}

/**
 * Should this event notify at all?
 *
 * Two rules, both inherited from the push controller. Only the agent's actions
 * are worth interrupting someone for — being told about your own click is
 * noise, not news. And a muted workspace stays silent.
 */
export function shouldNotify(event, { mutedWorkspaces = [] } = {}) {
  if (!event || !NOTIFIABLE_TYPES.includes(event.type)) return false

  const task = event.payload
  if (!task || typeof task !== 'object') return false
  if (!task.workspaceId || !taskIdOf(event)) return false
  if (mutedWorkspaces.includes(task.workspaceId)) return false

  // The agent's by construction, whoever created the task or spoke last.
  if (event.type === 'task.status') return Boolean(task.to) && task.to !== task.from

  // Who just spoke, when the payload says — and it does for every event the
  // forwarder publishes. A reply is not news because it is a reply; it is news
  // because somebody else wrote it. Replying from a browser, a phone or Slack
  // while this app is open used to announce your own message back at you,
  // because `reply.received` was simply assumed to mean "the agent".
  //
  // The fallback keeps the old reading for a payload carrying no messages:
  // `createdBy` for a task event, and the agent for a reply.
  const sender = lastMessageSender(task)
  const actor = sender || (event.type === 'reply.received' ? 'agent' : task.createdBy)
  if (actor !== 'agent') return false

  // The agent's reasoning, plan and usage counters are messages too, and each
  // one republishes its task. They are the agent working, not saying anything.
  return !(event.type === 'reply.received' && SILENT_TELEMETRY_TYPES.has(lastMessageType(task)))
}

/** The task an event is about: `task.status` names it, the others carry it. */
function taskIdOf(event) {
  return event.type === 'task.status' ? event.payload.taskId : event.payload.id
}

/** Agent telemetry metadata types that never notify. A notice still does. */
const SILENT_TELEMETRY_TYPES = new Set(['agent_thought', 'agent_plan', 'agent_usage'])

/** The metadata type of the newest message on a task, or '' when it has none. */
function lastMessageType(task) {
  const messages = task?.messages
  if (!Array.isArray(messages) || messages.length === 0) return ''
  return String(messages[messages.length - 1]?.metadata?.type ?? '')
}

/**
 * Build the notification for an event, or null when it should not fire.
 *
 * @param {object} event                 `{ type, payload }` from the stream
 * @param {object} [options]
 * @param {string[]} [options.mutedWorkspaces]
 * @param {(id: string) => string} [options.workspaceName] resolves a display
 *        name; falls back to nothing rather than showing a raw id
 */
export function mapEventToNotification(event, { mutedWorkspaces = [], workspaceName = () => '' } = {}) {
  if (!shouldNotify(event, { mutedWorkspaces })) return null

  const task = event.payload
  const workspace = workspaceName(task.workspaceId) || 'AgentRQ'
  const taskRoute = `/workspaces/${task.workspaceId}/tasks/${taskIdOf(event)}`

  switch (event.type) {
    case 'task.created':
      return {
        title: `New task: ${truncate(task.title, 60)}`,
        body: workspace,
        route: taskRoute,
        tag: `task-create-${task.id}`,
      }

    case 'task.status':
      return {
        title: `Task ${String(task.to).toUpperCase()}: ${truncate(task.title, 50)}`,
        body: workspace,
        route: taskRoute,
        // By the new status too: an agent going ongoing then completed in one
        // breath is two pieces of news, not one deduped away.
        tag: `task-status-${task.taskId}-${task.to}`,
      }

    default:
      // reply.received. The reply text the browser notification shows is not
      // used here — the workspace name is the honest stand-in — but the message
      // id is, as the tag.
      return {
        title: `Reply on: ${truncate(task.title, 55)}`,
        body: workspace,
        route: taskRoute,
        tag: lastMessageId(task) || `reply-${task.id}`,
      }
  }
}

/**
 * The task ID a reply/respond request names, or null when the request is
 * neither.
 *
 * These are the two ways this desktop instance's own action reaches the
 * server — a human typing a reply, or clicking allow/deny — and both land
 * back on this same SSE stream, indistinguishable from an agent's, because
 * the payload carries the task rather than the message (see module doc).
 * Recognising the outgoing request here, at the point the renderer's own API
 * call is proxied, is what lets the desktop tell "I just did this" apart from
 * "the agent did this" without the backend having to say so.
 */
export function taskIdFromSelfActionRequest(method, pathname) {
  if (method.toUpperCase() !== 'POST') return null
  const match = /^\/api\/v1\/workspaces\/[^/]+\/tasks\/([^/]+)\/(?:reply|respond)$/.exec(pathname)
  return match ? match[1] : null
}

/** How long a task stays muted after this desktop sends a reply/respond for it. */
export const SELF_ACTION_WINDOW_MS = 10000

/**
 * Event types that can be the SSE echo of a reply/respond this desktop
 * instance just sent and would notify: a message create republishes as
 * `reply.received`.
 *
 * Deliberately not "every notifiable type": `task.created` and `task.status`
 * are never the result of sending a reply, so muting them just because a reply
 * on the same task happened moments ago would drop a genuine, unrelated
 * notification instead of the self-sent echo this gate exists to catch.
 */
const SELF_ECHO_TYPES = new Set(['reply.received'])

/**
 * The id of the newest message on a task, or '' when the payload carries none.
 *
 * Used as a reply's notification tag, so the dedupe gate collapses a message
 * published twice into the one piece of news it is.
 */
export function lastMessageId(task) {
  const messages = task?.messages
  if (!Array.isArray(messages) || messages.length === 0) return ''
  return String(messages[messages.length - 1]?.id ?? '')
}

/**
 * Who wrote the newest message on a task, or '' when the payload carries none.
 *
 * The gate below used to work around not having this — the module doc once said
 * the payload "carries the task rather than the message". That stopped being
 * true when the forwarder began loading a task's messages before publishing, so
 * who just spoke is on the event rather than something to infer from timing.
 */
export function lastMessageSender(task) {
  const messages = task?.messages
  if (!Array.isArray(messages) || messages.length === 0) return ''
  return String(messages[messages.length - 1]?.sender ?? '')
}

/**
 * Removes entries whose timestamp is at least `windowMs` old.
 *
 * Shared by the two windowed-key trackers below, which differ in what they
 * key on and when they check but not in how a window expires.
 */
function pruneStale(map, windowMs, at) {
  for (const [key, timestamp] of map) {
    if (at - timestamp >= windowMs) map.delete(key)
  }
}

/**
 * Tracks tasks this desktop instance just acted on itself, so the
 * notification that same action produces on the SSE stream a moment later
 * can be suppressed.
 *
 * A time window rather than a one-shot latch consumed by the next event: a
 * genuine, later agent reply on the same task must still notify, and an
 * unrelated event of a different type arriving first (say, `task.created`)
 * must not use up the mute before the actual echo of this action arrives —
 * see `SELF_ECHO_TYPES`.
 *
 * Pruned on every call, not just a matching one: `markSelf` fires on every
 * reply/respond regardless of what (if anything) the stream echoes back for
 * it, so a check keyed only to the same task id could otherwise never run for
 * a stale entry and the map would grow for the life of the process.
 */
export function createSelfActionGate({ windowMs = SELF_ACTION_WINDOW_MS, now = () => Date.now() } = {}) {
  const markedAt = new Map()

  return {
    markSelf(taskId) {
      const at = now()
      pruneStale(markedAt, windowMs, at)
      if (taskId) markedAt.set(taskId, at)
    },
    /**
     * @returns {boolean} true when this event is this desktop's own
     *   reply/respond echoing back.
     *
     * Takes the whole event, because the payload settles it. The window alone
     * could not: an agent answering inside ten seconds — which is most of them
     * — landed in the mute this desktop had just armed for its own message, and
     * its reply was dropped. A status change arriving later, after the agent
     * had done some work, fell outside the window and still notified, which is
     * exactly the difference somebody reported.
     */
    isRecentSelfAction(event) {
      pruneStale(markedAt, windowMs, now())
      if (!SELF_ECHO_TYPES.has(event?.type)) return false
      // The agent wrote the last message, so this is not this desktop's echo,
      // whatever the clock says.
      if (lastMessageSender(event?.payload) === 'agent') return false
      return markedAt.has(event?.payload?.id)
    },
  }
}

/** How long a tag suppresses a repeat. */
export const DEDUPE_WINDOW_MS = 10000

/**
 * Suppress duplicate notifications by tag.
 *
 * A single task creation reaches the stream twice: the REST handler publishes
 * `task.created` directly, and the CRUD-event consumer publishes it again from
 * the same write. The renderer does not care — it just re-renders — but firing
 * two identical native notifications for one event is plainly wrong.
 *
 * The `tag` field carries the same value in both copies, which is exactly what
 * the browser uses to collapse duplicate web-push notifications; this is that
 * mechanism, applied on our side.
 *
 * A time window rather than an unbounded set: a genuine second event with the
 * same tag much later (a task edited twice, say) should still notify, and the
 * map must not grow forever.
 */
export function createNotificationGate({ windowMs = DEDUPE_WINDOW_MS, now = () => Date.now() } = {}) {
  const seen = new Map()

  return {
    /** @returns {boolean} true when this notification should be shown. */
    allow(tag) {
      const at = now()
      pruneStale(seen, windowMs, at)

      if (seen.has(tag)) return false
      seen.set(tag, at)
      return true
    },
  }
}

/**
 * Unread count behind the dock badge and taskbar overlay.
 *
 * Counting is per notification raised and cleared when the user looks at the
 * app, which is the behaviour a badge is understood to have — it answers "is
 * there anything new", not "how many events have ever occurred".
 */
export function createUnreadCounter({ setBadge }) {
  let count = 0

  const publish = () => setBadge(count)

  return {
    increment() {
      count += 1
      publish()
      return count
    },
    clear() {
      if (count === 0) return 0
      count = 0
      publish()
      return count
    },
    get value() {
      return count
    },
  }
}

/**
 * Windows has no numeric dock badge, so the count is rendered as a taskbar
 * overlay description instead; macOS and Linux take the number directly.
 *
 * Returns what should be shown, so the choice is testable without a window.
 */
export function badgeFor(count, platform) {
  if (count <= 0) return { badge: '', overlay: null }
  if (platform === 'win32') {
    return { badge: '', overlay: { count, description: `${count} unread notification${count === 1 ? '' : 's'}` } }
  }
  return { badge: String(count), overlay: null }
}
