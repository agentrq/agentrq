// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * A bare URL ends at a backtick. marked's GFM autolink takes one in, so a URL
 * written just before a code span's closing backtick links to `…%60`. A
 * backtick is never part of a real URL, and the full stop or comma left before
 * it is trimmed as GFM trims it at a URL's end.
 *
 * Imported by everything that reads links out of markdown, so the message's
 * links and the task's link panel agree.
 */
import { marked, Tokenizer } from 'marked';

const TRAILING_PUNCTUATION = /[?!.,:*_~]+$/;

/**
 * The autolink token with everything from its first backtick cut off, or the
 * token itself when it has none. marked links a URL only when something
 * follows its scheme or `www.` before the backtick, so something is always left.
 *
 * @param {object} token a `link` token from marked's url tokenizer
 */
export function trimAutolink(token) {
  const cut = token.raw.indexOf('`');
  if (cut < 0) return token;
  const raw = token.raw.slice(0, cut).replace(TRAILING_PUNCTUATION, '');
  // `www.` links carry a scheme the raw text has not got.
  const href = token.href.slice(0, token.href.length - token.raw.length) + raw;
  return { ...token, raw, text: raw, href, tokens: [{ type: 'text', raw, text: raw }] };
}

marked.use({
  tokenizer: {
    url(src) {
      const token = Tokenizer.prototype.url.call(this, src);
      return token && trimAutolink(token);
    },
  },
});
