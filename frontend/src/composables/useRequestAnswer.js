// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// The task with the card for requestId (a question or a permission request)
// resolved as answered, once the server has confirmed the answer reached the
// agent. The live event saying so is not enough on its own: when it never
// arrives, the card would offer its buttons until a reload.
//
// `answer` holds the fields the server writes into the card's metadata.
export function withRequestAnswered(task, requestId, answer) {
  if (!task?.messages || !requestId) return task;
  let found = false;
  const messages = task.messages.map((m) => {
    const id = m.metadata?.requestId ?? m.metadata?.request_id;
    if (id !== requestId) return m;
    found = true;
    return { ...m, metadata: { ...m.metadata, ...answer } };
  });
  return found ? { ...task, messages } : task;
}
