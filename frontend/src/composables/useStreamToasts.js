// Copyright 2026 Contextual, Inc. https://agentrq.com

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

/**
 * The toast an event deserves, or null for the ones that are not news.
 *
 * @returns {{ tone: 'success'|'info'|'error', message: string, title?: string }|null}
 */
export function toastFor(event, { platform = 'web', openTaskId = '' } = {}) {
  const task = event?.payload;
  if (!task) return null;

  // An agent starting work on its own initiative is worth saying; a task the
  // person in front of the screen just created is not.
  if (event.type === 'task.created') {
    return task.createdBy === 'agent'
      ? { tone: 'success', message: `Agent started a new task: ${task.title}` }
      : null;
  }

  if (event.type !== 'reply.received') return null;

  const message = lastMessage(task);
  // Your own message, echoing back off the stream you are subscribed to.
  if (message?.sender !== 'agent') return null;

  if (isOpenPermissionRequest(message)) {
    return {
      tone: 'error',
      title: 'Action Needed',
      // Both spellings, because both have been written: the metadata the MCP
      // server stores uses `toolName`, and older rows carry `tool_name`.
      message: `Permission required: ${message.metadata.toolName || message.metadata.tool_name}`,
    };
  }

  // The agent announcing its own status change, which reads better as the
  // status than as the sentence it wrote about it.
  if (message.text?.includes('Status updated to:')) {
    return { tone: 'info', message: `Task "${task.title}" is now ${task.status}` };
  }

  // Past here it is an ordinary reply, and two things make it not worth saying.
  //
  // On the desktop the shell runs its own stream and fires a real system
  // notification for this same event — a toast as well is the same news twice,
  // and the shell's is the one that honours the per-workspace mute, which lives
  // in the main process and is not known here. The two above are still worth a
  // toast there: a permission request names the tool, which the shell's
  // notification does not.
  if (platform === 'desktop') return null;

  // And nothing is worth announcing about a task already on screen: the message
  // renders itself there, and an agent is told to report every few steps — so
  // this is the difference between being kept informed and being talked over.
  if (openTaskId && openTaskId === task.id) return null;

  return { tone: 'info', message: `New reply on "${task.title}"` };
}
