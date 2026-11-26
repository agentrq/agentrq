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

/** The model's answer as a title: its first line, without a label, quotes, a loop or a final period. */
export function cleanTitle(answer) {
  const line = String(answer ?? '').trim().split('\n')[0];
  return dropLoop(
    unquote(line.replace(/^(task\s+)?title:\s*/i, '').replace(/�+$/, ''))
      .replace(/\.$/, '')
      .trim(),
  );
}

/**
 * Without the quotes around the whole title, or a stray one at either end, but
 * keeping a quoted name it ends with: "Refactor module 'Auth'" stays as it is.
 */
function unquote(title) {
  let t = title.trim();
  const pair = t.match(/^(["'`])(.*)\1$/s);
  if (pair) t = pair[2];
  const odd = (q) => t.split(q).length % 2 === 0;
  if (/^["'`]/.test(t) && odd(t[0])) t = t.slice(1);
  if (/["'`]$/.test(t) && odd(t.at(-1))) t = t.slice(0, -1);
  return t.trim();
}

/**
 * Keeps one copy of a phrase the model repeated instead of ending the title,
 * and drops the cut-off copy after it: "sosyal medya sosyal medya sosyal" →
 * "sosyal medya". One word must repeat three times, since "yavaş yavaş" and
 * "Bora Bora" are real; two to four words, twice.
 */
function dropLoop(title) {
  const words = cutLoop(title.split(/\s+/), 4, (k) => (k === 1 ? 3 : 2), () => true);
  if (words) return words.join(' ');
  // Scripts without spaces: "確認確認確認" → "確認", for up to six characters seen three times.
  const chars = cutLoop(Array.from(title), 6, () => 3, (unit) => unit.every((c) => SPACELESS.test(c)));
  return chars ? chars.join('') : title;
}

const SPACELESS = /[\p{Script=Han}\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Hangul}\p{Script=Thai}]/u;

/** The tokens with the first loop cut to one copy, or null when there is none. */
function cutLoop(tokens, maxUnit, minRepeats, eligible) {
  for (let i = 0; i < tokens.length; i++) {
    for (let k = 1; k <= maxUnit && i + k <= tokens.length; k++) {
      const unit = tokens.slice(i, i + k);
      if (!eligible(unit)) continue;
      let end = i + k;
      while (same(tokens.slice(end, end + k), unit)) end += k;
      if ((end - i) / k < minRepeats(k)) continue;
      const rest = tokens.slice(end);
      return [...tokens.slice(0, i + k), ...(isCutOffCopy(rest, unit) ? [] : rest)];
    }
  }
  return null;
}

/** Whether what follows a loop is only the start of one more copy, cut off by the token cap. */
function isCutOffCopy(rest, unit) {
  if (rest.length === 0 || rest.length > unit.length) return false;
  const last = rest.length - 1;
  return rest.every((t, j) =>
    j < last ? t.toLowerCase() === unit[j].toLowerCase() : unit[j].toLowerCase().startsWith(t.toLowerCase()),
  );
}

function same(a, b) {
  return a.length > 0 && a.length === b.length && a.every((t, i) => t.toLowerCase() === b[i].toLowerCase());
}
