// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * What a fork's settings page may send.
 *
 * A fork takes some settings from its parent and the server refuses any change
 * to them there. The list of which is the server's `Workspace.ForkSettings()`,
 * read from its source here rather than kept by hand: a setting added to it and
 * not here would be sent from the form's defaults, read as a change, and turn
 * every save of a fork's name into a refusal.
 */

import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import {
  FORK_INHERITED_FIELDS,
  FORK_INHERITED_TABS,
  shouldShowSettingsActionBar,
  workspaceUpdatePayload,
} from '../src/composables/useWorkspaceSettings';

function serverForkSettings() {
  const src = readFileSync(resolve(__dirname, '../../backend/internal/data/model/model.go'), 'utf-8');
  const body = src.match(/func \(w Workspace\) ForkSettings\(\) map\[string\]any \{([\s\S]*?)\n\}/);
  if (!body) throw new Error('Workspace.ForkSettings not found in the server source');
  return [...body[1].matchAll(/"([a-z_]+)":/g)].map((m) => m[1].replace(/_([a-z])/g, (_, c) => c.toUpperCase()));
}

const form = {
  name: 'ops-try',
  description: 'mine',
  notificationSettings: { taskCreated: false, channels: ['email'] },
  autoAllowedTools: [],
  allowAllCommands: false,
  clearContextDefault: false,
  selfLearningLoopNote: '',
  inputSendDelaySeconds: 0,
  workingDirectory: '/home/me/.agentrq/forks/f1',
};

describe('a fork\'s settings', () => {
  it('names exactly the fields the server says a fork inherits', () => {
    expect([...FORK_INHERITED_FIELDS].sort()).toEqual(serverForkSettings().sort());
  });

  it('sends a workspace\'s form as it is', () => {
    expect(workspaceUpdatePayload(form, { id: 'w' })).toBe(form);
    expect(workspaceUpdatePayload(form, null)).toBe(form);
  });

  it('sends a fork\'s inherited fields exactly as they came, not the form\'s defaults', () => {
    const loaded = {
      id: 'f1',
      forkOfId: 'p1',
      notificationSettings: { taskCreated: true },
      autoAllowedTools: ['Bash:ls'],
      allowAllCommands: true,
      clearContextDefault: true,
      selfLearningLoopNote: 'parent note',
      inputSendDelaySeconds: 5,
    };
    expect(workspaceUpdatePayload(form, loaded)).toEqual({
      ...form,
      notificationSettings: { taskCreated: true },
      autoAllowedTools: ['Bash:ls'],
      allowAllCommands: true,
      clearContextDefault: true,
      selfLearningLoopNote: 'parent note',
      inputSendDelaySeconds: 5,
    });
  });

  it('leaves out a JSON setting the fork does not have, and zeroes an absent scalar', () => {
    const payload = workspaceUpdatePayload({ ...form, allowAllCommands: true, selfLearningLoopNote: 'typed' }, { id: 'f1', forkOfId: 'p1' });
    expect(payload).not.toHaveProperty('notificationSettings');
    expect(payload).not.toHaveProperty('autoAllowedTools');
    expect(payload).toMatchObject({ allowAllCommands: false, clearContextDefault: false, selfLearningLoopNote: '', inputSendDelaySeconds: 0 });
    expect(payload).toMatchObject({ name: 'ops-try', description: 'mine', workingDirectory: '/home/me/.agentrq/forks/f1' });
  });

  it('offers no Save on the tabs a fork inherits whole', () => {
    const fork = { id: 'f1', forkOfId: 'p1' };
    for (const tab of FORK_INHERITED_TABS) expect(shouldShowSettingsActionBar(tab, fork)).toBe(false);
    expect(shouldShowSettingsActionBar('general', fork)).toBe(true);
    expect(shouldShowSettingsActionBar('automations', { id: 'w' })).toBe(true);
  });
});
