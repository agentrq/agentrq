// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// A click on a Button of an agent's A2UI card, sent from the task thread. A
// question asked as a card is answered where the agent waits for it; a
// reply's card posts the click as the person's reply. Either way the card
// shows answered once the server confirms it, not when the live event comes.

import { ref } from 'vue';

import { isElicitExpired } from './useElicitAnswer';
import { withCardAnswered } from './useRequestAnswer';

/**
 * Whether message m's card takes no answer: in a closed workspace, on a
 * teammate's task, or a question nobody waits for any more.
 */
export function cardReadOnly(m, { archived, teamMode, now }) {
  if (archived || teamMode) return true;
  return m.metadata?.type === 'elicitation_request' && isElicitExpired(m, now);
}

/**
 * deps: api { respondToElicitation, sendA2uiAction }; ids() → { workspaceId,
 * taskId }; task, the thread's ref; applyTaskUpdate(current, incoming);
 * markAnswered(requestId, answer); notifySuccess; notifyError.
 */
export function useCardActions(deps) {
  // The messages whose click is on its way: their buttons wait for it.
  const cardsSending = ref(new Set());

  function setSending(id, on) {
    const next = new Set(cardsSending.value);
    if (on) next.add(id);
    else next.delete(id);
    cardsSending.value = next;
  }

  async function sendCardAction(m, action) {
    if (cardsSending.value.has(m.id)) return;
    const { workspaceId, taskId } = deps.ids();
    const question = m.metadata.type === 'elicitation_request';
    setSending(m.id, true);
    try {
      if (question) {
        const content = { name: action.name, context: action.context, dataModel: action.dataModel };
        await deps.api.respondToElicitation(workspaceId, taskId, m.metadata.requestId, 'accept', content);
        deps.markAnswered(m.metadata.requestId, { status: 'accept', content });
        deps.notifySuccess('Response sent');
      } else {
        const res = await deps.api.sendA2uiAction(workspaceId, taskId, {
          messageId: m.id,
          surfaceId: action.surfaceId,
          name: action.name,
          sourceComponentId: action.sourceComponentId,
          context: action.context,
          userMessage: action.userMessage || undefined,
        });
        const updated = res?.task ? deps.applyTaskUpdate(deps.task.value, res.task) : deps.task.value;
        deps.task.value = withCardAnswered(updated, m.id, { name: action.name, at: new Date().toISOString() });
      }
    } catch (err) {
      // Answered already, here or elsewhere, or nobody waits any more: the
      // card is closed either way.
      if (!question && err.status === 409) deps.task.value = withCardAnswered(deps.task.value, m.id, { name: null });
      if (question && err.closed) deps.markAnswered(m.metadata.requestId, { status: 'cancel' });
      deps.notifyError('Failed to send the answer: ' + err.message);
    } finally {
      setSending(m.id, false);
    }
  }

  return { cardsSending, sendCardAction };
}
