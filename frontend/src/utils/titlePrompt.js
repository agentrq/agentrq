// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * What the local title model is asked, and how its answer becomes a title.
 *
 * The model is fine-tuned for exactly this prompt: a fixed instruction, then
 * what the person typed. Changing the instruction changes what it was trained
 * on, so do it only together with a new model.
 */

const INSTRUCTION = 'Write a short title for this task.';

/** The chat messages asking for a title for what the person typed. */
export function titleMessages(text) {
  return [
    { role: 'system', content: INSTRUCTION },
    { role: 'user', content: text },
  ];
}

/** The model's answer as a title: its first line, without a label, quotes or a final period. */
export function cleanTitle(answer) {
  const line = String(answer ?? '').trim().split('\n')[0];
  return line
    .replace(/^(task\s+)?title:\s*/i, '')
    .replace(/^["'`]+|["'`]+$/g, '')
    .replace(/\.$/, '')
    .trim();
}
