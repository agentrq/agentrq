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

  // Loops the model wrote on the 1,000-task test sets, cut where the token cap stopped them.
  it.each([
    [
      'Nebula X tanıtım sosyal medya sosyal medya sosyal medya sosyal medya sosyal',
      'Nebula X tanıtım sosyal medya',
    ],
    ['Eksik hata sayfası sayfası sayfası sayfası sayfası say', 'Eksik hata sayfası'],
    ['Sugu ni shouda ni naru to shouda ni naru to shouda ni naru to shouda', 'Sugu ni shouda ni naru to'],
    ['確認確認確認確認確', '確認'],
    ['回帰テストテストテストテスト', '回帰テスト'],
    ['Fix login login login bug', 'Fix login bug'],
    ['Update docs docs docs.', 'Update docs'],
    ['Teste regra SSO com regra SSO com regra SS', 'Teste regra SSO com'],
    ['Teste regra SSO com regra SSO com regra SSO fim', 'Teste regra SSO com regra SSO fim'],
    ['审计日志中的破解猫猫猫猫猫�', '审计日志中的破解猫'],
  ])('cuts the loop in %j to %j', (answer, title) => {
    expect(cleanTitle(answer)).toBe(title);
  });

  it.each([
    ["Refactor module 'Auth'", "Refactor module 'Auth'"],
    ['Fix color contrast for "sunny"', 'Fix color contrast for "sunny"'],
    ["'Rename 'beta' flag'", "Rename 'beta' flag"],
    ["Proje grafik başlıklarını Türkçe'", 'Proje grafik başlıklarını Türkçe'],
    ['"Bump Go', 'Bump Go'],
    ['Replace image �', 'Replace image'],
  ])('keeps the quotes that belong to %j: %j', (answer, title) => {
    expect(cleanTitle(answer)).toBe(title);
  });

  it.each([
    'Plan Bora Bora trip',
    'Dosyaları yavaş yavaş taşı',
    'Pay invoice 1212',
    'Check www.example.com',
    'Refactor the the parser',
    'Fix 404 on /api/v1/tasks',
    '東京大学の研究室に連絡',
    'Book Walla Walla hotel',
  ])('leaves %j as it is', (title) => {
    expect(cleanTitle(title)).toBe(title);
  });
});

describe('TITLE_MAX_NEW_TOKENS', () => {
  it('leaves room for a title in a script that spends several tokens per word', () => {
    // At 20, 45% of Arabic and 25% of Japanese titles were cut off mid-word.
    expect(TITLE_MAX_NEW_TOKENS).toBeGreaterThanOrEqual(32);
  });
});
