// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * What an event on the live stream is worth saying out loud, in the browser.
 *
 * A pure decision, separate from the component that shows it, because the
 * component is not in the coverage include list and this is the part with rules
 * in it. The desktop app makes the same decision in
 * `desktop/src/main/notifications.js`; the two differ in what they can show —
 * a toast here, a system notification there — not in what counts as news.
 *
 * ## Why the sender decides, and not the event type
 *
 * `reply.received` is published for **every** new message on a task, whoever
 * wrote it: the central forwarder maps a message create to it without looking
 * at who. So the type alone cannot tell "the agent answered" from "I just sent
 * that". The payload carries the messages, so the last one's `sender` can, and
 * does.
 *
 * That is also the whole of the bug this file was written for. The old branch
 * notified for a permission request and for the agent's own "Status updated
 * to:" text, and had no case at all for an agent simply replying — which is
 * the commonest thing that happens.
 */

/** The last message on a task, or null when the payload carries none. */
export function lastMessage(task) {
  const messages = task?.messages;
  if (!Array.isArray(messages) || messages.length === 0) return null;
  return messages[messages.length - 1] ?? null;
}

/** Whether a message is an agent asking to use a tool, still unanswered. */
export function isOpenPermissionRequest(message) {
  const metadata = message?.metadata;
  if (metadata?.type !== 'permission_request') return false;
  return metadata.status !== 'allow' && metadata.status !== 'deny';
}

// What each status is called on its pill, and what the move into it means.
// Read by Toast.vue too, so the words and the colours cannot drift apart.
export const STATUS_LABELS = {
  notstarted: 'Not started',
  ongoing: 'Ongoing',
  blocked: 'Blocked',
  completed: 'Done',
  rejected: 'Rejected',
  cron: 'Scheduled',
};

const STATUS_NEWS = {
  notstarted: 'Moved back to not started.',
  ongoing: 'The agent is working on it.',
  blocked: 'The agent needs your input.',
  completed: 'The agent finished this task.',
  rejected: 'The agent declined this task.',
  cron: 'Turned into a scheduled task.',
};

// The toast's title for a session that ended with a reason.
const SESSION_ENDS = { failed: 'Agent could not start', exited: 'Agent exited' };

// A reply's opening words, flattened to one line: the card clamps it to two,
// and the whole text is one click away.
const SNIPPET_MAX = 280;
export function snippet(text) {
  const flat = String(text ?? '').replace(/\s+/g, ' ').trim();
  return flat.length > SNIPPET_MAX ? `${flat.slice(0, SNIPPET_MAX - 1)}…` : flat;
}

/**
 * The toast an event deserves, or null for the ones that are not news.
 *
 * `kind` picks the card's status icon and pill; `from`/`to` are set for a
 * status change, which shows both. `taskId`/`workspaceId` are always present so
 * a caller can build a link without a special case — empty string means "no
 * task to link to", the same falsy sentinel a route param would reject anyway.
 *
 * @returns {{ tone: 'success'|'info'|'error', kind: string, title: string, message: string, from?: string, to?: string, taskId: string, workspaceId: string }|null}
 */
export function toastFor(event, { platform = 'web', openTaskId = '' } = {}) {
  const payload = event?.payload;
  if (!payload) return null;

  // A launch that was refused, which is the only place the reason is ever
  // said. The session row is deleted the moment it fails, so the event is the
  // whole record — and the page the launch sent you to has nothing left to
  // read and reports the generic "no longer on the machine". Without this the
  // reason exists only in the daemon's log, on the machine it was refused on.
  //
  // An agent that exits failing carries the last line it printed, for the same
  // reason: the terminal of a session that has ended cannot be opened.
  //
  // Gated on there being a reason, not on the status alone: an exit code with
  // nothing to explain it is not worth interrupting anybody for.
  if (event.type === 'session.updated') {
    if (!SESSION_ENDS[payload.status] || !payload.error) return null;
    // A session is not a task — there is nothing here to link to.
    return { tone: 'error', kind: 'failed', title: SESSION_ENDS[payload.status], message: payload.error, taskId: '', workspaceId: '' };
  }

  // An agent moving a task. The server publishes this only for the agent's own
  // change, with the status it came from — so a change you made yourself is
  // never announced back to you, and nothing here has to diff task.updated.
  // Said on the task you have open too: the status is the news, and the page
  // shows it only in its header.
  if (event.type === 'task.status') {
    const { from = '', to = '' } = payload;
    if (!to || from === to) return null;
    return {
      tone: to === 'blocked' ? 'error' : to === 'completed' ? 'success' : 'info',
      kind: 'status',
      title: payload.title ?? '',
      message: STATUS_NEWS[to] ?? `Now ${to}.`,
      from,
      to,
      taskId: payload.taskId ?? '',
      workspaceId: payload.workspaceId ?? '',
    };
  }

  // Everything below is about a task, which is what the rest of this stream
  // carries.
  const task = payload;
  const taskId = task.id ?? '';
  const workspaceId = task.workspaceId ?? '';
  const title = task.title ?? '';

  if (event.type === 'task.created') {
    // A scheduled run starts with nobody at the keyboard, whoever wrote the
    // schedule — which is exactly when it is worth saying.
    if (task.parentId) {
      return { tone: 'info', kind: 'scheduled', title, message: 'A scheduled run just started.', taskId, workspaceId };
    }
    // An agent starting work on its own initiative is worth saying; a task the
    // person in front of the screen just created is not.
    return task.createdBy === 'agent'
      ? { tone: 'success', kind: 'created', title, message: 'The agent opened a new task.', taskId, workspaceId }
      : null;
  }

  if (event.type !== 'reply.received') return null;

  const message = lastMessage(task);
  // Your own message, echoing back off the stream you are subscribed to.
  if (message?.sender !== 'agent') return null;

  if (isOpenPermissionRequest(message)) {
    return {
      tone: 'error',
      kind: 'permission',
      title,
      // Both spellings, because both have been written: the metadata the MCP
      // server stores uses `toolName`, and older rows carry `tool_name`.
      message: `Permission required: ${message.metadata.toolName || message.metadata.tool_name}`,
      taskId,
      workspaceId,
    };
  }

  // The line the server writes when an agent changes a status. The change
  // itself arrives as task.status, with the status it came from; this is the
  // same news again.
  if (message.text?.startsWith('Status updated to:')) return null;

  // Past here it is an ordinary reply, and two things make it not worth saying.
  //
  // On the desktop the shell runs its own stream and fires a real system
  // notification for this same event — a toast as well is the same news twice,
  // and the shell's is the one that honours the per-workspace mute, which lives
  // in the main process and is not known here. The ones above are still worth a
  // toast there: they say more than the shell's notification does.
  if (platform === 'desktop') return null;

  // And nothing is worth announcing about a task already on screen: the message
  // renders itself there, and an agent is told to report every few steps — so
  // this is the difference between being kept informed and being talked over.
  if (openTaskId && openTaskId === task.id) return null;

  return { tone: 'info', kind: 'reply', title, message: snippet(message.text) || 'New reply.', taskId, workspaceId };
}

/**
 * Toasts for an agent attaching to a workspace or leaving it.
 *
 * A dropped stream reconnects in a second or two and publishes a disconnect
 * and a connect on the way, so a disconnect is held for `graceMs` and dropped
 * if the agent is back by then — a blip is not news. `wasConnected` is read
 * before the store takes the event, and answers undefined for a workspace not
 * loaded yet, which says nothing.
 *
 * @returns {(payload: { workspaceId: string, connected: boolean }) => void}
 */
export function createPresenceToasts({ notify, wasConnected, graceMs = 15000 }) {
  const pending = new Map();
  const toast = (workspaceId, connected) => ({
    tone: connected ? 'success' : 'info',
    kind: connected ? 'connected' : 'disconnected',
    title: connected ? 'Agent connected' : 'Agent disconnected',
    message: connected ? 'Ready to take tasks.' : 'Tasks will wait until it is back.',
    taskId: '',
    workspaceId,
  });

  return ({ workspaceId, connected } = {}) => {
    if (!workspaceId) return;
    const before = wasConnected(workspaceId);
    if (connected) {
      if (pending.has(workspaceId)) {
        clearTimeout(pending.get(workspaceId));
        pending.delete(workspaceId);
        return;
      }
      if (before === false) notify(toast(workspaceId, true));
      return;
    }
    if (before !== true || pending.has(workspaceId)) return;
    pending.set(workspaceId, setTimeout(() => {
      pending.delete(workspaceId);
      notify(toast(workspaceId, false));
    }, graceMs));
  };
}
