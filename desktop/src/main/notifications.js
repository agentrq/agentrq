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
 * One rule the push controller does not need: the SSE payload is a task, not
 * a message, so there is no sender field to check before deciding whether a
 * reply/respond notification is about the human's own action. `protocol.js`
 * sees every outgoing request the renderer makes, so it reports reply/respond
 * calls here instead — `taskIdFromSelfActionRequest` and `createSelfActionGate`
 * are how that becomes "mute the next notification for this task".
 *
 * Pure functions, so the mapping and mute rules are testable without Electron.
 */

/** Event types that can produce a notification. Others are UI-only signals. */
export const NOTIFIABLE_TYPES = ['task.created', 'status.updated', 'task.updated', 'reply.received']

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
  if (!task.workspaceId || !task.id) return false

  // `createdBy` is the actor on the task view. The push controller gates on
  // `ev.Actor != ActorAgent`; this is the same rule expressed in what the SSE
  // payload actually carries.
  const actor = event.type === 'reply.received' ? 'agent' : task.createdBy
  if (actor !== 'agent') return false

  return !mutedWorkspaces.includes(task.workspaceId)
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
  const workspaceRoute = `/workspaces/${task.workspaceId}`

  switch (event.type) {
    case 'task.created':
      return {
        title: `New task: ${truncate(task.title, 60)}`,
        body: workspace,
        route: workspaceRoute,
        tag: `task-create-${task.id}`,
      }

    case 'status.updated':
    case 'task.updated':
      return {
        title: `Task ${String(task.status ?? '').toUpperCase()}: ${truncate(task.title, 50)}`,
        body: workspace,
        route: workspaceRoute,
        tag: `task-status-${task.id}`,
      }

    default:
      // reply.received. The stream carries the task, not the message, so the
      // reply text the browser notification shows is not available here; the
      // workspace name is the honest stand-in.
      return {
        title: `Reply on: ${truncate(task.title, 55)}`,
        body: workspace,
        route: `${workspaceRoute}/tasks/${task.id}`,
        tag: `reply-${task.id}`,
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
 * instance just sent — a message create republishes as `reply.received`, and
 * the task row touched by that same write republishes as `task.updated`.
 *
 * Deliberately not "every notifiable type": `task.created` and
 * `status.updated` are never the result of sending a reply, so muting them
 * just because a reply on the same task happened moments ago would drop a
 * genuine, unrelated notification instead of the self-sent echo this gate
 * exists to catch.
 */
const SELF_ECHO_TYPES = new Set(['reply.received', 'task.updated'])

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
    /** @returns {boolean} true when this event is likely this desktop's own reply/respond echoing back. */
    isRecentSelfAction(taskId, eventType) {
      pruneStale(markedAt, windowMs, now())
      return SELF_ECHO_TYPES.has(eventType) && markedAt.has(taskId)
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
