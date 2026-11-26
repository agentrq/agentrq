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

/**
 * The most tokens a title may take. Arabic, Japanese and Korean spend several
 * tokens per word: at 20, nearly half of Arabic titles were cut off mid-word.
 * 32 fits all but about 2% of them; a longer cap mostly lets a looping answer run on.
 */
export const TITLE_MAX_NEW_TOKENS = 32;

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
