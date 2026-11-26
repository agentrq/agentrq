// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect } from 'vitest';
import { titleMessages, cleanTitle, TITLE_MAX_NEW_TOKENS } from '../src/utils/titlePrompt.js';

describe('titleMessages', () => {
  it('sends the instruction the model was trained on, then exactly what was typed', () => {
    const typed = 'fix the flaky upload test';
    expect(titleMessages(typed)).toEqual([
      { role: 'system', content: 'Write a short title for this task.' },
      { role: 'user', content: typed },
    ]);
  });

  it('builds a fresh list each time, so a caller cannot change the next prompt', () => {
    const a = titleMessages('one');
    const b = titleMessages('two');
    expect(a).not.toBe(b);
    expect(a.at(-1).content).toBe('one');
    expect(b.at(-1).content).toBe('two');
  });
});

describe('cleanTitle', () => {
  it.each([
    ['Fix flaky admin upload test', 'Fix flaky admin upload test'],
    ['  Jira Hook Setup Guide  ', 'Jira Hook Setup Guide'],
    ['Title: Reconnect agent', 'Reconnect agent'],
    ['Task Title: Reconnect agent', 'Reconnect agent'],
    ['"Bump Go to 1.26"', 'Bump Go to 1.26'],
    ["'Upgrade Vite'", 'Upgrade Vite'],
    ['Update the Dockerfile.', 'Update the Dockerfile'],
    ['1. Create a webhook\n2. Select the events', '1. Create a webhook'],
    ['', ''],
    [undefined, ''],
    [null, ''],
  ])('%j becomes %j', (answer, title) => {
    expect(cleanTitle(answer)).toBe(title);
  });
});

describe('TITLE_MAX_NEW_TOKENS', () => {
  it('leaves room for a title in a script that spends several tokens per word', () => {
    // At 20, 45% of Arabic and 25% of Japanese titles were cut off mid-word.
    expect(TITLE_MAX_NEW_TOKENS).toBeGreaterThanOrEqual(32);
  });
});
